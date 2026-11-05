package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// handleTunnel: GET /api/workspaces/{name}/tunnel — a WebSocket whose binary frames are the
// bytes of a TCP connection to the workspace's sshd. This is how clients without kubectl
// (wsctl, VS Code through `wsctl proxy`) reach a workspace: the portal opens
// `socat - TCP:127.0.0.1:2222` in the pod through the Kubernetes exec API *as the caller*,
// so RBAC on pods/exec still decides. A stopped workspace is started first; progress goes
// to the client as text frames.
func (s *server) handleTunnel(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	st, ns, err := s.locate(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	ws, err := wsUpgrade(w, r, "")
	if err != nil {
		writeJSON(w, 400, Obj{"error": err.Error()})
		return
	}
	defer ws.Close()
	say := func(format string, a ...any) { _ = ws.WriteMessage(wsText, []byte(fmt.Sprintf(format, a...))) }

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	// start if stopped, wait for Ready
	if num(st, "spec", "replicas") == 0 {
		say("workspace %s is stopped — starting it (usually under a minute)", name)
		if err := k.MergePatch(ctx, nsPath("apps", "v1", "statefulsets", ns, "ws-"+name)+"/scale", Obj{"spec": Obj{"replicas": 1}}); err != nil {
			say("error: cannot start: %v", err)
			return
		}
	}
	pod := "ws-" + name + "-0"
	last := ""
	for deadline := time.Now().Add(8 * time.Minute); ; {
		p, err := k.Get(ctx, nsPath("", "v1", "pods", ns, pod))
		if err == nil {
			ready := false
			if conds, ok := getIn(p, "status", "conditions").([]any); ok {
				for _, c := range conds {
					if str(c.(Obj), "type") == "Ready" && str(c.(Obj), "status") == "True" {
						ready = true
					}
				}
			}
			if ready {
				break
			}
			if msg := waitingReason(p); msg != "" && msg != last {
				say("%s", msg)
				last = msg
			}
		}
		if time.Now().After(deadline) {
			say("error: workspace did not become ready in time")
			return
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}
	}

	// mock / Compose demo: plain TCP (or an in-process fake sshd)
	if s.exec == nil {
		var tcp net.Conn
		if s.mockTunnel != "" {
			tcp, err = net.DialTimeout("tcp", s.mockTunnel, 5*time.Second)
			if err != nil {
				say("error: cannot reach %s: %v", s.mockTunnel, err)
				return
			}
		} else {
			tcp = fakeSSHD()
		}
		bridgeTCP(ws, tcp)
		return
	}
	podWS, err := s.exec(ctx, u, ns, pod, "workspace", []string{"socat", "-", fmt.Sprintf("TCP:127.0.0.1:%d", sshPort)})
	if err != nil {
		say("error: exec into %s/%s: %v", ns, pod, err)
		return
	}
	bridge(ws, podWS)
}

func waitingReason(p Obj) string {
	if css, ok := getIn(p, "status", "containerStatuses").([]any); ok {
		for _, c := range css {
			if wtg, ok := getIn(c.(Obj), "state", "waiting").(Obj); ok {
				return str(wtg, "reason") + " " + str(wtg, "message")
			}
		}
	}
	if conds, ok := getIn(p, "status", "conditions").([]any); ok {
		for _, c := range conds {
			if str(c.(Obj), "type") == "PodScheduled" && str(c.(Obj), "status") == "False" {
				return str(c.(Obj), "message")
			}
		}
	}
	return ""
}

// fakeSSHD is what the mock tunnel talks to when no -mock-tunnel address is given: it
// sends an SSH-looking banner and echoes everything, enough to see the plumbing work.
func fakeSSHD() net.Conn {
	a, b := net.Pipe()
	go func() {
		defer b.Close()
		_, _ = b.Write([]byte("SSH-2.0-devspaces-mock (echo server: a real workspace would be OpenSSH)\r\n"))
		buf := make([]byte, 4096)
		for {
			n, err := b.Read(buf)
			if n > 0 {
				if _, werr := b.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					return
				}
				return
			}
		}
	}()
	return a
}
