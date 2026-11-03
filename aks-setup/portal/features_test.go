package main

import (
	"bufio"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mustCreate(t *testing.T, ts *httptest.Server, body Obj) {
	t.Helper()
	call(t, ts, "PUT", "/api/profile", Obj{"sshPublicKeys": []string{testKey}})
	code, out := call(t, ts, "POST", "/api/workspaces", body)
	if code != 201 {
		t.Fatalf("create %v: %d %v", body["name"], code, out)
	}
}

func TestDrivesLifecycle(t *testing.T) {
	s, ts := testServer(t)
	s.priv = s.kube(User{})
	s.sharing = true
	mustCreate(t, ts, Obj{"name": "api", "languages": []string{"go"}, "storage": "fast"})
	code, body := call(t, ts, "GET", "/api/drives", nil)
	drives := body["drives"].([]any)
	if code != 200 || len(drives) != 1 || drives[0].(Obj)["name"] != "ws-api-home" || drives[0].(Obj)["attachedTo"] != "api" || drives[0].(Obj)["protected"] != true {
		t.Fatalf("drives: %d %v", code, body)
	}
	// delete keeps the drive → detached
	code, body = call(t, ts, "DELETE", "/api/workspaces/api", nil)
	if code != 200 || body["driveKept"] != true {
		t.Fatalf("delete: %d %v", code, body)
	}
	_, body = call(t, ts, "GET", "/api/drives", nil)
	d := body["drives"].([]any)[0].(Obj)
	if d["attachedTo"] != nil && d["attachedTo"] != "" {
		t.Fatalf("expected detached drive: %v", d)
	}
	// deleting a protected drive works through the API (it sets allow-delete first) — but not while attached
	mustCreate(t, ts, Obj{"name": "engine", "languages": []string{"rust"}, "fromDrive": "ws-api-home"})
	code, body = call(t, ts, "GET", "/api/workspaces/engine", nil)
	if code != 200 || body["workspace"].(Obj)["drive"] != "ws-api-home" {
		t.Fatalf("engine should sit on ws-api-home: %v", body["workspace"])
	}
	code, body = call(t, ts, "DELETE", "/api/drives/ws-api-home", nil)
	if code != 409 {
		t.Fatalf("expected 409 for attached drive, got %d %v", code, body)
	}
	// stealing an attached drive is refused
	code, body = call(t, ts, "POST", "/api/workspaces", Obj{"name": "thief", "languages": []string{"go"}, "fromDrive": "ws-api-home"})
	if code != 400 || !strings.Contains(body["error"].(string), "attached to workspace engine") {
		t.Fatalf("expected refusal: %d %v", code, body)
	}
	// snapshots
	code, body = call(t, ts, "POST", "/api/drives/ws-api-home/snapshots", Obj{"name": "before-upgrade"})
	if code != 201 || body["drive"] != "ws-api-home" {
		t.Fatalf("snapshot: %d %v", code, body)
	}
	deadline := time.Now().Add(6 * time.Second)
	for {
		_, body = call(t, ts, "GET", "/api/drives/ws-api-home/snapshots", nil)
		sn := body["snapshots"].([]any)[0].(Obj)
		if sn["readyToUse"] == true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("snapshot never ready: %v", body)
		}
		time.Sleep(300 * time.Millisecond)
	}
	mustCreate(t, ts, Obj{"name": "restored", "languages": []string{"go"}, "fromSnapshot": "before-upgrade"})
	_, body = call(t, ts, "GET", "/api/drives", nil)
	if len(body["drives"].([]any)) != 2 {
		t.Fatalf("expected 2 drives: %v", body)
	}
	// shared drive + mount + share with bob
	code, body = call(t, ts, "POST", "/api/drives", Obj{"name": "team-data", "size": "10Gi"})
	if code != 201 || body["name"] != "shared-team-data" || body["kind"] != "shared" {
		t.Fatalf("shared drive: %d %v", code, body)
	}
	mustCreate(t, ts, Obj{"name": "web", "languages": []string{"node"}, "mounts": []string{"shared:team-data"}})
	code, body = call(t, ts, "GET", "/api/workspaces/web", nil)
	if m := body["workspace"].(Obj)["mounts"].([]any); code != 200 || len(m) != 1 || m[0] != "shared:team-data" {
		t.Fatalf("mounts: %d %v", code, body["workspace"])
	}
	call(t, ts, "GET", "/api/drives/shared-team-data", nil) // materialises the PV in the mock
	// bob must be onboarded
	bob := User{Name: "bob@corp.example", Short: "bob"}
	_ = s.priv.Apply(t.Context(), "/api/v1/namespaces/ws-bob", Obj{"apiVersion": "v1", "kind": "Namespace", "metadata": Obj{"name": "ws-bob", "labels": Obj{labelTenant: "bob"}}})
	code, body = call(t, ts, "POST", "/api/drives/shared-team-data/shares", Obj{"user": bob.Name, "readOnly": true})
	if code != 201 {
		t.Fatalf("share drive: %d %v", code, body)
	}
	pvc, err := s.priv.Get(t.Context(), nsPath("", "v1", "persistentvolumeclaims", "ws-bob", "shared-alice-team-data"))
	if err != nil || str(pvc, "spec", "volumeName") == "" {
		t.Fatalf("guest PVC missing: %v %v", err, pvc)
	}
	pv, err := s.priv.Get(t.Context(), clusterPath("", "v1", "persistentvolumes", str(pvc, "spec", "volumeName")))
	if err != nil || getIn(pv, "spec", "csi", "readOnly") != true || str(pv, "spec", "claimRef", "namespace") != "ws-bob" {
		t.Fatalf("guest PV wrong: %v %v", err, pv)
	}
	_, body = call(t, ts, "GET", "/api/drives/shared-team-data/shares", nil)
	if sh := body["shares"].([]any); len(sh) != 1 || sh[0].(Obj)["user"] != bob.Name {
		t.Fatalf("shares: %v", body)
	}
	code, _ = call(t, ts, "DELETE", "/api/drives/shared-team-data/shares/bob%40corp.example", nil)
	if code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	if _, err := s.priv.Get(t.Context(), nsPath("", "v1", "persistentvolumeclaims", "ws-bob", "shared-alice-team-data")); err == nil {
		t.Fatal("guest PVC should be gone")
	}
}

