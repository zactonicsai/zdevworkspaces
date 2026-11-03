package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// run executes the CLI against the fake portal and returns stdout, stderr, exit code.
func run(t *testing.T, f *fakePortal, stdin string, args ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WSCTL_SSH_DIR", filepath.Join(dir, "ssh"))
	t.Setenv("WSCTL_PORTAL", "")
	t.Setenv("WSCTL_TOKEN", "")
	cfg := filepath.Join(dir, "config.json")
	_ = saveConfig(cfg, &Config{Portal: f.srv.URL, Token: f.token, Auth: "token"})
	var out, errb bytes.Buffer
	a := &app{stdout: &out, stderr: &errb, stdin: strings.NewReader(stdin), exe: "/usr/local/bin/wsctl", ctx: context.Background()}
	code := a.main(append([]string{"--config", cfg}, args...))
	return out.String(), errb.String(), code
}

func TestLifecycle(t *testing.T) {
	f := newFakePortal(t, "tok123")

	out, errs, code := run(t, f, "", "create", "api", "--lang", "go,python", "--tools", "cmake,gdb", "--repo", "git@github.com:acme/api.git",
		"--storage", "fast", "--mount", "shared:team-data", "--project", "checkout", "--idle-timeout", "2h", "--timeout", "30s")
	if code != 0 {
		t.Fatalf("create failed: %s %s", out, errs)
	}
	if !strings.Contains(out, "api is Running") || !strings.Contains(errs, "deploy key") {
		t.Errorf("unexpected create output:\n%s\n%s", out, errs)
	}
	req := f.lastReq
	if req["name"] != "api" || req["namespace"] != "ws-proj-checkout" || req["storage"] != "fast" || req["idleTimeout"] != "2h" {
		t.Errorf("request body wrong: %v", req)
	}
	if langs, _ := json.Marshal(req["languages"]); string(langs) != `["go","python"]` {
		t.Errorf("languages = %s", langs)
	}
	if tools, _ := json.Marshal(req["tools"]); string(tools) != `["cmake","gdb"]` {
		t.Errorf("tools = %s", tools)
	}
	if m, _ := json.Marshal(req["mounts"]); string(m) != `["shared:team-data"]` {
		t.Errorf("mounts = %s", m)
	}
	if _, ok := req["expose"]; ok {
		t.Errorf("false bools must be omitted: %v", req)
	}
	// ssh config was written
	cfgTxt, _ := os.ReadFile(filepath.Join(os.Getenv("WSCTL_SSH_DIR"), "config"))
	if !strings.Contains(string(cfgTxt), "Host ws-api") || !strings.Contains(string(cfgTxt), "ProxyCommand /usr/local/bin/wsctl proxy api") {
		t.Errorf("ssh config:\n%s", cfgTxt)
	}
	kh, _ := os.ReadFile(filepath.Join(os.Getenv("WSCTL_SSH_DIR"), "known_hosts_wsctl"))
	if !strings.Contains(string(kh), "ws-api ssh-ed25519 AAAAC3host") {
		t.Errorf("known_hosts: %q", kh)
	}

	out, _, code = run(t, f, "", "list")
	if code != 0 || !strings.Contains(out, "api") || !strings.Contains(out, "ws-proj-checkout") || !strings.Contains(out, "go,python") {
		t.Errorf("list:\n%s", out)
	}
	out, _, _ = run(t, f, "", "list", "--shared")
	if !strings.Contains(out, "bob@example.com/api") {
		t.Errorf("shared list:\n%s", out)
	}
	out, _, _ = run(t, f, "", "-o", "json", "get", "api")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["workspace"] == nil {
		t.Errorf("get -o json: %v %s", err, out)
	}
	out, _, _ = run(t, f, "", "get", "api")
	if !strings.Contains(out, "ws-api.ws-proj-checkout.svc.cluster.local") || !strings.Contains(out, "SHA256:abc") {
		t.Errorf("get:\n%s", out)
	}
	if out, _, code = run(t, f, "", "stop", "api"); code != 0 || !strings.Contains(out, "stop requested") {
		t.Errorf("stop: %d %s", code, out)
	}
	if out, _, code = run(t, f, "", "start", "api", "--wait", "--timeout", "20s"); code != 0 || !strings.Contains(out, "is Running") {
		t.Errorf("start: %d %s", code, out)
	}
	if out, _, code = run(t, f, "", "idle", "api", "30m"); code != 0 || f.workspaces["api"].IdleTimeout != "30m" {
		t.Errorf("idle: %d %s", code, out)
	}
	if out, _, code = run(t, f, "", "logs", "api", "--tail", "5"); code != 0 || !strings.Contains(out, "sshd listening") || f.lastQuery["tail"] != "5" {
		t.Errorf("logs: %d %s %v", code, out, f.lastQuery)
	}
	if out, _, code = run(t, f, "", "network", "api"); code != 0 || !strings.Contains(out, "10.0.0.5") {
		t.Errorf("network: %d %s", code, out)
	}
	// owner/name → ?owner=
	run(t, f, "", "get", "bob@example.com/api")
	if f.lastQuery["owner"] != "bob@example.com" {
		t.Errorf("owner query not sent: %v", f.lastQuery)
	}
	// --ns → ?ns=
	run(t, f, "", "--ns", "ws-proj-checkout", "get", "api")
	if f.lastQuery["ns"] != "ws-proj-checkout" {
		t.Errorf("ns query not sent: %v", f.lastQuery)
	}
	// delete asks; "n" cancels
	if _, _, code = run(t, f, "n\n", "delete", "api"); code == 0 {
		t.Errorf("delete should be cancelled")
	}
	if out, _, code = run(t, f, "", "delete", "api", "-y"); code != 0 || !strings.Contains(out, "home drive kept") {
		t.Errorf("delete: %d %s", code, out)
	}
	if _, ok := f.workspaces["api"]; ok {
		t.Errorf("workspace still there")
	}
}

