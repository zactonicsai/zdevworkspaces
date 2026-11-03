package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Entra ID v2 endpoints. The tenant is a GUID or a verified domain; "organizations"
// also works for multi-tenant app registrations.
var entraBase = "https://login.microsoftonline.com"

const (
	defaultScope    = "api://devspaces/Workspaces.ReadWrite offline_access openid profile"
	defaultAudience = "api://devspaces"
)

func deviceCodeURL(tenant string) string { return entraBase + "/" + tenant + "/oauth2/v2.0/devicecode" }
func tokenURL(tenant string) string      { return entraBase + "/" + tenant + "/oauth2/v2.0/token" }

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
	Error           string `json:"error"`
	ErrorDesc       string `json:"error_description"`
}

// deviceLogin runs the OAuth 2.0 device authorization flow: we show a short
// code, the person types it at microsoft.com/devicelogin on any device, and we
// poll the token endpoint until Entra hands us the tokens.
func deviceLogin(ctx context.Context, hc *http.Client, cfg *Config, out io.Writer) error {
	if cfg.Tenant == "" || cfg.ClientID == "" {
		return errors.New("device login needs --tenant and --client-id (ask your platform admin; they are printed by deploy/cluster/providers/azure/entra-apps.sh)")
	}
	if cfg.Scope == "" {
		cfg.Scope = defaultScope
	}
	form := url.Values{"client_id": {cfg.ClientID}, "scope": {cfg.Scope}}
	var dc deviceCodeResponse
	if err := postForm(ctx, hc, deviceCodeURL(cfg.Tenant), form, &dc); err != nil {
		return fmt.Errorf("device code request: %w", err)
	}
	if dc.Error != "" {
		return fmt.Errorf("device code request: %s: %s", dc.Error, dc.ErrorDesc)
	}
	if dc.Message != "" {
		fmt.Fprintln(out, dc.Message)
	} else {
		fmt.Fprintf(out, "Open %s and enter the code %s\n", dc.VerificationURI, dc.UserCode)
	}
	interval := time.Duration(maxInt(dc.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(maxInt(dc.ExpiresIn, 300)) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		var tr tokenResponse
		err := postForm(ctx, hc, tokenURL(cfg.Tenant), url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {cfg.ClientID},
			"device_code": {dc.DeviceCode},
		}, &tr)
		if err != nil {
			return fmt.Errorf("token request: %w", err)
		}
		switch tr.Error {
		case "":
			storeToken(cfg, tr)
			cfg.Auth = "device"
			return nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		default:
			return fmt.Errorf("login failed: %s: %s", tr.Error, tr.ErrorDesc)
		}
	}
	return errors.New("login timed out: the code expired before it was entered")
}

// refreshToken exchanges the refresh token for a new access token.
func refreshToken(ctx context.Context, hc *http.Client, cfg *Config) (string, error) {
	if cfg.RefreshToken == "" || cfg.ClientID == "" || cfg.Tenant == "" {
		return "", errors.New("no refresh token")
	}
	var tr tokenResponse
	err := postForm(ctx, hc, tokenURL(cfg.Tenant), url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {cfg.ClientID},
		"refresh_token": {cfg.RefreshToken},
		"scope":         {firstNonEmpty(cfg.Scope, defaultScope)},
	}, &tr)
	if err != nil {
		return "", err
	}
	if tr.Error != "" {
		return "", fmt.Errorf("%s: %s", tr.Error, tr.ErrorDesc)
	}
	storeToken(cfg, tr)
	return tr.AccessToken, nil
}

func storeToken(cfg *Config, tr tokenResponse) {
	cfg.Token = tr.AccessToken
	if tr.RefreshToken != "" {
		cfg.RefreshToken = tr.RefreshToken
	}
	if tr.ExpiresIn > 0 {
		cfg.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
}

// azLogin asks the Azure CLI for a token for the API's audience. Handy where
// people already `az login` all day.
func azLogin(ctx context.Context, cfg *Config) error {
	aud := firstNonEmpty(cfg.Audience, defaultAudience)
	cmd := exec.CommandContext(ctx, "az", "account", "get-access-token", "--resource", aud, "-o", "json")
	cmd.Stderr = os.Stderr
	b, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("az account get-access-token failed (is the Azure CLI installed and logged in?): %w", err)
	}
	var r struct {
		AccessToken string `json:"accessToken"`
		ExpiresOn   string `json:"expiresOn"`
	}
	if err := json.Unmarshal(b, &r); err != nil || r.AccessToken == "" {
		return errors.New("az returned no accessToken")
	}
	cfg.Token, cfg.Auth, cfg.RefreshToken = r.AccessToken, "az", ""
	if t, err := time.ParseInLocation("2006-01-02 15:04:05.000000", r.ExpiresOn, time.Local); err == nil {
		cfg.ExpiresAt = t
	} else {
		cfg.ExpiresAt = time.Now().Add(55 * time.Minute)
	}
	return nil
}

// tokenLogin stores a token pasted or read from a file ("-" = stdin).
func tokenLogin(cfg *Config, src string, stdin io.Reader) error {
	var raw string
	if src == "-" {
		sc := bufio.NewScanner(stdin)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		if sc.Scan() {
			raw = sc.Text()
		}
	} else {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "Bearer "))
	if raw == "" {
		return errors.New("empty token")
	}
	cfg.Token, cfg.Auth, cfg.RefreshToken, cfg.ExpiresAt = raw, "token", "", time.Time{}
	return nil
}

func postForm(ctx context.Context, hc *http.Client, u string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	// Entra answers 400 for authorization_pending etc. with a JSON body; decode either way.
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wellKnown is GET <portal>/.well-known/devspaces: unauthenticated login hints.
type wellKnown struct {
	Version string `json:"version"`
	Login   struct {
		Mode     string `json:"mode"` // dev | session | oidc
		Issuer   string `json:"issuer"`
		Audience string `json:"audience"`
		Tenant   string `json:"tenant"`
		ClientID string `json:"clientId"`
		Scope    string `json:"scope"`
	} `json:"login"`
}

func fetchWellKnown(ctx context.Context, hc *http.Client, cfg *Config) (*wellKnown, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Portal+"/.well-known/devspaces", nil)
	if err != nil {
		return nil, err
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var wk wellKnown
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&wk); err != nil {
		return nil, err
	}
	return &wk, nil
}
