package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------- API types (api/openapi.yaml)

type Workspace struct {
	Name        string         `json:"name"`
	Namespace   string         `json:"namespace"`
	Tier        string         `json:"tier"`
	Status      string         `json:"status"`
	Ready       bool           `json:"ready"`
	Languages   []string       `json:"languages"`
	Tools       []string       `json:"tools"`
	Profile     string         `json:"profile"`
	Image       string         `json:"image"`
	Node        string         `json:"node"`
	IdleTimeout string         `json:"idleTimeout"`
	LastActive  string         `json:"lastActive"`
	StartedAt   string         `json:"startedAt"`
	CreatedAt   string         `json:"createdAt"`
	Waiting     string         `json:"waiting"`
	Expose      bool           `json:"expose"`
	PublicAddr  string         `json:"publicAddr"`
	Owner       string         `json:"owner"`
	Drive       string         `json:"drive"`
	Storage     string         `json:"storage"`
	Mounts      []string       `json:"mounts"`
	DNS         string         `json:"dns"`
	Spec        map[string]any `json:"spec"`
}

type SSHInfo struct {
	Alias              string `json:"alias"`
	User               string `json:"user"`
	Port               int    `json:"port"`
	HostKeyFingerprint string `json:"hostKeyFingerprint"`
	KnownHostsLine     string `json:"knownHostsLine"`
	AuthorizedKeys     []struct {
		Type, Fingerprint, Comment string
	} `json:"authorizedKeys"`
}

type Drive struct {
	Name          string   `json:"name"`
	Namespace     string   `json:"namespace"`
	Kind          string   `json:"kind"`
	Owner         string   `json:"owner"`
	Size          string   `json:"size"`
	Storage       string   `json:"storage"`
	StorageClass  string   `json:"storageClass"`
	AccessModes   []string `json:"accessModes"`
	Phase         string   `json:"phase"`
	AttachedTo    string   `json:"attachedTo"`
	MountedBy     []string `json:"mountedBy"`
	SharedWith    []string `json:"sharedWith"`
	SharedBy      string   `json:"sharedBy"`
	LastWorkspace string   `json:"lastWorkspace"`
	Languages     []string `json:"languages"`
	CreatedAt     string   `json:"createdAt"`
	Protected     bool     `json:"protected"`
}

type Snapshot struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Drive       string `json:"drive"`
	ReadyToUse  bool   `json:"readyToUse"`
	RestoreSize string `json:"restoreSize"`
	CreatedAt   string `json:"createdAt"`
}

type Share struct {
	User      string `json:"user"`
	Kind      string `json:"kind"`
	Resource  string `json:"resource"`
	ReadOnly  bool   `json:"readOnly"`
	CreatedAt string `json:"createdAt"`
}

type Link struct {
	Workspace string `json:"workspace"`
	Peer      string `json:"peer"`
	PeerDNS   string `json:"peerDNS"`
	Ports     []int  `json:"ports"`
	Status    string `json:"status"`
}

type Network struct {
	DNS           string   `json:"dns"`
	PodIP         string   `json:"podIP"`
	Namespace     string   `json:"namespace"`
	SameNamespace []string `json:"sameNamespace"`
	Links         []Link   `json:"links"`
	Multicast     struct {
		Mode, Iface string
	} `json:"multicast"`
	Mesh string `json:"mesh"`
}

type Project struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Members   []string `json:"members"`
	Mesh      string   `json:"mesh"`
}

// ---------------------------------------------------------------- helpers

// wsQuery builds the query for a workspace reference: --ns and/or owner/name.
func (a *app) wsQuery(ref string) (string, url.Values) {
	owner, name := splitRef(ref)
	q := url.Values{}
	if a.ns != "" {
		q.Set("ns", a.ns)
	} else if a.cfg != nil && a.cfg.DefaultNS != "" {
		q.Set("ns", a.cfg.DefaultNS)
	}
	if owner != "" {
		q.Set("owner", owner)
	}
	return name, q
}

func (a *app) wsPath(ref, action string) (string, url.Values) {
	name, q := a.wsQuery(ref)
	p := "/api/workspaces/" + url.PathEscape(name)
	if action != "" {
		p += "/" + action
	}
	return p, q
}

func (a *app) emit(v any) error {
	if a.out == "json" {
		return printJSON(a.stdout, v)
	}
	return nil
}

func need(pos []string, n int, what string) error {
	if len(pos) < n {
		return fmt.Errorf("missing argument: %s (see --help)", what)
	}
	return nil
}

func (a *app) getWorkspace(ref string) (*Workspace, *SSHInfo, map[string]any, error) {
	c, err := a.api()
	if err != nil {
		return nil, nil, nil, err
	}
	p, q := a.wsPath(ref, "")
	var resp struct {
		Workspace Workspace `json:"workspace"`
		SSH       *SSHInfo  `json:"ssh"`
	}
	var raw map[string]any
	if err := c.do(a.ctx, http.MethodGet, p, q, nil, &raw); err != nil {
		return nil, nil, nil, err
	}
	b, _ := jsonMarshal(raw)
	_ = jsonUnmarshal(b, &resp)
	return &resp.Workspace, resp.SSH, raw, nil
}

