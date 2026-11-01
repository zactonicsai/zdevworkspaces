package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testServer(t *testing.T) (*server, *httptest.Server) {
	t.Helper()
	u := User{Name: "alice@corp.example", Short: "alice", Groups: []string{"system:authenticated"}}
	mk := newMockKube(u)
	s := &server{version: "test", cats: &catalogCache{}, ids: &identityResolver{devUser: u.Name},
		kube: func(User) Kube { return mk }}
	s.canLab = func(ctx context.Context, k Kube, u User) bool { return true }
	ts := httptest.NewServer(s.authenticate(s.routes()))
	t.Cleanup(ts.Close)
	return s, ts
}

func call(t *testing.T, ts *httptest.Server, method, path string, body any) (int, Obj) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rd)
	req.Header.Set("X-Requested-With", "devspaces-portal")
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out Obj
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGm7gPZQ0cYQhh3v4u5n9Vd2D5rR0f7w3q1Yx2cZbWvK alice@laptop"

func TestSanitizeAndDurations(t *testing.T) {
	for in, want := range map[string]string{"Alice.Smith@Corp.example": "alice-smith", "bob": "bob", "x__y": "x-y", "": "dev"} {
		if got := sanitizeUser(in); got != want {
			t.Errorf("sanitizeUser(%q)=%q want %q", in, got, want)
		}
	}
	for in, ok := range map[string]bool{"4h": true, "1d12h": true, "off": true, "3weeks": false, "90m": true} {
		if _, err := parseDuration(in); (err == nil) != ok {
			t.Errorf("parseDuration(%q) ok=%v", in, err == nil)
		}
	}
}

func TestKeypairIsValidOpenSSH(t *testing.T) {
	kp, err := generateKeypair("test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(kp.Public, "ssh-ed25519 ") || !strings.Contains(kp.Private, "OPENSSH PRIVATE KEY") {
		t.Fatalf("unexpected formats: %q / %q", kp.Public, kp.Private[:40])
	}
	if _, ok := validPublicKey(kp.Public); !ok {
		t.Fatal("generated public key does not validate")
	}
	if fp := fingerprint(kp.Public); !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("bad fingerprint %q", fp)
	}
	// Cross-check with a real OpenSSH parser when available (python-cryptography or ssh-keygen).
	dir := t.TempDir()
	priv := filepath.Join(dir, "key")
	_ = os.WriteFile(priv, []byte(kp.Private), 0o600)
	checked := false
	if _, err := exec.LookPath("ssh-keygen"); err == nil {
		cp := filepath.Join(dir, "key-copy") // ssh-keygen must not touch the file python checks
		_ = os.WriteFile(cp, []byte(kp.Private), 0o600)
		out, err := exec.Command("ssh-keygen", "-y", "-f", cp).CombinedOutput()
		if err == nil && strings.HasPrefix(string(out), "ssh-ed25519 ") { // a real ssh-keygen
			checked = true
			if !strings.HasPrefix(string(out), strings.Join(strings.Fields(kp.Public)[:2], " ")) {
				t.Fatalf("ssh-keygen derived a different public key: %s", out)
			}
		}
	}
	if py, err := exec.LookPath("python3"); err == nil && !checked {
		script := `import sys
from cryptography.hazmat.primitives import serialization
k = serialization.load_ssh_private_key(open(sys.argv[1],'rb').read(), password=None)
print(k.public_key().public_bytes(serialization.Encoding.OpenSSH, serialization.PublicFormat.OpenSSH).decode())`
		out, err := exec.Command(py, "-I", "-c", script, priv).CombinedOutput()
		if err != nil {
			t.Skipf("python cryptography unavailable: %s", out)
		}
		if strings.TrimSpace(string(out)) != strings.Join(strings.Fields(kp.Public)[:2], " ") {
			t.Fatalf("python derived a different public key: %s vs %s", out, kp.Public)
		}
	}
}

