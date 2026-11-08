package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to the portal's HTTP API. Every request carries the bearer
// token (if any) and the X-Requested-With header the portal uses as a CSRF
// guard for browser sessions.
type Client struct {
	Base     string
	Token    string
	Insecure bool
	HTTP     *http.Client
	// refresh is called once on a 401 to obtain a fresh token (device flow).
	refresh func(ctx context.Context) (string, error)
}

// APIError is a non-2xx answer from the portal.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	switch e.Status {
	case 401:
		return "not signed in (" + e.Message + ") — run: wsctl login"
	case 403:
		return "forbidden: " + e.Message
	case 404:
		return "not found: " + e.Message
	}
	return fmt.Sprintf("portal returned HTTP %d: %s", e.Status, e.Message)
}

func newClient(cfg *Config, portal, token string, insecure bool) (*Client, error) {
	base := strings.TrimRight(firstNonEmpty(portal, cfg.Portal), "/")
	if base == "" {
		return nil, errors.New("no portal URL: pass --portal https://… once (it is remembered), or set WSCTL_PORTAL")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 10 * time.Second,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext}
	if insecure || cfg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit --insecure
	}
	return &Client{Base: base, Token: firstNonEmpty(token, cfg.Token), Insecure: insecure || cfg.Insecure,
		HTTP: &http.Client{Transport: tr, Timeout: 120 * time.Second}}, nil
}

// do performs one API call. query may be nil; body (if non-nil) is JSON
// encoded; out (if non-nil) receives the decoded JSON answer. Text answers
// (logs) are returned as the raw string when out is a *string.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	err := c.once(ctx, method, path, query, body, out)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == 401 && c.refresh != nil {
		if tok, rerr := c.refresh(ctx); rerr == nil && tok != "" {
			c.Token = tok
			return c.once(ctx, method, path, query, body, out)
		}
	}
	return err
}

func (c *Client) once(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	u := c.Base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	c.headers(req.Header)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return friendlyNetErr(err, c.Base)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Message: errMessage(data, resp.Status)}
	}
	if out == nil {
		return nil
	}
	if s, ok := out.(*string); ok {
		// text endpoints answer text/plain; the portal's /logs answers {"logs": "..."} — accept both
		var wrapped struct {
			Logs *string `json:"logs"`
			Note string  `json:"note"`
		}
		if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) && json.Unmarshal(data, &wrapped) == nil && wrapped.Logs != nil {
			*s = *wrapped.Logs
			if *s == "" && wrapped.Note != "" {
				*s = wrapped.Note + "\n"
			}
			return nil
		}
		*s = string(data)
		return nil
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("unexpected answer from %s (not JSON: %s) — is --portal pointing at the portal?", u, snippet(data))
	}
	return nil
}

func (c *Client) headers(h http.Header) {
	h.Set("Accept", "application/json, text/plain")
	h.Set("X-Requested-With", "devspaces-portal")
	h.Set("User-Agent", "wsctl/"+version)
	if c.Token != "" {
		h.Set("Authorization", "Bearer "+c.Token)
	}
}

func errMessage(data []byte, status string) string {
	var m struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &m) == nil && m.Error != "" {
		return m.Error
	}
	if s := snippet(data); s != "" {
		return s
	}
	return status
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

func friendlyNetErr(err error, base string) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("cannot reach the portal at %s (connection refused) — is it running? use --portal URL to point elsewhere", base)
	case strings.Contains(msg, "no such host"):
		return fmt.Errorf("cannot resolve the portal host in %s — check --portal (or WSCTL_PORTAL)", base)
	case strings.Contains(msg, "certificate"):
		return fmt.Errorf("TLS problem talking to %s: %v (for a demo with a self-signed cert use --insecure)", base, err)
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "Timeout"):
		return fmt.Errorf("timed out talking to %s — VPN? wrong --portal?", base)
	}
	return err
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