func (a *app) waitRunning(ref string, timeout time.Duration) (*Workspace, error) {
	deadline := time.Now().Add(timeout)
	last := ""
	for {
		ws, _, _, err := a.getWorkspace(ref)
		if err != nil {
			return nil, err
		}
		if ws.Status == "Running" && ws.Ready {
			return ws, nil
		}
		if ws.Status == "Failed" {
			return ws, fmt.Errorf("workspace failed: %s", ws.Waiting)
		}
		msg := ws.Status
		if ws.Waiting != "" {
			msg += " — " + ws.Waiting
		}
		if msg != last {
			fmt.Fprintln(a.stderr, "  "+msg)
			last = msg
		}
		if time.Now().After(deadline) {
			return ws, fmt.Errorf("still %s after %s (wsctl logs %s shows the boot log)", ws.Status, timeout, ref)
		}
		select {
		case <-a.ctx.Done():
			return ws, a.ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// ---------------------------------------------------------------- login / me / version / catalog

func init() {
	register(command{"login", "login [--tenant T --client-id ID | --az | --token FILE|- | --dev]", "sign in and remember the portal", cmdLogin})
	register(command{"logout", "logout", "forget the stored token", cmdLogout})
	register(command{"me", "me", "who am I, which namespaces and projects", cmdMe})
	register(command{"version", "version", "print the version", func(a *app, _ []string) error { fmt.Fprintln(a.stdout, "wsctl", version); return nil }})
	register(command{"catalog", "catalog", "languages, tools, profiles, storage choices", cmdCatalog})
	register(command{"completion", "completion bash|zsh", "shell completion script", cmdCompletion})
}

func cmdLogin(a *app, args []string) error {
	fs := a.newFlags("login", "login [--portal URL] [--tenant TENANT --client-id ID [--scope S]] [--az [--audience A]] [--token FILE|-] [--dev]")
	tenant := fs.String("tenant", "", "Entra tenant id or domain (device-code login)")
	issuer := fs.String("issuer", "", "any OIDC issuer for device-code login, e.g. https://keycloak.example/realms/dev (endpoints via discovery)")
	clientID := fs.String("client-id", "", "public client application id (device-code login)")
	scope := fs.String("scope", "", "OAuth scope (default "+defaultScope+")")
	az := fs.Bool("az", false, "get the token from the Azure CLI (az account get-access-token)")
	audience := fs.String("audience", "", "API audience for --az (default "+defaultAudience+")")
	tokenSrc := fs.String("token", "", "read a bearer token from FILE, or - for stdin")
	dev := fs.Bool("dev", false, "no token: portal runs with -dev-user (demos)")
	tunnelHost := fs.String("tunnel-host", "", "portal URL used for the SSH tunnel when --portal is an API gateway that cannot carry WebSockets (default: same as --portal)")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	cfg := a.cfg
	if a.portal != "" {
		cfg.Portal = strings.TrimRight(a.portal, "/")
	}
	if cfg.Portal == "" {
		return errors.New("tell me where the portal is: wsctl login --portal https://workspaces.example.com …")
	}
	if a.insecure {
		cfg.Insecure = true
	}
	if *tenant != "" {
		cfg.Tenant = *tenant
	}
	if *issuer != "" {
		cfg.Issuer = strings.TrimRight(*issuer, "/")
	}
	if *clientID != "" {
		cfg.ClientID = *clientID
	}
	if *scope != "" {
		cfg.Scope = *scope
	}
	if *audience != "" {
		cfg.Audience = *audience
	}
	if *tunnelHost != "" {
		cfg.TunnelHost = strings.TrimRight(*tunnelHost, "/")
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	// The portal publishes its login settings; use them unless given explicitly.
	if !*dev && !*az && *tokenSrc == "" && ((cfg.Tenant == "" && cfg.Issuer == "") || cfg.ClientID == "") {
		if wk, err := fetchWellKnown(a.ctx, hc, cfg); err == nil {
			if cfg.Tenant == "" {
				cfg.Tenant = wk.Login.Tenant
			}
			if cfg.Tenant == "" && cfg.Issuer == "" && wk.Login.Issuer != "" {
				cfg.Issuer = strings.TrimRight(wk.Login.Issuer, "/") // Keycloak & friends: discovery-based device flow
			}
			if cfg.ClientID == "" {
				cfg.ClientID = wk.Login.ClientID
			}
			if cfg.Scope == "" && wk.Login.Scope != "" {
				cfg.Scope = wk.Login.Scope + " offline_access openid profile"
			}
			if cfg.Audience == "" {
				cfg.Audience = wk.Login.Audience
			}
			if wk.Login.Mode == "dev" || wk.Login.Mode == "stub" {
				*dev = true // the portal has no sign-in (demo / stub); any token is accepted
			}
		}
	}
	switch {
	case *dev:
		cfg.Auth, cfg.Token, cfg.RefreshToken = "dev", "", ""
	case *az:
		if err := azLogin(a.ctx, cfg); err != nil {
			return err
		}
	case *tokenSrc != "":
		if err := tokenLogin(cfg, *tokenSrc, a.stdin); err != nil {
			return err
		}
	default:
		if err := deviceLogin(a.ctx, hc, cfg, a.stderr); err != nil {
			return err
		}
	}
	if err := saveConfig(a.cfgPath, cfg); err != nil {
		return err
	}
	a.client = nil
	// prove it works
	c, err := a.api()
	if err != nil {
		return err
	}
	var me struct {
		User User `json:"user"`
	}
	if err := c.do(a.ctx, http.MethodGet, "/api/me", nil, nil, &me); err != nil {
		return fmt.Errorf("signed in, but the portal rejected the token: %w", err)
	}
	fmt.Fprintf(a.stdout, "Signed in to %s as %s (%s). Config: %s\n", cfg.Portal, me.User.Name, cfg.Auth, a.cfgPath)
	return nil
}

func cmdLogout(a *app, _ []string) error {
	a.cfg.Token, a.cfg.RefreshToken, a.cfg.ExpiresAt = "", "", time.Time{}
	if err := saveConfig(a.cfgPath, a.cfg); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "token removed from", a.cfgPath)
	return nil
}

type User struct {
	Name   string   `json:"name"`
	Short  string   `json:"short"`
	Groups []string `json:"groups"`
}

func cmdMe(a *app, args []string) error {
	fs := a.newFlags("me", "me")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	var me struct {
		User       User `json:"user"`
		Namespaces []struct {
			Name, Tier string
			Exists     bool
		} `json:"namespaces"`
		Projects  []Project `json:"projects"`
		Auth      string    `json:"auth"`
		Namespace string    `json:"namespace"`
		Onboarded bool      `json:"onboarded"`
	}
	if err := c.do(a.ctx, http.MethodGet, "/api/me", nil, nil, &me); err != nil {
		return err
	}
	if a.out == "json" {
		return printJSON(a.stdout, me)
	}
	pairs := [][2]string{{"User", me.User.Name}, {"Short name", me.User.Short}, {"Portal", c.Base}, {"Auth", orDash(firstNonEmpty(me.Auth, a.cfg.Auth))}}
	if len(me.User.Groups) > 0 {
		pairs = append(pairs, [2]string{"Groups", strings.Join(me.User.Groups, ", ")})
	}
	if len(me.Namespaces) > 0 {
		var ns []string
		for _, n := range me.Namespaces {
			s := n.Name + " (" + n.Tier
			if !n.Exists {
				s += ", not onboarded"
			}
			ns = append(ns, s+")")
		}
		pairs = append(pairs, [2]string{"Namespaces", strings.Join(ns, ", ")})
	} else if me.Namespace != "" {
		s := me.Namespace
		if !me.Onboarded {
			s += " (not onboarded yet — ask an admin: workspace admin onboard " + me.User.Name + ")"
		}
		pairs = append(pairs, [2]string{"Namespace", s})
	}
	if len(me.Projects) > 0 {
		var ps []string
		for _, p := range me.Projects {
			ps = append(ps, p.Name+" ("+p.Namespace+")")
		}
		pairs = append(pairs, [2]string{"Projects", strings.Join(ps, ", ")})
	}
	if !a.cfg.ExpiresAt.IsZero() {
		pairs = append(pairs, [2]string{"Token expires", a.cfg.ExpiresAt.Local().Format(time.RFC1123)})
	}
	kv(a.stdout, pairs)
	return nil
}

func cmdCatalog(a *app, args []string) error {
	fs := a.newFlags("catalog", "catalog")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	var cat map[string]any
	if err := c.do(a.ctx, http.MethodGet, "/api/catalog", nil, nil, &cat); err != nil {
		return err
	}
	if a.out == "json" {
		return printJSON(a.stdout, cat)
	}
	section := func(title, key string) {
		m, ok := cat[key].(map[string]any)
		if !ok || len(m) == 0 {
			return
		}
		fmt.Fprintln(a.stdout, title)
		var rows [][]string
		for k, v := range m {
			desc := ""
			if vm, ok := v.(map[string]any); ok {
				desc = firstNonEmpty(str(vm["description"]), str(vm["desc"]), str(vm["label"]), str(vm["image"]))
			}
			rows = append(rows, []string{"  " + k, desc})
		}
		sortRows(rows)
		table(a.stdout, []string{"  NAME", "DESCRIPTION"}, rows)
		fmt.Fprintln(a.stdout)
	}
	section("Languages", "languages")
	section("Tools", "tools")
	section("Profiles", "profiles")
	if st, ok := cat["storage"].(map[string]any); ok {
		if choices, ok := st["choices"].([]any); ok && len(choices) > 0 {
			fmt.Fprintln(a.stdout, "Storage choices (--storage)")
			var rows [][]string
			for _, ch := range choices {
				m, _ := ch.(map[string]any)
				def := ""
				if b, _ := m["default"].(bool); b {
					def = "default"
				}
				rows = append(rows, []string{"  " + str(m["name"]), str(m["kind"]), str(m["storageClass"]), def, str(m["description"])})
			}
			table(a.stdout, []string{"  NAME", "KIND", "STORAGECLASS", "", "DESCRIPTION"}, rows)
			fmt.Fprintln(a.stdout)
		}
	}
	if nw, ok := cat["networking"].(map[string]any); ok {
		fmt.Fprintf(a.stdout, "Networking: multicast=%s mesh=%s\n", orDash(str(nw["multicastMode"])), orDash(str(nw["mesh"])))
	}
	return nil
}

func cmdCompletion(a *app, args []string) error {
	fs := a.newFlags("completion", "completion bash|zsh")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "shell (bash|zsh)"); err != nil {
		return err
	}
	var names []string
	for _, c := range commands {
		names = append(names, c.name)
	}
	list := strings.Join(names, " ")
	switch pos[0] {
	case "bash":
		fmt.Fprintf(a.stdout, `_wsctl() {
  local cur=${COMP_WORDS[COMP_CWORD]}
  if [ "$COMP_CWORD" -eq 1 ]; then COMPREPLY=($(compgen -W "%s" -- "$cur")); return; fi
  case "${COMP_WORDS[1]}" in
    drives) COMPREPLY=($(compgen -W "list show delete resize snapshot snapshots restore create-shared" -- "$cur"));;
    share|link) COMPREPLY=($(compgen -W "add rm list" -- "$cur"));;
    get|start|stop|delete|logs|ssh|code|network|idle|keys|ssh-config) COMPREPLY=($(compgen -W "$(wsctl list -o json 2>/dev/null | sed -n 's/.*"name": *"\([^"]*\)".*/\1/p')" -- "$cur"));;
  esac
}
complete -F _wsctl wsctl
`, list)
	case "zsh":
		fmt.Fprintf(a.stdout, "#compdef wsctl\n_wsctl() { local -a cmds; cmds=(%s); if (( CURRENT == 2 )); then _describe 'command' cmds; fi }\ncompdef _wsctl wsctl\n", list)
	default:
		return fmt.Errorf("unknown shell %q (bash or zsh)", pos[0])
	}
	return nil
}

// ---------------------------------------------------------------- workspaces

func init() {
	register(command{"create", "create [NAME] --lang L[,L2] [flags]", "create a workspace (re-create keeps the home drive)", cmdCreate})
	register(command{"list", "list [--shared] [--all]", "my workspaces", cmdList})
	register(command{"get", "get NAME", "details and SSH info", cmdGet})
	register(command{"start", "start NAME [--wait]", "start a stopped workspace", cmdStartStop(true)})
	register(command{"stop", "stop NAME", "stop (files on the home drive stay)", cmdStartStop(false)})
	register(command{"delete", "delete NAME [--purge] [-y]", "delete; the home drive is kept unless --purge", cmdDelete})
	register(command{"logs", "logs NAME [--tail N]", "boot / container log", cmdLogs})
	register(command{"idle", "idle NAME DURATION|off", "change the idle timeout", cmdIdle})
	register(command{"keys", "keys NAME [--add FILE]", "list or add SSH public keys", cmdKeys})
	register(command{"ssh", "ssh NAME [-- ssh args]", "open an SSH session through the portal tunnel", cmdSSH})
	register(command{"code", "code NAME", "open VS Code (Remote-SSH) on /workspace", cmdCode})
	register(command{"proxy", "proxy NAME", "ssh ProxyCommand: bridge stdin/stdout to the tunnel", cmdProxy})
	register(command{"ssh-config", "ssh-config NAME [--remove]", "(re)write the ~/.ssh/config entry", cmdSSHConfig})
	register(command{"network", "network NAME", "DNS name, IP, peers, links", cmdNetwork})
	register(command{"projects", "projects", "project namespaces I belong to", cmdProjects})
}

type createRequest struct {
	Name          string            `json:"name,omitempty"`
	Namespace     string            `json:"namespace,omitempty"`
	Languages     []string          `json:"languages"`
	Tools         []string          `json:"tools,omitempty"`
	Profile       string            `json:"profile,omitempty"`
	Isolation     string            `json:"isolation,omitempty"`
	Image         string            `json:"image,omitempty"`
	Tag           string            `json:"tag,omitempty"`
	Repo          string            `json:"repo,omitempty"`
	Branch        string            `json:"branch,omitempty"`
	Dotfiles      string            `json:"dotfiles,omitempty"`
	GitAuth       string            `json:"gitAuth,omitempty"`
	GitToken      string            `json:"gitToken,omitempty"`
	GitTokenUser  string            `json:"gitTokenUser,omitempty"`
	GitName       string            `json:"gitName,omitempty"`
	GitEmail      string            `json:"gitEmail,omitempty"`
	Size          string            `json:"size,omitempty"`
	Storage       string            `json:"storage,omitempty"`
	StorageClass  string            `json:"storageClass,omitempty"`
	FromDrive     string            `json:"fromDrive,omitempty"`
	FromSnapshot  string            `json:"fromSnapshot,omitempty"`
	Mounts        []string          `json:"mounts,omitempty"`
	CPU           string            `json:"cpu,omitempty"`
	Memory        string            `json:"memory,omitempty"`
	CPURequest    string            `json:"cpuRequest,omitempty"`
	MemoryRequest string            `json:"memoryRequest,omitempty"`
	Multicast     bool              `json:"multicast,omitempty"`
	Devices       map[string]string `json:"devices,omitempty"`
	HostPaths     []string          `json:"hostPaths,omitempty"`
	Caps          []string          `json:"caps,omitempty"`
	Expose        bool              `json:"expose,omitempty"`
	IdleTimeout   string            `json:"idleTimeout,omitempty"`
	TZ            string            `json:"tz,omitempty"`
	SSHPublicKeys []string          `json:"sshPublicKeys,omitempty"`
}

func cmdCreate(a *app, args []string) error {
	fs := a.newFlags("create", "create [NAME] --lang LANG[,LANG2…] [--tools T,T] [--repo URL] [--storage CHOICE] [--mount shared:NAME] …")
	var langs, tools, mounts, hostPaths, caps, devices multiFlag
	fs.Var(&langs, "lang", "language(s); the first picks the image, the rest are installed at boot (repeatable or comma list)")
	fs.Var(&tools, "tools", "tools from the catalog (comma list)")
	fs.Var(&tools, "tool", "same as --tools (repeatable)")
	profile := fs.String("profile", "", "standard | hardware | kernel")
	isolation := fs.String("isolation", "", "userns | gvisor | kata | none")
	image := fs.String("image", "", "override the catalog image")
	tag := fs.String("tag", "", "image tag")
	repo := fs.String("repo", "", "git URL to clone on first boot")
	branch := fs.String("branch", "", "branch to check out")
	dotfiles := fs.String("dotfiles", "", "dotfiles repo URL")
	gitAuth := fs.String("git-auth", "", "deploy-key | token | none")
	gitTokenStdin := fs.Bool("git-token-stdin", false, "read an HTTPS git token from stdin (sets --git-auth token)")
	gitTokenUser := fs.String("git-token-user", "", "username for the token (default x-access-token)")
	gitName := fs.String("git-name", "", "git user.name")
	gitEmail := fs.String("git-email", "", "git user.email")
	size := fs.String("size", "", "home drive size, e.g. 50Gi (only when the drive is created)")
	storage := fs.String("storage", "", "storage choice from the catalog (wsctl catalog)")
	storageClass := fs.String("storage-class", "", "raw StorageClass name (overrides --storage)")
	fromDrive := fs.String("from-drive", "", "re-attach a detached home drive (wsctl drives list)")
	fromSnapshot := fs.String("from-snapshot", "", "create the home drive from this snapshot")
	fs.Var(&mounts, "mount", "shared drive to mount: shared:NAME or shared:OWNER/NAME (repeatable)")
	project := fs.String("project", "", "create in project namespace ws-proj-NAME")
	namespace := fs.String("namespace", "", "explicit namespace (project namespaces)")
	cpu := fs.String("cpu", "", "CPU limit")
	memory := fs.String("memory", "", "memory limit")
	cpuReq := fs.String("cpu-request", "", "CPU request")
	memReq := fs.String("memory-request", "", "memory request")
	multicast := fs.Bool("multicast", false, "attach the multicast network")
	fs.Var(&devices, "device", "RESOURCE=QTY device plugin request (repeatable)")
	fs.Var(&hostPaths, "host-path", "host path to mount (lab profiles)")
	fs.Var(&caps, "cap", "extra Linux capability (lab profiles)")
	expose := fs.Bool("expose", false, "also publish sshd on a LoadBalancer")
	idle := fs.String("idle-timeout", "", "auto-stop after inactivity, e.g. 4h, 30m, off")
	tz := fs.String("tz", "", "container timezone")
	keyFile := fs.String("key", "", "SSH public key file to authorise (default: keys from your profile)")
	wait := fs.Bool("wait", true, "wait until Running")
	noWait := fs.Bool("no-wait", false, "return immediately")
	timeout := fs.Duration("timeout", 10*time.Minute, "how long --wait waits")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(langs) == 0 {
		return errors.New("--lang is required (e.g. --lang go or --lang go,python); see wsctl catalog")
	}
	req := createRequest{Languages: langs, Tools: tools, Profile: *profile, Isolation: *isolation, Image: *image, Tag: *tag,
		Repo: *repo, Branch: *branch, Dotfiles: *dotfiles, GitAuth: *gitAuth, GitTokenUser: *gitTokenUser, GitName: *gitName, GitEmail: *gitEmail,
		Size: *size, Storage: *storage, StorageClass: *storageClass, FromDrive: *fromDrive, FromSnapshot: *fromSnapshot, Mounts: mounts,
		CPU: *cpu, Memory: *memory, CPURequest: *cpuReq, MemoryRequest: *memReq, Multicast: *multicast, HostPaths: hostPaths, Caps: caps,
		Expose: *expose, IdleTimeout: *idle, TZ: *tz}
	if len(pos) > 0 {
		req.Name = pos[0]
	}
	switch {
	case *namespace != "":
		req.Namespace = *namespace
	case *project != "":
		req.Namespace = "ws-proj-" + *project
	case a.ns != "":
		req.Namespace = a.ns
	}
	if len(devices) > 0 {
		req.Devices = map[string]string{}
		for _, d := range devices {
			k, v, ok := strings.Cut(d, "=")
			if !ok {
				return fmt.Errorf("--device wants RESOURCE=QTY, got %q", d)
			}
			req.Devices[k] = v
		}
	}
	if *gitTokenStdin {
		req.GitAuth = "token"
		sc := bufio.NewScanner(a.stdin)
		if sc.Scan() {
			req.GitToken = strings.TrimSpace(sc.Text())
		}
		if req.GitToken == "" {
			return errors.New("--git-token-stdin: nothing read from stdin")
		}
	}
	if *keyFile != "" {
		b, err := os.ReadFile(*keyFile)
		if err != nil {
			return err
		}
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				req.SSHPublicKeys = append(req.SSHPublicKeys, l)
			}
		}
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	var resp struct {
		Name               string         `json:"name"`
		Namespace          string         `json:"namespace"`
		DeployKey          string         `json:"deployKey"`
		HostKeyFingerprint string         `json:"hostKeyFingerprint"`
		Spec               map[string]any `json:"spec"`
	}
	if err := c.do(a.ctx, http.MethodPost, "/api/workspaces", nil, req, &resp); err != nil {
		return err
	}
	ref := resp.Name
	if a.out == "json" && (*noWait || !*wait) {
		return printJSON(a.stdout, resp)
	}
	fmt.Fprintf(a.stderr, "created %s in %s (image %s)\n", resp.Name, resp.Namespace, str(resp.Spec["image"]))
	if resp.DeployKey != "" {
		fmt.Fprintf(a.stderr, "deploy key (add it to your git host with write access):\n  %s\n", resp.DeployKey)
	}
	if req.Namespace != "" && a.ns == "" {
		a.ns = resp.Namespace
	}
	if *noWait || !*wait {
		return nil
	}
	ws, err := a.waitRunning(ref, *timeout)
	if err != nil {
		return err
	}
	if a.out == "json" {
		return printJSON(a.stdout, ws)
	}
	if _, err := a.ensureSSHConfig(ref); err != nil {
		fmt.Fprintln(a.stderr, "warning: could not write ssh config:", err)
	}
	fmt.Fprintf(a.stdout, "%s is Running on %s\n  wsctl ssh %s        # shell\n  wsctl code %s       # VS Code\n", ws.Name, orDash(ws.Node), ref, ref)
	return nil
}

func cmdList(a *app, args []string) error {
	fs := a.newFlags("list", "list [--shared]")
	shared := fs.Bool("shared", false, "also show workspaces shared with me")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	q := url.Values{}
	if *shared {
		q.Set("shared", "true")
	}
	if a.ns != "" {
		q.Set("ns", a.ns)
	}
	var resp struct {
		Workspaces   []Workspace `json:"workspaces"`
		SharedWithMe []Workspace `json:"sharedWithMe"`
	}
	if err := c.do(a.ctx, http.MethodGet, "/api/workspaces", q, nil, &resp); err != nil {
		return err
	}
	if a.out == "json" {
		return printJSON(a.stdout, resp)
	}
	var rows [][]string
	for _, w := range resp.Workspaces {
		rows = append(rows, []string{w.Name, w.Namespace, w.Status, joinOrDash(w.Languages), joinOrDash(w.Tools), orDash(w.Profile), orDash(w.IdleTimeout), age(w.CreatedAt)})
	}
	if len(rows) == 0 && !*shared {
		fmt.Fprintln(a.stdout, "no workspaces yet — try: wsctl create --lang go")
		return nil
	}
	table(a.stdout, []string{"NAME", "NAMESPACE", "STATUS", "LANGUAGES", "TOOLS", "PROFILE", "IDLE", "AGE"}, rows)
	if *shared {
		fmt.Fprintln(a.stdout, "\nShared with me (use owner/name):")
		var srows [][]string
		for _, w := range resp.SharedWithMe {
			srows = append(srows, []string{w.Owner + "/" + w.Name, w.Namespace, w.Status, joinOrDash(w.Languages)})
		}
		if len(srows) == 0 {
			fmt.Fprintln(a.stdout, "  (none)")
		} else {
			table(a.stdout, []string{"REF", "NAMESPACE", "STATUS", "LANGUAGES"}, srows)
		}
	}
	return nil
}

func cmdGet(a *app, args []string) error {
	fs := a.newFlags("get", "get NAME")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "workspace name"); err != nil {
		return err
	}
	ws, ssh, raw, err := a.getWorkspace(pos[0])
	if err != nil {
		return err
	}
	if a.out == "json" {
		return printJSON(a.stdout, raw)
	}
	pairs := [][2]string{
		{"Name", ws.Name}, {"Namespace", ws.Namespace}, {"Status", ws.Status + ifs(ws.Waiting != "", " ("+ws.Waiting+")", "")},
		{"Languages", joinOrDash(ws.Languages)}, {"Tools", joinOrDash(ws.Tools)}, {"Profile", orDash(ws.Profile)},
		{"Image", orDash(ws.Image)}, {"Node", orDash(ws.Node)}, {"Home drive", orDash(firstNonEmpty(ws.Drive, "ws-"+ws.Name+"-home"))},
		{"Storage", orDash(ws.Storage)}, {"Mounts", joinOrDash(ws.Mounts)}, {"DNS", orDash(firstNonEmpty(ws.DNS, "ws-"+ws.Name+"."+ws.Namespace+".svc.cluster.local"))},
		{"Idle timeout", orDash(ws.IdleTimeout)}, {"Last active", orDash(ws.LastActive)}, {"Created", orDash(ws.CreatedAt)},
	}
	if ws.PublicAddr != "" {
		pairs = append(pairs, [2]string{"Public address", ws.PublicAddr})
	}
	if dk := str(raw["deployKey"]); dk != "" {
		pairs = append(pairs, [2]string{"Deploy key", dk})
	}
	if ssh != nil {
		pairs = append(pairs, [2]string{"SSH", fmt.Sprintf("ssh %s   (user %s, host key %s)", ssh.Alias, ssh.User, ssh.HostKeyFingerprint)})
		var ks []string
		for _, k := range ssh.AuthorizedKeys {
			ks = append(ks, k.Fingerprint+" "+k.Comment)
		}
		pairs = append(pairs, [2]string{"Authorized keys", joinOrDash(ks)})
	}
	kv(a.stdout, pairs)
	return nil
}

