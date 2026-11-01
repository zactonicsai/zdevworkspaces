package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// WorkspaceRequest is what the UI posts. Field names match the CLI's spec
// annotation so `workspace status` and the portal describe the same thing.
type WorkspaceRequest struct {
	Name          string            `json:"name"`
	Languages     []string          `json:"languages"`
	Tools         []string          `json:"tools"`
	Profile       string            `json:"profile"`
	Isolation     string            `json:"isolation"`
	Image         string            `json:"image"` // optional override (e.g. an Artifactory variant)
	Tag           string            `json:"tag"`
	Repo          string            `json:"repo"`
	Branch        string            `json:"branch"`
	Dotfiles      string            `json:"dotfiles"`
	GitAuth       string            `json:"gitAuth"` // deploy-key | token | none
	GitToken      string            `json:"gitToken"`
	GitTokenUser  string            `json:"gitTokenUser"`
	GitName       string            `json:"gitName"`
	GitEmail      string            `json:"gitEmail"`
	Size          string            `json:"size"`
	StorageClass  string            `json:"storageClass"`
	CPU           string            `json:"cpu"`
	Memory        string            `json:"memory"`
	CPURequest    string            `json:"cpuRequest"`
	MemoryRequest string            `json:"memoryRequest"`
	Multicast     bool              `json:"multicast"`
	Devices       map[string]string `json:"devices"`
	HostPaths     []string          `json:"hostPaths"`
	Caps          []string          `json:"caps"`
	Expose        bool              `json:"expose"`
	IdleTimeout   string            `json:"idleTimeout"`
	TZ            string            `json:"tz"`
	SSHPublicKeys []string          `json:"sshPublicKeys"`
}

// resolved is the request after defaults and validation; it is also what gets
// stored in the devspaces.io/spec annotation (same keys as the CLI).
type resolved struct {
	Lang             string            `json:"lang"`
	Languages        []string          `json:"languages"`
	Tools            []string          `json:"tools"`
	Profile          string            `json:"profile"`
	Isolation        string            `json:"isolation"`
	Image            string            `json:"image"`
	Repo             string            `json:"repo"`
	Branch           string            `json:"branch"`
	Dotfiles         string            `json:"dotfiles"`
	GitAuth          string            `json:"gitAuth"`
	GitName          string            `json:"gitName"`
	GitEmail         string            `json:"gitEmail"`
	Size             string            `json:"size"`
	StorageClass     string            `json:"storageClass"`
	CPU              string            `json:"cpu"`
	Memory           string            `json:"memory"`
	CPURequest       string            `json:"cpuRequest"`
	MemoryRequest    string            `json:"memoryRequest"`
	EphemeralStorage string            `json:"ephemeralStorage"`
	Multicast        bool              `json:"multicast"`
	Devices          map[string]string `json:"devices"`
	HostPaths        []string          `json:"hostPaths"`
	Caps             []string          `json:"caps"`
	Expose           bool              `json:"expose"`
	TZ               string            `json:"tz"`
	IdleTimeout      string            `json:"idleTimeout"`
	CreatedAt        string            `json:"createdAt"`
	CreatedBy        string            `json:"createdBy"`
	Tier             string            `json:"-"`
	profile          Obj
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }
func invalid(format string, a ...any) error {
	return &validationError{fmt.Sprintf(format, a...)}
}

var devRE = regexp.MustCompile(`^[a-z0-9.-]+(/[a-z0-9._-]+)?$`)

