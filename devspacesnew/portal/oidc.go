package main

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// oidcVerifier validates bearer JWTs (RS256) from one OIDC issuer — for Azure that is
// https://login.microsoftonline.com/<tenant>/v2.0 with audience api://devspaces. It is what
// lets the Go CLI (wsctl) and Azure API Management call the API without a browser session.
// Standard library only: the JWKS is fetched through OIDC discovery and cached; an unknown
// key id triggers one refresh per minute at most (key rotation).
type oidcVerifier struct {
	issuer, audience, userClaim, groupsClaim string
	http                                     *http.Client

	mu           sync.Mutex
	discoveryURL string
	jwksURL      string
	keys         map[string]*rsa.PublicKey
	fetched      time.Time
}

func newOIDCVerifier(issuer, audience, userClaim, groupsClaim string) *oidcVerifier {
	return &oidcVerifier{issuer: strings.TrimRight(issuer, "/"), audience: audience,
		userClaim: orDefault(userClaim, "preferred_username"), groupsClaim: orDefault(groupsClaim, "groups"),
		http: &http.Client{Timeout: 10 * time.Second}, discoveryURL: strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"}
}

// withDiscoveryURL: fetch discovery/JWKS from another URL than the issuer — needed when the
// provider is reached by an internal name (http://keycloak:8180 inside Docker/Kubernetes)
// while tokens carry the public issuer (http://localhost:8180, https://sso.corp.example).
func (v *oidcVerifier) withDiscoveryURL(u string) *oidcVerifier {
	if u != "" {
		v.discoveryURL = u
	}
	return v
}

var errBadToken = errors.New("invalid token")

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

func b64url(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// verify checks signature, issuer, audience and time claims and maps the token to a User.
func (v *oidcVerifier) verify(ctx context.Context, token string) (User, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return User{}, errBadToken
	}
	hb, err := b64url(parts[0])
	if err != nil {
		return User{}, errBadToken
	}
	var h jwtHeader
	if json.Unmarshal(hb, &h) != nil || h.Alg != "RS256" {
		return User{}, fmt.Errorf("%w: only RS256 tokens are accepted", errBadToken)
	}
	pb, err := b64url(parts[1])
	if err != nil {
		return User{}, errBadToken
	}
	var claims map[string]any
	if json.Unmarshal(pb, &claims) != nil {
		return User{}, errBadToken
	}
	sig, err := b64url(parts[2])
	if err != nil {
		return User{}, errBadToken
	}
	key, err := v.key(ctx, h.Kid)
	if err != nil {
		return User{}, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig) != nil {
		return User{}, fmt.Errorf("%w: bad signature", errBadToken)
	}
	// issuer: Entra v1 tokens use sts.windows.net; v2 tokens use login.microsoftonline.com/<tid>/v2.0.
	if iss, _ := claims["iss"].(string); strings.TrimRight(iss, "/") != v.issuer {
		return User{}, fmt.Errorf("%w: issuer %q is not %q", errBadToken, iss, v.issuer)
	}
	if !audienceOK(claims["aud"], v.audience) {
		return User{}, fmt.Errorf("%w: audience is not %q (request a token for that scope)", errBadToken, v.audience)
	}
	now := float64(time.Now().Unix())
	if exp, ok := claims["exp"].(float64); !ok || now > exp+60 {
		return User{}, fmt.Errorf("%w: expired — run `wsctl login` again", errBadToken)
	}
	if nbf, ok := claims["nbf"].(float64); ok && now < nbf-60 {
		return User{}, fmt.Errorf("%w: not valid yet", errBadToken)
	}
	name := ""
	for _, c := range []string{v.userClaim, "preferred_username", "email", "upn", "unique_name", "sub"} {
		if s, _ := claims[c].(string); s != "" {
			name = s
			break
		}
	}
	if name == "" {
		return User{}, fmt.Errorf("%w: no %s claim", errBadToken, v.userClaim)
	}
	var groups []string
	if gs, ok := claims[v.groupsClaim].([]any); ok {
		for _, g := range gs {
			groups = append(groups, fmt.Sprint(g))
		}
	}
	return User{Name: name, Short: sanitizeUser(name), Groups: groups, Auth: "bearer"}, nil
}

func audienceOK(aud any, want string) bool {
	switch a := aud.(type) {
	case string:
		return a == want
	case []any:
		for _, x := range a {
			if fmt.Sprint(x) == want {
				return true
			}
		}
	}
	return false
}

func (v *oidcVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k, ok := v.keys[kid]; ok && time.Since(v.fetched) < time.Hour {
		return k, nil
	}
	if time.Since(v.fetched) > time.Minute || v.keys == nil { // refresh (rotation), rate-limited
		if err := v.refresh(ctx); err != nil {
			if k, ok := v.keys[kid]; ok {
				return k, nil // stale but known
			}
			return nil, fmt.Errorf("cannot fetch signing keys from %s: %w", v.issuer, err)
		}
	}
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("%w: unknown signing key %q", errBadToken, kid)
}

func (v *oidcVerifier) refresh(ctx context.Context) error {
	if v.jwksURL == "" {
		var disc struct {
			JWKS string `json:"jwks_uri"`
		}
		if err := v.getJSON(ctx, v.discoveryURL, &disc); err != nil {
			return err
		}
		if disc.JWKS == "" {
			return errors.New("discovery document has no jwks_uri")
		}
		// The provider advertises its public URL; when discovery came from an internal address,
		// fetch the keys from that address too (Keycloak behind a different hostname in Docker/K8s).
		internal := strings.TrimSuffix(v.discoveryURL, "/.well-known/openid-configuration")
		if internal != v.issuer && strings.HasPrefix(disc.JWKS, v.issuer) {
			disc.JWKS = internal + strings.TrimPrefix(disc.JWKS, v.issuer)
		}
		v.jwksURL = disc.JWKS
	}
	var set struct {
		Keys []struct {
			Kty, Kid, Use, N, E string
		} `json:"keys"`
	}
	if err := v.getJSON(ctx, v.jwksURL, &set); err != nil {
		return err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		nb, err1 := b64url(k.N)
		eb, err2 := b64url(k.E)
		if err1 != nil || err2 != nil || len(nb) == 0 || len(eb) == 0 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(new(big.Int).SetBytes(eb).Int64())}
	}
	if len(keys) == 0 {
		return errors.New("no RSA signing keys in JWKS")
	}
	v.keys, v.fetched = keys, time.Now()
	return nil
}

func (v *oidcVerifier) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := v.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("GET %s: HTTP %d", url, res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
