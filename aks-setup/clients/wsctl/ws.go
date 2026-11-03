package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6455 mandates SHA-1 for the accept key
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

// wsConn is a deliberately small RFC 6455 client: enough for a byte pipe.
// Client→server frames are masked (mandatory), server→client are not.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
	wmu  sync.Mutex
}

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// wsDial opens a WebSocket to urlStr (http(s):// or ws(s)://), sending the
// given headers (Authorization etc.) with the upgrade request.
func wsDial(ctx context.Context, urlStr string, hdr http.Header, insecure bool) (*wsConn, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	scheme := u.Scheme
	switch scheme {
	case "http", "ws":
		scheme = "ws"
	case "https", "wss":
		scheme = "wss"
	default:
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	host := u.Host
	if u.Port() == "" {
		if scheme == "wss" {
			host = net.JoinHostPort(u.Hostname(), "443")
		} else {
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}
	d := &net.Dialer{Timeout: 15 * time.Second}
	var c net.Conn
	if scheme == "wss" {
		tc := &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}
		if insecure {
			tc.InsecureSkipVerify = true //nolint:gosec // explicit --insecure
		}
		c, err = (&tls.Dialer{NetDialer: d, Config: tc}).DialContext(ctx, "tcp", host)
	} else {
		c, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, err
	}
	keyB := make([]byte, 16)
	if _, err := rand.Read(keyB); err != nil {
		c.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyB)
	path := u.RequestURI()
	var sb strings.Builder
	fmt.Fprintf(&sb, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", path, u.Host, key)
	for k, vs := range hdr {
		for _, v := range vs {
			fmt.Fprintf(&sb, "%s: %s\r\n", k, v)
		}
	}
	sb.WriteString("\r\n")
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}
	if _, err := io.WriteString(c, sb.String()); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(c, 64<<10)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("websocket handshake: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		c.Close()
		return nil, &APIError{Status: resp.StatusCode, Message: errMessage(body, resp.Status)}
	}
	want := acceptKey(key)
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != want {
		c.Close()
		return nil, errors.New("websocket handshake: bad Sec-WebSocket-Accept (not a WebSocket endpoint?)")
	}
	_ = c.SetDeadline(time.Time{})
	return &wsConn{conn: c, br: br}, nil
}

func acceptKey(key string) string {
	h := sha1.New() //nolint:gosec
	h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// writeFrame sends one masked frame.
func (w *wsConn) writeFrame(op byte, payload []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|op)
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n < 1<<16:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	hdr = append(hdr, mask...)
	buf := make([]byte, n)
	for i := range payload {
		buf[i] = payload[i] ^ mask[i%4]
	}
	if _, err := w.conn.Write(append(hdr, buf...)); err != nil {
		return err
	}
	return nil
}

func (w *wsConn) WriteBinary(b []byte) error { return w.writeFrame(opBinary, b) }
func (w *wsConn) WriteText(s string) error   { return w.writeFrame(opText, []byte(s)) }

// Close sends a close frame and shuts the socket.
func (w *wsConn) Close() error {
	_ = w.writeFrame(opClose, []byte{0x03, 0xE8}) // 1000 normal closure
	return w.conn.Close()
}

// ReadMessage returns the next complete message (fragments reassembled).
// Pings are answered transparently. io.EOF on a clean close.
func (w *wsConn) ReadMessage() (op byte, payload []byte, err error) {
	var msgOp byte
	var msg []byte
	for {
		fin, fop, data, err := w.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch fop {
		case opPing:
			_ = w.writeFrame(opPong, data)
			continue
		case opPong:
			continue
		case opClose:
			_ = w.writeFrame(opClose, data)
			return opClose, data, io.EOF
		case opContinuation:
			msg = append(msg, data...)
		default:
			msgOp = fop
			msg = append(msg[:0], data...)
		}
		if fin {
			return msgOp, msg, nil
		}
	}
}

func (w *wsConn) readFrame() (fin bool, op byte, data []byte, err error) {
	h := make([]byte, 2)
	if _, err = io.ReadFull(w.br, h); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	op = h[0] & 0x0F
	masked := h[1]&0x80 != 0
	n := uint64(h[1] & 0x7F)
	switch n {
	case 126:
		b := make([]byte, 2)
		if _, err = io.ReadFull(w.br, b); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(b))
	case 127:
		b := make([]byte, 8)
		if _, err = io.ReadFull(w.br, b); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(b)
	}
	if n > 64<<20 {
		err = errors.New("websocket frame too large")
		return
	}
	var mask []byte
	if masked {
		mask = make([]byte, 4)
		if _, err = io.ReadFull(w.br, mask); err != nil {
			return
		}
	}
	data = make([]byte, n)
	if _, err = io.ReadFull(w.br, data); err != nil {
		return
	}
	if masked {
		for i := range data {
			data[i] ^= mask[i%4]
		}
	}
	return
}

// pipe copies stdin → websocket (binary) and websocket → stdout until either
// side ends. Text frames are progress lines and go to progress.
func (w *wsConn) pipe(stdin io.Reader, stdout io.Writer, progress io.Writer) error {
	errc := make(chan error, 2)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := stdin.Read(buf)
			if n > 0 {
				if werr := w.WriteBinary(buf[:n]); werr != nil {
					errc <- werr
					return
				}
			}
			if err != nil {
				if err == io.EOF {
					errc <- nil
				} else {
					errc <- err
				}
				return
			}
		}
	}()
	go func() {
		for {
			op, data, err := w.ReadMessage()
			if err != nil {
				if err == io.EOF {
					errc <- nil
				} else {
					errc <- err
				}
				return
			}
			if op == opText {
				fmt.Fprintln(progress, strings.TrimRight(string(data), "\n"))
				continue
			}
			if _, err := stdout.Write(data); err != nil {
				errc <- err
				return
			}
		}
	}()
	err := <-errc
	_ = w.conn.Close()
	return err
}