func TestSharesLinksDrives(t *testing.T) {
	f := newFakePortal(t, "")
	run(t, f, "", "create", "web", "--lang", "node", "--no-wait")
	out, _, code := run(t, f, "", "share", "add", "web", "bob@example.com")
	if code != 0 || !strings.Contains(out, "bob@example.com can now connect") {
		t.Errorf("share add: %d %s", code, out)
	}
	if out, _, _ = run(t, f, "", "share", "list", "web"); !strings.Contains(out, "bob@example.com") {
		t.Errorf("share list: %s", out)
	}
	if _, _, code = run(t, f, "", "share", "rm", "web", "bob@example.com"); code != 0 {
		t.Errorf("share rm")
	}
	out, _, code = run(t, f, "", "link", "add", "web", "bob@example.com/api", "--port", "8080")
	if code != 0 || !strings.Contains(out, "pending-peer") || !strings.Contains(out, "ask bob@example.com to run") {
		t.Errorf("link add: %d %s", code, out)
	}
	if p, _ := json.Marshal(f.lastReq["ports"]); string(p) != "[8080]" {
		t.Errorf("ports: %s", p)
	}
	if out, _, _ = run(t, f, "", "link", "list", "web"); !strings.Contains(out, "ws-api.ws-bob.svc.cluster.local") {
		t.Errorf("link list: %s", out)
	}
	// drives
	out, _, code = run(t, f, "", "drives", "create-shared", "team-data", "--size", "200Gi")
	if code != 0 || !strings.Contains(out, "--mount shared:team-data") {
		t.Errorf("create-shared: %d %s", code, out)
	}
	if out, _, _ = run(t, f, "", "drives", "list"); !strings.Contains(out, "shared-team-data") || !strings.Contains(out, "200Gi") {
		t.Errorf("drives list: %s", out)
	}
	if out, _, code = run(t, f, "", "drives", "snapshot", "shared-team-data", "--name", "before-refactor"); code != 0 || !strings.Contains(out, "before-refactor") {
		t.Errorf("snapshot: %d %s", code, out)
	}
	if out, _, _ = run(t, f, "", "drives", "snapshots", "shared-team-data"); !strings.Contains(out, "before-refactor") {
		t.Errorf("snapshots: %s", out)
	}
	if out, _, _ = run(t, f, "", "drives", "show", "shared-team-data"); !strings.Contains(out, "ReadWriteMany") || !strings.Contains(out, "before-refactor") {
		t.Errorf("show: %s", out)
	}
	if _, _, code = run(t, f, "", "drives", "resize", "shared-team-data", "300Gi"); code != 0 || f.drives["shared-team-data"].Size != "300Gi" {
		t.Errorf("resize")
	}
	if out, _, code = run(t, f, "", "share", "add", "drive:shared-team-data", "bob@example.com", "--read-only"); code != 0 || !strings.Contains(out, "--mount shared:<you>/shared-team-data") {
		t.Errorf("share drive: %d %s", code, out)
	}
	if ro, _ := f.lastReq["readOnly"].(bool); !ro {
		t.Errorf("readOnly not sent: %v", f.lastReq)
	}
	// restore = create --from-snapshot
	_, _, code = run(t, f, "", "drives", "restore", "before-refactor", "web2", "--lang", "node", "--no-wait")
	if code != 0 || f.lastReq["fromSnapshot"] != "before-refactor" || f.lastReq["name"] != "web2" {
		t.Errorf("restore: %d %v", code, f.lastReq)
	}
	if _, _, code = run(t, f, "", "drives", "delete", "shared-team-data", "-y"); code != 0 {
		t.Errorf("drives delete")
	}
	out, _, _ = run(t, f, "", "projects")
	if !strings.Contains(out, "ws-proj-checkout") {
		t.Errorf("projects: %s", out)
	}
	out, _, _ = run(t, f, "", "catalog")
	if !strings.Contains(out, "fast") || !strings.Contains(out, "premium") {
		t.Errorf("catalog: %s", out)
	}
	out, _, _ = run(t, f, "", "me")
	if !strings.Contains(out, "alice@example.com") {
		t.Errorf("me: %s", out)
	}
}

