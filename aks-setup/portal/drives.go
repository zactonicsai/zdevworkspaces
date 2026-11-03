package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// ---------------------------------------------------------------- drives

type driveView struct {
	Name          string   `json:"name"`
	Namespace     string   `json:"namespace"`
	Kind          string   `json:"kind"` // home | shared | shared-mount
	Owner         string   `json:"owner"`
	Size          string   `json:"size"`
	Storage       string   `json:"storage,omitempty"`
	StorageClass  string   `json:"storageClass,omitempty"`
	AccessModes   []string `json:"accessModes"`
	Phase         string   `json:"phase"`
	AttachedTo    string   `json:"attachedTo,omitempty"`
	MountedBy     []string `json:"mountedBy"`
	SharedWith    []string `json:"sharedWith"`
	SharedBy      string   `json:"sharedBy,omitempty"`
	LastWorkspace string   `json:"lastWorkspace,omitempty"`
	Languages     []string `json:"languages"`
	CreatedAt     string   `json:"createdAt,omitempty"`
	Protected     bool     `json:"protected"`
	Snapshots     int      `json:"snapshots"`
}

type snapshotView struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Drive       string `json:"drive"`
	ReadyToUse  bool   `json:"readyToUse"`
	RestoreSize string `json:"restoreSize,omitempty"`
	Languages   string `json:"languages,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	Error       string `json:"error,omitempty"`
}

var snapNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,61}[a-z0-9])?$`)

// attachments maps PVC name → workspaces using it, for one namespace.
func attachments(ctx context.Context, k Kube, ns string) map[string][]string {
	out := map[string][]string{}
	sts, _ := k.List(ctx, nsPath("apps", "v1", "statefulsets", ns, ""), labelWS)
	for _, st := range sts {
		vols, _ := getIn(st, "spec", "template", "spec", "volumes").([]any)
		for _, v := range vols {
			if c := str(v.(Obj), "persistentVolumeClaim", "claimName"); c != "" {
				out[c] = append(out[c], labels(st)[labelWS])
			}
		}
	}
	return out
}

func (s *server) driveView(p Obj, att map[string][]string, u User) driveView {
	l, a := labels(p), annotations(p)
	ns := str(p, "metadata", "namespace")
	v := driveView{Name: str(p, "metadata", "name"), Namespace: ns, Kind: l[labelDrive], Owner: strings.TrimPrefix(ns, "ws-"),
		Size:    orDefault(str(p, "status", "capacity", "storage"), str(p, "spec", "resources", "requests", "storage")),
		Storage: a[annStorage], StorageClass: str(p, "spec", "storageClassName"), Phase: str(p, "status", "phase"),
		MountedBy: []string{}, SharedWith: []string{}, SharedBy: l[labelShared], LastWorkspace: a[annLastWS],
		Languages: splitList(a[annLangs]), CreatedAt: str(p, "metadata", "creationTimestamp"),
		Protected:   l[labelDrive] == "home" && a[annAllowDel] != "true",
		AccessModes: []string{},
	}
	if ams, ok := getIn(p, "spec", "accessModes").([]any); ok {
		for _, m := range ams {
			v.AccessModes = append(v.AccessModes, fmt.Sprint(m))
		}
	}
	users := att[v.Name]
	switch v.Kind {
	case "home":
		if len(users) > 0 {
			v.AttachedTo = users[0]
		}
	default:
		v.MountedBy = append(v.MountedBy, users...)
	}
	if v.Kind == "shared-mount" {
		v.Owner = v.SharedBy
	}
	return v
}

// sharedGuests: who has a mount of my shared drive (PVCs in other namespaces labelled shared-by=me).
func (s *server) sharedGuests(ctx context.Context, owner, drive string) []string {
	if s.priv == nil {
		return nil
	}
	items, err := s.priv.List(ctx, clusterPath("", "v1", "persistentvolumeclaims", ""), labelDrive+"=shared-mount,"+labelShared+"="+owner)
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range items {
		if annotations(p)["devspaces.io/source-drive"] == drive {
			out = append(out, orDefault(annotations(p)[annGuestUser], strings.TrimPrefix(str(p, "metadata", "namespace"), "ws-")))
		}
	}
	sort.Strings(out)
	return out
}

