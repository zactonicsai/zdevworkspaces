package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type server struct {
	ids     *identityResolver
	kube    func(User) Kube // real client impersonating the user, or the mock
	cats    *catalogCache
	af      *artifactory
	canLab  func(ctx context.Context, k Kube, u User) bool
	version string
}

// ---------------------------------------------------------------- helpers

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var ve *validationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, 400, Obj{"error": ve.msg})
	case errors.Is(err, errNotFound):
		writeJSON(w, 404, Obj{"error": "not found"})
	case errors.Is(err, errForbidden):
		writeJSON(w, 403, Obj{"error": "your Kubernetes account is not allowed to do that: " + err.Error()})
	default:
		log.Printf("error: %v", err)
		writeJSON(w, 502, Obj{"error": err.Error()})
	}
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalid("bad request body: %v", err)
	}
	return nil
}

type ctxKey int

const userKey ctxKey = 1

func userFrom(r *http.Request) User { u, _ := r.Context().Value(userKey).(User); return u }

// ---------------------------------------------------------------- /api/me

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	k := s.kube(u)
	cat, source := s.cats.get(r.Context(), k)
	std := u.Namespace("standard")
	_, err := k.Get(r.Context(), "/api/v1/namespaces/"+std)
	onboarded := err == nil
	writeJSON(w, 200, Obj{
		"user": u, "namespace": std, "labNamespace": u.Namespace("lab"),
		"onboarded": onboarded, "canLab": s.canLab(r.Context(), k, u),
		"catalogSource": source, "artifactory": s.af.enabled(), "artifactoryUrl": cat.Artifactory.URL,
		"version": s.version, "mock": source == "embedded" && s.af == nil,
	})
}

// ---------------------------------------------------------------- /api/catalog

func (s *server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	cat, source := s.cats.get(r.Context(), s.kube(userFrom(r)))
	out := cat.Public()
	out["source"] = source
	writeJSON(w, 200, out)
}

func (s *server) handleImages(w http.ResponseWriter, r *http.Request) {
	cat, _ := s.cats.get(r.Context(), s.kube(userFrom(r)))
	resp := Obj{"registry": cat.Registry, "defaultTag": cat.Tag, "source": "catalog", "images": Obj{}}
	images := map[string][]string{}
	for _, l := range cat.Languages {
		images[str(l, "image")] = []string{cat.Tag}
	}
	if s.af.enabled() {
		if live, err := s.af.Images(r.Context(), cat.Artifactory.Repos["docker"], "workspace-"); err == nil && len(live) > 0 {
			for img, tags := range live {
				images[img] = tags
			}
			resp["source"] = "artifactory"
		} else if err != nil {
			resp["warning"] = "Artifactory image listing unavailable: " + err.Error()
		}
	}
	resp["images"] = images
	writeJSON(w, 200, resp)
}

func (s *server) handleTools(w http.ResponseWriter, r *http.Request) {
	cat, _ := s.cats.get(r.Context(), s.kube(userFrom(r)))
	type toolView struct {
		Name         string            `json:"name"`
		Category     string            `json:"category"`
		Description  string            `json:"description"`
		Docs         string            `json:"docs,omitempty"`
		Builtin      bool              `json:"builtin"`
		Source       string            `json:"source"` // image-feature | artifactory
		Artifact     Obj               `json:"artifact,omitempty"`
		Availability *toolAvailability `json:"availability,omitempty"`
	}
	var avail map[string]toolAvailability
	if s.af.enabled() {
		avail = s.af.Tools(r.Context(), cat)
	}
	var out []toolView
	for name, t := range cat.Tools {
		if strings.HasPrefix(name, "$") {
			continue
		}
		tv := toolView{Name: name, Category: orDefault(str(t, "category"), "other"), Description: str(t, "description"),
			Docs: str(t, "docs"), Source: "image-feature"}
		tv.Builtin, _ = t["builtin"].(bool)
		if a, ok := t["artifact"].(Obj); ok {
			tv.Source, tv.Artifact = "artifactory", a
			if avail != nil {
				if av, ok := avail[name]; ok {
					tv.Availability = &av
				}
			}
		}
		out = append(out, tv)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, 200, Obj{"tools": out, "artifactory": s.af.enabled(), "artifactoryUrl": cat.Artifactory.URL})
}

// ---------------------------------------------------------------- workspaces