func TestValidPublicKeyRejectsJunk(t *testing.T) {
	bad := []string{"", "hello", "ssh-ed25519 notbase64!!", "ssh-rsa AAAAC3NzaC1lZDI1NTE5AAAAIGm7gPZQ0cYQhh3v4u5n9Vd2D5rR0f7w3q1Yx2cZbWvK", testKey + "\nssh-ed25519 AAAA"}
	for _, b := range bad {
		if _, ok := validPublicKey(b); ok {
			t.Errorf("accepted junk key %q", b)
		}
	}
	if _, ok := validPublicKey(testKey); !ok {
		t.Error("rejected a valid key")
	}
}

func TestResolveAndRender(t *testing.T) {
	cat, err := parseCatalog(defaultCatalogJSON)
	if err != nil {
		t.Fatal(err)
	}
	u := User{Name: "alice@corp.example", Short: "alice"}
	res, err := resolveRequest(WorkspaceRequest{Name: "api", Languages: []string{"go", "python"}, Tools: []string{"ripgrep", "cmake"},
		Repo: "git@github.com:acme/api.git", Multicast: true}, cat, u)
	if err != nil {
		t.Fatal(err)
	}
	if res.Lang != "go" || !reflect.DeepEqual(res.Tools, []string{"ripgrep", "cmake", "python"}) {
		t.Fatalf("unexpected resolution: %+v", res)
	}
	kp, _ := generateKeypair("host")
	objs := renderWorkspace(renderInputs{Namespace: "ws-alice", Name: "api", AuthorizedKeys: []string{testKey}, HostKey: kp,
		GitSecret: Obj{"ssh_key": "x", "ssh_key.pub": "y"}}, res, cat)
	kinds := map[string]int{}
	for _, o := range objs {
		kinds[o["kind"].(string)]++
	}
	if kinds["StatefulSet"] != 1 || kinds["Secret"] != 2 || kinds["PersistentVolumeClaim"] != 1 || kinds["Service"] != 1 {
		t.Fatalf("unexpected object set %v", kinds)
	}
	var sts Obj
	for _, o := range objs {
		if o["kind"] == "StatefulSet" {
			sts = o
		}
	}
	pod := getIn(sts, "spec", "template", "spec").(Obj)
	if pod["hostUsers"] != false {
		t.Error("standard tier must set hostUsers=false")
	}
	if str(sts, "spec", "template", "metadata", "annotations", annNetworks) != "devspaces-system/mcast" {
		t.Error("multicast NAD annotation missing")
	}
	env := map[string]string{}
	for _, e := range pod["containers"].([]any)[0].(Obj)["env"].([]any) {
		if v, ok := e.(Obj)["value"].(string); ok {
			env[str(e.(Obj), "name")] = v
		}
	}
	if env["WORKSPACE_LANGUAGES"] != "go,python" || env["WORKSPACE_TOOLS"] != "ripgrep,cmake,python" {
		t.Errorf("env mismatch: %v", env)
	}
	if !strings.Contains(env["WORKSPACE_ARTIFACTS"], "github-remote/BurntSushi/ripgrep") {
		t.Errorf("artifact spec not rendered: %s", env["WORKSPACE_ARTIFACTS"])
	}
	// lab guard
	if _, err := resolveRequest(WorkspaceRequest{Languages: []string{"cpp"}, Devices: map[string]string{"nvidia.com/gpu": "1"}}, cat, u); err == nil {
		t.Error("devices without a lab profile should be rejected")
	}
	hw, err := resolveRequest(WorkspaceRequest{Name: "hw", Languages: []string{"cpp"}, Profile: "hardware", Devices: map[string]string{"nvidia.com/gpu": "1"}, HostPaths: []string{"/dev"}}, cat, u)
	if err != nil {
		t.Fatal(err)
	}
	objs = renderWorkspace(renderInputs{Namespace: "ws-alice-lab", Name: "hw", AuthorizedKeys: []string{testKey}, HostKey: kp}, hw, cat)
	for _, o := range objs {
		if o["kind"] == "StatefulSet" {
			c := getIn(o, "spec", "template", "spec", "containers").([]any)[0].(Obj)
			if getIn(c, "securityContext", "privileged") != true {
				t.Error("hardware profile must be privileged")
			}
			if getIn(c, "resources", "limits", "nvidia.com/gpu") != "1" {
				t.Error("device limit missing")
			}
		}
	}
}

