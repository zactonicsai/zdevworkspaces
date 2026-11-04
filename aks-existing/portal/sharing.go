package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ---------------------------------------------------------------- where may a user look?

// namespacesFor returns the namespaces the caller may hold workspaces in: personal,
// lab and every project namespace they are a member of (by name or group).
func (s *server) namespacesFor(ctx context.Context, u User) []string {
	out := []string{u.Namespace("standard"), u.Namespace("lab")}
	for _, p := range s.projectsFor(ctx, u) {
		out = append(out, p.Namespace)
	}
	return out
}

type projectView struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Members   []string `json:"members"`
	Groups    []string `json:"groups"`
	Tier      string   `json:"tier"`
	Mesh      string   `json:"mesh"`
}

func (s *server) projectsFor(ctx context.Context, u User) []projectView {
	if s.priv == nil {
		return nil
	}
	items, err := s.priv.List(ctx, clusterPath("", "v1", "namespaces", ""), labelKind+"=project")
	if err != nil {
		return nil
	}
	var out []projectView
	for _, n := range items {
		a, l := annotations(n), labels(n)
		members := splitList(a[annMembers])
		groups := splitList(a["devspaces.io/member-groups"])
		ok := contains(members, u.Name)
		for _, g := range groups {
			ok = ok || contains(u.Groups, g)
		}
		if !ok {
			continue
		}
		mesh := meshOf(l)
		out = append(out, projectView{Name: l["devspaces.io/project"], Namespace: str(n, "metadata", "name"),
			Members: members, Groups: groups, Tier: orDefault(l[labelTier], "standard"), Mesh: mesh})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// targetNamespace honours ?ns= and ?owner= (a workspace shared with me lives in the
// owner's namespace). Returns "" when the caller gave neither.
func targetNamespace(r *http.Request) (string, error) {
	q := r.URL.Query()
	if o := strings.TrimSpace(q.Get("owner")); o != "" {
		return "ws-" + sanitizeUser(o), nil
	}
	if ns := strings.TrimSpace(q.Get("ns")); ns != "" {
		if !strings.HasPrefix(ns, "ws-") || !nameRE.MatchString(strings.TrimPrefix(ns, "ws-")) {
			return "", invalid("bad namespace %q", ns)
		}
		return ns, nil
	}
	return "", nil
}

// locate finds a workspace by name in the namespaces the caller may use, or in the
// namespace they named (?ns=, ?owner=). RBAC — impersonation — still decides.
func (s *server) locate(r *http.Request, k Kube, u User, name string) (Obj, string, error) {
	if !nameRE.MatchString(name) {
		return nil, "", invalid("bad workspace name")
	}
	ns, err := targetNamespace(r)
	if err != nil {
		return nil, "", err
	}
	candidates := []string{ns}
	if ns == "" {
		candidates = s.namespacesFor(r.Context(), u)
	}
	for _, c := range candidates {
		st, err := k.Get(r.Context(), nsPath("apps", "v1", "statefulsets", c, "ws-"+name))
		if err == nil {
			return st, c, nil
		}
		if !errors.Is(err, errNotFound) && !errors.Is(err, errForbidden) {
			return nil, "", err
		}
	}
	return nil, "", errNotFound
}

// ownsNamespace: is ns one of the caller's own (not a shared/foreign one)?
func (s *server) ownsNamespace(ctx context.Context, u User, ns string) bool {
	return contains(s.namespacesFor(ctx, u), ns)
}

// ---------------------------------------------------------------- workspace shares

type shareView struct {
	User      string `json:"user"`
	Kind      string `json:"kind"` // workspace | drive
	Resource  string `json:"resource"`
	ReadOnly  bool   `json:"readOnly"`
	CreatedAt string `json:"createdAt,omitempty"`
}

func guestRole(ns, name string) Obj {
	obj := "ws-" + name
	return Obj{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "Role",
		"metadata": Obj{"name": "workspace-guest-" + name, "namespace": ns, "labels": Obj{labelWS: name, labelManaged: "devspaces-portal"}},
		"rules": []any{
			Obj{"apiGroups": []any{""}, "resources": []any{"pods"}, "resourceNames": []any{obj + "-0"}, "verbs": []any{"get"}},
			Obj{"apiGroups": []any{""}, "resources": []any{"pods/exec", "pods/portforward"}, "resourceNames": []any{obj + "-0"}, "verbs": []any{"create", "get"}},
			Obj{"apiGroups": []any{"apps"}, "resources": []any{"statefulsets"}, "resourceNames": []any{obj}, "verbs": []any{"get"}},
			Obj{"apiGroups": []any{"apps"}, "resources": []any{"statefulsets/scale"}, "resourceNames": []any{obj}, "verbs": []any{"get", "patch", "update"}},
			Obj{"apiGroups": []any{""}, "resources": []any{"services"}, "resourceNames": []any{obj}, "verbs": []any{"get"}},
			Obj{"apiGroups": []any{""}, "resources": []any{"secrets"}, "resourceNames": []any{obj + "-ssh"}, "verbs": []any{"get"}},
		}}
}

func guestBinding(ns, name, guest string) Obj {
	return Obj{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "RoleBinding",
		"metadata": Obj{"name": "share-" + name + "-" + sanitizeUser(guest), "namespace": ns,
			"labels":      Obj{labelWS: name, labelShare: "workspace", labelGuest: sanitizeUser(guest), labelManaged: "devspaces-portal"},
			"annotations": Obj{annGuestUser: guest}},
		"subjects": []any{Obj{"kind": "User", "name": guest, "apiGroup": "rbac.authorization.k8s.io"}},
		"roleRef":  Obj{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "workspace-guest-" + name}}
}

func (s *server) requireSharing(w http.ResponseWriter) bool {
	if !s.sharing || s.priv == nil {
		writeJSON(w, 501, Obj{"error": "sharing is disabled on this portal (-sharing=false or no ServiceAccount)"})
		return false
	}
	return true
}

func (s *server) handleShares(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	_, ns, err := s.locate(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, err := k.List(r.Context(), nsPath("rbac.authorization.k8s.io", "v1", "rolebindings", ns, ""), labelShare+"=workspace,"+labelWS+"="+name)
	if err != nil && !errors.Is(err, errForbidden) {
		writeErr(w, err)
		return
	}
	shares := []shareView{}
	for _, rb := range items {
		shares = append(shares, shareView{User: orDefault(annotations(rb)[annGuestUser], labels(rb)[labelGuest]), Kind: "workspace",
			Resource: name, CreatedAt: str(rb, "metadata", "creationTimestamp")})
	}
	writeJSON(w, 200, Obj{"shares": shares})
}

func (s *server) handleShareAdd(w http.ResponseWriter, r *http.Request, name string) {
	if !s.requireSharing(w) {
		return
	}
	u := userFrom(r)
	k := s.kube(u)
	var req struct{ User, PublicKey string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	guest := strings.TrimSpace(req.User)
	if guest == "" || strings.ContainsAny(guest, " \t\n\"'") {
		writeErr(w, invalid("user is required (e-mail / UPN as the API server knows it)"))
		return
	}
	if strings.EqualFold(guest, u.Name) {
		writeErr(w, invalid("that is you"))
		return
	}
	_, ns, err := s.locate(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !s.ownsNamespace(r.Context(), u, ns) {
		writeErr(w, invalid("only the owner can share a workspace"))
		return
	}
	// guest keys: explicit, else their profile (read with the portal's own identity)
	keys := []string{}
	if norm, ok := validPublicKey(req.PublicKey); ok {
		keys = append(keys, norm)
	}
	if len(keys) == 0 {
		gu := User{Name: guest, Short: sanitizeUser(guest)}
		if sec, err := s.priv.Get(r.Context(), nsPath("", "v1", "secrets", gu.Namespace("standard"), "workspace-profile")); err == nil {
			for _, line := range strings.Split(decodeB64(str(sec, "data", "ssh_public_keys")), "\n") {
				if norm, ok := validPublicKey(line); ok {
					keys = append(keys, norm)
				}
			}
		}
	}
	if len(keys) == 0 {
		writeErr(w, invalid("%s has no SSH key in their profile yet — ask them to add one on the Profile page, or paste their public key", guest))
		return
	}
	// authorized_keys: the owner's secret, patched as the owner (impersonation)
	secPath := nsPath("", "v1", "secrets", ns, "ws-"+name+"-ssh")
	sec, err := k.Get(r.Context(), secPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	tag := " guest:" + guest
	var lines []string
	for _, l := range strings.Split(decodeB64(str(sec, "data", "authorized_keys")), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasSuffix(l, tag) {
			lines = append(lines, l)
		}
	}
	for _, kline := range keys {
		f := strings.Fields(kline)
		lines = append(lines, f[0]+" "+f[1]+tag)
	}
	if err := k.MergePatch(r.Context(), secPath, Obj{"stringData": Obj{"authorized_keys": strings.Join(lines, "\n") + "\n"}}); err != nil {
		writeErr(w, err)
		return
	}
	// RBAC for exec/port-forward: with the portal's identity (the guest's binding lives in the owner's namespace)
	for _, o := range []Obj{guestRole(ns, name), guestBinding(ns, name, guest)} {
		if err := s.priv.Apply(r.Context(), pathFor(o), o); err != nil {
			writeErr(w, fmt.Errorf("grant access: %w", err))
			return
		}
	}
	log.Printf("user=%s shared workspace %s/%s with %s", u.Name, ns, name, guest)
	writeJSON(w, 201, shareView{User: guest, Kind: "workspace", Resource: name})
}

func (s *server) handleShareDelete(w http.ResponseWriter, r *http.Request, name, guest string) {
	if !s.requireSharing(w) {
		return
	}
	u := userFrom(r)
	k := s.kube(u)
	guest, _ = url.PathUnescape(guest)
	_, ns, err := s.locate(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !s.ownsNamespace(r.Context(), u, ns) {
		writeErr(w, invalid("only the owner can revoke a share"))
		return
	}
	secPath := nsPath("", "v1", "secrets", ns, "ws-"+name+"-ssh")
	if sec, err := k.Get(r.Context(), secPath); err == nil {
		var lines []string
		for _, l := range strings.Split(decodeB64(str(sec, "data", "authorized_keys")), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasSuffix(l, " guest:"+guest) {
				lines = append(lines, l)
			}
		}
		_ = k.MergePatch(r.Context(), secPath, Obj{"stringData": Obj{"authorized_keys": strings.Join(lines, "\n") + "\n"}})
	}
	if err := s.priv.Delete(r.Context(), nsPath("rbac.authorization.k8s.io", "v1", "rolebindings", ns, "share-"+name+"-"+sanitizeUser(guest))); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Obj{"revoked": guest})
}

// sharedWithMe lists workspaces other developers shared with the caller (RoleBindings
// naming them, found with the portal's identity; details read as the caller).
func (s *server) sharedWithMe(ctx context.Context, k Kube, u User) []wsView {
	if s.priv == nil {
		return nil
	}
	rbs, err := s.priv.List(ctx, clusterPath("rbac.authorization.k8s.io", "v1", "rolebindings", ""), labelShare+"=workspace,"+labelGuest+"="+u.Short)
	if err != nil {
		return nil
	}
	var out []wsView
	for _, rb := range rbs {
		if annotations(rb)[annGuestUser] != u.Name {
			continue
		}
		ns, name := str(rb, "metadata", "namespace"), labels(rb)[labelWS]
		st, err := k.Get(ctx, nsPath("apps", "v1", "statefulsets", ns, "ws-"+name))
		if err != nil {
			continue
		}
		pod, _ := k.Get(ctx, nsPath("", "v1", "pods", ns, "ws-"+name+"-0"))
		v := s.view(st, pod)
		v.Owner = strings.TrimPrefix(ns, "ws-")
		out = append(out, v)
	}
	return out
}

// ---------------------------------------------------------------- links (pod-to-pod across namespaces)

type linkView struct {
	Workspace string `json:"workspace"`
	Peer      string `json:"peer"`
	PeerDNS   string `json:"peerDNS"`
	Ports     []int  `json:"ports"`
	Status    string `json:"status"` // linked | pending-peer
}

func peerPolicyName(name, peerNS, peerWS string) string {
	n := "peer-" + name + "-" + strings.TrimPrefix(peerNS, "ws-") + "-" + peerWS
	if len(n) > 63 {
		n = n[:63]
	}
	return strings.TrimRight(n, "-")
}

func renderLink(ns, name, peerNS, peerWS string, ports []int) Obj {
	peerSel := []any{Obj{"namespaceSelector": Obj{"matchLabels": Obj{"kubernetes.io/metadata.name": peerNS}},
		"podSelector": Obj{"matchLabels": Obj{labelWS: peerWS}}}}
	ingress := Obj{"from": peerSel}
	if len(ports) > 0 {
		ps := []any{}
		for _, p := range ports {
			ps = append(ps, Obj{"protocol": "TCP", "port": p})
		}
		ingress["ports"] = ps
	}
	return Obj{"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
		"metadata": Obj{"name": peerPolicyName(name, peerNS, peerWS), "namespace": ns,
			"labels":      Obj{labelWS: name, labelLink: "peer", labelManaged: "devspaces-portal"},
			"annotations": Obj{annPeer: strings.TrimPrefix(peerNS, "ws-") + "/" + peerWS}},
		"spec": Obj{"podSelector": Obj{"matchLabels": Obj{labelWS: name}}, "policyTypes": []any{"Ingress", "Egress"},
			"ingress": []any{ingress}, "egress": []any{Obj{"to": peerSel}}}}
}

func parsePeer(peer string) (ns, ws string, err error) {
	peer = strings.TrimSpace(peer)
	i := strings.LastIndex(peer, "/")
	if i <= 0 || i == len(peer)-1 {
		return "", "", invalid("peer must be OWNER/WORKSPACE (workspaces in your own namespace are already connected)")
	}
	owner, ws := peer[:i], peer[i+1:]
	if !nameRE.MatchString(ws) {
		return "", "", invalid("bad peer workspace name %q", ws)
	}
	if strings.HasPrefix(owner, "ws-") { // a namespace was given
		return owner, ws, nil
	}
	return "ws-" + sanitizeUser(owner), ws, nil
}

func (s *server) linksOf(ctx context.Context, k Kube, ns, name string) []linkView {
	items, _ := k.List(ctx, nsPath("networking.k8s.io", "v1", "networkpolicies", ns, ""), labelLink+"=peer,"+labelWS+"="+name)
	out := []linkView{}
	for _, np := range items {
		peer := annotations(np)[annPeer]
		tenant, pws, _ := strings.Cut(peer, "/")
		peerNS := "ws-" + tenant
		lv := linkView{Workspace: name, Peer: peer, PeerDNS: "ws-" + pws + "." + peerNS + ".svc.cluster.local", Ports: []int{}, Status: "pending-peer"}
		if ing, ok := getIn(np, "spec", "ingress").([]any); ok && len(ing) > 0 {
			if ps, ok := ing[0].(Obj)["ports"].([]any); ok {
				for _, p := range ps {
					lv.Ports = append(lv.Ports, int(num(p.(Obj), "port")))
				}
			}
		}
		// has the peer linked back? (read with the portal's identity; developers cannot see each other's policies)
		if s.priv != nil {
			if _, err := s.priv.Get(ctx, nsPath("networking.k8s.io", "v1", "networkpolicies", peerNS, peerPolicyName(pws, ns, name))); err == nil {
				lv.Status = "linked"
			}
		}
		out = append(out, lv)
	}
	return out
}

func (s *server) handleLinks(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	_, ns, err := s.locate(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Obj{"links": s.linksOf(r.Context(), k, ns, name)})
}

func (s *server) handleLinkAdd(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	cat, _ := s.cats.get(r.Context(), k)
	if !cat.LinksEnabled() {
		writeErr(w, invalid("links are disabled by the platform (networking.links.enabled=false)"))
		return
	}
	var req struct {
		Peer  string `json:"peer"`
		Ports []int  `json:"ports"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	_, ns, err := s.locate(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !s.ownsNamespace(r.Context(), u, ns) {
		writeErr(w, invalid("you can only link your own workspaces"))
		return
	}
	peerNS, peerWS, err := parsePeer(req.Peer)
	if err != nil {
		writeErr(w, err)
		return
	}
	if peerNS == ns {
		writeErr(w, invalid("%s is in the same namespace — already reachable, no link needed", peerWS))
		return
	}
	for _, p := range req.Ports {
		if p < 1 || p > 65535 {
			writeErr(w, invalid("bad port %d", p))
			return
		}
	}
	np := renderLink(ns, name, peerNS, peerWS, req.Ports)
	if err := k.Apply(r.Context(), pathFor(np), np); err != nil {
		writeErr(w, err)
		return
	}
	for _, l := range s.linksOf(r.Context(), k, ns, name) {
		if l.Peer == strings.TrimPrefix(peerNS, "ws-")+"/"+peerWS {
			writeJSON(w, 201, l)
			return
		}
	}
	writeJSON(w, 201, linkView{Workspace: name, Peer: req.Peer, Status: "pending-peer", Ports: req.Ports})
}

func (s *server) handleLinkDelete(w http.ResponseWriter, r *http.Request, name, peer string) {
	u := userFrom(r)
	k := s.kube(u)
	peer, _ = url.PathUnescape(peer)
	_, ns, err := s.locate(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	peerNS, peerWS, err := parsePeer(peer)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := k.Delete(r.Context(), nsPath("networking.k8s.io", "v1", "networkpolicies", ns, peerPolicyName(name, peerNS, peerWS))); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Obj{"removed": peer})
}

// ---------------------------------------------------------------- network view

func (s *server) networkOf(ctx context.Context, k Kube, cat *Catalog, st Obj, ns, name string) Obj {
	pod, _ := k.Get(ctx, nsPath("", "v1", "pods", ns, "ws-"+name+"-0"))
	same := []string{}
	if sts, err := k.List(ctx, nsPath("apps", "v1", "statefulsets", ns, ""), labelWS); err == nil {
		for _, o := range sts {
			if n := labels(o)[labelWS]; n != "" && n != name {
				same = append(same, n)
			}
		}
	}
	spec := Obj{}
	_ = jsonUnmarshal(annotations(st)[annSpec], &spec)
	mesh := "none"
	if nso, err := k.Get(ctx, "/api/v1/namespaces/"+ns); err == nil {
		mesh = meshOf(labels(nso))
	} else if s.priv != nil {
		if nso, err := s.priv.Get(ctx, "/api/v1/namespaces/"+ns); err == nil {
			mesh = meshOf(labels(nso))
		}
	}
	mc := Obj{"mode": "off"}
	if b, _ := spec["multicast"].(bool); b {
		mode := orDefault(str(cat.Networking, "multicast", "mode"), "multus")
		mc = Obj{"mode": mode, "iface": ifThen(mode == "cilium", "eth0", orDefault(str(cat.Networking, "multicast", "iface"), "net1"))}
	}
	return Obj{"dns": "ws-" + name + "." + ns + ".svc.cluster.local", "podIP": str(pod, "status", "podIP"), "namespace": ns,
		"sameNamespace": same, "links": s.linksOf(ctx, k, ns, name), "multicast": mc, "mesh": mesh,
		"linksEnabled": cat.LinksEnabled()}
}

func (s *server) handleNetwork(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	st, ns, err := s.locate(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	cat, _ := s.cats.get(r.Context(), k)
	writeJSON(w, 200, s.networkOf(r.Context(), k, cat, st, ns, name))
}

func (s *server) handleProjects(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	p := s.projectsFor(r.Context(), u)
	if p == nil {
		p = []projectView{}
	}
	writeJSON(w, 200, Obj{"projects": p})
}