func resolveRequest(req WorkspaceRequest, cat *Catalog, user User) (*resolved, error) {
	r := &resolved{}
	// languages
	seen := map[string]bool{}
	for _, l := range req.Languages {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		if _, ok := cat.Languages[l]; !ok {
			return nil, invalid("unknown language %q", l)
		}
		seen[l] = true
		r.Languages = append(r.Languages, l)
	}
	if len(r.Languages) == 0 {
		return nil, invalid("pick at least one language (or 'base' for plain Linux)")
	}
	r.Lang = r.Languages[0]
	// tools (+ extra languages as features)
	for _, t := range req.Tools {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := cat.Tools[t]; !ok {
			return nil, invalid("unknown tool %q", t)
		}
		if !contains(r.Tools, t) {
			r.Tools = append(r.Tools, t)
		}
	}
	for _, l := range r.Languages[1:] {
		if l != "base" && !contains(r.Tools, l) {
			if _, ok := cat.Tools[l]; ok {
				r.Tools = append(r.Tools, l)
			}
		}
	}
	// name
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = r.Lang
	}
	if !nameRE.MatchString(name) {
		return nil, invalid("name %q must be lowercase letters, digits or dashes (max 40)", name)
	}
	req.Name = name
	// profile / isolation
	r.Profile = req.Profile
	if r.Profile == "" {
		r.Profile = cat.Default("profile", "standard")
	}
	prof, ok := cat.Profiles[r.Profile]
	if !ok {
		return nil, invalid("unknown profile %q", r.Profile)
	}
	r.profile = prof
	r.Tier = str(prof, "tier")
	if r.Tier == "" {
		r.Tier = "standard"
	}
	r.Isolation = req.Isolation
	if r.Isolation == "" {
		r.Isolation = str(prof, "isolation")
	}
	if r.Isolation == "" {
		r.Isolation = cat.Default("isolation", "userns")
	}
	if _, ok := cat.Isolation[r.Isolation]; !ok {
		return nil, invalid("unknown isolation %q", r.Isolation)
	}
	// image
	tag := req.Tag
	if tag == "" {
		tag = cat.Tag
	}
	if req.Image != "" {
		if strings.ContainsAny(req.Image, " \t\n\"'") {
			return nil, invalid("bad image reference")
		}
		r.Image = req.Image
	} else {
		r.Image = fmt.Sprintf("%s/%s:%s", cat.Registry, str(cat.Languages[r.Lang], "image"), tag)
	}
	// git
	r.Repo, r.Branch, r.Dotfiles = strings.TrimSpace(req.Repo), strings.TrimSpace(req.Branch), strings.TrimSpace(req.Dotfiles)
	for _, u := range []string{r.Repo, r.Branch, r.Dotfiles} {
		if strings.ContainsAny(u, " \t\n\"'`$") {
			return nil, invalid("repository, branch and dotfiles must not contain spaces or quotes")
		}
	}
	r.GitAuth = req.GitAuth
	if r.GitAuth == "" {
		r.GitAuth = "deploy-key"
	}
	switch r.GitAuth {
	case "deploy-key", "token", "none":
	default:
		return nil, invalid("gitAuth must be deploy-key, token or none")
	}
	if r.GitAuth == "token" && strings.TrimSpace(req.GitToken) == "" {
		return nil, invalid("a token is required for gitAuth=token")
	}
	r.GitName, r.GitEmail = strings.TrimSpace(req.GitName), strings.TrimSpace(req.GitEmail)
	// sizes
	r.Size = orDefault(req.Size, cat.Default("size", "30Gi"))
	r.StorageClass = orDefault(req.StorageClass, cat.Default("storageClass", ""))
	r.CPU = orDefault(req.CPU, cat.Default("cpu", "4"))
	r.Memory = orDefault(req.Memory, cat.Default("memory", "8Gi"))
	r.CPURequest = orDefault(req.CPURequest, cat.Default("cpuRequest", "1"))
	r.MemoryRequest = orDefault(req.MemoryRequest, cat.Default("memoryRequest", "2Gi"))
	r.EphemeralStorage = cat.Default("ephemeralStorage", "20Gi")
	for _, q := range []string{r.Size, r.CPU, r.Memory, r.CPURequest, r.MemoryRequest} {
		if !quantityRE.MatchString(q) {
			return nil, invalid("bad quantity %q (examples: 2, 500m, 8Gi)", q)
		}
	}
	// hardware
	r.Multicast, r.Expose = req.Multicast, req.Expose
	r.Devices = map[string]string{}
	for res, q := range req.Devices {
		if !devRE.MatchString(res) || !quantityRE.MatchString(q) {
			return nil, invalid("bad device request %s=%s", res, q)
		}
		r.Devices[res] = q
	}
	for _, p := range req.HostPaths {
		if !strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
			return nil, invalid("host paths must be absolute")
		}
		r.HostPaths = append(r.HostPaths, p)
	}
	for _, c := range req.Caps {
		c = strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(c), "CAP_"))
		if !regexp.MustCompile(`^[A-Z_]+$`).MatchString(c) {
			return nil, invalid("bad capability %q", c)
		}
		r.Caps = append(r.Caps, c)
	}
	if r.Tier != "lab" && (len(r.Devices) > 0 || len(r.HostPaths) > 0 || len(r.Caps) > 0) {
		return nil, invalid("devices, host paths and extra capabilities need the hardware or kernel profile")
	}
	// misc
	r.TZ = strings.TrimSpace(req.TZ)
	r.IdleTimeout = orDefault(req.IdleTimeout, cat.Default("idleTimeout", "4h"))
	if _, err := parseDuration(r.IdleTimeout); err != nil {
		return nil, invalid("%v", err)
	}
	r.CreatedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	r.CreatedBy = user.Name
	if r.Devices == nil {
		r.Devices = map[string]string{}
	}
	if r.HostPaths == nil {
		r.HostPaths = []string{}
	}
	if r.Caps == nil {
		r.Caps = []string{}
	}
	if r.Tools == nil {
		r.Tools = []string{}
	}
	return r, nil
}