func ifs(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

func cmdStartStop(start bool) func(*app, []string) error {
	return func(a *app, args []string) error {
		verb := "stop"
		if start {
			verb = "start"
		}
		fs := a.newFlags(verb, verb+" NAME")
		wait := fs.Bool("wait", false, "wait until Running (start only)")
		timeout := fs.Duration("timeout", 10*time.Minute, "how long --wait waits")
		pos, err := parseMixed(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 1, "workspace name"); err != nil {
			return err
		}
		c, err := a.api()
		if err != nil {
			return err
		}
		p, q := a.wsPath(pos[0], verb)
		var resp map[string]any
		if err := c.do(a.ctx, http.MethodPost, p, q, nil, &resp); err != nil {
			return err
		}
		if a.out == "json" && !*wait {
			return printJSON(a.stdout, resp)
		}
		fmt.Fprintf(a.stdout, "%s: %s requested\n", pos[0], verb)
		if start && *wait {
			ws, err := a.waitRunning(pos[0], *timeout)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "%s is Running\n", ws.Name)
		}
		return nil
	}
}

func cmdDelete(a *app, args []string) error {
	fs := a.newFlags("delete", "delete NAME [--purge] [-y]")
	purge := fs.Bool("purge", false, "ALSO delete the home drive (everything not pushed is lost)")
	yes := fs.Bool("y", false, "do not ask")
	fs.BoolVar(yes, "yes", false, "do not ask")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "workspace name"); err != nil {
		return err
	}
	if !*yes {
		what := "the workspace (its home drive is KEPT; re-create with --from-drive or the same name to get it back)"
		if *purge {
			what = "the workspace AND its home drive — files not pushed to git are gone"
		}
		fmt.Fprintf(a.stderr, "Delete %s: %s? [y/N] ", pos[0], what)
		sc := bufio.NewScanner(a.stdin)
		if !sc.Scan() || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(sc.Text())), "y") {
			return errors.New("cancelled")
		}
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	p, q := a.wsPath(pos[0], "")
	if *purge {
		q.Set("purge", "true")
	}
	var resp map[string]any
	if err := c.do(a.ctx, http.MethodDelete, p, q, nil, &resp); err != nil {
		return err
	}
	_, name := splitRef(pos[0])
	alias := "ws-" + name
	_ = removeSSHBlock(filepath.Join(sshDir(), "config"), alias)
	_ = writeKnownHost(filepath.Join(sshDir(), "known_hosts_wsctl"), alias, "")
	if a.out == "json" {
		return printJSON(a.stdout, resp)
	}
	if *purge {
		fmt.Fprintf(a.stdout, "deleted %s and its home drive\n", pos[0])
	} else {
		fmt.Fprintf(a.stdout, "deleted %s (home drive kept — see: wsctl drives list)\n", pos[0])
	}
	return nil
}

func cmdLogs(a *app, args []string) error {
	fs := a.newFlags("logs", "logs NAME [--tail N]")
	tail := fs.Int("tail", 200, "lines")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "workspace name"); err != nil {
		return err
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	p, q := a.wsPath(pos[0], "logs")
	q.Set("tail", fmt.Sprint(*tail))
	var out string
	if err := c.do(a.ctx, http.MethodGet, p, q, nil, &out); err != nil {
		return err
	}
	fmt.Fprint(a.stdout, out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Fprintln(a.stdout)
	}
	return nil
}

func cmdIdle(a *app, args []string) error {
	fs := a.newFlags("idle", "idle NAME DURATION   (e.g. 4h, 30m, off)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 2, "workspace name and duration"); err != nil {
		return err
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	p, q := a.wsPath(pos[0], "idle")
	var resp map[string]any
	if err := c.do(a.ctx, http.MethodPut, p, q, map[string]string{"idleTimeout": pos[1]}, &resp); err != nil {
		return err
	}
	if a.out == "json" {
		return printJSON(a.stdout, resp)
	}
	fmt.Fprintf(a.stdout, "%s: idle timeout is now %s\n", pos[0], pos[1])
	return nil
}

func cmdKeys(a *app, args []string) error {
	fs := a.newFlags("keys", "keys NAME [--add FILE]")
	add := fs.String("add", "", "public key file to authorise on this workspace")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "workspace name"); err != nil {
		return err
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	if *add != "" {
		b, err := os.ReadFile(*add)
		if err != nil {
			return err
		}
		p, q := a.wsPath(pos[0], "keys")
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				if err := c.do(a.ctx, http.MethodPost, p, q, map[string]string{"publicKey": l}, nil); err != nil {
					return err
				}
			}
		}
		fmt.Fprintf(a.stdout, "added key(s) from %s\n", *add)
	}
	_, ssh, raw, err := a.getWorkspace(pos[0])
	if err != nil {
		return err
	}
	if a.out == "json" {
		return printJSON(a.stdout, raw["ssh"])
	}
	if ssh == nil {
		fmt.Fprintln(a.stdout, "no SSH secret yet")
		return nil
	}
	var rows [][]string
	for _, k := range ssh.AuthorizedKeys {
		rows = append(rows, []string{k.Type, k.Fingerprint, k.Comment})
	}
	table(a.stdout, []string{"TYPE", "FINGERPRINT", "COMMENT"}, rows)
	return nil
}