func (s *server) handleDrives(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	k := s.kube(u)
	drives := []driveView{}
	for _, ns := range s.namespacesFor(r.Context(), u) {
		pvcs, err := k.List(r.Context(), nsPath("", "v1", "persistentvolumeclaims", ns, ""), labelDrive)
		if err != nil {
			if errors.Is(err, errNotFound) || errors.Is(err, errForbidden) {
				continue
			}
			writeErr(w, err)
			return
		}
		att := attachments(r.Context(), k, ns)
		snaps, _ := k.List(r.Context(), nsPath("snapshot.storage.k8s.io", "v1", "volumesnapshots", ns, ""), labelDrive)
		for _, p := range pvcs {
			v := s.driveView(p, att, u)
			for _, sn := range snaps {
				if labels(sn)[labelDrive] == v.Name {
					v.Snapshots++
				}
			}
			if v.Kind == "shared" {
				v.SharedWith = append(v.SharedWith, s.sharedGuests(r.Context(), strings.TrimPrefix(ns, "ws-"), v.Name)...)
			}
			drives = append(drives, v)
		}
	}
	writeJSON(w, 200, Obj{"drives": drives})
}

// findDrive locates a PVC by name in the caller's namespaces (or ?ns=).
func (s *server) findDrive(r *http.Request, k Kube, u User, name string) (Obj, string, error) {
	if !nameRE.MatchString(name) {
		return nil, "", invalid("bad drive name")
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
		p, err := k.Get(r.Context(), nsPath("", "v1", "persistentvolumeclaims", c, name))
		if err == nil {
			if labels(p)[labelDrive] == "" {
				return nil, "", invalid("%s is a volume but not a workspace drive", name)
			}
			return p, c, nil
		}
		if !errors.Is(err, errNotFound) && !errors.Is(err, errForbidden) {
			return nil, "", err
		}
	}
	// a workspace name is accepted too: its home drive
	for _, c := range candidates {
		if st, err := k.Get(r.Context(), nsPath("apps", "v1", "statefulsets", c, "ws-"+name)); err == nil {
			spec := Obj{}
			_ = jsonUnmarshal(annotations(st)[annSpec], &spec)
			drive := orDefault(str(spec, "drive"), "ws-"+name+"-home")
			if p, err := k.Get(r.Context(), nsPath("", "v1", "persistentvolumeclaims", c, drive)); err == nil {
				return p, c, nil
			}
		}
	}
	return nil, "", errNotFound
}

func (s *server) snapshotsOf(ctx context.Context, k Kube, ns, drive string) []snapshotView {
	items, _ := k.List(ctx, nsPath("snapshot.storage.k8s.io", "v1", "volumesnapshots", ns, ""), labelDrive+"="+drive)
	out := []snapshotView{}
	for _, sn := range items {
		st, _ := sn["status"].(Obj)
		out = append(out, snapshotView{Name: str(sn, "metadata", "name"), Namespace: ns, Drive: drive,
			ReadyToUse: truthy(getIn(st, "readyToUse")), RestoreSize: fmt.Sprint(orAny(getIn(st, "restoreSize"), "")),
			Languages: annotations(sn)[annLangs], CreatedAt: orDefault(str(st, "creationTime"), str(sn, "metadata", "creationTimestamp")),
			Error: str(st, "error", "message")})
	}
	return out
}

func orAny(v any, d string) any {
	if v == nil {
		return d
	}
	return v
}