func orDefault(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return strings.TrimSpace(v)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func gitHost(u string) string {
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		if m := regexp.MustCompile(`^(?:[\w.-]+@)?([\w.-]+):`).FindStringSubmatch(u); m != nil {
			return m[1]
		}
		return ""
	}
	rest := u[strings.Index(u, "://")+3:]
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndexByte(rest, '@'); i >= 0 {
		rest = rest[i+1:]
	}
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func repoDirname(u string) string {
	tail := strings.TrimRight(u, "/")
	tail = regexp.MustCompile(`\.git/?$`).ReplaceAllString(tail, "")
	if i := strings.LastIndexByte(tail, '/'); i >= 0 {
		tail = tail[i+1:]
	}
	if i := strings.LastIndexByte(tail, ':'); i >= 0 {
		tail = tail[i+1:]
	}
	tail = regexp.MustCompile(`[^A-Za-z0-9._-]`).ReplaceAllString(tail, "-")
	if tail == "" {
		return "repo"
	}
	return tail
}

// artifactSpecs mirrors the CLI: catalog tools with an `artifact` block become
// download instructions for /opt/workspace/bin/install-artifact.
func artifactSpecs(cat *Catalog, tools []string) []Obj {
	out := []Obj{}
	base := strings.TrimRight(cat.Artifactory.URL, "/")
	for _, t := range tools {
		a, _ := cat.Tools[t]["artifact"].(Obj)
		if a == nil {
			continue
		}
		repoKey := str(a, "repo")
		if repoKey == "" {
			repoKey = "generic"
		}
		repo := cat.Artifactory.Repos[repoKey]
		if repo == "" {
			repo = repoKey
		}
		version := fmt.Sprint(a["version"])
		if a["version"] == nil {
			version = ""
		}
		path := strings.ReplaceAll(str(a, "path"), "{version}", version)
		// A repo value that is itself a URL is used verbatim (a mirror elsewhere, or the
		// public site in a demo without Artifactory); otherwise it is a repo under `url`.
		repoBase := strings.TrimRight(repo, "/")
		if !strings.Contains(repo, "://") {
			parts := []string{}
			for _, x := range []string{base, repo} {
				if x != "" {
					parts = append(parts, x)
				}
			}
			repoBase = strings.Join(parts, "/")
		}
		u := str(a, "url")
		if u == "" {
			u = repoBase + "/" + path
		}
		spec := Obj{"name": t, "url": u, "install": orDefault(str(a, "install"), "tar")}
		if bins, ok := a["bin"].([]any); ok {
			spec["bin"] = bins
		} else {
			spec["bin"] = []any{t}
		}
		if a["strip"] != nil {
			spec["strip"] = a["strip"]
		}
		if a["sha256"] != nil {
			spec["sha256"] = a["sha256"]
		}
		out = append(out, spec)
	}
	return out
}

const (
	labelWS      = "devspaces.io/workspace"
	labelLang    = "devspaces.io/lang"
	labelProfile = "devspaces.io/profile"
	labelTier    = "devspaces.io/tier"
	labelManaged = "app.kubernetes.io/managed-by"
	annIdle      = "devspaces.io/idle-timeout"
	annActive    = "devspaces.io/last-active"
	annSpec      = "devspaces.io/spec"
	annNetworks  = "k8s.v1.cni.cncf.io/networks"
	sshPort      = 2222
	devGID       = 1000
)

type renderInputs struct {
	Namespace      string
	Name           string
	AuthorizedKeys []string
	HostKey        keypair
	GitSecret      Obj // stringData, nil to keep an existing Secret
}

// renderWorkspace is a line-for-line port of the CLI's render_workspace, so a
// workspace created in the portal and one created from the terminal are the
// same objects (and either tool can manage the other's).
func renderWorkspace(in renderInputs, r *resolved, cat *Catalog) []Obj {
	ns, name := in.Namespace, in.Name
	obj := "ws-" + name
	lbl := Obj{labelWS: name, labelLang: r.Lang, labelProfile: r.Profile, labelTier: r.Tier, labelManaged: "devspaces-portal"}
	sel := Obj{labelWS: name}
	meta := func(n string) Obj { return Obj{"name": n, "namespace": ns, "labels": deepCopy(lbl)} }
	var objs []Obj

	pvc := Obj{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": meta(obj + "-home"),
		"spec": Obj{"accessModes": []any{"ReadWriteOnce"}, "resources": Obj{"requests": Obj{"storage": r.Size}}}}
	if r.StorageClass != "" {
		pvc["spec"].(Obj)["storageClassName"] = r.StorageClass
	}
	objs = append(objs, pvc)

	objs = append(objs, Obj{"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "metadata": meta(obj + "-ssh"),
		"stringData": Obj{
			"authorized_keys":          strings.Join(in.AuthorizedKeys, "\n") + "\n",
			"ssh_host_ed25519_key":     in.HostKey.Private,
			"ssh_host_ed25519_key.pub": in.HostKey.Public + "\n",
		}})
	if in.GitSecret != nil && r.GitAuth != "none" {
		objs = append(objs, Obj{"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "metadata": meta(obj + "-git"), "stringData": in.GitSecret})
	}
	objs = append(objs, Obj{"apiVersion": "v1", "kind": "Service", "metadata": meta(obj),
		"spec": Obj{"clusterIP": "None", "selector": sel, "ports": []any{Obj{"name": "ssh", "port": sshPort, "targetPort": "ssh"}}}})

	gitAuthMode := r.GitAuth
	if gitAuthMode == "deploy-key" {
		gitAuthMode = "ssh"
	}
	artifacts, _ := json.Marshal(artifactSpecs(cat, r.Tools))
	mcMode := strings.ToLower(orDefault(str(cat.Networking, "multicast", "mode"), "multus"))
	mcIface := ""
	if r.Multicast {
		mcIface = orDefault(str(cat.Networking, "multicast", "iface"), "net1")
		if mcMode == "cilium" { // eBPF datapath: multicast flows on the pod's normal interface
			mcIface = "eth0"
		}
	}
	listen := "127.0.0.1"
	if r.Expose {
		listen = "0.0.0.0"
	}
	tz := r.TZ
	if tz == "" {
		tz = "UTC"
	}
	env := []any{
		Obj{"name": "WORKSPACE_NAME", "value": name},
		Obj{"name": "WORKSPACE_NAMESPACE", "valueFrom": Obj{"fieldRef": Obj{"fieldPath": "metadata.namespace"}}},
		Obj{"name": "POD_NAME", "valueFrom": Obj{"fieldRef": Obj{"fieldPath": "metadata.name"}}},
		Obj{"name": "WORKSPACE_LANG", "value": r.Lang},
		Obj{"name": "WORKSPACE_LANGUAGES", "value": strings.Join(r.Languages, ",")},
		Obj{"name": "WORKSPACE_TOOLS", "value": strings.Join(r.Tools, ",")},
		Obj{"name": "WORKSPACE_ARTIFACTS", "value": string(artifacts)},
		Obj{"name": "WORKSPACE_PROFILE", "value": r.Profile},
		Obj{"name": "WORKSPACE_REPO", "value": r.Repo},
		Obj{"name": "WORKSPACE_REPO_DIR", "value": ifThen(r.Repo != "", repoDirname(r.Repo), "")},
		Obj{"name": "WORKSPACE_BRANCH", "value": r.Branch},
		Obj{"name": "WORKSPACE_DOTFILES", "value": r.Dotfiles},
		Obj{"name": "GIT_AUTH_MODE", "value": gitAuthMode},
		Obj{"name": "GIT_HOST", "value": gitHost(r.Repo)},
		Obj{"name": "GIT_USER_NAME", "value": r.GitName},
		Obj{"name": "GIT_USER_EMAIL", "value": r.GitEmail},
		Obj{"name": "WORKSPACE_SSH_LISTEN", "value": listen},
		Obj{"name": "WORKSPACE_MCAST_IFACE", "value": mcIface},
		Obj{"name": "TZ", "value": tz},
	}
	if cat.Artifactory.URL != "" {
		repos, _ := json.Marshal(orMap(cat.Artifactory.Repos))
		mirrors, _ := json.Marshal(orMap(cat.Artifactory.Mirrors))
		env = append(env,
			Obj{"name": "ARTIFACTORY_URL", "value": strings.TrimRight(cat.Artifactory.URL, "/")},
			Obj{"name": "ARTIFACTORY_REPOS", "value": string(repos)},
			Obj{"name": "ARTIFACTORY_MIRRORS", "value": string(mirrors)},
			Obj{"name": "ARTIFACTORY_TOKEN", "valueFrom": Obj{"secretKeyRef": Obj{"name": "workspace-profile", "key": "artifactory_token", "optional": true}}},
			Obj{"name": "ARTIFACTORY_USER", "valueFrom": Obj{"secretKeyRef": Obj{"name": "workspace-profile", "key": "artifactory_user", "optional": true}}},
		)
	}

	volumes := []any{
		Obj{"name": "home", "persistentVolumeClaim": Obj{"claimName": obj + "-home"}},
		Obj{"name": "ssh", "secret": Obj{"secretName": obj + "-ssh", "defaultMode": 0o440}},
		Obj{"name": "features-extra", "configMap": Obj{"name": "workspace-features", "optional": true, "defaultMode": 0o755}},
	}
	mounts := []any{
		Obj{"name": "home", "mountPath": "/home/dev"},
		Obj{"name": "ssh", "mountPath": "/etc/workspace/ssh", "readOnly": true},
		Obj{"name": "features-extra", "mountPath": "/opt/workspace/features.d", "readOnly": true},
	}
	if r.GitAuth != "none" {
		volumes = append(volumes, Obj{"name": "git", "secret": Obj{"secretName": obj + "-git", "defaultMode": 0o440}})
		mounts = append(mounts, Obj{"name": "git", "mountPath": "/etc/workspace/git", "readOnly": true})
	}
	hostPaths := []string{}
	if hp, ok := r.profile["hostPaths"].([]any); ok {
		for _, p := range hp {
			hostPaths = append(hostPaths, fmt.Sprint(p))
		}
	}
	hostPaths = append(hostPaths, r.HostPaths...)
	seenVol := map[string]bool{}
	for _, p := range hostPaths {
		vname := "host-" + strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(strings.Trim(p, "/")), "-"), "-")
		if vname == "host-" {
			vname = "host-root"
		}
		if seenVol[vname] {
			continue
		}
		seenVol[vname] = true
		ro := p == "/lib/modules" || p == "/usr/src" || p == "/lib/firmware"
		volumes = append(volumes, Obj{"name": vname, "hostPath": Obj{"path": p}})
		mounts = append(mounts, Obj{"name": vname, "mountPath": p, "readOnly": ro})
	}

	requests := Obj{"cpu": r.CPURequest, "memory": r.MemoryRequest, "ephemeral-storage": r.EphemeralStorage}
	limits := Obj{"cpu": r.CPU, "memory": r.Memory, "ephemeral-storage": r.EphemeralStorage}
	devKeys := make([]string, 0, len(r.Devices))
	for k := range r.Devices {
		devKeys = append(devKeys, k)
	}
	sort.Strings(devKeys)
	for _, res := range devKeys {
		requests[res], limits[res] = r.Devices[res], r.Devices[res]
		if strings.HasPrefix(res, "hugepages-") && !seenVol["hugepages"] {
			seenVol["hugepages"] = true
			volumes = append(volumes, Obj{"name": "hugepages", "emptyDir": Obj{"medium": "HugePages"}})
			mounts = append(mounts, Obj{"name": "hugepages", "mountPath": "/dev/hugepages"})
		}
	}

	var csc Obj
	if b, _ := r.profile["privileged"].(bool); b {
		csc = Obj{"privileged": true, "runAsUser": 0}
	} else {
		capSet := map[string]bool{}
		if caps, ok := r.profile["capabilities"].([]any); ok {
			for _, c := range caps {
				capSet[fmt.Sprint(c)] = true
			}
		}
		for _, c := range r.Caps {
			capSet[c] = true
		}
		caps := make([]any, 0, len(capSet))
		keys := make([]string, 0, len(capSet))
		for c := range capSet {
			keys = append(keys, c)
		}
		sort.Strings(keys)
		for _, c := range keys {
			caps = append(caps, c)
		}
		csc = Obj{"runAsUser": 0, "allowPrivilegeEscalation": true, "capabilities": Obj{"drop": []any{"ALL"}, "add": caps}}
	}

	probe := []any{"bash", "-c", fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d", sshPort)}
	container := Obj{
		"name": "workspace", "image": r.Image, "imagePullPolicy": "IfNotPresent", "env": env,
		"ports":           []any{Obj{"name": "ssh", "containerPort": sshPort, "protocol": "TCP"}},
		"resources":       Obj{"requests": requests, "limits": limits},
		"securityContext": csc,
		"volumeMounts":    mounts,
		"readinessProbe":  Obj{"exec": Obj{"command": probe}, "initialDelaySeconds": 2, "periodSeconds": 5},
		"livenessProbe":   Obj{"exec": Obj{"command": probe}, "initialDelaySeconds": 30, "periodSeconds": 30, "failureThreshold": 5},
	}
	sysctls := []any{
		Obj{"name": "net.ipv4.ping_group_range", "value": "0 2147483647"},
		Obj{"name": "net.ipv4.ip_unprivileged_port_start", "value": "0"},
	}
	if extra, ok := r.profile["sysctls"].([]any); ok {
		sysctls = append(sysctls, extra...)
	}
	podSpec := Obj{
		"serviceAccountName": "workspace", "automountServiceAccountToken": true, "terminationGracePeriodSeconds": 30,
		"securityContext": Obj{"fsGroup": devGID, "fsGroupChangePolicy": "OnRootMismatch",
			"seccompProfile": Obj{"type": "RuntimeDefault"}, "sysctls": sysctls},
		"containers": []any{container}, "volumes": volumes,
	}
	iso := cat.Isolation[r.Isolation]
	if hu, ok := iso["hostUsers"]; ok {
		podSpec["hostUsers"] = hu
	}
	if rc := str(iso, "runtimeClass"); rc != "" {
		podSpec["runtimeClassName"] = rc
	}
	if ns := r.profile["nodeSelector"]; ns != nil {
		podSpec["nodeSelector"] = ns
	}
	if tol := r.profile["tolerations"]; tol != nil {
		podSpec["tolerations"] = tol
	}
	if ex, _ := r.profile["exclusiveNode"].(bool); ex {
		podSpec["affinity"] = Obj{"podAntiAffinity": Obj{"requiredDuringSchedulingIgnoredDuringExecution": []any{
			Obj{"topologyKey": "kubernetes.io/hostname", "namespaceSelector": Obj{},
				"labelSelector": Obj{"matchExpressions": []any{Obj{"key": labelTier, "operator": "In", "values": []any{"lab"}}}}}}}}
	}
	if cat.PullSecret != "" {
		podSpec["imagePullSecrets"] = []any{Obj{"name": cat.PullSecret}}
	}
	tplMeta := Obj{"labels": deepCopy(lbl)}
	if r.Multicast && mcMode == "multus" { // cilium mode needs no extra interface
		tplMeta["annotations"] = Obj{annNetworks: str(cat.Networking, "multicast", "nad")}
	}
	specJSON, _ := json.Marshal(r)
	sts := Obj{"apiVersion": "apps/v1", "kind": "StatefulSet",
		"metadata": Obj{"name": obj, "namespace": ns, "labels": deepCopy(lbl),
			"annotations": Obj{annIdle: r.IdleTimeout, annSpec: string(specJSON)}},
		"spec": Obj{"serviceName": obj, "replicas": 1, "selector": Obj{"matchLabels": sel},
			"updateStrategy": Obj{"type": "RollingUpdate"},
			"template":       Obj{"metadata": tplMeta, "spec": podSpec}}}
	objs = append(objs, sts)

	if r.Expose {
		objs = append(objs,
			Obj{"apiVersion": "v1", "kind": "Service", "metadata": meta(obj + "-public"),
				"spec": Obj{"type": "LoadBalancer", "selector": sel, "ports": []any{Obj{"name": "ssh", "port": 22, "targetPort": "ssh"}}}},
			Obj{"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": meta(obj + "-allow-ssh"),
				"spec": Obj{"podSelector": Obj{"matchLabels": sel}, "policyTypes": []any{"Ingress"},
					"ingress": []any{Obj{"ports": []any{Obj{"port": sshPort, "protocol": "TCP"}}}}}})
	}
	return objs
}

func ifThen(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

func orMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
