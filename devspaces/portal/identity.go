package main

import (
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
)

// User is the developer behind a request. The portal never authenticates on
// its own: it sits behind an authenticating proxy (oauth2-proxy, an identity
// aware proxy, an Istio RequestAuthentication …) that sets the X-Forwarded-*
// headers. Those headers are trusted only from the configured proxy CIDRs.
// Every Kubernetes call is then made *as that user* (impersonation), so the
// developer's own RBAC — the same they have with kubectl — decides what works.
type User struct {
	Name   string   `json:"name"`   // identity as the API server knows it (usually the e-mail)
	Short  string   `json:"short"`  // sanitised: namespace suffix, ssh comments
	Groups []string `json:"groups"` // forwarded groups (optional)
	Auth   string   `json:"-"`      // session | bearer | dev — bearer callers skip the CSRF header
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9-]+`)

// sanitizeUser mirrors the CLI: lowercase, local part of an e-mail, [a-z0-9-], max 40.
func sanitizeUser(u string) string {
	u = strings.ToLower(u)
	if i := strings.IndexByte(u, '@'); i >= 0 {
		u = u[:i]
	}
	u = strings.Trim(nonAlnum.ReplaceAllString(u, "-"), "-")
	if len(u) > 40 {
		u = u[:40]
	}
	if u == "" {
		u = "dev"
	}
	return u
}

func (u User) Namespace(tier string) string {
	if tier == "lab" {
		return "ws-" + u.Short + "-lab"
	}
	return "ws-" + u.Short
}

type identityResolver struct {
	devUser    string
	userHeader string
	grpHeader  string
	trusted    []*net.IPNet
	oidc       *oidcVerifier // nil → bearer tokens are not accepted
}

// resolve returns the caller. Order: bearer token (CLI, APIM) → dev user → proxy headers.
// A bad bearer token is an error, never a fall-through to another identity.
func (r *identityResolver) resolve(req *http.Request) (User, error) {
	if ah := req.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(ah), "bearer ") {
		if r.oidc == nil {
			if r.devUser != "" { // demo portals: any token means "the dev user"
				return User{Name: r.devUser, Short: sanitizeUser(r.devUser), Groups: []string{"system:authenticated"}, Auth: "dev"}, nil
			}
			return User{}, errors.New("bearer tokens are not enabled on this portal (-oidc-issuer)")
		}
		return r.oidc.verify(req.Context(), strings.TrimSpace(ah[7:]))
	}
	if r.devUser != "" {
		return User{Name: r.devUser, Short: sanitizeUser(r.devUser), Groups: []string{"system:authenticated"}, Auth: "dev"}, nil
	}
	if !r.fromTrustedProxy(req) {
		return User{}, errors.New("not signed in: the portal must run behind an authenticating proxy, accept bearer tokens (-oidc-issuer), or run with -dev-user")
	}
	name := strings.TrimSpace(req.Header.Get(r.userHeader))
	if name == "" {
		name = strings.TrimSpace(req.Header.Get("X-Forwarded-Email"))
	}
	if name == "" {
		return User{}, errors.New("not signed in (no identity header from the proxy)")
	}
	var groups []string
	for _, g := range strings.Split(req.Header.Get(r.grpHeader), ",") {
		if g = strings.TrimSpace(g); g != "" {
			groups = append(groups, g)
		}
	}
	return User{Name: name, Short: sanitizeUser(name), Groups: groups, Auth: "session"}, nil
}

func (r *identityResolver) fromTrustedProxy(req *http.Request) bool {
	if len(r.trusted) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range r.trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func parseCIDRs(s string) []*net.IPNet {
	var out []*net.IPNet
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			if strings.Contains(part, ":") {
				part += "/128"
			} else {
				part += "/32"
			}
		}
		if _, n, err := net.ParseCIDR(part); err == nil {
			out = append(out, n)
		}
	}
	return out
}