func (s *server) handleDriveGet(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	p, ns, err := s.findDrive(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	name = str(p, "metadata", "name")
	v := s.driveView(p, attachments(r.Context(), k, ns), u)
	snaps := s.snapshotsOf(r.Context(), k, ns, name)
	v.Snapshots = len(snaps)
	if v.Kind == "shared" {
		v.SharedWith = append(v.SharedWith, s.sharedGuests(r.Context(), strings.TrimPrefix(ns, "ws-"), name)...)
	}
	writeJSON(w, 200, Obj{"drive": v, "snapshots": snaps})
}

func (s *server) handleDriveCreate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	k := s.kube(u)
	var req struct{ Name, Size, Storage, Namespace string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if !nameRE.MatchString(req.Name) {
		writeErr(w, invalid("drive name: lowercase letters, digits, dashes"))
		return
	}
	size := orDefault(req.Size, "100Gi")
	if !quantityRE.MatchString(size) {
		writeErr(w, invalid("bad size %q", size))
		return
	}
	cat, _ := s.cats.get(r.Context(), k)
	choice, err := cat.StorageChoice(req.Storage, "shared")
	if err != nil {
		writeErr(w, err)
		return
	}
	ns := u.Namespace("standard")
	if req.Namespace != "" {
		if !s.ownsNamespace(r.Context(), u, req.Namespace) {
			writeErr(w, invalid("not your namespace"))
			return
		}
		ns = req.Namespace
	}
	modes := []any{}
	for _, m := range choice.AccessModes {
		modes = append(modes, m)
	}
	if len(modes) == 0 {
		modes = []any{"ReadWriteMany"}
	}
	pvc := Obj{"apiVersion": "v1", "kind": "PersistentVolumeClaim",
		"metadata": Obj{"name": "shared-" + req.Name, "namespace": ns,
			"labels":      Obj{labelDrive: "shared", labelManaged: "devspaces-portal", labelTenant: strings.TrimPrefix(ns, "ws-")},
			"annotations": Obj{annStorage: choice.Name}},
		"spec": Obj{"accessModes": modes, "resources": Obj{"requests": Obj{"storage": size}}}}
	if choice.StorageClass != "" {
		pvc["spec"].(Obj)["storageClassName"] = choice.StorageClass
	}
	if _, err := k.Get(r.Context(), pathFor(pvc)); err == nil {
		writeErr(w, invalid("shared drive %s already exists", req.Name))
		return
	}
	if err := k.Apply(r.Context(), pathFor(pvc), pvc); err != nil {
		writeErr(w, err)
		return
	}
	p, _ := k.Get(r.Context(), pathFor(pvc))
	writeJSON(w, 201, s.driveView(p, nil, u))
}

func (s *server) handleDriveDelete(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	p, ns, err := s.findDrive(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	name = str(p, "metadata", "name")
	if users := attachments(r.Context(), k, ns)[name]; len(users) > 0 {
		writeJSON(w, 409, Obj{"error": fmt.Sprintf("%s is in use by workspace %s — delete that workspace first (the drive stays), or delete it with purge", name, users[0])})
		return
	}
	if labels(p)[labelDrive] == "shared" && len(s.sharedGuests(r.Context(), strings.TrimPrefix(ns, "ws-"), name)) > 0 {
		writeJSON(w, 409, Obj{"error": "the drive is still shared with other developers — revoke the shares first"})
		return
	}
	path := nsPath("", "v1", "persistentvolumeclaims", ns, name)
	// the admission policy refuses to delete a home drive without this mark
	if err := k.MergePatch(r.Context(), path, Obj{"metadata": Obj{"annotations": Obj{annAllowDel: "true"}}}); err != nil {
		writeErr(w, err)
		return
	}
	if err := k.Delete(r.Context(), path); err != nil {
		writeErr(w, err)
		return
	}
	log.Printf("user=%s deleted drive %s/%s", u.Name, ns, name)
	writeJSON(w, 200, Obj{"deleted": name})
}

func (s *server) handleDriveResize(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	var req struct{ Size string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if !quantityRE.MatchString(req.Size) {
		writeErr(w, invalid("bad size %q", req.Size))
		return
	}
	p, ns, err := s.findDrive(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	name = str(p, "metadata", "name")
	if err := k.MergePatch(r.Context(), nsPath("", "v1", "persistentvolumeclaims", ns, name),
		Obj{"spec": Obj{"resources": Obj{"requests": Obj{"storage": req.Size}}}}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Obj{"name": name, "size": req.Size, "note": "grows online if the StorageClass allows expansion; some drivers need a stop/start"})
}

func (s *server) handleSnapshots(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	p, ns, err := s.findDrive(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Obj{"snapshots": s.snapshotsOf(r.Context(), k, ns, str(p, "metadata", "name"))})
}

func (s *server) handleSnapshotCreate(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	var req struct{ Name string }
	_ = readJSON(r, &req) // body optional
	p, ns, err := s.findDrive(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	name = str(p, "metadata", "name")
	snap := strings.TrimSpace(req.Name)
	if snap == "" {
		snap = name + "-" + time.Now().UTC().Format("20060102-150405")
	}
	if !snapNameRE.MatchString(snap) {
		writeErr(w, invalid("bad snapshot name %q", snap))
		return
	}
	cat, _ := s.cats.get(r.Context(), k)
	obj := Obj{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshot",
		"metadata": Obj{"name": snap, "namespace": ns, "labels": Obj{labelDrive: name, labelManaged: "devspaces-portal"},
			"annotations": Obj{annLangs: annotations(p)[annLangs], annLastWS: annotations(p)[annLastWS]}},
		"spec": Obj{"source": Obj{"persistentVolumeClaimName": name}}}
	if cat.Storage.SnapshotClass != "" {
		obj["spec"].(Obj)["volumeSnapshotClassName"] = cat.Storage.SnapshotClass
	}
	if err := k.Apply(r.Context(), pathFor(obj), obj); err != nil {
		writeErr(w, fmt.Errorf("snapshot: %w (is the CSI snapshot controller installed? CONTRACT C7)", err))
		return
	}
	log.Printf("user=%s snapshot %s of %s/%s", u.Name, snap, ns, name)
	writeJSON(w, 201, snapshotView{Name: snap, Namespace: ns, Drive: name, Languages: annotations(p)[annLangs]})
}

func (s *server) handleSnapshotDelete(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	if !snapNameRE.MatchString(name) {
		writeErr(w, invalid("bad snapshot name"))
		return
	}
	ns, err := targetNamespace(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	candidates := []string{ns}
	if ns == "" {
		candidates = s.namespacesFor(r.Context(), u)
	}
	for _, c := range candidates {
		path := nsPath("snapshot.storage.k8s.io", "v1", "volumesnapshots", c, name)
		if _, err := k.Get(r.Context(), path); err == nil {
			if err := k.Delete(r.Context(), path); err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, 200, Obj{"deleted": name})
			return
		}
	}
	writeErr(w, errNotFound)
}

// ---------------------------------------------------------------- drive shares (RWX only)

func renderSharedMount(srcPV Obj, ownerShort, drive, guestNS, guestUser string, readOnly bool) ([]Obj, error) {
	csi, ok := getIn(srcPV, "spec", "csi").(Obj)
	if !ok {
		return nil, invalid("only CSI-backed shared drives can be shared across namespaces")
	}
	pvcName := "shared-" + ownerShort + "-" + drive
	pvName := pvcName + "-" + strings.TrimPrefix(guestNS, "ws-")
	csiCopy := deepCopy(csi)
	csiCopy["readOnly"] = readOnly
	lbl := Obj{labelDrive: "shared-mount", labelShared: ownerShort, labelManaged: "devspaces-portal"}
	pv := Obj{"apiVersion": "v1", "kind": "PersistentVolume",
		"metadata": Obj{"name": pvName, "labels": deepCopy(lbl)},
		"spec": Obj{"capacity": getIn(srcPV, "spec", "capacity"), "accessModes": getIn(srcPV, "spec", "accessModes"),
			"persistentVolumeReclaimPolicy": "Retain", "storageClassName": str(srcPV, "spec", "storageClassName"),
			"csi": csiCopy, "claimRef": Obj{"namespace": guestNS, "name": pvcName}}}
	if mo := getIn(srcPV, "spec", "mountOptions"); mo != nil {
		pv["spec"].(Obj)["mountOptions"] = mo
	}
	pvc := Obj{"apiVersion": "v1", "kind": "PersistentVolumeClaim",
		"metadata": Obj{"name": pvcName, "namespace": guestNS, "labels": deepCopy(lbl),
			"annotations": Obj{annStorage: "shared", "devspaces.io/source-drive": "shared-" + drive, annGuestUser: guestUser,
				"devspaces.io/read-only": fmt.Sprint(readOnly)}},
		"spec": Obj{"accessModes": getIn(srcPV, "spec", "accessModes"), "volumeName": pvName,
			"storageClassName": str(srcPV, "spec", "storageClassName"),
			"resources":        Obj{"requests": Obj{"storage": str(srcPV, "spec", "capacity", "storage")}}}}
	return []Obj{pv, pvc}, nil
}

func (s *server) handleDriveShares(w http.ResponseWriter, r *http.Request, name string) {
	u := userFrom(r)
	k := s.kube(u)
	_, ns, err := s.findDrive(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	shares := []shareView{}
	for _, g := range s.sharedGuests(r.Context(), strings.TrimPrefix(ns, "ws-"), name) {
		shares = append(shares, shareView{User: g, Kind: "drive", Resource: name})
	}
	writeJSON(w, 200, Obj{"shares": shares})
}

func (s *server) handleDriveShareAdd(w http.ResponseWriter, r *http.Request, name string) {
	if !s.requireSharing(w) {
		return
	}
	u := userFrom(r)
	k := s.kube(u)
	var req struct {
		User     string `json:"user"`
		ReadOnly bool   `json:"readOnly"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	guest := strings.TrimSpace(req.User)
	if guest == "" || strings.ContainsAny(guest, " \t\n\"'") {
		writeErr(w, invalid("user is required"))
		return
	}
	p, ns, err := s.findDrive(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !s.ownsNamespace(r.Context(), u, ns) || labels(p)[labelDrive] != "shared" {
		writeErr(w, invalid("only your own shared drives can be shared (home drives are single-writer disks: share the workspace instead)"))
		return
	}
	rwx := false
	if ams, ok := getIn(p, "spec", "accessModes").([]any); ok {
		for _, m := range ams {
			rwx = rwx || fmt.Sprint(m) == "ReadWriteMany"
		}
	}
	if !rwx {
		writeErr(w, invalid("this drive is not ReadWriteMany; its StorageClass cannot be mounted by two namespaces"))
		return
	}
	pvName := str(p, "spec", "volumeName")
	if pvName == "" {
		writeErr(w, invalid("the drive has no volume yet (mount it in a workspace once so the storage driver provisions it)"))
		return
	}
	pv, err := s.priv.Get(r.Context(), clusterPath("", "v1", "persistentvolumes", pvName))
	if err != nil {
		writeErr(w, fmt.Errorf("read volume: %w", err))
		return
	}
	gu := User{Name: guest, Short: sanitizeUser(guest)}
	guestNS := gu.Namespace("standard")
	if _, err := s.priv.Get(r.Context(), "/api/v1/namespaces/"+guestNS); err != nil {
		writeErr(w, invalid("%s is not onboarded (namespace %s missing)", guest, guestNS))
		return
	}
	objs, err := renderSharedMount(pv, strings.TrimPrefix(ns, "ws-"), strings.TrimPrefix(name, "shared-"), guestNS, guest, req.ReadOnly)
	if err != nil {
		writeErr(w, err)
		return
	}
	for _, o := range objs {
		if err := s.priv.Apply(r.Context(), pathFor(o), o); err != nil {
			writeErr(w, fmt.Errorf("share drive: %w", err))
			return
		}
	}
	log.Printf("user=%s shared drive %s/%s with %s (ro=%v)", u.Name, ns, name, guest, req.ReadOnly)
	writeJSON(w, 201, shareView{User: guest, Kind: "drive", Resource: name, ReadOnly: req.ReadOnly})
}

func (s *server) handleDriveShareDelete(w http.ResponseWriter, r *http.Request, name, guest string) {
	if !s.requireSharing(w) {
		return
	}
	u := userFrom(r)
	k := s.kube(u)
	guest, _ = url.PathUnescape(guest)
	_, ns, err := s.findDrive(r, k, u, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !s.ownsNamespace(r.Context(), u, ns) {
		writeErr(w, invalid("only the owner can revoke"))
		return
	}
	owner := strings.TrimPrefix(ns, "ws-")
	guestNS := (User{Name: guest, Short: sanitizeUser(guest)}).Namespace("standard")
	pvcName := "shared-" + owner + "-" + strings.TrimPrefix(name, "shared-")
	if err := s.priv.Delete(r.Context(), nsPath("", "v1", "persistentvolumeclaims", guestNS, pvcName)); err != nil {
		writeErr(w, err)
		return
	}
	_ = s.priv.Delete(r.Context(), clusterPath("", "v1", "persistentvolumes", pvcName+"-"+strings.TrimPrefix(guestNS, "ws-")))
	writeJSON(w, 200, Obj{"revoked": guest})
}
