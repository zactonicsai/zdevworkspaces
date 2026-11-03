package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6455 handshake, not for security
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// A minimal RFC 6455 WebSocket — server side (for /api/workspaces/{name}/tunnel) and
// client side (for the Kubernetes exec API, which speaks WebSocket too). The standard
// library has no WebSocket package and the portal stays dependency-free, so this is the
// ~250 lines it takes: handshake, framing, masking, ping/pong/close. No extensions.

const (
	wsContinuation = 0x0
	wsText         = 0x1
	wsBinary       = 0x2
	wsClose        = 0x8
	wsPing         = 0x9
	wsPong         = 0xA
	wsMaxMessage   = 8 << 20
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type wsConn struct {
	conn   net.Conn
	br     *bufio.Reader
	wmu    sync.Mutex
	client bool // client frames are masked
	closed bool
}

func wsAccept(key string) string {
	h := sha1.New() //nolint:gosec
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// wsUpgrade completes the server handshake and hijacks the connection.
func wsUpgrade(w http.ResponseWriter, r *http.Request, subprotocol string) (*wsConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return nil, errors.New("not a websocket upgrade request")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("connection cannot be hijacked")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + wsAccept(key) + "\r\n"
	if subprotocol != "" {
		resp += "Sec-WebSocket-Protocol: " + subprotocol + "\r\n"
	}
	if _, err := conn.Write([]byte(resp + "\r\n")); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return &wsConn{conn: conn, br: rw.Reader}, nil
}

// wsDial opens a client WebSocket to urlStr (ws://, wss://, http://, https:// accepted).
func wsDial(ctx context.Context, urlStr string, hdr http.Header, tlsCfg *tls.Config, subprotocols []string) (*wsConn, string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, "", err
	}
	secure := u.Scheme == "wss" || u.Scheme == "https"
	host := u.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, ifThen(secure, "443", "80"))
	}
	d := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	if secure {
		cfg := tlsCfg
		if cfg == nil {
			cfg = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			cfg = cfg.Clone()
		}
		if cfg.ServerName == "" {
			cfg.ServerName, _, _ = net.SplitHostPort(host)
		}
		conn, err = (&tls.Dialer{NetDialer: d, Config: cfg}).DialContext(ctx, "tcp", host)
	} else {
		conn, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, "", err
	}
	keyb := make([]byte, 16)
	_, _ = rand.Read(keyb)
	key := base64.StdEncoding.EncodeToString(keyb)
	var b strings.Builder
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", u.RequestURI(), u.Host, key)
	if len(subprotocols) > 0 {
		fmt.Fprintf(&b, "Sec-WebSocket-Protocol: %s\r\n", strings.Join(subprotocols, ", "))
	}
	for k, vs := range hdr {
		for _, v := range vs {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	if _, err := conn.Write([]byte(b.String())); err != nil {
		conn.Close()
		return nil, "", err
	}
	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		conn.Close()
		return nil, "", err
	}
	if res.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		conn.Close()
		return nil, "", fmt.Errorf("websocket handshake: HTTP %d %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	if res.Header.Get("Sec-WebSocket-Accept") != wsAccept(key) {
		conn.Close()
		return nil, "", errors.New("websocket handshake: bad accept key")
	}
	_ = conn.SetDeadline(time.Time{})
	return &wsConn{conn: conn, br: br, client: true}, res.Header.Get("Sec-WebSocket-Protocol"), nil
}

// ReadMessage returns the next complete data message (ping/pong handled transparently).
func (c *wsConn) ReadMessage() (int, []byte, error) {
	var msg []byte
	msgOp := -1
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case wsPing:
			_ = c.WriteMessage(wsPong, payload)
			continue
		case wsPong:
			continue
		case wsClose:
			_ = c.WriteMessage(wsClose, payload)
			c.closed = true
			return wsClose, payload, io.EOF
		case wsContinuation:
			if msgOp < 0 {
				return 0, nil, errors.New("websocket: unexpected continuation frame")
			}
		default:
			msgOp = op
		}
		msg = append(msg, payload...)
		if len(msg) > wsMaxMessage {
			return 0, nil, errors.New("websocket: message too large")
		}
		if fin {
			return msgOp, msg, nil
		}
	}
}