type wsView struct {
	Name        string   `json:"name"`
	Namespace   string   `json:"namespace"`
	Tier        string   `json:"tier"`
	Status      string   `json:"status"` // Stopped | Starting | Running | Pending | Failed …
	Ready       bool     `json:"ready"`
	Languages   []string `json:"languages"`
	Tools       []string `json:"tools"`
	Profile     string   `json:"profile"`
	Image       string   `json:"image"`
	Node        string   `json:"node,omitempty"`
	IdleTimeout string   `json:"idleTimeout"`
	LastActive  string   `json:"lastActive,omitempty"`
	StartedAt   string   `json:"startedAt,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	Waiting     string   `json:"waiting,omitempty"`
	Spec        Obj      `json:"spec"`
	Expose      bool     `json:"expose"`
	PublicAddr  string   `json:"publicAddr,omitempty"`
}

func (s *server) listNamespace(ctx context.Context, k Kube, ns string) ([]wsView, error) {
	sts, err := k.List(ctx, nsPath("apps", "v1", "statefulsets", ns, ""), labelWS)
	if err != nil {
		if errors.Is(err, errNotFound) || errors.Is(err, errForbidden) {
			return nil, nil // namespace not onboarded
		}
		return nil, err
	}
	pods, _ := k.List(ctx, nsPath("", "v1", "pods", ns, ""), labelWS)
	podBy := map[string]Obj{}
	for _, p := range pods {
		podBy[str(p, "metadata", "name")] = p
	}
	var out []wsView
	for _, st := range sts {
		v := s.view(st, podBy[str(st, "metadata", "name")+"-0"])
		if v.Expose {
			if svc, err := k.Get(ctx, nsPath("", "v1", "services", ns, str(st, "metadata", "name")+"-public")); err == nil {
				if ing, ok := getIn(svc, "status", "loadBalancer", "ingress").([]any); ok && len(ing) > 0 {
					first, _ := ing[0].(Obj)
					v.PublicAddr = orDefault(str(first, "ip"), str(first, "hostname"))
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *server) view(st, pod Obj) wsView {
	l, a := labels(st), annotations(st)
	v := wsView{Name: l[labelWS], Namespace: str(st, "metadata", "namespace"), Tier: l[labelTier],
		Profile: l[labelProfile], IdleTimeout: a[annIdle], Spec: Obj{}}
	_ = json.Unmarshal([]byte(a[annSpec]), &v.Spec)
	v.Image = str(v.Spec, "image")
	v.CreatedAt = str(v.Spec, "createdAt")
	v.Expose, _ = v.Spec["expose"].(bool)
	if langs, ok := v.Spec["languages"].([]any); ok {
		for _, x := range langs {
			v.Languages = append(v.Languages, fmt.Sprint(x))
		}
	} else if lang := str(v.Spec, "lang"); lang != "" {
		v.Languages = []string{lang}
	}
	if tools, ok := v.Spec["tools"].([]any); ok {
		for _, x := range tools {
			v.Tools = append(v.Tools, fmt.Sprint(x))
		}
	}
	if v.Tools == nil {
		v.Tools = []string{}
	}
	switch {
	case num(st, "spec", "replicas") == 0:
		v.Status = "Stopped"
	case num(st, "status", "readyReplicas") > 0:
		v.Status, v.Ready = "Running", true
	case pod != nil:
		v.Status = orDefault(str(pod, "status", "phase"), "Starting")
		if v.Status == "Running" {
			v.Status = "Starting" // running but not Ready yet
		}
	default:
		v.Status = "Starting"
	}
	if pod != nil {
		v.Node = str(pod, "spec", "nodeName")
		v.StartedAt = str(pod, "status", "startTime")
		v.LastActive = annotations(pod)[annActive]
		if css, ok := getIn(pod, "status", "containerStatuses").([]any); ok {
			for _, c := range css {
				if wtg, ok := getIn(c.(Obj), "state", "waiting").(Obj); ok {
					v.Waiting = strings.TrimSpace(str(wtg, "reason") + " " + str(wtg, "message"))
					if str(wtg, "reason") == "CrashLoopBackOff" || str(wtg, "reason") == "ImagePullBackOff" || str(wtg, "reason") == "ErrImagePull" {
						v.Status = "Failed"
					}
				}
			}
		}
		if v.Status == "Pending" {
			if conds, ok := getIn(pod, "status", "conditions").([]any); ok {
				for _, c := range conds {
					co := c.(Obj)
					if str(co, "type") == "PodScheduled" && str(co, "status") == "False" {
						v.Waiting = str(co, "message")
					}
				}
			}
		}
	}
	return v
}

func (s *server) handleList(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	k := s.kube(u)
	var all []wsView
	for _, ns := range []string{u.Namespace("standard"), u.Namespace("lab")} {
		items, err := s.listNamespace(r.Context(), k, ns)
		if err != nil {
			writeErr(w, err)
			return
		}
		all = append(all, items...)
	}
	if all == nil {
		all = []wsView{}
	}
	writeJSON(w, 200, Obj{"workspaces": all})
}

// find locates a workspace by name in either of the user's namespaces.
func (s *server) find(ctx context.Context, k Kube, u User, name string) (Obj, string, error) {
	if !nameRE.MatchString(name) {
		return nil, "", invalid("bad workspace name")
	}
	for _, ns := range []string{u.Namespace("standard"), u.Namespace("lab")} {
		st, err := k.Get(ctx, nsPath("apps", "v1", "statefulsets", ns, "ws-"+name))
		if err == nil {
			return st, ns, nil
		}
		if !errors.Is(err, errNotFound) && !errors.Is(err, errForbidden) {
			return nil, "", err
		}
	}
	return nil, "", errNotFound
}

func (s *server) handleGet(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	st, ns, err := s.find(r.Context(), k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	pod, _ := k.Get(r.Context(), nsPath("", "v1", "pods", ns, "ws-"+name+"-0"))
	v := s.view(st, pod)
	resp := Obj{"workspace": v}
	// connection details: host key (for known_hosts) and authorised keys
	if sec, err := k.Get(r.Context(), nsPath("", "v1", "secrets", ns, "ws-"+name+"-ssh")); err == nil {
		hk := decodeB64(str(sec, "data", "ssh_host_ed25519_key.pub"))
		ak := decodeB64(str(sec, "data", "authorized_keys"))
		var keys []Obj
		for _, line := range strings.Split(strings.TrimSpace(ak), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				f := strings.Fields(line)
				comment := ""
				if len(f) > 2 {
					comment = strings.Join(f[2:], " ")
				}
				keys = append(keys, Obj{"type": f[0], "fingerprint": fingerprint(line), "comment": comment})
			}
		}
		if keys == nil {
			keys = []Obj{}
		}
		hkf := strings.Fields(strings.TrimSpace(hk))
		known := ""
		if len(hkf) >= 2 {
			known = "ws-" + name + " " + hkf[0] + " " + hkf[1]
		}
		resp["ssh"] = Obj{"hostKeyFingerprint": fingerprint(hk), "knownHostsLine": known, "authorizedKeys": keys,
			"alias": "ws-" + name, "user": "dev", "port": sshPort}
	}
	if v.Spec["gitAuth"] == "deploy-key" {
		if sec, err := k.Get(r.Context(), nsPath("", "v1", "secrets", ns, "ws-"+name+"-git")); err == nil {
			resp["deployKey"] = strings.TrimSpace(decodeB64(str(sec, "data", "ssh_key.pub")))
		}
	}
	if v.Expose {
		if svc, err := k.Get(r.Context(), nsPath("", "v1", "services", ns, "ws-"+name+"-public")); err == nil {
			if ing, ok := getIn(svc, "status", "loadBalancer", "ingress").([]any); ok && len(ing) > 0 {
				first, _ := ing[0].(Obj)
				resp["publicAddr"] = orDefault(str(first, "ip"), str(first, "hostname"))
			}
		}
	}
	writeJSON(w, 200, resp)
}

func decodeB64(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *server) handleCreate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	k := s.kube(u)
	var req WorkspaceRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	cat, _ := s.cats.get(r.Context(), k)
	res, err := resolveRequest(req, cat, u)
	if err != nil {
		writeErr(w, err)
		return
	}
	ns := u.Namespace(res.Tier)
	if _, err := k.Get(r.Context(), "/api/v1/namespaces/"+ns); err != nil {
		if errors.Is(err, errNotFound) || errors.Is(err, errForbidden) {
			hint := "ask a platform admin to run: workspace admin onboard " + u.Name
			if res.Tier == "lab" {
				hint += " --lab"
			}
			writeErr(w, invalid("your namespace %s does not exist yet — %s", ns, hint))
			return
		}
		writeErr(w, err)
		return
	}
	name := orDefault(req.Name, res.Lang)
	obj := "ws-" + name

	// SSH keys: request + profile; sshd needs at least one or nobody can log in.
	keys := []string{}
	for _, kline := range req.SSHPublicKeys {
		if norm, ok := validPublicKey(kline); ok {
			keys = append(keys, norm)
		} else if strings.TrimSpace(kline) != "" {
			writeErr(w, invalid("that does not look like an SSH public key (expected e.g. 'ssh-ed25519 AAAA… comment')"))
			return
		}
	}
	for _, kline := range s.profileKeys(r.Context(), k, u) {
		if !contains(keys, kline) {
			keys = append(keys, kline)
		}
	}
	// keep keys the CLI already registered on an existing workspace
	if sec, err := k.Get(r.Context(), nsPath("", "v1", "secrets", ns, obj+"-ssh")); err == nil {
		for _, line := range strings.Split(decodeB64(str(sec, "data", "authorized_keys")), "\n") {
			if norm, ok := validPublicKey(line); ok && !contains(keys, norm) {
				keys = append(keys, norm)
			}
		}
	}
	if len(keys) == 0 {
		writeErr(w, invalid("add an SSH public key first (Profile page, or paste one in the Connect step) — without one nobody can log in"))
		return
	}

	// host key: reuse so VS Code never sees a changed key
	var hostKey keypair
	if sec, err := k.Get(r.Context(), nsPath("", "v1", "secrets", ns, obj+"-ssh")); err == nil &&
		str(sec, "data", "ssh_host_ed25519_key") != "" {
		hostKey = keypair{Private: decodeB64(str(sec, "data", "ssh_host_ed25519_key")),
			Public: strings.TrimSpace(decodeB64(str(sec, "data", "ssh_host_ed25519_key.pub")))}
	} else if hostKey, err = generateKeypair("host:" + ns + "/" + obj); err != nil {
		writeErr(w, err)
		return
	}

	// git credentials
	var gitSecret Obj
	deployPub := ""
	switch res.GitAuth {
	case "deploy-key":
		if sec, err := k.Get(r.Context(), nsPath("", "v1", "secrets", ns, obj+"-git")); err == nil && str(sec, "data", "ssh_key.pub") != "" {
			deployPub = strings.TrimSpace(decodeB64(str(sec, "data", "ssh_key.pub")))
			gitSecret = nil // keep
		} else {
			kp, err := generateKeypair("workspace:" + ns + "/" + obj)
			if err != nil {
				writeErr(w, err)
				return
			}
			gitSecret = Obj{"ssh_key": kp.Private, "ssh_key.pub": kp.Public}
			deployPub = kp.Public
		}
	case "token":
		gitSecret = Obj{"token": strings.TrimSpace(req.GitToken), "username": orDefault(req.GitTokenUser, "x-access-token")}
	}

	objs := renderWorkspace(renderInputs{Namespace: ns, Name: name, AuthorizedKeys: keys, HostKey: hostKey, GitSecret: gitSecret}, res, cat)
	for _, o := range objs {
		if o["kind"] == "PersistentVolumeClaim" {
			if _, err := k.Get(r.Context(), pathFor(o)); err == nil {
				continue // immutable; keep the developer's volume
			}
		}
		if err := k.Apply(r.Context(), pathFor(o), o); err != nil {
			writeErr(w, fmt.Errorf("apply %s/%s: %w", o["kind"], str(o, "metadata", "name"), err))
			return
		}
	}
	log.Printf("user=%s created workspace %s/%s image=%s tools=%v", u.Name, ns, name, res.Image, res.Tools)
	writeJSON(w, 201, Obj{"name": name, "namespace": ns, "deployKey": deployPub, "spec": res,
		"hostKeyFingerprint": fingerprint(hostKey.Public)})
}

func (s *server) profileKeys(ctx context.Context, k Kube, u User) []string {
	sec, err := k.Get(ctx, nsPath("", "v1", "secrets", u.Namespace("standard"), "workspace-profile"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(decodeB64(str(sec, "data", "authorized_keys")), "\n") {
		if norm, ok := validPublicKey(line); ok {
			out = append(out, norm)
		}
	}
	return out
}

func (s *server) handleScale(w http.ResponseWriter, r *http.Request, name string, replicas int) {
	u := userFrom(r)
	k := s.kube(u)
	_, ns, err := s.find(r.Context(), k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := k.MergePatch(r.Context(), nsPath("apps", "v1", "statefulsets", ns, "ws-"+name)+"/scale", Obj{"spec": Obj{"replicas": replicas}}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Obj{"ok": true, "replicas": replicas})
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	_, ns, err := s.find(r.Context(), k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	purge := r.URL.Query().Get("purge") == "true"
	obj := "ws-" + name
	paths := []string{
		nsPath("apps", "v1", "statefulsets", ns, obj),
		nsPath("", "v1", "services", ns, obj),
		nsPath("", "v1", "services", ns, obj+"-public"),
		nsPath("networking.k8s.io", "v1", "networkpolicies", ns, obj+"-allow-ssh"),
		nsPath("", "v1", "secrets", ns, obj+"-ssh"),
		nsPath("", "v1", "secrets", ns, obj+"-git"),
	}
	if purge {
		paths = append(paths, nsPath("", "v1", "persistentvolumeclaims", ns, obj+"-home"))
	}
	for _, p := range paths {
		if err := k.Delete(r.Context(), p); err != nil {
			writeErr(w, err)
			return
		}
	}
	log.Printf("user=%s deleted workspace %s/%s purge=%v", u.Name, ns, name, purge)
	writeJSON(w, 200, Obj{"ok": true, "purged": purge})
}

func (s *server) handleIdle(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	var body struct {
		IdleTimeout string `json:"idleTimeout"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := parseDuration(body.IdleTimeout); err != nil {
		writeErr(w, invalid("%v", err))
		return
	}
	_, ns, err := s.find(r.Context(), k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := k.MergePatch(r.Context(), nsPath("apps", "v1", "statefulsets", ns, "ws-"+name),
		Obj{"metadata": Obj{"annotations": Obj{annIdle: body.IdleTimeout}}}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Obj{"ok": true, "idleTimeout": body.IdleTimeout})
}

func (s *server) handleAddKey(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	var body struct {
		PublicKey string `json:"publicKey"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	norm, ok := validPublicKey(body.PublicKey)
	if !ok {
		writeErr(w, invalid("that does not look like an SSH public key"))
		return
	}
	_, ns, err := s.find(r.Context(), k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	p := nsPath("", "v1", "secrets", ns, "ws-"+name+"-ssh")
	sec, err := k.Get(r.Context(), p)
	if err != nil {
		writeErr(w, err)
		return
	}
	existing := strings.TrimSpace(decodeB64(str(sec, "data", "authorized_keys")))
	if strings.Contains(existing, norm) {
		writeJSON(w, 200, Obj{"ok": true, "added": false})
		return
	}
	if err := k.MergePatch(r.Context(), p, Obj{"stringData": Obj{"authorized_keys": existing + "\n" + norm + "\n"}}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Obj{"ok": true, "added": true, "fingerprint": fingerprint(norm)})
}

func (s *server) handleLogs(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	_, ns, err := s.find(r.Context(), k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 || tail > 2000 {
		tail = 200
	}
	logs, err := k.Logs(r.Context(), ns, "ws-"+name+"-0", "workspace", tail)
	if err != nil {
		if errors.Is(err, errNotFound) {
			writeJSON(w, 200, Obj{"logs": "", "note": "workspace is stopped; start it to see boot logs"})
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Obj{"logs": logs})
}

// ---------------------------------------------------------------- profile

type profile struct {
	SSHPublicKeys       []string `json:"sshPublicKeys"`
	GitName             string   `json:"gitName"`
	GitEmail            string   `json:"gitEmail"`
	ArtifactoryUser     string   `json:"artifactoryUser"`
	HasArtifactoryToken bool     `json:"hasArtifactoryToken"`
	ArtifactoryToken    string   `json:"artifactoryToken,omitempty"` // write-only
}

func (s *server) handleProfile(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	k := s.kube(u)
	p := nsPath("", "v1", "secrets", u.Namespace("standard"), "workspace-profile")
	switch r.Method {
	case http.MethodGet:
		out := profile{SSHPublicKeys: []string{}}
		if sec, err := k.Get(r.Context(), p); err == nil {
			for _, line := range strings.Split(decodeB64(str(sec, "data", "authorized_keys")), "\n") {
				if norm, ok := validPublicKey(line); ok {
					out.SSHPublicKeys = append(out.SSHPublicKeys, norm)
				}
			}
			out.GitName = decodeB64(str(sec, "data", "git_name"))
			out.GitEmail = decodeB64(str(sec, "data", "git_email"))
			out.ArtifactoryUser = decodeB64(str(sec, "data", "artifactory_user"))
			out.HasArtifactoryToken = str(sec, "data", "artifactory_token") != ""
		}
		writeJSON(w, 200, out)
	case http.MethodPut:
		var in profile
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		var keys []string
		for _, kline := range in.SSHPublicKeys {
			norm, ok := validPublicKey(kline)
			if !ok {
				writeErr(w, invalid("invalid SSH public key: %.40q…", kline))
				return
			}
			if !contains(keys, norm) {
				keys = append(keys, norm)
			}
		}
		data := Obj{"authorized_keys": strings.Join(keys, "\n") + "\n", "git_name": in.GitName, "git_email": in.GitEmail,
			"artifactory_user": in.ArtifactoryUser}
		if in.ArtifactoryToken != "" {
			data["artifactory_token"] = in.ArtifactoryToken
		}
		sec := Obj{"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
			"metadata": Obj{"name": "workspace-profile", "namespace": u.Namespace("standard"),
				"labels": Obj{labelManaged: "devspaces-portal"}},
			"stringData": data}
		if err := k.Apply(r.Context(), p, sec); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, Obj{"ok": true, "keys": len(keys)})
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeJSON(w, 405, Obj{"error": "method not allowed"})
	}
}

// ---------------------------------------------------------------- router

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/catalog", s.handleCatalog)
	mux.HandleFunc("/api/artifactory/images", s.handleImages)
	mux.HandleFunc("/api/artifactory/tools", s.handleTools)
	mux.HandleFunc("/api/profile", s.handleProfile)
	mux.HandleFunc("/api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleList(w, r)
		case http.MethodPost:
			s.handleCreate(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeJSON(w, 405, Obj{"error": "method not allowed"})
		}
	})
	mux.HandleFunc("/api/workspaces/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
		name, action, _ := strings.Cut(rest, "/")
		switch {
		case action == "" && r.Method == http.MethodGet:
			s.handleGet(w, r, name)
		case action == "" && r.Method == http.MethodDelete:
			s.handleDelete(w, r, name)
		case action == "start" && r.Method == http.MethodPost:
			s.handleScale(w, r, name, 1)
		case action == "stop" && r.Method == http.MethodPost:
			s.handleScale(w, r, name, 0)
		case action == "idle" && r.Method == http.MethodPut:
			s.handleIdle(w, r, name)
		case action == "keys" && r.Method == http.MethodPost:
			s.handleAddKey(w, r, name)
		case action == "logs" && r.Method == http.MethodGet:
			s.handleLogs(w, r, name)
		default:
			writeJSON(w, 404, Obj{"error": "no such endpoint"})
		}
	})
	return mux
}

// ---------------------------------------------------------------- middleware

// authenticate resolves the user and enforces the CSRF rule: state-changing
// calls must carry X-Requested-With (browsers cannot add it cross-site without
// a CORS preflight, which we never grant).
func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.ids.resolve(r)
		if !ok {
			writeJSON(w, 401, Obj{"error": "not signed in: the portal must run behind an authenticating proxy (or with -dev-user)"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Requested-With") != "devspaces-portal" {
			writeJSON(w, 403, Obj{"error": "missing X-Requested-With header"})
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// simulationCSP applies only to /simulation.html: inline code allowed, no network at all.
const simulationCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; " +
	"img-src data:; font-src data:; connect-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

func securityHeaders(csp string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if csp != "" {
			if r.URL.Path == "/simulation.html" {
				// The standalone simulation page is one self-contained file (inline
				// script + inlined Tailwind) so it also works from file://. It never
				// talks to the API, so it gets its own, offline-only policy.
				h.Set("Content-Security-Policy", simulationCSP)
			} else {
				h.Set("Content-Security-Policy", csp)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(rw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.code, time.Since(start).Round(time.Millisecond))
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }
