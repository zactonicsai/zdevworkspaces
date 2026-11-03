package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakePortal implements just enough of api/openapi.yaml to drive the CLI.
type fakePortal struct {
	mu         sync.Mutex
	token      string // required bearer; "" = no auth
	workspaces map[string]*Workspace
	drives     map[string]*Drive
	snaps      map[string][]Snapshot
	shares     map[string][]Share
	links      map[string][]Link
	lastReq    map[string]any
	lastQuery  map[string]string
	gets       map[string]int
	srv        *httptest.Server
}

func newFakePortal(t *testing.T, token string) *fakePortal {
	f := &fakePortal{token: token, workspaces: map[string]*Workspace{}, drives: map[string]*Drive{}, snaps: map[string][]Snapshot{},
		shares: map[string][]Share{}, links: map[string][]Link{}, gets: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePortal) serve(w http.ResponseWriter, r *http.Request) {
	if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
		w.WriteHeader(401)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad token"})
		return
	}
	if r.Method != http.MethodGet && r.Header.Get("X-Requested-With") != "devspaces-portal" {
		w.WriteHeader(403)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing X-Requested-With"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = map[string]string{}
	for k := range r.URL.Query() {
		f.lastQuery[k] = r.URL.Query().Get(k)
	}
	js := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	body := map[string]any{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if len(body) > 0 {
		f.lastReq = body
	}
	p := strings.TrimPrefix(r.URL.Path, "/api/")
	parts := strings.Split(p, "/")
	switch {
	case p == "me":
		js(200, map[string]any{"user": User{Name: "alice@example.com", Short: "alice"}, "auth": "bearer",
			"namespaces": []map[string]any{{"name": "ws-alice", "tier": "standard", "exists": true}}})
	case p == "catalog":
		js(200, map[string]any{"languages": map[string]any{"go": map[string]any{"image": "workspace-go"}},
			"tools":   map[string]any{"cmake": map[string]any{"description": "CMake"}},
			"storage": map[string]any{"choices": []map[string]any{{"name": "fast", "kind": "home", "storageClass": "premium", "default": true}}}})
	case p == "projects":
		js(200, map[string]any{"projects": []Project{{Name: "checkout", Namespace: "ws-proj-checkout", Members: []string{"alice", "bob"}}}})
	case p == "workspaces" && r.Method == http.MethodGet:
		var list []Workspace
		for _, ws := range f.workspaces {
			list = append(list, *ws)
		}
		if list == nil {
			list = []Workspace{}
		}
		out := map[string]any{"workspaces": list}
		if r.URL.Query().Get("shared") == "true" {
			out["sharedWithMe"] = []Workspace{{Name: "api", Namespace: "ws-bob", Owner: "bob@example.com", Status: "Running", Languages: []string{"go"}}}
		}
		js(200, out)
	case p == "workspaces" && r.Method == http.MethodPost:
		langs, _ := body["languages"].([]any)
		var ls []string
		for _, l := range langs {
			ls = append(ls, l.(string))
		}
		name, _ := body["name"].(string)
		if name == "" {
			name = ls[0]
		}
		ns := "ws-alice"
		if n, _ := body["namespace"].(string); n != "" {
			ns = n
		}
		f.workspaces[name] = &Workspace{Name: name, Namespace: ns, Status: "Starting", Languages: ls, Tools: []string{}, CreatedAt: "2026-11-02T10:00:00Z", Node: "node-1"}
		js(201, map[string]any{"name": name, "namespace": ns, "deployKey": "ssh-ed25519 AAAA deploy", "spec": map[string]any{"image": "workspace-go:dev"}})
	case parts[0] == "workspaces" && len(parts) >= 2:
		name := parts[1]
		action := ""
		if len(parts) > 2 {
			action = parts[2]
		}
		ws, ok := f.workspaces[name]
		if !ok && action != "tunnel" {
			js(404, map[string]string{"error": "no workspace " + name})
			return
		}
		switch {
		case action == "" && r.Method == http.MethodGet:
			f.gets[name]++
			if f.gets[name] >= 2 { // second poll: Running
				ws.Status, ws.Ready = "Running", true
			} else {
				ws.Waiting = "pulling image"
			}
			js(200, map[string]any{"workspace": ws, "ssh": SSHInfo{Alias: "ws-" + name, User: "dev", Port: 2222, HostKeyFingerprint: "SHA256:abc",
				KnownHostsLine: "ws-" + name + " ssh-ed25519 AAAAC3host"}, "deployKey": "ssh-ed25519 AAAA deploy"})
		case action == "" && r.Method == http.MethodDelete:
			delete(f.workspaces, name)
			js(200, map[string]any{"deleted": name, "driveKept": r.URL.Query().Get("purge") != "true"})
		case action == "start" || action == "stop":
			if action == "start" {
				ws.Status = "Starting"
			} else {
				ws.Status = "Stopped"
			}
			js(200, map[string]any{"ok": true})
		case action == "idle":
			ws.IdleTimeout = body["idleTimeout"].(string)
			js(200, map[string]any{"ok": true})
		case action == "keys":
			js(200, map[string]any{"ok": true})
		case action == "logs":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "[workspace] sshd listening\n[workspace] ready\n")
		case action == "network":
			js(200, Network{DNS: "ws-" + name + ".ws-alice.svc.cluster.local", PodIP: "10.0.0.5", Namespace: "ws-alice", SameNamespace: []string{"other"}})
		case action == "shares" && r.Method == http.MethodGet:
			js(200, map[string]any{"shares": f.shares[name]})
		case action == "shares" && r.Method == http.MethodPost:
			sh := Share{User: body["user"].(string), Kind: "workspace", Resource: name}
			f.shares[name] = append(f.shares[name], sh)
			js(201, sh)
		case action == "shares" && r.Method == http.MethodDelete:
			f.shares[name] = nil
			js(200, map[string]any{"ok": true})
		case action == "links" && r.Method == http.MethodPost:
			l := Link{Workspace: name, Peer: body["peer"].(string), PeerDNS: "ws-api.ws-bob.svc.cluster.local", Status: "pending-peer"}
			f.links[name] = append(f.links[name], l)
			js(201, l)
		case action == "links" && r.Method == http.MethodGet:
			js(200, map[string]any{"links": f.links[name]})
		case action == "links" && r.Method == http.MethodDelete:
			f.links[name] = nil
			js(200, map[string]any{"ok": true})
		case action == "tunnel":
			f.mu.Unlock()
			wsEcho(w, r)
			f.mu.Lock()
		default:
			js(404, map[string]string{"error": "no such endpoint"})
		}
	case p == "drives" && r.Method == http.MethodGet:
		var list []Drive
		for _, d := range f.drives {
			list = append(list, *d)
		}
		js(200, map[string]any{"drives": list})
	case p == "drives" && r.Method == http.MethodPost:
		d := &Drive{Name: "shared-" + body["name"].(string), Namespace: "ws-alice", Kind: "shared", Size: body["size"].(string), AccessModes: []string{"ReadWriteMany"}}
		f.drives[d.Name] = d
		js(201, d)
	case parts[0] == "drives" && len(parts) >= 2:
		name := parts[1]
		action := ""
		if len(parts) > 2 {
			action = parts[2]
		}
		switch {
		case action == "" && r.Method == http.MethodGet:
			js(200, map[string]any{"drive": f.drives[name], "snapshots": f.snaps[name]})
		case action == "" && r.Method == http.MethodDelete:
			delete(f.drives, name)
			js(200, map[string]any{"ok": true})
		case action == "resize":
			f.drives[name].Size = body["size"].(string)
			js(200, map[string]any{"ok": true})
		case action == "snapshots" && r.Method == http.MethodPost:
			n, _ := body["name"].(string)
			if n == "" {
				n = name + "-20261102"
			}
			s := Snapshot{Name: n, Drive: name, ReadyToUse: false, CreatedAt: "2026-11-02T10:00:00Z"}
			f.snaps[name] = append(f.snaps[name], s)
			js(201, s)
		case action == "snapshots":
			js(200, map[string]any{"snapshots": f.snaps[name]})
		case action == "shares" && r.Method == http.MethodPost:
			sh := Share{User: body["user"].(string), Kind: "drive", Resource: name}
			if ro, _ := body["readOnly"].(bool); ro {
				sh.ReadOnly = true
			}
			f.shares["drive:"+name] = append(f.shares["drive:"+name], sh)
			js(201, sh)
		case action == "shares" && r.Method == http.MethodGet:
			js(200, map[string]any{"shares": f.shares["drive:"+name]})
		default:
			js(404, map[string]string{"error": "no such endpoint"})
		}
	default:
		js(404, map[string]string{"error": "no such endpoint " + p})
	}
}

// wsEcho is a tiny server-side WebSocket: it echoes binary frames back with
// "echo:" prepended, sends one text progress frame first, and closes on close.
func wsEcho(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		w.WriteHeader(400)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		w.WriteHeader(500)
		return
	}
	accept := acceptKey(r.Header.Get("Sec-WebSocket-Key"))
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n")
	_ = rw.Flush()
	writeServerFrame(conn, opText, []byte("workspace is Running"))
	writeServerFrame(conn, opPing, []byte("hi"))
	br := rw.Reader
	for {
		fin, op, data, err := readServerFrame(br)
		if err != nil {
			return
		}
		_ = fin
		switch op {
		case opBinary:
			writeServerFrame(conn, opBinary, append([]byte("echo:"), data...))
		case opClose:
			writeServerFrame(conn, opClose, data)
			return
		case opPong, opPing:
		}
	}
}

func writeServerFrame(c net.Conn, op byte, payload []byte) {
	hdr := []byte{0x80 | op}
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n < 1<<16:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	_, _ = c.Write(append(hdr, payload...))
}

func readServerFrame(br *bufio.Reader) (bool, byte, []byte, error) {
	h := make([]byte, 2)
	if _, err := io.ReadFull(br, h); err != nil {
		return false, 0, nil, err
	}
	fin := h[0]&0x80 != 0
	op := h[0] & 0x0F
	masked := h[1]&0x80 != 0
	n := uint64(h[1] & 0x7F)
	if n == 126 {
		b := make([]byte, 2)
		if _, err := io.ReadFull(br, b); err != nil {
			return false, 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(b))
	} else if n == 127 {
		b := make([]byte, 8)
		if _, err := io.ReadFull(br, b); err != nil {
			return false, 0, nil, err
		}
		n = binary.BigEndian.Uint64(b)
	}
	if !masked {
		return false, 0, nil, io.ErrUnexpectedEOF // clients MUST mask
	}
	mask := make([]byte, 4)
	if _, err := io.ReadFull(br, mask); err != nil {
		return false, 0, nil, err
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(br, data); err != nil {
		return false, 0, nil, err
	}
	for i := range data {
		data[i] ^= mask[i%4]
	}
	return fin, op, data, nil
}