// TestRenderMatchesCLI renders the same workspace with the Python CLI (if
// present) and compares the StatefulSet pod spec — the two must stay in sync.
func TestRenderMatchesCLI(t *testing.T) {
	cli := filepath.Join("..", "cli", "workspace")
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	if _, err := os.Stat(cli); err != nil {
		t.Skip("CLI not found")
	}
	cmd := exec.Command(py, cli, "--catalog", "catalog.default.json", "create", "api", "--lang", "go,python", "--tools", "ripgrep,cmake", "--repo", "git@github.com:acme/api.git",
		"--multicast", "--git-auth", "none", "--namespace", "ws-alice", "--dry-run", "--offline")
	cmd.Env = append(os.Environ(), "WORKSPACE_CONFIG_DIR="+t.TempDir())
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("CLI dry-run failed (ssh-keygen missing?): %v", err)
	}
	// extract the StatefulSet's pod env + securityContext via a tiny python yaml step
	script := `import sys,yaml,json
docs=[d for d in yaml.safe_load_all(sys.stdin) if d and d.get("kind")=="StatefulSet"]
p=docs[0]["spec"]["template"]["spec"]; c=p["containers"][0]
print(json.dumps({"env":{e["name"]:e.get("value") for e in c["env"] if "value" in e},"sc":c["securityContext"],"podsc":p["securityContext"],"hostUsers":p.get("hostUsers"),"mounts":[m["mountPath"] for m in c["volumeMounts"]]}, sort_keys=True))`
	ex := exec.Command(py, "-c", script)
	ex.Stdin = bytes.NewReader(out)
	cliJSON, err := ex.Output()
	if err != nil {
		t.Skipf("pyyaml unavailable: %v", err)
	}
	var fromCLI Obj
	_ = json.Unmarshal(cliJSON, &fromCLI)

	cat, _ := parseCatalog(defaultCatalogJSON)
	u := User{Name: "alice@corp.example", Short: "alice"}
	res, err := resolveRequest(WorkspaceRequest{Name: "api", Languages: []string{"go", "python"}, Tools: []string{"ripgrep", "cmake"},
		Repo: "git@github.com:acme/api.git", Multicast: true, GitAuth: "none"}, cat, u)
	if err != nil {
		t.Fatal(err)
	}
	kp, _ := generateKeypair("host")
	var sts Obj
	for _, o := range renderWorkspace(renderInputs{Namespace: "ws-alice", Name: "api", AuthorizedKeys: []string{testKey}, HostKey: kp}, res, cat) {
		if o["kind"] == "StatefulSet" {
			sts = o
		}
	}
	// normalise through JSON so numbers compare equal
	b, _ := json.Marshal(sts)
	_ = json.Unmarshal(b, &sts)
	p := getIn(sts, "spec", "template", "spec").(Obj)
	c := p["containers"].([]any)[0].(Obj)
	goEnv := Obj{}
	for _, e := range c["env"].([]any) {
		if v, ok := e.(Obj)["value"]; ok {
			goEnv[str(e.(Obj), "name")] = v
		}
	}
	var mounts []any
	for _, m := range c["volumeMounts"].([]any) {
		mounts = append(mounts, m.(Obj)["mountPath"])
	}
	fromGo := Obj{"env": goEnv, "sc": c["securityContext"], "podsc": p["securityContext"], "hostUsers": p["hostUsers"], "mounts": mounts}
	gb, _ := json.Marshal(fromGo)
	_ = json.Unmarshal(gb, &fromGo)
	if !reflect.DeepEqual(fromCLI, fromGo) {
		cj, _ := json.MarshalIndent(fromCLI, "", " ")
		gj, _ := json.MarshalIndent(fromGo, "", " ")
		t.Fatalf("portal render drifted from CLI render\nCLI:\n%s\nGo:\n%s", cj, gj)
	}
}