// ensureSSHConfig writes the ~/.ssh/config block + known_hosts line and returns the alias.
func (a *app) ensureSSHConfig(ref string) (string, error) {
	_, ssh, _, err := a.getWorkspace(ref)
	if err != nil {
		return "", err
	}
	_, name := splitRef(ref)
	alias := "ws-" + name
	if ssh != nil && ssh.Alias != "" {
		alias = ssh.Alias
	}
	if owner, _ := splitRef(ref); owner != "" {
		alias = "ws-" + sanitize(owner) + "-" + name // shared workspaces get a distinct alias
	}
	dir := sshDir()
	block := sshConfigBlock(alias, ref, a.ns, a.exe, a.insecure, firstNonEmpty(a.portal, ""))
	if err := upsertSSHBlock(filepath.Join(dir, "config"), alias, block); err != nil {
		return "", err
	}
	if ssh != nil && ssh.KnownHostsLine != "" {
		f := strings.Fields(ssh.KnownHostsLine)
		if len(f) >= 3 {
			if err := writeKnownHost(filepath.Join(dir, "known_hosts_wsctl"), alias, alias+" "+f[1]+" "+f[2]); err != nil {
				return "", err
			}
		}
	}
	return alias, nil
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[:i]
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func cmdSSHConfig(a *app, args []string) error {
	fs := a.newFlags("ssh-config", "ssh-config NAME [--remove]")
	remove := fs.Bool("remove", false, "remove the entry")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "workspace name"); err != nil {
		return err
	}
	if *remove {
		_, name := splitRef(pos[0])
		alias := "ws-" + name
		if err := removeSSHBlock(filepath.Join(sshDir(), "config"), alias); err != nil {
			return err
		}
		_ = writeKnownHost(filepath.Join(sshDir(), "known_hosts_wsctl"), alias, "")
		fmt.Fprintf(a.stdout, "removed %s from %s\n", alias, filepath.Join(sshDir(), "config"))
		return nil
	}
	alias, err := a.ensureSSHConfig(pos[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote Host %s to %s — now: ssh %s\n", alias, filepath.Join(sshDir(), "config"), alias)
	return nil
}

func cmdSSH(a *app, args []string) error {
	fs := a.newFlags("ssh", "ssh NAME [-- extra ssh arguments]")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "workspace name"); err != nil {
		return err
	}
	alias, err := a.ensureSSHConfig(pos[0])
	if err != nil {
		return err
	}
	sshArgs := append([]string{alias}, pos[1:]...)
	return a.exec("ssh", sshArgs...)
}

func cmdCode(a *app, args []string) error {
	fs := a.newFlags("code", "code NAME [PATH]")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "workspace name"); err != nil {
		return err
	}
	alias, err := a.ensureSSHConfig(pos[0])
	if err != nil {
		return err
	}
	path := "/workspace"
	if len(pos) > 1 {
		path = pos[1]
	}
	if _, err := exec.LookPath("code"); err != nil {
		return fmt.Errorf("the `code` command is not on PATH (VS Code → Command Palette → \"Shell Command: Install 'code' command\"); then: code --remote ssh-remote+%s %s", alias, path)
	}
	return a.exec("code", "--remote", "ssh-remote+"+alias, path)
}

// exec runs an external program attached to our terminal.
func (a *app) exec(name string, args ...string) error {
	cmd := exec.CommandContext(a.ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func cmdProxy(a *app, args []string) error {
	fs := a.newFlags("proxy", "proxy NAME   (used as ssh ProxyCommand)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "workspace name"); err != nil {
		return err
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	p, q := a.wsPath(pos[0], "tunnel")
	base := c.Base
	if a.cfg.TunnelHost != "" {
		base = a.cfg.TunnelHost
	}
	u := base + p
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	hdr := http.Header{}
	c.headers(hdr)
	ws, err := wsDial(a.ctx, u, hdr, c.Insecure)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 401 && c.refresh != nil {
			if tok, rerr := c.refresh(a.ctx); rerr == nil {
				c.Token = tok
				hdr.Set("Authorization", "Bearer "+tok)
				ws, err = wsDial(a.ctx, u, hdr, c.Insecure)
			}
		}
		if err != nil {
			return fmt.Errorf("tunnel to %s: %w", pos[0], err)
		}
	}
	return ws.pipe(a.stdin, a.stdout, a.stderr)
}

func cmdNetwork(a *app, args []string) error {
	fs := a.newFlags("network", "network NAME")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "workspace name"); err != nil {
		return err
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	p, q := a.wsPath(pos[0], "network")
	var n Network
	if err := c.do(a.ctx, http.MethodGet, p, q, nil, &n); err != nil {
		return err
	}
	if a.out == "json" {
		return printJSON(a.stdout, n)
	}
	kv(a.stdout, [][2]string{{"DNS", orDash(n.DNS)}, {"Pod IP", orDash(n.PodIP)}, {"Namespace", orDash(n.Namespace)},
		{"Reachable (same namespace)", joinOrDash(n.SameNamespace)}, {"Multicast", orDash(n.Multicast.Mode) + ifs(n.Multicast.Iface != "", " on "+n.Multicast.Iface, "")}, {"Mesh", orDash(n.Mesh)}})
	if len(n.Links) > 0 {
		fmt.Fprintln(a.stdout, "\nLinks:")
		var rows [][]string
		for _, l := range n.Links {
			rows = append(rows, []string{l.Peer, orDash(l.PeerDNS), portsOrAll(l.Ports), l.Status})
		}
		table(a.stdout, []string{"PEER", "DNS", "PORTS", "STATUS"}, rows)
	} else {
		fmt.Fprintln(a.stdout, "\nNo links to other developers' workspaces (wsctl link add NAME OWNER/WS).")
	}
	return nil
}

func portsOrAll(ps []int) string {
	if len(ps) == 0 {
		return "all"
	}
	var s []string
	for _, p := range ps {
		s = append(s, fmt.Sprint(p))
	}
	return strings.Join(s, ",")
}

func cmdProjects(a *app, args []string) error {
	fs := a.newFlags("projects", "projects")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	c, err := a.api()
	if err != nil {
		return err
	}
	var resp struct {
		Projects []Project `json:"projects"`
	}
	if err := c.do(a.ctx, http.MethodGet, "/api/projects", nil, nil, &resp); err != nil {
		return err
	}
	if a.out == "json" {
		return printJSON(a.stdout, resp)
	}
	if len(resp.Projects) == 0 {
		fmt.Fprintln(a.stdout, "you are not a member of any project (admins create them: workspace admin project create NAME --members …)")
		return nil
	}
	var rows [][]string
	for _, p := range resp.Projects {
		rows = append(rows, []string{p.Name, p.Namespace, strings.Join(p.Members, ","), orDash(p.Mesh)})
	}
	table(a.stdout, []string{"PROJECT", "NAMESPACE", "MEMBERS", "MESH"}, rows)
	return nil
}

// ---------------------------------------------------------------- drives

func init() {
	register(command{"drives", "drives list|show|delete|resize|snapshot|snapshots|restore|create-shared …", "home drives (survive delete) and shared drives", cmdDrives})
	register(command{"share", "share add|rm|list …", "give another developer SSH access to a workspace or a shared drive", cmdShare})
	register(command{"link", "link add|rm|list …", "allow traffic between my workspace and another developer's", cmdLink})
}

func cmdDrives(a *app, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(a.stderr, `Usage:
  wsctl drives list                      all my drives (home drives attached/detached, shared drives)
  wsctl drives show DRIVE                one drive with its snapshots
  wsctl drives delete DRIVE [-y]         delete a detached drive for good
  wsctl drives resize DRIVE SIZE         grow a drive (e.g. 100Gi)
  wsctl drives snapshot DRIVE [--name N] take a snapshot
  wsctl drives snapshots DRIVE           list snapshots
  wsctl drives restore SNAPSHOT NAME --lang L  new workspace whose home drive comes from SNAPSHOT
  wsctl drives create-shared NAME [--size 100Gi] [--storage CHOICE]   shared (ReadWriteMany) drive
A detached home drive is re-attached with: wsctl create NAME --from-drive DRIVE --lang …`)
		return nil
	}
	sub, rest := args[0], args[1:]
	c, err := a.api()
	if err != nil {
		return err
	}
	drivePath := func(name, action string) (string, url.Values) {
		q := url.Values{}
		if a.ns != "" {
			q.Set("ns", a.ns)
		}
		p := "/api/drives/" + url.PathEscape(name)
		if action != "" {
			p += "/" + action
		}
		return p, q
	}
	switch sub {
	case "list", "ls":
		var resp struct {
			Drives []Drive `json:"drives"`
		}
		if err := c.do(a.ctx, http.MethodGet, "/api/drives", nil, nil, &resp); err != nil {
			return err
		}
		if a.out == "json" {
			return printJSON(a.stdout, resp)
		}
		if len(resp.Drives) == 0 {
			fmt.Fprintln(a.stdout, "no drives yet (a home drive appears with your first workspace)")
			return nil
		}
		var rows [][]string
		for _, d := range resp.Drives {
			state := "detached"
			switch {
			case d.Kind == "shared" && d.SharedBy != "":
				state = "shared by " + d.SharedBy
			case d.Kind == "shared":
				state = "mounted by " + joinOrDash(d.MountedBy)
			case d.AttachedTo != "":
				state = "attached to " + d.AttachedTo
			}
			rows = append(rows, []string{d.Name, d.Namespace, d.Kind, d.Size, orDash(firstNonEmpty(d.Storage, d.StorageClass)), state, joinOrDash(d.Languages), age(d.CreatedAt)})
		}
		table(a.stdout, []string{"DRIVE", "NAMESPACE", "KIND", "SIZE", "STORAGE", "STATE", "LAST LANGUAGES", "AGE"}, rows)
		fmt.Fprintln(a.stdout, "\nRe-attach a detached home drive:  wsctl create NAME --from-drive DRIVE --lang …")
		return nil
	case "show":
		if err := need(rest, 1, "drive name"); err != nil {
			return err
		}
		p, q := drivePath(rest[0], "")
		var resp struct {
			Drive     Drive      `json:"drive"`
			Snapshots []Snapshot `json:"snapshots"`
		}
		if err := c.do(a.ctx, http.MethodGet, p, q, nil, &resp); err != nil {
			return err
		}
		if a.out == "json" {
			return printJSON(a.stdout, resp)
		}
		d := resp.Drive
		kv(a.stdout, [][2]string{{"Drive", d.Name}, {"Namespace", d.Namespace}, {"Kind", d.Kind}, {"Size", d.Size}, {"StorageClass", orDash(d.StorageClass)},
			{"Access", joinOrDash(d.AccessModes)}, {"Phase", orDash(d.Phase)}, {"Attached to", orDash(d.AttachedTo)}, {"Mounted by", joinOrDash(d.MountedBy)},
			{"Shared with", joinOrDash(d.SharedWith)}, {"Last workspace", orDash(d.LastWorkspace)}, {"Protected", fmt.Sprint(d.Protected)}, {"Created", orDash(d.CreatedAt)}})
		printSnapshots(a, resp.Snapshots)
		return nil
	case "delete", "rm":
		fs := a.newFlags("drives delete", "drives delete DRIVE [-y]")
		yes := fs.Bool("y", false, "do not ask")
		pos, err := parseMixed(fs, rest)
		if err != nil {
			return err
		}
		if err := need(pos, 1, "drive name"); err != nil {
			return err
		}
		if !*yes {
			fmt.Fprintf(a.stderr, "Delete drive %s for good (all files on it)? [y/N] ", pos[0])
			sc := bufio.NewScanner(a.stdin)
			if !sc.Scan() || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(sc.Text())), "y") {
				return errors.New("cancelled")
			}
		}
		p, q := drivePath(pos[0], "")
		if err := c.do(a.ctx, http.MethodDelete, p, q, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "deleted drive %s\n", pos[0])
		return nil
	case "resize":
		if err := need(rest, 2, "drive name and size"); err != nil {
			return err
		}
		p, q := drivePath(rest[0], "resize")
		if err := c.do(a.ctx, http.MethodPut, p, q, map[string]string{"size": rest[1]}, nil); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "%s: resize to %s requested (the file system grows on the next start)\n", rest[0], rest[1])
		return nil
	case "snapshot":
		fs := a.newFlags("drives snapshot", "drives snapshot DRIVE [--name NAME]")
		name := fs.String("name", "", "snapshot name (default DRIVE-<timestamp>)")
		pos, err := parseMixed(fs, rest)
		if err != nil {
			return err
		}
		if err := need(pos, 1, "drive name"); err != nil {
			return err
		}
		p, q := drivePath(pos[0], "snapshots")
		var snap Snapshot
		if err := c.do(a.ctx, http.MethodPost, p, q, map[string]string{"name": *name}, &snap); err != nil {
			return err
		}
		if a.out == "json" {
			return printJSON(a.stdout, snap)
		}
		fmt.Fprintf(a.stdout, "snapshot %s of %s requested (ready: %v)\n", snap.Name, pos[0], snap.ReadyToUse)
		return nil
	case "snapshots":
		if err := need(rest, 1, "drive name"); err != nil {
			return err
		}
		p, q := drivePath(rest[0], "snapshots")
		var resp struct {
			Snapshots []Snapshot `json:"snapshots"`
		}
		if err := c.do(a.ctx, http.MethodGet, p, q, nil, &resp); err != nil {
			return err
		}
		if a.out == "json" {
			return printJSON(a.stdout, resp)
		}
		printSnapshots(a, resp.Snapshots)
		return nil
	case "restore":
		if err := need(rest, 2, "snapshot name and new workspace name"); err != nil {
			return err
		}
		return cmdCreate(a, append([]string{rest[1], "--from-snapshot", rest[0]}, rest[2:]...))
	case "create-shared":
		fs := a.newFlags("drives create-shared", "drives create-shared NAME [--size 100Gi] [--storage CHOICE]")
		size := fs.String("size", "100Gi", "size")
		storage := fs.String("storage", "", "shared storage choice from the catalog")
		pos, err := parseMixed(fs, rest)
		if err != nil {
			return err
		}
		if err := need(pos, 1, "drive name"); err != nil {
			return err
		}
		var d Drive
		if err := c.do(a.ctx, http.MethodPost, "/api/drives", nil, map[string]string{"name": pos[0], "size": *size, "storage": *storage}, &d); err != nil {
			return err
		}
		if a.out == "json" {
			return printJSON(a.stdout, d)
		}
		fmt.Fprintf(a.stdout, "shared drive %s created (%s). Mount it:  wsctl create … --mount shared:%s   → /shared/%s\n", d.Name, d.Size, pos[0], pos[0])
		return nil
	}
	return fmt.Errorf("unknown drives subcommand %q (wsctl drives --help)", sub)
}

func printSnapshots(a *app, snaps []Snapshot) {
	if len(snaps) == 0 {
		fmt.Fprintln(a.stdout, "\nNo snapshots.")
		return
	}
	fmt.Fprintln(a.stdout, "\nSnapshots:")
	var rows [][]string
	for _, s := range snaps {
		rows = append(rows, []string{s.Name, fmt.Sprint(s.ReadyToUse), orDash(s.RestoreSize), age(s.CreatedAt)})
	}
	table(a.stdout, []string{"SNAPSHOT", "READY", "SIZE", "AGE"}, rows)
	fmt.Fprintln(a.stdout, "Restore:  wsctl drives restore SNAPSHOT NEW-WORKSPACE --lang …")
}

// ---------------------------------------------------------------- share / link

func cmdShare(a *app, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(a.stderr, `Usage:
  wsctl share add WORKSPACE USER [--key FILE]     let USER ssh into my workspace (they use: wsctl ssh me@example.com/WORKSPACE)
  wsctl share add drive:DRIVE USER [--read-only]  let USER mount my shared drive (they use: --mount shared:me@example.com/DRIVE)
  wsctl share rm WORKSPACE|drive:DRIVE USER        revoke
  wsctl share list WORKSPACE|drive:DRIVE           who has access`)
		return nil
	}
	sub, rest := args[0], args[1:]
	c, err := a.api()
	if err != nil {
		return err
	}
	base := func(target string) (string, url.Values) {
		if d, ok := strings.CutPrefix(target, "drive:"); ok {
			q := url.Values{}
			if a.ns != "" {
				q.Set("ns", a.ns)
			}
			return "/api/drives/" + url.PathEscape(d) + "/shares", q
		}
		return a.wsPath(target, "shares")
	}
	switch sub {
	case "add":
		fs := a.newFlags("share add", "share add WORKSPACE|drive:DRIVE USER [--key FILE] [--read-only]")
		keyFile := fs.String("key", "", "guest's public key file (if they have no profile yet)")
		ro := fs.Bool("read-only", false, "drive shares: mount read-only")
		pos, err := parseMixed(fs, rest)
		if err != nil {
			return err
		}
		if err := need(pos, 2, "target and user"); err != nil {
			return err
		}
		p, q := base(pos[0])
		body := map[string]any{"user": pos[1]}
		if *keyFile != "" {
			b, err := os.ReadFile(*keyFile)
			if err != nil {
				return err
			}
			body["publicKey"] = strings.TrimSpace(string(b))
		}
		if *ro {
			body["readOnly"] = true
		}
		var sh Share
		if err := c.do(a.ctx, http.MethodPost, p, q, body, &sh); err != nil {
			return err
		}
		if a.out == "json" {
			return printJSON(a.stdout, sh)
		}
		if strings.HasPrefix(pos[0], "drive:") {
			fmt.Fprintf(a.stdout, "%s can now mount %s:  wsctl create … --mount shared:<you>/%s\n", pos[1], pos[0], strings.TrimPrefix(pos[0], "drive:"))
		} else {
			fmt.Fprintf(a.stdout, "%s can now connect:  wsctl ssh <you>/%s   (their key was added; revoke with: wsctl share rm %s %s)\n", pos[1], pos[0], pos[0], pos[1])
		}
		return nil
	case "rm", "remove", "revoke":
		if err := need(rest, 2, "target and user"); err != nil {
			return err
		}
		p, q := base(rest[0])
		if err := c.do(a.ctx, http.MethodDelete, p+"/"+url.PathEscape(rest[1]), q, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "revoked %s from %s\n", rest[1], rest[0])
		return nil
	case "list", "ls":
		if err := need(rest, 1, "target"); err != nil {
			return err
		}
		p, q := base(rest[0])
		var resp struct {
			Shares []Share `json:"shares"`
		}
		if err := c.do(a.ctx, http.MethodGet, p, q, nil, &resp); err != nil {
			return err
		}
		if a.out == "json" {
			return printJSON(a.stdout, resp)
		}
		if len(resp.Shares) == 0 {
			fmt.Fprintf(a.stdout, "%s is not shared with anyone\n", rest[0])
			return nil
		}
		var rows [][]string
		for _, s := range resp.Shares {
			rows = append(rows, []string{s.User, s.Kind, orDash(s.Resource), fmt.Sprint(s.ReadOnly), age(s.CreatedAt)})
		}
		table(a.stdout, []string{"USER", "KIND", "RESOURCE", "READ-ONLY", "AGE"}, rows)
		return nil
	}
	return fmt.Errorf("unknown share subcommand %q (wsctl share --help)", sub)
}

func cmdLink(a *app, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(a.stderr, `Usage:
  wsctl link add WORKSPACE OWNER/PEER [--port 8080 --port 9090]   allow traffic between my workspace and OWNER's PEER
  wsctl link rm WORKSPACE OWNER/PEER
  wsctl link list WORKSPACE
Both sides must link (consent); until then the status is pending-peer. Workspaces in the
same namespace (yours, or a project) talk to each other without links.`)
		return nil
	}
	sub, rest := args[0], args[1:]
	c, err := a.api()
	if err != nil {
		return err
	}
	switch sub {
	case "add":
		fs := a.newFlags("link add", "link add WORKSPACE OWNER/PEER [--port N]…")
		var ports multiFlag
		fs.Var(&ports, "port", "restrict to TCP port (repeatable); default all")
		portsCSV := fs.String("ports", "", "comma-separated TCP ports (same as several --port)")
		pos, err := parseMixed(fs, rest)
		if err != nil {
			return err
		}
		if err := need(pos, 2, "workspace and peer (owner/name)"); err != nil {
			return err
		}
		if !strings.Contains(pos[1], "/") {
			return errors.New("peer must be OWNER/NAME, e.g. bob@example.com/api")
		}
		body := map[string]any{"peer": pos[1]}
		for _, p := range strings.Split(*portsCSV, ",") {
			if p = strings.TrimSpace(p); p != "" {
				ports = append(ports, p)
			}
		}
		if len(ports) > 0 {
			var ps []int
			for _, p := range ports {
				var n int
				if _, err := fmt.Sscanf(p, "%d", &n); err != nil {
					return fmt.Errorf("bad port %q", p)
				}
				ps = append(ps, n)
			}
			body["ports"] = ps
		}
		p, q := a.wsPath(pos[0], "links")
		var l Link
		if err := c.do(a.ctx, http.MethodPost, p, q, body, &l); err != nil {
			return err
		}
		if a.out == "json" {
			return printJSON(a.stdout, l)
		}
		fmt.Fprintf(a.stdout, "link %s ↔ %s: %s\n", pos[0], pos[1], l.Status)
		if l.Status == "pending-peer" {
			owner, peer := splitRef(pos[1])
			fmt.Fprintf(a.stdout, "ask %s to run:  wsctl link add %s <you>/%s\n", owner, peer, pos[0])
		}
		if l.PeerDNS != "" {
			fmt.Fprintf(a.stdout, "reach the peer at %s\n", l.PeerDNS)
		}
		return nil
	case "rm", "remove":
		if err := need(rest, 2, "workspace and peer"); err != nil {
			return err
		}
		p, q := a.wsPath(rest[0], "links")
		if err := c.do(a.ctx, http.MethodDelete, p+"/"+url.PathEscape(rest[1]), q, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "removed link %s ↔ %s\n", rest[0], rest[1])
		return nil
	case "list", "ls":
		if err := need(rest, 1, "workspace"); err != nil {
			return err
		}
		p, q := a.wsPath(rest[0], "links")
		var resp struct {
			Links []Link `json:"links"`
		}
		if err := c.do(a.ctx, http.MethodGet, p, q, nil, &resp); err != nil {
			return err
		}
		if a.out == "json" {
			return printJSON(a.stdout, resp)
		}
		if len(resp.Links) == 0 {
			fmt.Fprintf(a.stdout, "%s has no links\n", rest[0])
			return nil
		}
		var rows [][]string
		for _, l := range resp.Links {
			rows = append(rows, []string{l.Peer, orDash(l.PeerDNS), portsOrAll(l.Ports), l.Status})
		}
		table(a.stdout, []string{"PEER", "DNS", "PORTS", "STATUS"}, rows)
		return nil
	}
	return fmt.Errorf("unknown link subcommand %q (wsctl link --help)", sub)
}

// ---------------------------------------------------------------- small utils

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func sortRows(rows [][]string) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j][0] < rows[j-1][0]; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}