func TestErrors(t *testing.T) {
	f := newFakePortal(t, "secret")
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	_ = saveConfig(cfg, &Config{Portal: f.srv.URL, Token: "wrong"})
	var out, errb bytes.Buffer
	a := &app{stdout: &out, stderr: &errb, stdin: strings.NewReader(""), ctx: context.Background()}
	if code := a.main([]string{"--config", cfg, "list"}); code != 4 || !strings.Contains(errb.String(), "wsctl login") {
		t.Errorf("401 handling: code=%d err=%s", code, errb.String())
	}
	errb.Reset()
	a = &app{stdout: &out, stderr: &errb, stdin: strings.NewReader(""), ctx: context.Background()}
	if code := a.main([]string{"--config", cfg, "--portal", "http://127.0.0.1:1", "list"}); code != 1 || !strings.Contains(errb.String(), "connection refused") {
		t.Errorf("refused handling: code=%d err=%s", code, errb.String())
	}
	errb.Reset()
	a = &app{stdout: &out, stderr: &errb, stdin: strings.NewReader(""), ctx: context.Background()}
	if code := a.main([]string{"--config", cfg, "frobnicate"}); code != 2 {
		t.Errorf("unknown command code=%d", code)
	}
	out.Reset()
	a = &app{stdout: &out, stderr: &errb, stdin: strings.NewReader(""), ctx: context.Background()}
	if code := a.main(nil); code != 0 || !strings.Contains(out.String(), "Commands:") {
		t.Errorf("usage: %d %s", code, out.String())
	}
	errb.Reset()
	a = &app{stdout: &out, stderr: &errb, stdin: strings.NewReader(""), ctx: context.Background()}
	if code := a.main([]string{"--config", cfg, "create"}); code != 1 || !strings.Contains(errb.String(), "--lang is required") {
		t.Errorf("create without lang: %d %s", code, errb.String())
	}
}

