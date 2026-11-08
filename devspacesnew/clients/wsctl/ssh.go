package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The ~/.ssh/config block wsctl manages for one workspace sits between two
// marker lines, so re-running replaces it and `delete` removes it cleanly.
func blockMarkers(alias string) (string, string) {
	return "# >>> wsctl " + alias, "# <<< wsctl " + alias
}

func sshDir() string {
	if d := os.Getenv("WSCTL_SSH_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh")
}

// sshConfigBlock renders the entry. The ProxyCommand runs wsctl itself, which
// bridges stdin/stdout to the portal's WebSocket tunnel: ssh never needs to
// reach the pod's IP, and no kubectl is involved.
func sshConfigBlock(alias, ref, ns, exe string, insecure bool, portal string) string {
	start, end := blockMarkers(alias)
	var sb strings.Builder
	sb.WriteString(start + "\n")
	fmt.Fprintf(&sb, "Host %s\n", alias)
	fmt.Fprintf(&sb, "  HostName %s\n", alias)
	sb.WriteString("  User dev\n")
	sb.WriteString("  Port 2222\n")
	proxy := fmt.Sprintf("%s proxy %s", shellQuote(exe), shellQuote(ref))
	if ns != "" {
		proxy += " --ns " + shellQuote(ns)
	}
	if portal != "" {
		proxy += " --portal " + shellQuote(portal)
	}
	if insecure {
		proxy += " --insecure"
	}
	fmt.Fprintf(&sb, "  ProxyCommand %s\n", proxy)
	sb.WriteString("  UserKnownHostsFile " + filepath.Join(sshDir(), "known_hosts_wsctl") + "\n")
	sb.WriteString("  StrictHostKeyChecking yes\n")
	sb.WriteString("  ServerAliveInterval 30\n")
	sb.WriteString("  ForwardAgent no\n")
	sb.WriteString(end + "\n")
	return sb.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '/' || r == '.' || r == ':' || r == '@' || r == '=' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// upsertSSHBlock inserts or replaces the managed block in the config file.
func upsertSSHBlock(path, alias, block string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := removeBlock(string(existing), alias)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" && !strings.HasSuffix(content, "\n\n") {
		content += "\n"
	}
	content += block
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func removeSSHBlock(path, alias string) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.WriteFile(path, []byte(removeBlock(string(existing), alias)), 0o600)
}

func removeBlock(content, alias string) string {
	start, end := blockMarkers(alias)
	var out []string
	skip := false
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.TrimSpace(line) == start:
			skip = true
			continue
		case strings.TrimSpace(line) == end:
			skip = false
			continue
		}
		if !skip {
			out = append(out, line)
		}
	}
	s := strings.Join(out, "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

// writeKnownHost replaces the host-key line for alias in known_hosts_wsctl.
// An empty line removes the entry.
func writeKnownHost(path, alias, line string) error {
	existing, _ := os.ReadFile(path)
	var out []string
	for _, l := range strings.Split(string(existing), "\n") {
		f := strings.Fields(l)
		if len(f) > 0 && f[0] == alias {
			continue
		}
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	if strings.TrimSpace(line) != "" {
		out = append(out, strings.TrimSpace(line))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data := strings.Join(out, "\n")
	if data != "" {
		data += "\n"
	}
	return os.WriteFile(path, []byte(data), 0o600)
}
