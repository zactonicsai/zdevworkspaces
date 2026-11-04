package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Obj is an untyped Kubernetes object. The portal needs ~10 fields of ~6
// kinds, so a generic map keeps it dependency-free (client-go would add ~40 MB
// of modules for no benefit here).
type Obj = map[string]any

// Kube is the small slice of the Kubernetes API the portal uses. There are two
// implementations: apiClient (real cluster, impersonating the user) and
// mockKube (in-memory, for `-mock` demos and tests).
type Kube interface {
	Get(ctx context.Context, path string) (Obj, error) // GET; returns errNotFound for 404
	List(ctx context.Context, path string, selector string) ([]Obj, error)
	Apply(ctx context.Context, path string, obj Obj) error // server-side apply (create or update)
	MergePatch(ctx context.Context, path string, patch Obj) error
	Delete(ctx context.Context, path string) error // 404 is not an error
	Logs(ctx context.Context, ns, pod, container string, tail int) (string, error)
}

var errNotFound = errors.New("not found")
var errForbidden = errors.New("forbidden")

type apiError struct {
	Status  int
	Reason  string
	Message string
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("kubernetes api: HTTP %d %s", e.Status, e.Reason)
}

// ---------------------------------------------------------------- real client

type kubeConfig struct {
	BaseURL   string
	TokenFile string // re-read periodically (projected tokens rotate)
	Token     string
	CAFile    string
	Insecure  bool
}

func kubeConfigFromEnv(flagURL, flagToken, flagCA string) kubeConfig {
	cfg := kubeConfig{BaseURL: flagURL, Token: flagToken, CAFile: flagCA}
	if cfg.BaseURL == "" {
		if h := os.Getenv("KUBERNETES_SERVICE_HOST"); h != "" {
			p := os.Getenv("KUBERNETES_SERVICE_PORT")
			if p == "" {
				p = "443"
			}
			cfg.BaseURL = "https://" + joinHostPort(h, p)
		}
	}
	const sa = "/var/run/secrets/kubernetes.io/serviceaccount"
	if cfg.Token == "" {
		if _, err := os.Stat(sa + "/token"); err == nil {
			cfg.TokenFile = sa + "/token"
		}
	}
	if cfg.CAFile == "" {
		if _, err := os.Stat(sa + "/ca.crt"); err == nil {
			cfg.CAFile = sa + "/ca.crt"
		}
	}
	return cfg
}

func joinHostPort(h, p string) string {
	if strings.Contains(h, ":") && !strings.HasPrefix(h, "[") {
		return "[" + h + "]:" + p
	}
	return h + ":" + p
}

type apiClient struct {
	base string
	http *http.Client
	cfg  kubeConfig
	user *User       // nil → act as the service account itself (health checks only)
	tok  *tokenCache // shared by all impersonating copies
}

type tokenCache struct {
	mu    sync.Mutex
	token string
	at    time.Time
}

func newAPIClient(cfg kubeConfig) (*apiClient, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("no Kubernetes API endpoint: run in-cluster, or pass -kube-url (e.g. http://127.0.0.1:8001 from `kubectl proxy`)")
	}
	tr := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 20}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA file contains no certificates")
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	} else if cfg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit dev flag
	}
	return &apiClient{base: strings.TrimRight(cfg.BaseURL, "/"), cfg: cfg, tok: &tokenCache{},
		http: &http.Client{Transport: tr, Timeout: 60 * time.Second}}, nil
}

// As returns a client that impersonates u (same transport, same connection pool).
func (c *apiClient) As(u User) *apiClient {
	cp := *c
	cp.user = &u
	return &cp
}

func (c *apiClient) bearer() string {
	if c.cfg.Token != "" {
		return c.cfg.Token
	}
	if c.cfg.TokenFile == "" {
		return ""
	}
	c.tok.mu.Lock()
	defer c.tok.mu.Unlock()
	if time.Since(c.tok.at) > time.Minute || c.tok.token == "" {
		if b, err := os.ReadFile(c.cfg.TokenFile); err == nil {
			c.tok.token = strings.TrimSpace(string(b))
			c.tok.at = time.Now()
		}
	}
	return c.tok.token
}