func (c *wsConn) readFrame() (fin bool, op int, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(c.br, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	op = int(h[0] & 0x0F)
	masked := h[1]&0x80 != 0
	n := uint64(h[1] & 0x7F)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	if n > wsMaxMessage {
		err = errors.New("websocket: frame too large")
		return
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return
}

// WriteMessage sends one unfragmented frame.
func (c *wsConn) WriteMessage(op int, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|byte(op))
	n := len(payload)
	maskBit := byte(0)
	if c.client {
		maskBit = 0x80
	}
	switch {
	case n < 126:
		hdr = append(hdr, maskBit|byte(n))
	case n < 65536:
		hdr = append(hdr, maskBit|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, maskBit|127)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		hdr = append(hdr, b[:]...)
	}
	if c.client {
		var mask [4]byte
		_, _ = rand.Read(mask[:])
		hdr = append(hdr, mask[:]...)
		masked := make([]byte, n)
		for i := range payload {
			masked[i] = payload[i] ^ mask[i%4]
		}
		payload = masked
	}
	if _, err := c.conn.Write(hdr); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

func (c *wsConn) Close() error {
	if !c.closed {
		c.closed = true
		_ = c.WriteMessage(wsClose, []byte{0x03, 0xE8}) // 1000 normal closure
	}
	return c.conn.Close()
}

// ---------------------------------------------------------------- Kubernetes exec over WebSocket

// execDialer opens `command` in a pod and returns the stream. Protocol v4.channel.k8s.io:
// binary frames whose first byte is the channel (0 stdin, 1 stdout, 2 stderr, 3 error).
type execDialer func(ctx context.Context, u User, ns, pod, container string, command []string) (*wsConn, error)

func (c *apiClient) execDialer() execDialer {
	var tlsCfg *tls.Config
	if tr, ok := c.http.Transport.(*http.Transport); ok {
		tlsCfg = tr.TLSClientConfig
	}
	return func(ctx context.Context, u User, ns, pod, container string, command []string) (*wsConn, error) {
		q := url.Values{"container": {container}, "stdin": {"true"}, "stdout": {"true"}, "stderr": {"true"}, "tty": {"false"}}
		for _, a := range command {
			q.Add("command", a)
		}
		target := c.base + "/api/v1/namespaces/" + ns + "/pods/" + pod + "/exec?" + q.Encode()
		hdr := http.Header{}
		if t := c.bearer(); t != "" {
			hdr.Set("Authorization", "Bearer "+t)
		}
		hdr.Set("Impersonate-User", u.Name) // the developer's RBAC decides (pods/exec)
		for _, g := range u.Groups {
			if g != "system:authenticated" {
				hdr.Add("Impersonate-Group", g)
			}
		}
		ws, _, err := wsDial(ctx, target, hdr, tlsCfg, []string{"v4.channel.k8s.io"})
		return ws, err
	}
}

// bridge copies raw bytes between a client WebSocket (binary frames = TCP payload) and a
// Kubernetes exec stream running `socat - TCP:127.0.0.1:2222` in the pod. stderr lines
// from the pod go to the client as text frames (progress / errors, never SSH data).
func bridge(client, pod *wsConn) {
	done := make(chan struct{}, 2)
	go func() { // client → pod stdin
		defer func() { done <- struct{}{} }()
		for {
			op, data, err := client.ReadMessage()
			if err != nil {
				return
			}
			if op != wsBinary {
				continue
			}
			if err := pod.WriteMessage(wsBinary, append([]byte{0}, data...)); err != nil {
				return
			}
		}
	}()
	go func() { // pod stdout/stderr → client
		defer func() { done <- struct{}{} }()
		for {
			op, data, err := pod.ReadMessage()
			if err != nil {
				return
			}
			if op != wsBinary || len(data) == 0 {
				continue
			}
			switch data[0] {
			case 1:
				if err := client.WriteMessage(wsBinary, data[1:]); err != nil {
					return
				}
			case 2, 3:
				if msg := strings.TrimSpace(string(data[1:])); msg != "" {
					_ = client.WriteMessage(wsText, []byte(msg))
				}
			}
		}
	}()
	<-done
	client.Close()
	pod.Close()
	<-done
}

// bridgeTCP is the mock/Compose variant: client WebSocket ⇄ a plain TCP socket.
func bridgeTCP(client *wsConn, tcp net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			op, data, err := client.ReadMessage()
			if err != nil {
				return
			}
			if op == wsBinary {
				if _, err := tcp.Write(data); err != nil {
					return
				}
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, err := tcp.Read(buf)
			if n > 0 {
				if werr := client.WriteMessage(wsBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	<-done
	client.Close()
	tcp.Close()
	<-done
}
