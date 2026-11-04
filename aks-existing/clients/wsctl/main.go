// wsctl — the Go command-line client for Developer Workspaces.
//
// It talks only to the portal's HTTP API (see api/openapi.yaml): no kubectl,
// no kubeconfig. SSH goes through the portal's WebSocket tunnel, so
// `wsctl ssh NAME` and VS Code Remote-SSH work from any laptop that can reach
// the portal URL.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
)

var version = "dev"

type app struct {
	stdout, stderr io.Writer
	stdin          io.Reader
	cfgPath        string
	cfg            *Config
	portal         string // --portal override
	token          string // WSCTL_TOKEN / --token override
	ns             string // --ns
	out            string // table | json
	insecure       bool
	exe            string // path of this binary (for ProxyCommand)
	client         *Client
	ctx            context.Context
}

type command struct {
	name, usage, help string
	run               func(a *app, args []string) error
}

var commands []command

func register(c command) { commands = append(commands, c) }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	exe, _ := os.Executable()
	a := &app{stdout: os.Stdout, stderr: os.Stderr, stdin: os.Stdin, exe: exe, ctx: ctx}
	code := a.main(os.Args[1:])
	stop()
	os.Exit(code)
}

// main is the testable entry point.
func (a *app) main(args []string) int {
	if a.ctx == nil {
		a.ctx = context.Background()
	}
	if a.exe == "" {
		a.exe = "wsctl"
	}
	a.cfgPath = defaultConfigPath()
	a.out = "table"
	a.portal = os.Getenv("WSCTL_PORTAL")
	a.token = os.Getenv("WSCTL_TOKEN")
	rest, err := a.globals(args)
	if err != nil {
		fmt.Fprintln(a.stderr, "error:", err)
		return 2
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		if len(rest) > 1 {
			return a.main([]string{rest[1], "--help"})
		}
		a.usage()
		return 0
	}
	a.cfg, err = loadConfig(a.cfgPath)
	if err != nil {
		fmt.Fprintln(a.stderr, "error:", err)
		return 2
	}
	name := rest[0]
	for _, c := range commands {
		if c.name == name {
			if err := c.run(a, rest[1:]); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return 0
				}
				fmt.Fprintln(a.stderr, "error:", err)
				var ae *APIError
				if errors.As(err, &ae) && ae.Status == 401 {
					return 4
				}
				return 1
			}
			return 0
		}
	}
	fmt.Fprintf(a.stderr, "wsctl: unknown command %q (try: wsctl help)\n", name)
	return 2
}

// globals pulls the global flags out of args wherever they appear.
func (a *app) globals(args []string) ([]string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, val, hasVal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		isFlag := strings.HasPrefix(arg, "-") && arg != "-"
		next := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		if !isFlag {
			rest = append(rest, arg)
			continue
		}
		switch name {
		case "portal":
			v, err := next()
			if err != nil {
				return nil, err
			}
			a.portal = v
		case "ns", "namespace", "n":
			v, err := next()
			if err != nil {
				return nil, err
			}
			a.ns = v
		case "o", "output":
			v, err := next()
			if err != nil {
				return nil, err
			}
			if v != "table" && v != "json" {
				return nil, fmt.Errorf("-o must be table or json")
			}
			a.out = v
		case "config":
			v, err := next()
			if err != nil {
				return nil, err
			}
			a.cfgPath = v
		case "token":
			v, err := next()
			if err != nil {
				return nil, err
			}
			a.token = v
		case "insecure":
			a.insecure = true
		default:
			rest = append(rest, arg)
		}
	}
	return rest, nil
}

func (a *app) usage() {
	fmt.Fprintf(a.stdout, "wsctl %s — developer workspaces from the command line (talks to the portal API)\n\n", version)
	fmt.Fprintln(a.stdout, "Usage: wsctl [global flags] COMMAND [flags] [args]")
	fmt.Fprintln(a.stdout, "\nGlobal flags:")
	fmt.Fprintln(a.stdout, "  --portal URL      portal address (remembered by `wsctl login`; env WSCTL_PORTAL)")
	fmt.Fprintln(a.stdout, "  --ns NAMESPACE    namespace when a name is ambiguous (project namespaces, other developers)")
	fmt.Fprintln(a.stdout, "  -o table|json     output format")
	fmt.Fprintln(a.stdout, "  --insecure        skip TLS verification (demos only)")
	fmt.Fprintln(a.stdout, "  --config PATH     config file (default ~/.config/wsctl/config.json)")
	fmt.Fprintln(a.stdout, "\nCommands:")
	cs := append([]command(nil), commands...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	w := 0
	for _, c := range cs {
		if len(c.usage) > w {
			w = len(c.usage)
		}
	}
	for _, c := range cs {
		fmt.Fprintf(a.stdout, "  %-*s  %s\n", w, c.usage, c.help)
	}
	fmt.Fprintln(a.stdout, "\nNames may be written owner/name for workspaces shared with you (e.g. alice@example.com/api).")
	fmt.Fprintln(a.stdout, "Run `wsctl COMMAND --help` for the flags of one command.")
}

// api returns the authenticated client (created once).
func (a *app) api() (*Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	c, err := newClient(a.cfg, a.portal, a.token, a.insecure)
	if err != nil {
		return nil, err
	}
	if a.cfg.Auth == "device" && a.cfg.RefreshToken != "" {
		c.refresh = func(ctx context.Context) (string, error) {
			tok, err := refreshToken(ctx, &http.Client{Transport: c.HTTP.Transport}, a.cfg)
			if err == nil {
				_ = saveConfig(a.cfgPath, a.cfg)
			}
			return tok, err
		}
	}
	a.client = c
	return c, nil
}

// newFlags makes a FlagSet whose --help prints usage and returns flag.ErrHelp.
func (a *app) newFlags(name, synopsis string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() {
		fmt.Fprintf(a.stderr, "Usage: wsctl %s\n", synopsis)
		fs.PrintDefaults()
	}
	return fs
}

// parseMixed lets flags and positionals appear in any order
// (`wsctl create api --lang go` and `wsctl create --lang go api` both work).
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos, flags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			pos = append(pos, arg)
			continue
		}
		flags = append(flags, arg)
		name, _, hasVal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		f := fs.Lookup(name)
		if f == nil {
			continue // let fs.Parse report it
		}
		if bv, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bv.IsBoolFlag() {
			continue
		}
		if !hasVal && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return pos, nil
}

// splitRef turns "owner/name" into (owner, name); plain names give owner "".
func splitRef(ref string) (owner, name string) {
	if i := strings.LastIndexByte(ref, '/'); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

// multiFlag collects repeatable, comma-separable values (--lang go,python --lang rust).
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*m = append(*m, p)
		}
	}
	return nil
}