func (c *apiClient) do(ctx context.Context, method, path string, contentType string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if t := c.bearer(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	if c.user != nil {
		// Kubernetes impersonation: RBAC is evaluated for the developer, not for the portal.
		req.Header.Set("Impersonate-User", c.user.Name)
		for _, g := range c.user.Groups {
			if g != "system:authenticated" { // added automatically by the API server
				req.Header.Add("Impersonate-Group", g)
			}
		}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "devspaces-portal/"+version)
	res, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	return res.StatusCode, b, err
}

func statusErr(code int, body []byte) error {
	switch code {
	case 404:
		return errNotFound
	case 401, 403:
		var st struct{ Message string }
		_ = json.Unmarshal(body, &st)
		return fmt.Errorf("%w: %s", errForbidden, st.Message)
	}
	var st struct {
		Reason  string
		Message string
	}
	_ = json.Unmarshal(body, &st)
	return &apiError{Status: code, Reason: st.Reason, Message: st.Message}
}

func (c *apiClient) Get(ctx context.Context, path string) (Obj, error) {
	code, b, err := c.do(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, statusErr(code, b)
	}
	var o Obj
	return o, json.Unmarshal(b, &o)
}

func (c *apiClient) List(ctx context.Context, path, selector string) ([]Obj, error) {
	if selector != "" {
		path += "?labelSelector=" + url.QueryEscape(selector)
	}
	o, err := c.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	items, _ := o["items"].([]any)
	out := make([]Obj, 0, len(items))
	for _, it := range items {
		if m, ok := it.(Obj); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// Apply uses server-side apply, which creates or updates in one idempotent call
// and records the portal as field manager. force=true takes over fields the CLI
// may have set on the same object (both are legitimate editors).
func (c *apiClient) Apply(ctx context.Context, path string, obj Obj) error {
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	code, rb, err := c.do(ctx, http.MethodPatch, path+"?fieldManager=devspaces-portal&force=true", "application/apply-patch+yaml", b)
	if err != nil {
		return err
	}
	if code >= 300 {
		return statusErr(code, rb)
	}
	return nil
}

func (c *apiClient) MergePatch(ctx context.Context, path string, patch Obj) error {
	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	code, rb, err := c.do(ctx, http.MethodPatch, path, "application/merge-patch+json", b)
	if err != nil {
		return err
	}
	if code >= 300 {
		return statusErr(code, rb)
	}
	return nil
}

func (c *apiClient) Delete(ctx context.Context, path string) error {
	code, rb, err := c.do(ctx, http.MethodDelete, path, "application/json", []byte(`{"propagationPolicy":"Foreground"}`))
	if err != nil {
		return err
	}
	if code == 404 {
		return nil
	}
	if code >= 300 {
		return statusErr(code, rb)
	}
	return nil
}

func (c *apiClient) Logs(ctx context.Context, ns, pod, container string, tail int) (string, error) {
	p := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log?container=%s&tailLines=%d", url.PathEscape(ns), url.PathEscape(pod), url.QueryEscape(container), tail)
	code, b, err := c.do(ctx, http.MethodGet, p, "", nil)
	if err != nil {
		return "", err
	}
	if code >= 300 {
		return "", statusErr(code, b)
	}
	return string(b), nil
}

// ---------------------------------------------------------------- path helpers

func nsPath(group, version, resource, ns, name string) string {
	var p string
	if group == "" {
		p = "/api/" + version
	} else {
		p = "/apis/" + group + "/" + version
	}
	p += "/namespaces/" + url.PathEscape(ns) + "/" + resource
	if name != "" {
		p += "/" + url.PathEscape(name)
	}
	return p
}

func pathFor(obj Obj) string {
	kind, _ := obj["kind"].(string)
	apiVersion, _ := obj["apiVersion"].(string)
	meta, _ := obj["metadata"].(Obj)
	ns, _ := meta["namespace"].(string)
	name, _ := meta["name"].(string)
	group, version := "", apiVersion
	if i := strings.IndexByte(apiVersion, '/'); i >= 0 {
		group, version = apiVersion[:i], apiVersion[i+1:]
	}
	resource := map[string]string{
		"PersistentVolumeClaim": "persistentvolumeclaims", "Secret": "secrets", "Service": "services",
		"StatefulSet": "statefulsets", "NetworkPolicy": "networkpolicies", "ConfigMap": "configmaps", "Pod": "pods",
		"VolumeSnapshot": "volumesnapshots", "Role": "roles", "RoleBinding": "rolebindings",
		"PersistentVolume": "persistentvolumes", "Namespace": "namespaces",
	}[kind]
	if ns == "" { // cluster-scoped
		return clusterPath(group, version, resource, name)
	}
	return nsPath(group, version, resource, ns, name)
}

// clusterPath is nsPath for cluster-scoped resources or cluster-wide lists.
func clusterPath(group, version, resource, name string) string {
	var p string
	if group == "" {
		p = "/api/" + version
	} else {
		p = "/apis/" + group + "/" + version
	}
	p += "/" + resource
	if name != "" {
		p += "/" + url.PathEscape(name)
	}
	return p
}

// ---------------------------------------------------------------- JSON helpers

func getIn(o Obj, path ...string) any {
	var cur any = o
	for _, p := range path {
		m, ok := cur.(Obj)
		if !ok {
			return nil
		}
		cur, ok = m[p]
		if !ok {
			return nil
		}
	}
	return cur
}

func str(o Obj, path ...string) string {
	s, _ := getIn(o, path...).(string)
	return s
}

func num(o Obj, path ...string) float64 {
	switch v := getIn(o, path...).(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func labels(o Obj) map[string]string {
	out := map[string]string{}
	if m, ok := getIn(o, "metadata", "labels").(Obj); ok {
		for k, v := range m {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

func annotations(o Obj) map[string]string {
	out := map[string]string{}
	if m, ok := getIn(o, "metadata", "annotations").(Obj); ok {
		for k, v := range m {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

func deepCopy(o Obj) Obj {
	b, _ := json.Marshal(o)
	var out Obj
	_ = json.Unmarshal(b, &out)
	return out
}