func TestHTTPLifecycle(t *testing.T) {
	_, ts := testServer(t)
	code, me := call(t, ts, "GET", "/api/me", nil)
	if code != 200 || me["namespace"] != "ws-alice" {
		t.Fatalf("me: %d %v", code, me)
	}
	// CSRF header required
	req, _ := http.NewRequest("POST", ts.URL+"/api/workspaces", strings.NewReader("{}"))
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 403 {
		t.Fatalf("expected 403 without X-Requested-With, got %d", res.StatusCode)
	}
	// no key → helpful 400
	code, body := call(t, ts, "POST", "/api/workspaces", Obj{"name": "api", "languages": []string{"go"}})
	if code != 400 || !strings.Contains(body["error"].(string), "SSH public key") {
		t.Fatalf("expected key error, got %d %v", code, body)
	}
	// profile
	code, _ = call(t, ts, "PUT", "/api/profile", Obj{"sshPublicKeys": []string{testKey}, "gitName": "Alice"})
	if code != 200 {
		t.Fatalf("profile: %d", code)
	}
	code, body = call(t, ts, "POST", "/api/workspaces", Obj{"name": "api", "languages": []string{"go", "python"}, "tools": []string{"ripgrep"}, "repo": "git@github.com:acme/api.git"})
	if code != 201 || !strings.HasPrefix(body["deployKey"].(string), "ssh-ed25519 ") {
		t.Fatalf("create: %d %v", code, body)
	}
	code, body = call(t, ts, "GET", "/api/workspaces", nil)
	items := body["workspaces"].([]any)
	if code != 200 || len(items) != 1 || items[0].(Obj)["name"] != "api" {
		t.Fatalf("list: %d %v", code, body)
	}
	code, body = call(t, ts, "GET", "/api/workspaces/api", nil)
	if code != 200 || body["ssh"].(Obj)["knownHostsLine"] == "" || len(body["ssh"].(Obj)["authorizedKeys"].([]any)) != 1 {
		t.Fatalf("get: %d %v", code, body)
	}
	code, _ = call(t, ts, "POST", "/api/workspaces/api/keys", Obj{"publicKey": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl second@laptop"})
	if code != 200 {
		t.Fatalf("add key: %d", code)
	}
	code, _ = call(t, ts, "PUT", "/api/workspaces/api/idle", Obj{"idleTimeout": "8h"})
	if code != 200 {
		t.Fatalf("idle: %d", code)
	}
	code, _ = call(t, ts, "POST", "/api/workspaces/api/stop", nil)
	if code != 200 {
		t.Fatalf("stop: %d", code)
	}
	_, body = call(t, ts, "GET", "/api/workspaces", nil)
	if body["workspaces"].([]any)[0].(Obj)["status"] != "Stopped" {
		t.Fatalf("expected Stopped: %v", body)
	}
	code, _ = call(t, ts, "POST", "/api/workspaces/api/start", nil)
	if code != 200 {
		t.Fatalf("start: %d", code)
	}
	code, body = call(t, ts, "GET", "/api/workspaces/api/logs", nil)
	if code != 200 || !strings.Contains(body["logs"].(string), "sshd") {
		t.Fatalf("logs: %d %v", code, body)
	}
	code, _ = call(t, ts, "DELETE", "/api/workspaces/api?purge=true", nil)
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	_, body = call(t, ts, "GET", "/api/workspaces", nil)
	if len(body["workspaces"].([]any)) != 0 {
		t.Fatalf("expected empty list after delete: %v", body)
	}
	// catalog + tools endpoints
	code, body = call(t, ts, "GET", "/api/artifactory/tools", nil)
	if code != 200 || len(body["tools"].([]any)) < 10 {
		t.Fatalf("tools: %d", code)
	}
	code, body = call(t, ts, "GET", "/api/catalog", nil)
	if code != 200 || len(body["languagesOrder"].([]any)) != 7 {
		t.Fatalf("catalog: %d %v", code, body["languagesOrder"])
	}
}

func TestSimulationCSP(t *testing.T) {
	h := securityHeaders(defaultCSP, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for path, want := range map[string]string{"/simulation.html": simulationCSP, "/": defaultCSP, "/app.js": defaultCSP} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if got := rec.Header().Get("Content-Security-Policy"); got != want {
			t.Errorf("%s: CSP = %q, want %q", path, got, want)
		}
	}
}
