package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"strings"
)

// Ed25519 keys in OpenSSH's own formats, with the standard library only:
// the public line ("ssh-ed25519 AAAA… comment") and the unencrypted
// "openssh-key-v1" private key, as `ssh-keygen -t ed25519` would write them.

type keypair struct {
	Private string // PEM, OPENSSH PRIVATE KEY
	Public  string // one authorized_keys / known_hosts line
}

func sshString(b []byte) []byte {
	out := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], b)
	return out
}

func generateKeypair(comment string) (keypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return keypair{}, err
	}
	// wire-format public key: string "ssh-ed25519", string pub
	pubWire := append(sshString([]byte("ssh-ed25519")), sshString(pub)...)
	// private section
	var check [4]byte
	if _, err := rand.Read(check[:]); err != nil {
		return keypair{}, err
	}
	var sec bytes.Buffer
	sec.Write(check[:])
	sec.Write(check[:])
	sec.Write(sshString([]byte("ssh-ed25519")))
	sec.Write(sshString(pub))
	sec.Write(sshString(priv)) // 64 bytes: seed || pub
	sec.Write(sshString([]byte(comment)))
	for i := 1; sec.Len()%8 != 0; i++ {
		sec.WriteByte(byte(i))
	}
	var body bytes.Buffer
	body.WriteString("openssh-key-v1\x00")
	body.Write(sshString([]byte("none"))) // cipher
	body.Write(sshString([]byte("none"))) // kdf
	body.Write(sshString(nil))            // kdf options
	var one [4]byte
	binary.BigEndian.PutUint32(one[:], 1)
	body.Write(one[:])
	body.Write(sshString(pubWire))
	body.Write(sshString(sec.Bytes()))
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body.Bytes()})
	return keypair{
		Private: string(pemBlock),
		Public:  fmt.Sprintf("ssh-ed25519 %s %s", base64.StdEncoding.EncodeToString(pubWire), comment),
	}, nil
}

// fingerprint returns the SHA256 fingerprint ssh prints for a public key line.
func fingerprint(pubLine string) string {
	f := strings.Fields(pubLine)
	if len(f) < 2 {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
}

// validPublicKey accepts the key types OpenSSH accepts for authorized_keys and
// rejects anything that is not a single key line (no newlines, no options).
func validPublicKey(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if strings.ContainsAny(line, "\n\r") {
		return "", false
	}
	f := strings.Fields(line)
	if len(f) < 2 || len(f) > 3 {
		return "", false
	}
	switch f[0] {
	case "ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
		"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com":
	default:
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil || len(raw) < 20 {
		return "", false
	}
	// the wire blob must start with the same type name
	if len(raw) < 4 || int(binary.BigEndian.Uint32(raw)) > len(raw)-4 || string(raw[4:4+binary.BigEndian.Uint32(raw)]) != f[0] {
		return "", false
	}
	return strings.Join(f, " "), true
}