func TestShareWorkspaceAndLinks(t *testing.T) {
	s, ts := testServer(t)
	s.priv = s.kube(User{})
	s.sharing = true
	mustCreate(t, ts, Obj{"name": "api", "languages": []string{"go"}})
	// bob has a profile key in his namespace
	ctx := t.Context()
	_ = s.priv.Apply(ctx, "/api/v1/namespaces/ws-bob", Obj{"apiVersion": "v1", "kind": "Namespace", "metadata": Obj{"name": "ws-bob", "labels": Obj{labelTenant: "bob"}}})
	bobKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl bob@laptop"
	_ = s.priv.Apply(ctx, nsPath("", "v1", "secrets", "ws-bob", "workspace-profile"), Obj{"apiVersion": "v1", "kind": "Secret",
		"metadata": Obj{"name": "workspace-profile", "namespace": "ws-bob"}, "stringData": Obj{"ssh_public_keys": bobKey + "\n"}})
	code, body := call(t, ts, "POST", "/api/workspaces/api/shares", Obj{"user": "bob@corp.example"})
	if code != 201 {
		t.Fatalf("share: %d %v", code, body)
	}
	sec, _ := s.priv.Get(ctx, nsPath("", "v1", "secrets", "ws-alice", "ws-api-ssh"))
	ak := decodeB64(str(sec, "data", "authorized_keys"))
	if !strings.Contains(ak, "guest:bob@corp.example") || !strings.Contains(ak, "alice@laptop") {
		t.Fatalf("authorized_keys: %q", ak)
	}
	if _, err := s.priv.Get(ctx, nsPath("rbac.authorization.k8s.io", "v1", "rolebindings", "ws-alice", "share-api-bob")); err != nil {
		t.Fatal("rolebinding missing")
	}
	_, body = call(t, ts, "GET", "/api/workspaces/api/shares", nil)
	if sh := body["shares"].([]any); len(sh) != 1 || sh[0].(Obj)["user"] != "bob@corp.example" {
		t.Fatalf("shares: %v", body)
	}
	// bob sees it under sharedWithMe and can open it with ?owner=
	s.ids.devUser = "bob@corp.example"
	code, body = call(t, ts, "GET", "/api/workspaces?shared=true", nil)
	if sw := body["sharedWithMe"].([]any); code != 200 || len(sw) != 1 || sw[0].(Obj)["owner"] != "alice" {
		t.Fatalf("sharedWithMe: %d %v", code, body)
	}
	code, body = call(t, ts, "GET", "/api/workspaces/api?owner=alice@corp.example", nil)
	if code != 200 || body["workspace"].(Obj)["owner"] != "alice" {
		t.Fatalf("guest get: %d %v", code, body)
	}
	code, body = call(t, ts, "DELETE", "/api/workspaces/api?owner=alice@corp.example", nil)
	if code != 400 {
		t.Fatalf("guest must not delete: %d %v", code, body)
	}
	// links: bob links his workspace to alice's; pending until alice links back
	mustCreate(t, ts, Obj{"name": "web", "languages": []string{"node"}})
	code, body = call(t, ts, "POST", "/api/workspaces/web/links", Obj{"peer": "alice@corp.example/api", "ports": []int{8080}})
	if code != 201 || body["status"] != "pending-peer" || body["peerDNS"] != "ws-api.ws-alice.svc.cluster.local" {
		t.Fatalf("link: %d %v", code, body)
	}
	np, err := s.priv.Get(ctx, nsPath("networking.k8s.io", "v1", "networkpolicies", "ws-bob", "peer-web-alice-api"))
	if err != nil || str(np, "spec", "ingress") == "" && len(getIn(np, "spec", "ingress").([]any)) != 1 {
		t.Fatalf("policy: %v %v", err, np)
	}
	s.ids.devUser = "alice@corp.example"
	code, body = call(t, ts, "POST", "/api/workspaces/api/links", Obj{"peer": "bob@corp.example/web"})
	if code != 201 || body["status"] != "linked" {
		t.Fatalf("link back: %d %v", code, body)
	}
	_, body = call(t, ts, "GET", "/api/workspaces/api/network", nil)
	if body["dns"] != "ws-api.ws-alice.svc.cluster.local" || len(body["links"].([]any)) != 1 || body["mesh"] != "none" {
		t.Fatalf("network: %v", body)
	}
	code, _ = call(t, ts, "DELETE", "/api/workspaces/api/links/bob%40corp.example%2Fweb", nil)
	if code != 200 {
		t.Fatalf("unlink: %d", code)
	}
	// revoke bob
	code, _ = call(t, ts, "DELETE", "/api/workspaces/api/shares/bob@corp.example", nil)
	if code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	sec, _ = s.priv.Get(ctx, nsPath("", "v1", "secrets", "ws-alice", "ws-api-ssh"))
	if strings.Contains(decodeB64(str(sec, "data", "authorized_keys")), "guest:") {
		t.Fatal("guest key still present")
	}
}