func TestProxyTunnel(t *testing.T) {
	f := newFakePortal(t, "tok")
	run(t, f, "", "create", "api", "--lang", "go", "--no-wait")
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	_ = saveConfig(cfg, &Config{Portal: f.srv.URL, Token: "tok"})
	var out, errb bytes.Buffer
	a := &app{stdout: &out, stderr: &errb, stdin: strings.NewReader("SSH-2.0-OpenSSH\r\n"), ctx: context.Background()}
	// the fake echoes "echo:"+data then our stdin hits EOF → proxy returns
	if code := a.main([]string{"--config", cfg, "proxy", "api"}); code != 0 {
		t.Fatalf("proxy exit %d: %s", code, errb.String())
	}
	// stdout may arrive slightly after EOF; the pipe closes on stdin EOF, so allow either order
	if !strings.Contains(out.String(), "echo:SSH-2.0-OpenSSH") && !strings.Contains(errb.String(), "workspace is Running") {
		t.Errorf("tunnel output: out=%q err=%q", out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "workspace is Running") {
		t.Errorf("progress text frame not shown: %q", errb.String())
	}
}

func TestWebSocketFraming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(wsEcho))
	defer srv.Close()
	ws, err := wsDial(context.Background(), srv.URL+"/api/workspaces/x/tunnel", http.Header{"Authorization": {"Bearer t"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	// first message is the text progress frame; the ping is answered transparently
	op, data, err := ws.ReadMessage()
	if err != nil || op != opText || string(data) != "workspace is Running" {
		t.Fatalf("text frame: %v %d %q", err, op, data)
	}
	for _, n := range []int{1, 125, 126, 70000} { // 7-bit, 16-bit and 64-bit length encodings
		payload := bytes.Repeat([]byte{0xAB}, n)
		if err := ws.WriteBinary(payload); err != nil {
			t.Fatal(err)
		}
		op, data, err := ws.ReadMessage()
		if err != nil || op != opBinary || len(data) != n+5 || !bytes.Equal(data[5:], payload) || string(data[:5]) != "echo:" {
			t.Fatalf("n=%d: %v op=%d len=%d", n, err, op, len(data))
		}
	}
	// a non-websocket endpoint must fail cleanly
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer plain.Close()
	if _, err := wsDial(context.Background(), plain.URL, nil, false); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("expected API error, got %v", err)
	}
}

func TestSSHConfigBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	_ = os.WriteFile(path, []byte("Host github.com\n  User git\n"), 0o600)
	b1 := sshConfigBlock("ws-api", "api", "", "/bin/wsctl", false, "")
	if err := upsertSSHBlock(path, "ws-api", b1); err != nil {
		t.Fatal(err)
	}
	b2 := sshConfigBlock("ws-api", "api", "ws-proj-x", "/bin/wsctl", true, "https://p.example.com")
	if err := upsertSSHBlock(path, "ws-api", b2); err != nil {
		t.Fatal(err)
	}
	if err := upsertSSHBlock(path, "ws-web", sshConfigBlock("ws-web", "web", "", "/bin/wsctl", false, "")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if strings.Count(s, "# >>> wsctl ws-api") != 1 || !strings.Contains(s, "--ns ws-proj-x --portal https://p.example.com --insecure") || !strings.Contains(s, "Host github.com") {
		t.Errorf("after upsert:\n%s", s)
	}
	if err := removeSSHBlock(path, "ws-api"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	s = string(got)
	if strings.Contains(s, "ws-api") || !strings.Contains(s, "# >>> wsctl ws-web") || !strings.HasPrefix(s, "Host github.com") {
		t.Errorf("after remove:\n%s", s)
	}
	kh := filepath.Join(dir, "known_hosts")
	_ = writeKnownHost(kh, "ws-api", "ws-api ssh-ed25519 AAA1")
	_ = writeKnownHost(kh, "ws-web", "ws-web ssh-ed25519 AAA2")
	_ = writeKnownHost(kh, "ws-api", "ws-api ssh-ed25519 AAA3")
	got, _ = os.ReadFile(kh)
	if string(got) != "ws-web ssh-ed25519 AAA2\nws-api ssh-ed25519 AAA3\n" {
		t.Errorf("known_hosts: %q", got)
	}
	_ = writeKnownHost(kh, "ws-api", "")
	got, _ = os.ReadFile(kh)
	if string(got) != "ws-web ssh-ed25519 AAA2\n" {
		t.Errorf("known_hosts after remove: %q", got)
	}
	if q := shellQuote("it's"); q != `'it'\''s'` {
		t.Errorf("shellQuote: %s", q)
	}
}

func TestDeviceCodeFlow(t *testing.T) {
	polls := 0
	entra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/devicecode"):
			if r.Form.Get("client_id") != "cid" || !strings.Contains(r.Form.Get("scope"), "api://devspaces") {
				t.Errorf("devicecode form: %v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc", "user_code": "ABCD-1234", "verification_uri": "https://microsoft.com/devicelogin",
				"expires_in": 60, "interval": 0, "message": "go to https://microsoft.com/devicelogin and enter ABCD-1234"})
		case strings.HasSuffix(r.URL.Path, "/token"):
			polls++
			switch r.Form.Get("grant_type") {
			case "urn:ietf:params:oauth:grant-type:device_code":
				if r.Form.Get("device_code") != "dc" {
					t.Errorf("device_code missing")
				}
				if polls < 3 {
					w.WriteHeader(400)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending", "error_description": "waiting"})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "AT1", "refresh_token": "RT1", "expires_in": 3600, "token_type": "Bearer"})
			case "refresh_token":
				if r.Form.Get("refresh_token") != "RT1" {
					t.Errorf("refresh token missing")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "AT2", "refresh_token": "RT2", "expires_in": 3600})
			default:
				t.Errorf("grant %q", r.Form.Get("grant_type"))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer entra.Close()
	old := entraBase
	entraBase = entra.URL
	defer func() { entraBase = old }()

	cfg := &Config{Tenant: "contoso.onmicrosoft.com", ClientID: "cid"}
	var msg bytes.Buffer
	if err := deviceLogin(context.Background(), entra.Client(), cfg, &msg); err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "AT1" || cfg.RefreshToken != "RT1" || cfg.Auth != "device" || !strings.Contains(msg.String(), "ABCD-1234") || time.Until(cfg.ExpiresAt) < 50*time.Minute {
		t.Errorf("after login: %+v msg=%q", cfg, msg.String())
	}
	if polls != 3 {
		t.Errorf("expected 3 polls, got %d", polls)
	}
	tok, err := refreshToken(context.Background(), entra.Client(), cfg)
	if err != nil || tok != "AT2" || cfg.RefreshToken != "RT2" {
		t.Errorf("refresh: %v %s %+v", err, tok, cfg)
	}

	// The client retries once with a refreshed token after a 401.
	f := newFakePortal(t, "AT3")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	_ = saveConfig(cfgPath, &Config{Portal: f.srv.URL, Token: "stale", RefreshToken: "RT2", Auth: "device", Tenant: "t", ClientID: "cid"})
	var out, errb bytes.Buffer
	a := &app{stdout: &out, stderr: &errb, stdin: strings.NewReader(""), ctx: context.Background()}
	a.cfgPath = cfgPath
	a.cfg, _ = loadConfig(cfgPath)
	c, _ := a.api()
	c.refresh = func(context.Context) (string, error) { return "AT3", nil }
	if err := c.do(context.Background(), http.MethodGet, "/api/me", nil, nil, &map[string]any{}); err != nil {
		t.Errorf("retry after refresh failed: %v", err)
	}
	// token login from stdin
	cfg2 := &Config{}
	if err := tokenLogin(cfg2, "-", strings.NewReader("Bearer eyJabc\n")); err != nil || cfg2.Token != "eyJabc" || cfg2.Auth != "token" {
		t.Errorf("tokenLogin: %v %+v", err, cfg2)
	}
	_ = io.EOF
}

func TestConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	c := &Config{Portal: "https://x", Token: "t", ExpiresAt: time.Now().Round(time.Second)}
	if err := saveConfig(p, c); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v", st.Mode())
	}
	got, err := loadConfig(p)
	if err != nil || got.Portal != "https://x" || got.Token != "t" || !got.ExpiresAt.Equal(c.ExpiresAt) {
		t.Errorf("round trip: %v %+v", err, got)
	}
	if empty, err := loadConfig(filepath.Join(t.TempDir(), "nope.json")); err != nil || empty.Portal != "" {
		t.Errorf("missing config should be empty: %v", err)
	}
}

func TestParseMixed(t *testing.T) {
	a := &app{stderr: io.Discard}
	fs := a.newFlags("x", "x")
	var langs multiFlag
	fs.Var(&langs, "lang", "")
	b := fs.Bool("flag", false, "")
	pos, err := parseMixed(fs, []string{"name", "--lang", "go,python", "--flag", "--lang=rust", "extra"})
	if err != nil || strings.Join(pos, " ") != "name extra" || langs.String() != "go,python,rust" || !*b {
		t.Errorf("parseMixed: %v %v %v %v", err, pos, langs, *b)
	}
	if o, n := splitRef("bob@example.com/api"); o != "bob@example.com" || n != "api" {
		t.Errorf("splitRef")
	}
	if o, n := splitRef("api"); o != "" || n != "api" {
		t.Errorf("splitRef plain")
	}
}