func TestTunnelMock(t *testing.T) {
	_, ts := testServer(t)
	mustCreate(t, ts, Obj{"name": "api", "languages": []string{"go"}})
	// wait until Running (the mock matures pods after ~4s)
	for i := 0; i < 40; i++ {
		_, body := call(t, ts, "GET", "/api/workspaces/api", nil)
		if body["workspace"].(Obj)["status"] == "Running" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	ws, _, err := wsDial(t.Context(), ts.URL+"/api/workspaces/api/tunnel", http.Header{"X-Requested-With": {"devspaces-portal"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	op, banner, err := ws.ReadMessage()
	if err != nil || op != wsBinary || !strings.HasPrefix(string(banner), "SSH-2.0-") {
		t.Fatalf("banner: %d %q %v", op, banner, err)
	}
	payload := []byte("SSH-2.0-OpenSSH_9.9\r\n")
	if err := ws.WriteMessage(wsBinary, payload); err != nil {
		t.Fatal(err)
	}
	op, echo, err := ws.ReadMessage()
	if err != nil || op != wsBinary || string(echo) != string(payload) {
		t.Fatalf("echo: %d %q %v", op, echo, err)
	}
	// large frame (>65535 → 64-bit length) survives
	big := make([]byte, 70000)
	for i := range big {
		big[i] = byte(i)
	}
	if err := ws.WriteMessage(wsBinary, big); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for len(got) < len(big) {
		_, chunk, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, chunk...)
	}
	if string(got) != string(big) {
		t.Fatal("large payload corrupted")
	}
	// non-upgrade request is a clean 400
	req, _ := http.NewRequest("GET", ts.URL+"/api/workspaces/api/tunnel", nil)
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 400 {
		t.Fatalf("expected 400 without upgrade, got %d", res.StatusCode)
	}
}

func TestWebSocketServerFraming(t *testing.T) {
	// a raw client that masks frames (as browsers / wsctl do) against wsUpgrade + echo
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := wsUpgrade(w, r, "")
		if err != nil {
			return
		}
		defer c.Close()
		for {
			op, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			_ = c.WriteMessage(op, data)
		}
	}))
	defer srv.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, nil)
	if err != nil || res.StatusCode != 101 || res.Header.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("handshake: %v %v", err, res)
	}
	c := &wsConn{conn: conn, br: br, client: true}
	for _, n := range []int{1, 125, 126, 65535, 65536} {
		msg := strings.Repeat("x", n)
		if err := c.WriteMessage(wsText, []byte(msg)); err != nil {
			t.Fatal(err)
		}
		op, data, err := c.ReadMessage()
		if err != nil || op != wsText || string(data) != msg {
			t.Fatalf("n=%d: %v op=%d len=%d", n, err, op, len(data))
		}
	}
	_ = c.WriteMessage(wsPing, []byte("hi"))
	_ = c.WriteMessage(wsText, []byte("after-ping"))
	if _, data, err := c.ReadMessage(); err != nil || string(data) != "after-ping" {
		t.Fatalf("ping handling: %v %q", err, data)
	}
}

// ---- OIDC bearer tokens

func signJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims Obj) string {
	t.Helper()
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	head := enc(Obj{"alg": "RS256", "typ": "JWT", "kid": kid})
	body := enc(claims)
	sum := sha256.Sum256([]byte(head + "." + body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return head + "." + body + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestOIDCBearer(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	var issuer string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2.0/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(Obj{"issuer": issuer, "jwks_uri": issuer + "/discovery/keys"})
		case "/v2.0/discovery/keys":
			n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
			e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
			_ = json.NewEncoder(w).Encode(Obj{"keys": []any{Obj{"kty": "RSA", "use": "sig", "kid": "k1", "n": n, "e": e}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer idp.Close()
	issuer = idp.URL + "/v2.0"
	v := newOIDCVerifier(issuer, "api://devspaces", "preferred_username", "groups")
	now := time.Now().Unix()
	good := Obj{"iss": issuer, "aud": "api://devspaces", "exp": now + 600, "nbf": now - 10, "preferred_username": "alice@corp.example", "groups": []string{"devs"}}
	u, err := v.verify(t.Context(), signJWT(t, key, "k1", good))
	if err != nil || u.Name != "alice@corp.example" || u.Short != "alice" || u.Auth != "bearer" || len(u.Groups) != 1 {
		t.Fatalf("good token rejected: %v %+v", err, u)
	}
	bad := map[string]Obj{
		"expired":      {"iss": issuer, "aud": "api://devspaces", "exp": now - 600, "preferred_username": "a@b"},
		"wrong aud":    {"iss": issuer, "aud": "api://other", "exp": now + 600, "preferred_username": "a@b"},
		"wrong issuer": {"iss": "https://evil.example/v2.0", "aud": "api://devspaces", "exp": now + 600, "preferred_username": "a@b"},
	}
	for name, c := range bad {
		if _, err := v.verify(t.Context(), signJWT(t, key, "k1", c)); err == nil {
			t.Errorf("%s token accepted", name)
		}
	}
	if _, err := v.verify(t.Context(), signJWT(t, other, "k1", good)); err == nil {
		t.Error("token signed by another key accepted")
	}
	if _, err := v.verify(t.Context(), signJWT(t, key, "k2", good)); err == nil {
		t.Error("unknown kid accepted")
	}
	// end to end through the middleware: bearer works without the CSRF header; junk is 401
	u0 := User{Name: "alice@corp.example", Short: "alice"}
	mk := newMockKube(u0)
	s := &server{version: "test", cats: &catalogCache{}, ids: &identityResolver{oidc: v}, kube: func(User) Kube { return mk }}
	s.canLab = func(ctx context.Context, k Kube, u User) bool { return true }
	ts := httptest.NewServer(s.authenticate(s.routes()))
	defer ts.Close()
	req, _ := http.NewRequest("PUT", ts.URL+"/api/profile", strings.NewReader(`{"gitName":"Alice"}`))
	req.Header.Set("Authorization", "Bearer "+signJWT(t, key, "k1", good))
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 200 {
		t.Fatalf("bearer PUT without CSRF header should pass: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("GET", ts.URL+"/api/me", nil)
	req.Header.Set("Authorization", "Bearer nope")
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 401 {
		t.Fatalf("junk bearer: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("GET", ts.URL+"/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+signJWT(t, key, "k1", good))
	res, _ = http.DefaultClient.Do(req)
	var me Obj
	_ = json.NewDecoder(res.Body).Decode(&me)
	if me["auth"] != "bearer" {
		t.Fatalf("me.auth: %v", me)
	}
}

func TestTunnelThroughLoggingWrapper(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(logRequests(securityHeaders(defaultCSP, s.authenticate(s.routes()))))
	defer ts.Close()
	mustCreate(t, ts, Obj{"name": "api", "languages": []string{"go"}})
	for i := 0; i < 40; i++ {
		_, body := call(t, ts, "GET", "/api/workspaces/api", nil)
		if body["workspace"].(Obj)["status"] == "Running" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	ws, _, err := wsDial(t.Context(), ts.URL+"/api/workspaces/api/tunnel", http.Header{"X-Requested-With": {"devspaces-portal"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, banner, err := ws.ReadMessage(); err != nil || !strings.HasPrefix(string(banner), "SSH-2.0-") {
		t.Fatalf("banner through wrapper: %q %v", banner, err)
	}
}
