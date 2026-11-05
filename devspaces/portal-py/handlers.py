"""handlers.py — the Workspaces API on a real cluster, in Python. Every function takes a Ctx
(caller, impersonating kubectl client, privileged client for sharing, query, body) and
returns (status, json). Manifests come from the CLI module (wscli) so what this API creates
is identical to `workspace create` and to the Go portal. Shapes: api/openapi.yaml."""
from __future__ import annotations

import base64
import json
import re
import time
from dataclasses import dataclass, field

from kube import Kube, KubeError, wscli

W = wscli
NO_NS = W.NO_NS
LABEL_WS, LABEL_DRIVE, LABEL_SHARE, LABEL_GUEST, LABEL_LINK = W.LABEL_WS, W.LABEL_DRIVE, W.LABEL_SHARE, W.LABEL_GUEST, W.LABEL_LINK
ANN = {"spec": W.ANN_SPEC, "idle": W.ANN_IDLE, "active": W.ANN_ACTIVE, "lastws": W.ANN_LAST_WS, "langs": W.ANN_LANGS, "storage": W.ANN_STORAGE,
       "allow": W.ANN_ALLOW_DELETE, "guest": W.ANN_GUEST_USER, "peer": W.ANN_PEER, "members": W.ANN_MEMBERS}
KEY_RE = re.compile(r"^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp\d+|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) [A-Za-z0-9+/=]+( .*)?$")
QTY_RE = re.compile(r"^[0-9]+(\.[0-9]+)?(m|k|Ki|Mi|Gi|Ti|M|G|T)?$")


class ApiError(Exception):
    def __init__(self, status, message):
        super().__init__(message)
        self.status = status


def bad(msg):
    return ApiError(400, msg)


@dataclass
class Ctx:
    user: dict
    k: Kube
    priv: Kube | None
    query: dict = field(default_factory=dict)
    body: dict = field(default_factory=dict)
    params: dict = field(default_factory=dict)
    cfg: object = None

    def q(self, name, default=""):
        return self.query.get(name, default)


def b64d(s):
    return base64.b64decode(s or "").decode("utf-8", "replace")


def ns_of(user, tier="standard"):
    return f"ws-{user['short']}" + ("-lab" if tier == "lab" else "")


def meta_labels(o):
    return (o.get("metadata") or {}).get("labels") or {}


def meta_ann(o):
    return (o.get("metadata") or {}).get("annotations") or {}


def now_iso():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def fingerprint(line):
    try:
        import hashlib
        raw = base64.b64decode(line.split()[1])
        return "SHA256:" + base64.b64encode(hashlib.sha256(raw).digest()).decode().rstrip("=")
    except Exception:
        return ""


def valid_key(line):
    line = (line or "").strip()
    if not KEY_RE.match(line) or "\n" in line:
        return None
    f = line.split()
    return " ".join(f[:3]) if len(f) >= 3 else " ".join(f[:2])


# ------------------------------------------------------------------ catalog / namespaces
def catalog(ctx) -> dict:
    return W.load_catalog(ctx.k)


def public_catalog(cat):
    out = json.loads(json.dumps(cat))
    for k in ("languages", "tools", "profiles", "isolation", "devices"):
        out[k + "Order"] = sorted(x for x in (out.get(k) or {}) if not x.startswith("$"))
    out["source"] = "cluster"
    return out


def projects_for(ctx):
    if not ctx.priv:
        return []
    out = []
    for n in ctx.priv.items("namespaces", selector=f"{W.LABEL_KIND}=project", all_namespaces=False):
        a, l = meta_ann(n), meta_labels(n)
        members = [m for m in a.get(ANN["members"], "").split(",") if m]
        groups = [g for g in a.get("devspaces.io/member-groups", "").split(",") if g]
        if ctx.user["name"] not in members and not set(groups) & set(ctx.user.get("groups") or []):
            continue
        out.append({"name": l.get("devspaces.io/project", ""), "namespace": n["metadata"]["name"], "members": members, "groups": groups,
                    "tier": l.get(W.LABEL_TIER, "standard"), "mesh": mesh_of(l)})
    return sorted(out, key=lambda p: p["name"])


def mesh_of(labels):
    if labels.get("istio.io/dataplane-mode") == "ambient":
        return "istio-ambient"
    if labels.get("istio.io/rev"):
        return f"istio-sidecar (rev {labels['istio.io/rev']})"
    if labels.get("istio-injection") == "enabled":
        return "istio-sidecar"
    return "none"


def namespaces_for(ctx):
    return [ns_of(ctx.user), ns_of(ctx.user, "lab")] + [p["namespace"] for p in projects_for(ctx)]


def target_ns(ctx):
    if ctx.q("owner"):
        return "ws-" + W.sanitize_user(ctx.q("owner"))
    ns = ctx.q("ns")
    if ns and (not ns.startswith("ws-") or not W.NAME_RE.match(ns[3:])):
        raise bad(f"bad namespace {ns!r}")
    return ns


def owns(ctx, ns):
    return ns in namespaces_for(ctx)


def locate(ctx, name):
    if not W.NAME_RE.match(name or ""):
        raise bad("bad workspace name")
    cands = [target_ns(ctx)] if target_ns(ctx) else namespaces_for(ctx)
    for ns in cands:
        try:
            st = ctx.k.get("statefulset", f"ws-{name}", ns=ns)
        except KubeError as e:
            if e.status == 403:
                continue
            raise
        if st:
            return st, ns
    raise ApiError(404, "not found")


# ------------------------------------------------------------------ views
def view(st, pod):
    l, a = meta_labels(st), meta_ann(st)
    try:
        spec = json.loads(a.get(ANN["spec"], "{}"))
    except json.JSONDecodeError:
        spec = {}
    name, ns = l.get(LABEL_WS, ""), st["metadata"]["namespace"]
    v = {"name": name, "namespace": ns, "tier": l.get(W.LABEL_TIER, ""), "profile": l.get(W.LABEL_PROFILE, ""), "idleTimeout": a.get(ANN["idle"], ""),
         "spec": spec, "image": spec.get("image", ""), "createdAt": spec.get("createdAt", ""), "expose": bool(spec.get("expose")),
         "languages": spec.get("languages") or ([spec["lang"]] if spec.get("lang") else []), "tools": spec.get("tools") or [],
         "drive": spec.get("drive") or f"ws-{name}-home", "storage": spec.get("storage", ""), "mounts": spec.get("mounts") or [],
         "dns": f"ws-{name}.{ns}.svc.cluster.local", "ready": False}
    replicas = (st.get("spec") or {}).get("replicas") or 0
    ready_n = (st.get("status") or {}).get("readyReplicas") or 0
    if replicas == 0:
        v["status"] = "Stopped"
    elif ready_n > 0:
        v["status"], v["ready"] = "Running", True
    elif pod:
        v["status"] = (pod.get("status") or {}).get("phase") or "Starting"
        if v["status"] == "Running":
            v["status"] = "Starting"
    else:
        v["status"] = "Starting"
    if pod:
        st_ = pod.get("status") or {}
        v["node"] = (pod.get("spec") or {}).get("nodeName", "")
        v["startedAt"] = st_.get("startTime", "")
        v["lastActive"] = meta_ann(pod).get(ANN["active"], "")
        for cs in st_.get("containerStatuses") or []:
            wt = (cs.get("state") or {}).get("waiting")
            if wt:
                v["waiting"] = f"{wt.get('reason', '')} {wt.get('message', '')}".strip()
                if wt.get("reason") in ("CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull"):
                    v["status"] = "Failed"
        if v["status"] == "Pending":
            for c in st_.get("conditions") or []:
                if c.get("type") == "PodScheduled" and c.get("status") == "False":
                    v["waiting"] = c.get("message", "")
    return v


def list_ns(ctx, ns):
    sts = ctx.k.items("statefulsets", ns=ns, selector=LABEL_WS)
    pods = {p["metadata"]["name"]: p for p in ctx.k.items("pods", ns=ns, selector=LABEL_WS)}
    out = []
    for st in sts:
        v = view(st, pods.get(st["metadata"]["name"] + "-0"))
        if v["expose"]:
            svc = ctx.k.get("service", st["metadata"]["name"] + "-public", ns=ns)
            ing = (((svc or {}).get("status") or {}).get("loadBalancer") or {}).get("ingress") or [{}]
            v["publicAddr"] = ing[0].get("ip") or ing[0].get("hostname") or ""
        out.append(v)
    return out


# ------------------------------------------------------------------ me / profile
def me(ctx):
    cat = catalog(ctx)
    std, lab = ns_of(ctx.user), ns_of(ctx.user, "lab")
    exists = lambda n: bool(ctx.k.get("namespace", n, ns=NO_NS))  # noqa: E731
    try:
        onboarded, has_lab = exists(std), exists(lab)
    except KubeError:
        onboarded, has_lab = False, False
    projects = projects_for(ctx)
    nss = [{"name": std, "tier": "standard", "exists": onboarded}, {"name": lab, "tier": "lab", "exists": has_lab}] + \
          [{"name": p["namespace"], "tier": "project", "exists": True, "project": p["name"]} for p in projects]
    lab_group = getattr(ctx.cfg, "lab_group", "")
    can_lab = has_lab and (not lab_group or lab_group in (ctx.user.get("groups") or []))
    net = cat.get("networking") or {}
    return 200, {"user": {"name": ctx.user["name"], "short": ctx.user["short"], "groups": ctx.user.get("groups") or []}, "namespace": std, "labNamespace": lab,
                 "namespaces": nss, "projects": projects, "auth": ctx.user.get("auth", "session"), "onboarded": onboarded, "canLab": can_lab,
                 "catalogSource": "cluster", "artifactory": False, "artifactoryUrl": (cat.get("artifactory") or {}).get("url", ""),
                 "sharing": bool(ctx.priv), "links": (net.get("links") or {}).get("enabled", True), "mesh": (net.get("mesh") or {}).get("mode", "none"),
                 "storage": cat.get("storage") or {}, "login": ctx.cfg.login_config() if ctx.cfg else {}, "version": getattr(ctx.cfg, "version", "py"), "mock": False}


def profile_get(ctx):
    sec = ctx.k.get("secret", "workspace-profile", ns=ns_of(ctx.user))
    d = (sec or {}).get("data") or {}
    keys = [valid_key(l) for l in b64d(d.get("authorized_keys")).splitlines()]
    return 200, {"sshPublicKeys": [k for k in keys if k], "gitName": b64d(d.get("git_name")), "gitEmail": b64d(d.get("git_email")),
                 "artifactoryUser": b64d(d.get("artifactory_user")), "hasArtifactoryToken": bool(d.get("artifactory_token"))}


def profile_put(ctx):
    b = ctx.body
    keys = []
    for line in b.get("sshPublicKeys") or []:
        norm = valid_key(line)
        if not norm:
            if str(line).strip():
                raise bad("that does not look like an SSH public key (expected e.g. 'ssh-ed25519 AAAA… comment')")
            continue
        if norm not in keys:
            keys.append(norm)
    data = {"authorized_keys": "\n".join(keys) + "\n", "git_name": b.get("gitName", ""), "git_email": b.get("gitEmail", ""), "artifactory_user": b.get("artifactoryUser", "")}
    if b.get("artifactoryToken"):
        data["artifactory_token"] = b["artifactoryToken"]
    ns = ns_of(ctx.user)
    existing = ctx.k.get("secret", "workspace-profile", ns=ns)
    if existing and not b.get("artifactoryToken") and (existing.get("data") or {}).get("artifactory_token"):
        data["artifactory_token"] = b64d(existing["data"]["artifactory_token"])
    ctx.k.apply([{"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "metadata": {"name": "workspace-profile", "namespace": ns, "labels": {W.LABEL_MANAGED: "devspaces-portal"}}, "stringData": data}])
    return 200, {"ok": True, "keys": len(keys)}


def profile_keys(ctx, user=None):
    u = user or ctx.user
    k = ctx.priv if user and ctx.priv else ctx.k
    try:
        sec = k.get("secret", "workspace-profile", ns=ns_of(u))
    except KubeError:
        return []
    return [x for x in (valid_key(l) for l in b64d(((sec or {}).get("data") or {}).get("authorized_keys")).splitlines()) if x]


# ------------------------------------------------------------------ workspaces
def ws_list(ctx):
    nss = [ctx.q("ns")] if ctx.q("ns") else namespaces_for(ctx)
    out = []
    for ns in nss:
        out += list_ns(ctx, ns)
    resp = {"workspaces": out}
    if ctx.q("shared") == "true":
        resp["sharedWithMe"] = shared_with_me(ctx)
    return 200, resp


def shared_with_me(ctx):
    if not ctx.priv:
        return []
    out = []
    for rb in ctx.priv.items("rolebindings", selector=f"{LABEL_SHARE}=workspace,{LABEL_GUEST}={ctx.user['short']}", all_namespaces=True):
        if meta_ann(rb).get(ANN["guest"]) != ctx.user["name"]:
            continue
        ns, name = rb["metadata"]["namespace"], meta_labels(rb).get(LABEL_WS, "")
        try:
            st = ctx.k.get("statefulset", f"ws-{name}", ns=ns)
        except KubeError:
            continue
        if not st:
            continue
        v = view(st, ctx.k.get("pod", f"ws-{name}-0", ns=ns))
        v["owner"] = ns[3:]
        out.append(v)
    return out


def ws_get(ctx):
    name = ctx.params["name"]
    st, ns = locate(ctx, name)
    pod = ctx.k.get("pod", f"ws-{name}-0", ns=ns)
    v = view(st, pod)
    resp = {"workspace": v}
    sec = ctx.k.get("secret", f"ws-{name}-ssh", ns=ns)
    if sec:
        d = sec.get("data") or {}
        hk = b64d(d.get("ssh_host_ed25519_key.pub")).strip()
        keys = []
        for line in b64d(d.get("authorized_keys")).splitlines():
            f = line.split()
            if f:
                keys.append({"type": f[0], "fingerprint": fingerprint(line), "comment": " ".join(f[2:])})
        hkf = hk.split()
        resp["ssh"] = {"hostKeyFingerprint": fingerprint(hk), "knownHostsLine": f"ws-{name} {hkf[0]} {hkf[1]}" if len(hkf) >= 2 else "", "authorizedKeys": keys,
                       "alias": f"ws-{name}", "user": "dev", "port": W.SSH_PORT}
    if v["spec"].get("gitAuth") == "deploy-key":
        gs = ctx.k.get("secret", f"ws-{name}-git", ns=ns)
        if gs:
            resp["deployKey"] = b64d((gs.get("data") or {}).get("ssh_key.pub")).strip()
    if v["expose"]:
        svc = ctx.k.get("service", f"ws-{name}-public", ns=ns)
        ing = (((svc or {}).get("status") or {}).get("loadBalancer") or {}).get("ingress") or [{}]
        resp["publicAddr"] = ing[0].get("ip") or ing[0].get("hostname") or ""
    if not owns(ctx, ns):
        v["owner"] = ns[3:]
    resp["network"] = network_of(ctx, catalog(ctx), st, ns, name)
    return 200, resp


def attachments(ctx, ns):
    out = {}
    for st in ctx.k.items("statefulsets", ns=ns, selector=LABEL_WS):
        for vol in ((st.get("spec") or {}).get("template") or {}).get("spec", {}).get("volumes") or []:
            c = (vol.get("persistentVolumeClaim") or {}).get("claimName")
            if c:
                out.setdefault(c, []).append(meta_labels(st).get(LABEL_WS, ""))
    return out


def ws_create(ctx):
    b, cat = ctx.body, catalog(ctx)
    d = cat["defaults"]
    langs = [l.strip() for l in (b.get("languages") or []) if l and l.strip()]
    langs = list(dict.fromkeys(langs))
    if not langs:
        raise bad("pick at least one language (or 'base' for plain Linux)")
    for l in langs:
        if l not in cat["languages"]:
            raise bad(f"unknown language {l!r}")
    tools = []
    for t in b.get("tools") or []:
        t = t.strip()
        if t and t not in cat["tools"]:
            raise bad(f"unknown tool {t!r}")
        if t and t not in tools:
            tools.append(t)
    for l in langs[1:]:
        if l != "base" and l not in tools and l in cat["tools"]:
            tools.append(l)
    name = (b.get("name") or langs[0]).strip()
    if not W.NAME_RE.match(name):
        raise bad(f"name {name!r} must be lowercase letters, digits or dashes (max 40)")
    prof_name = b.get("profile") or d["profile"]
    profile = cat["profiles"].get(prof_name)
    if not profile:
        raise bad(f"unknown profile {prof_name!r}")
    tier = profile.get("tier", "standard")
    isolation = b.get("isolation") or profile.get("isolation") or d["isolation"]
    if isolation not in cat["isolation"]:
        raise bad(f"unknown isolation {isolation!r}")
    git_auth = b.get("gitAuth") or "deploy-key"
    if git_auth not in ("deploy-key", "token", "none"):
        raise bad("gitAuth must be deploy-key, token or none")
    if git_auth == "token" and not (b.get("gitToken") or "").strip():
        raise bad("a token is required for gitAuth=token")
    for u in (b.get("repo", ""), b.get("branch", ""), b.get("dotfiles", "")):
        if any(c in u for c in " \t\n\"'`$"):
            raise bad("repository, branch and dotfiles must not contain spaces or quotes")
    choice = W.storage_choice(cat, (b.get("storage") or "").strip() or None, "home")
    storage_class = b.get("storageClass") or choice.get("storageClass") or d.get("storageClass", "")
    mounts = []
    for m_ in b.get("mounts") or []:
        if m_ and m_.strip():
            W.parse_mount(m_.strip())
            if m_.strip() not in mounts:
                mounts.append(m_.strip())
    drive = (b.get("fromDrive") or "").strip() or f"ws-{name}-home"
    if b.get("fromDrive") and b.get("fromSnapshot"):
        raise bad("fromDrive and fromSnapshot are mutually exclusive")
    devices = {}
    for res, qty in (b.get("devices") or {}).items():
        if not re.match(r"^[a-z0-9.-]+(/[a-z0-9._-]+)?$", res) or not QTY_RE.match(str(qty)):
            raise bad(f"bad device request {res}={qty}")
        devices[res] = str(qty)
    host_paths = [p for p in (b.get("hostPaths") or []) if p]
    for p in host_paths:
        if not p.startswith("/") or ".." in p:
            raise bad("host paths must be absolute")
    caps = [c.strip().upper().removeprefix("CAP_") for c in (b.get("caps") or []) if c.strip()]
    if tier != "lab" and (devices or host_paths or caps):
        raise bad("devices, host paths and extra capabilities need the hardware or kernel profile")
    for qname, qv in (("size", b.get("size") or d["size"]), ("cpu", b.get("cpu") or d["cpu"]), ("memory", b.get("memory") or d["memory"])):
        if not QTY_RE.match(str(qv)):
            raise bad(f"bad quantity {qv!r} for {qname}")
    idle = b.get("idleTimeout") or d["idleTimeout"]
    W.parse_duration(idle)
    ns = ns_of(ctx.user, tier)
    if b.get("namespace"):
        if not owns(ctx, b["namespace"]):
            raise bad(f"you are not a member of {b['namespace']}")
        ns = b["namespace"]
    if not ctx.k.get("namespace", ns, ns=NO_NS):
        raise bad(f"your namespace {ns} does not exist yet — ask a platform admin to run: workspace admin onboard {ctx.user['name']}" + (" --lab" if tier == "lab" else ""))
    obj = f"ws-{name}"
    # drives
    if b.get("fromDrive"):
        old = ctx.k.get("persistentvolumeclaim", drive, ns=ns)
        if not old:
            raise bad(f"drive {drive} not found in {ns} (see Drives)")
        if meta_labels(old).get(LABEL_DRIVE) != "home":
            raise bad(f"{drive} is not a home drive")
        users = attachments(ctx, ns).get(drive, [])
        if users and users[0] != name:
            raise bad(f"drive {drive} is attached to workspace {users[0]} — delete that workspace first (the drive stays)")
    for m_ in mounts:
        pvc, dname, owner = W.parse_mount(m_)
        if not ctx.k.get("persistentvolumeclaim", pvc, ns=ns):
            raise bad(f"shared drive {dname} ({pvc}) is not in {ns} — " + (f"ask {owner} to share it with you" if owner else "create it on the Drives page"))
    # SSH keys: request + profile + existing secret
    keys = []
    for line in b.get("sshPublicKeys") or []:
        norm = valid_key(line)
        if not norm and str(line).strip():
            raise bad("that does not look like an SSH public key (expected e.g. 'ssh-ed25519 AAAA… comment')")
        if norm and norm not in keys:
            keys.append(norm)
    for kline in profile_keys(ctx):
        if kline not in keys:
            keys.append(kline)
    existing_sec = ctx.k.get("secret", f"{obj}-ssh", ns=ns)
    if existing_sec:
        for line in b64d((existing_sec.get("data") or {}).get("authorized_keys")).splitlines():
            norm = valid_key(line)
            if norm and norm not in keys:
                keys.append(norm)
    if not keys:
        raise bad("add an SSH public key first (Profile page, or paste one in the Connect step) — without one nobody can log in")
    hostkey = W.host_keypair(ctx.k, ns, obj, False)
    git_secret, deploy_pub = None, ""
    if git_auth == "deploy-key":
        gs = ctx.k.get("secret", f"{obj}-git", ns=ns)
        if gs and (gs.get("data") or {}).get("ssh_key.pub"):
            deploy_pub = b64d(gs["data"]["ssh_key.pub"]).strip()
        else:
            priv_, pub_ = W.generate_keypair(f"workspace:{ns}/{obj}")
            git_secret, deploy_pub = {"ssh_key": priv_, "ssh_key.pub": pub_}, pub_
    elif git_auth == "token":
        git_secret = {"token": b["gitToken"].strip(), "username": b.get("gitTokenUser") or "x-access-token"}
    spec = {"lang": langs[0], "languages": langs, "tools": tools, "profile": prof_name, "isolation": isolation,
            "image": b.get("image") or f"{cat['registry']}/{cat['languages'][langs[0]]['image']}:{b.get('tag') or cat['tag']}",
            "repo": b.get("repo", "").strip(), "branch": b.get("branch", "").strip(), "dotfiles": b.get("dotfiles", "").strip(),
            "gitName": b.get("gitName", "").strip(), "gitEmail": b.get("gitEmail", "").strip(),
            "size": b.get("size") or d["size"], "storageClass": storage_class, "storage": choice.get("name", ""), "drive": drive,
            "fromSnapshot": (b.get("fromSnapshot") or "").strip(), "mounts": mounts,
            "cpu": b.get("cpu") or d["cpu"], "memory": b.get("memory") or d["memory"], "cpuRequest": b.get("cpuRequest") or d["cpuRequest"],
            "memoryRequest": b.get("memoryRequest") or d["memoryRequest"], "ephemeralStorage": d.get("ephemeralStorage", "20Gi"),
            "multicast": bool(b.get("multicast")), "devices": devices, "hostPaths": host_paths, "caps": caps, "expose": bool(b.get("expose")),
            "tz": b.get("tz", "").strip(), "idleTimeout": idle, "createdAt": now_iso(), "createdBy": ctx.user["name"], "gitAuth": "ssh" if git_auth == "deploy-key" else git_auth}
    objs = W.render_workspace(ns, name, spec, cat, profile, "\n".join(keys), hostkey, git_secret)
    if git_auth != "none" and git_secret is None:
        objs = [o for o in objs if not (o["kind"] == "Secret" and o["metadata"]["name"] == f"{obj}-git")]
    if ctx.k.get("persistentvolumeclaim", drive, ns=ns):
        objs = [o for o in objs if o["kind"] != "PersistentVolumeClaim"]
        ctx.k.annotate("persistentvolumeclaim", drive, ns, **{ANN["lastws"]: name, ANN["langs"]: ",".join(langs)})
    for o in objs:  # label objects as portal-managed, like the Go portal
        o.setdefault("metadata", {}).setdefault("labels", {})[W.LABEL_MANAGED] = "devspaces-portal"
    ctx.k.apply(objs)
    return 201, {"name": name, "namespace": ns, "deployKey": deploy_pub, "spec": spec, "hostKeyFingerprint": fingerprint(hostkey[1])}


def ws_delete(ctx):
    name = ctx.params["name"]
    st, ns = locate(ctx, name)
    if not owns(ctx, ns):
        raise bad("only the owner can delete a workspace")
    purge = ctx.q("purge") == "true"
    spec = json.loads(meta_ann(st).get(ANN["spec"], "{}") or "{}")
    drive = spec.get("drive") or f"ws-{name}-home"
    ctx.k.run(["delete", "statefulset,service,secret,networkpolicy,role,rolebinding", "-l", f"{LABEL_WS}={name}", "--ignore-not-found"], ns=ns)
    if purge:
        ctx.k.annotate("persistentvolumeclaim", drive, ns, **{ANN["allow"]: "true"})
        ctx.k.delete("persistentvolumeclaim", drive, ns=ns)
    return 200, {"ok": True, "purged": purge, "deleted": name, "drive": drive, "driveKept": not purge}


def ws_scale(ctx, replicas):
    name = ctx.params["name"]
    _, ns = locate(ctx, name)
    ctx.k.scale(f"ws-{name}", replicas, ns)
    return 200, {"ok": True, "replicas": replicas}


def ws_idle(ctx):
    name = ctx.params["name"]
    _, ns = locate(ctx, name)
    v = (ctx.body.get("idleTimeout") or "").strip()
    W.parse_duration(v)
    ctx.k.annotate("statefulset", f"ws-{name}", ns, **{ANN["idle"]: v})
    return 200, {"ok": True, "idleTimeout": v}


def ws_keys(ctx):
    name = ctx.params["name"]
    _, ns = locate(ctx, name)
    norm = valid_key(ctx.body.get("publicKey"))
    if not norm:
        raise bad("that does not look like an SSH public key")
    sec = ctx.k.get("secret", f"ws-{name}-ssh", ns=ns) or {}
    existing = b64d((sec.get("data") or {}).get("authorized_keys")).strip()
    if norm in existing.splitlines():
        return 200, {"ok": True, "added": False}
    ctx.k.patch("secret", f"ws-{name}-ssh", {"stringData": {"authorized_keys": existing + "\n" + norm + "\n"}}, ns=ns)
    return 200, {"ok": True, "added": True, "fingerprint": fingerprint(norm)}


def ws_logs(ctx):
    name = ctx.params["name"]
    st, ns = locate(ctx, name)
    tail = int(ctx.q("tail") or 200)
    if ((st.get("spec") or {}).get("replicas") or 0) == 0:
        return 200, {"logs": "", "note": "workspace is stopped; start it to see boot logs"}
    try:
        return 200, {"logs": ctx.k.logs(f"ws-{name}-0", ns, tail=max(1, min(tail, 2000)))}
    except KubeError:
        return 200, {"logs": "", "note": "no logs yet (pod starting)"}


# ------------------------------------------------------------------ network / links / shares
def links_of(ctx, ns, name):
    out = []
    for np in ctx.k.items("networkpolicies", ns=ns, selector=f"{LABEL_LINK}=peer,{LABEL_WS}={name}"):
        peer = meta_ann(np).get(ANN["peer"], "?")
        tenant, _, pws = peer.partition("/")
        ports = [p.get("port") for p in ((np.get("spec") or {}).get("ingress") or [{}])[0].get("ports", [])]
        status = "pending-peer"
        if ctx.priv:
            try:
                if ctx.priv.get("networkpolicy", W._peer_policy_name(pws, ns, name), ns=f"ws-{tenant}"):
                    status = "linked"
            except KubeError:
                pass
        out.append({"workspace": name, "peer": peer, "peerDNS": f"ws-{pws}.ws-{tenant}.svc.cluster.local", "ports": ports, "status": status})
    return out


def network_of(ctx, cat, st, ns, name):
    pod = ctx.k.get("pod", f"ws-{name}-0", ns=ns) or {}
    same = [meta_labels(s).get(LABEL_WS) for s in ctx.k.items("statefulsets", ns=ns, selector=LABEL_WS) if meta_labels(s).get(LABEL_WS) != name]
    spec = json.loads(meta_ann(st).get(ANN["spec"], "{}") or "{}")
    nso = None
    for k in (ctx.k, ctx.priv):
        if k:
            try:
                nso = k.get("namespace", ns, ns=NO_NS)
            except KubeError:
                nso = None
            if nso:
                break
    mc = {"mode": "off"}
    if spec.get("multicast"):
        mode = W.mcast_mode(cat)
        mc = {"mode": mode, "iface": W.mcast_iface(cat)}
    net = cat.get("networking") or {}
    return {"dns": f"ws-{name}.{ns}.svc.cluster.local", "podIP": (pod.get("status") or {}).get("podIP", ""), "namespace": ns, "sameNamespace": same,
            "links": links_of(ctx, ns, name), "multicast": mc, "mesh": mesh_of(meta_labels(nso or {})), "linksEnabled": (net.get("links") or {}).get("enabled", True)}


def ws_network(ctx):
    name = ctx.params["name"]
    st, ns = locate(ctx, name)
    return 200, network_of(ctx, catalog(ctx), st, ns, name)


def links_get(ctx):
    name = ctx.params["name"]
    _, ns = locate(ctx, name)
    return 200, {"links": links_of(ctx, ns, name)}


def links_add(ctx):
    name = ctx.params["name"]
    cat = catalog(ctx)
    if not ((cat.get("networking") or {}).get("links") or {}).get("enabled", True):
        raise bad("links are disabled by the platform (networking.links.enabled=false)")
    _, ns = locate(ctx, name)
    if not owns(ctx, ns):
        raise bad("you can only link your own workspaces")
    peer_ns, peer_ws = W.split_ref((ctx.body.get("peer") or "").strip())
    if not peer_ns or not W.NAME_RE.match(peer_ws or ""):
        raise bad("peer must be OWNER/WORKSPACE (workspaces in your own namespace are already connected)")
    if peer_ns == ns:
        raise bad(f"{peer_ws} is in the same namespace — already reachable, no link needed")
    ports = []
    for p in ctx.body.get("ports") or []:
        if not isinstance(p, int) or not 1 <= p <= 65535:
            raise bad(f"bad port {p}")
        ports.append(p)
    ctx.k.apply([W.render_link(ns, name, peer_ns, peer_ws, ports)])
    for l in links_of(ctx, ns, name):
        if l["peer"] == f"{peer_ns[3:]}/{peer_ws}":
            return 201, l
    return 201, {"workspace": name, "peer": ctx.body.get("peer"), "status": "pending-peer", "ports": ports}


def links_delete(ctx):
    name, peer = ctx.params["name"], ctx.params["peer"]
    _, ns = locate(ctx, name)
    peer_ns, peer_ws = W.split_ref(peer)
    if not peer_ns:
        raise bad("peer must be OWNER/WORKSPACE")
    ctx.k.delete("networkpolicy", W._peer_policy_name(name, peer_ns, peer_ws), ns=ns)
    return 200, {"removed": peer}


def need_priv(ctx):
    if not ctx.priv:
        raise ApiError(501, "sharing is disabled on this portal (-sharing=false or no ServiceAccount)")


def shares_get(ctx):
    name = ctx.params["name"]
    _, ns = locate(ctx, name)
    rbs = ctx.k.items("rolebindings", ns=ns, selector=f"{LABEL_SHARE}=workspace,{LABEL_WS}={name}")
    return 200, {"shares": [{"user": meta_ann(rb).get(ANN["guest"]) or meta_labels(rb).get(LABEL_GUEST), "kind": "workspace", "resource": name,
                             "createdAt": rb["metadata"].get("creationTimestamp", "")} for rb in rbs]}


def shares_add(ctx):
    need_priv(ctx)
    name = ctx.params["name"]
    guest = (ctx.body.get("user") or "").strip()
    if not guest or any(c in guest for c in " \t\n\"'"):
        raise bad("user is required (e-mail / UPN as the API server knows it)")
    if guest.lower() == ctx.user["name"].lower():
        raise bad("that is you")
    _, ns = locate(ctx, name)
    if not owns(ctx, ns):
        raise bad("only the owner can share a workspace")
    keys = []
    norm = valid_key(ctx.body.get("publicKey"))
    if norm:
        keys.append(norm)
    if not keys:
        keys = profile_keys(ctx, {"name": guest, "short": W.sanitize_user(guest)})
    if not keys:
        raise bad(f"{guest} has no SSH key in their profile yet — ask them to add one on the Profile page, or paste their public key")
    sec = ctx.k.get("secret", f"ws-{name}-ssh", ns=ns) or {}
    tag = f" guest:{guest}"
    lines = [l for l in b64d((sec.get("data") or {}).get("authorized_keys")).splitlines() if l.strip() and not l.endswith(tag)]
    lines += [" ".join(k.split()[:2]) + tag for k in keys]
    ctx.k.patch("secret", f"ws-{name}-ssh", {"stringData": {"authorized_keys": "\n".join(lines) + "\n"}}, ns=ns)
    ctx.priv.apply([W._guest_role(ns, name), W._guest_binding(ns, name, guest)])
    return 201, {"user": guest, "kind": "workspace", "resource": name}


def shares_delete(ctx):
    need_priv(ctx)
    name, guest = ctx.params["name"], ctx.params["user"]
    _, ns = locate(ctx, name)
    if not owns(ctx, ns):
        raise bad("only the owner can revoke a share")
    sec = ctx.k.get("secret", f"ws-{name}-ssh", ns=ns)
    if sec:
        lines = [l for l in b64d((sec.get("data") or {}).get("authorized_keys")).splitlines() if l.strip() and not l.endswith(f" guest:{guest}")]
        ctx.k.patch("secret", f"ws-{name}-ssh", {"stringData": {"authorized_keys": "\n".join(lines) + "\n"}}, ns=ns)
    ctx.priv.delete("rolebinding", f"share-{name}-{W.sanitize_user(guest)}", ns=ns)
    return 200, {"revoked": guest}


# ------------------------------------------------------------------ drives & snapshots
def drive_view(p, att):
    l, a = meta_labels(p), meta_ann(p)
    ns = p["metadata"]["namespace"]
    spec = p.get("spec") or {}
    v = {"name": p["metadata"]["name"], "namespace": ns, "kind": l.get(LABEL_DRIVE, ""), "owner": ns[3:],
         "size": ((p.get("status") or {}).get("capacity") or {}).get("storage") or (spec.get("resources") or {}).get("requests", {}).get("storage", ""),
         "storage": a.get(ANN["storage"], ""), "storageClass": spec.get("storageClassName", ""), "accessModes": spec.get("accessModes") or [],
         "phase": (p.get("status") or {}).get("phase", ""), "mountedBy": [], "sharedWith": [], "sharedBy": l.get("devspaces.io/shared-by", ""),
         "lastWorkspace": a.get(ANN["lastws"], ""), "languages": [x for x in a.get(ANN["langs"], "").split(",") if x], "createdAt": p["metadata"].get("creationTimestamp", ""),
         "protected": l.get(LABEL_DRIVE) == "home" and a.get(ANN["allow"]) != "true", "snapshots": 0}
    users = att.get(v["name"], [])
    if v["kind"] == "home":
        v["attachedTo"] = users[0] if users else ""
    else:
        v["mountedBy"] = users
    if v["kind"] == "shared-mount":
        v["owner"] = v["sharedBy"]
    return v


def shared_guests(ctx, owner, drive):
    if not ctx.priv:
        return []
    out = []
    for p in ctx.priv.items("persistentvolumeclaims", selector=f"{LABEL_DRIVE}=shared-mount,devspaces.io/shared-by={owner}", all_namespaces=True):
        if meta_ann(p).get("devspaces.io/source-drive") == drive:
            out.append(meta_ann(p).get(ANN["guest"]) or p["metadata"]["namespace"][3:])
    return sorted(out)


def drives_list(ctx):
    out = []
    for ns in namespaces_for(ctx):
        att = attachments(ctx, ns)
        snaps = ctx.k.items("volumesnapshots", ns=ns, selector=LABEL_DRIVE)
        for p in ctx.k.items("persistentvolumeclaims", ns=ns, selector=LABEL_DRIVE):
            v = drive_view(p, att)
            v["snapshots"] = sum(1 for s in snaps if meta_labels(s).get(LABEL_DRIVE) == v["name"])
            if v["kind"] == "shared":
                v["sharedWith"] = shared_guests(ctx, ns[3:], v["name"])
            out.append(v)
    return 200, {"drives": out}


def find_drive(ctx, name):
    if not W.NAME_RE.match(name or ""):
        raise bad("bad drive name")
    cands = [target_ns(ctx)] if target_ns(ctx) else namespaces_for(ctx)
    for ns in cands:
        try:
            p = ctx.k.get("persistentvolumeclaim", name, ns=ns)
        except KubeError as e:
            if e.status == 403:
                continue
            raise
        if p:
            if not meta_labels(p).get(LABEL_DRIVE):
                raise bad(f"{name} is a volume but not a workspace drive")
            return p, ns
    for ns in cands:  # a workspace name → its home drive
        try:
            st = ctx.k.get("statefulset", f"ws-{name}", ns=ns)
        except KubeError:
            st = None
        if st:
            drive = json.loads(meta_ann(st).get(ANN["spec"], "{}") or "{}").get("drive") or f"ws-{name}-home"
            p = ctx.k.get("persistentvolumeclaim", drive, ns=ns)
            if p:
                return p, ns
    raise ApiError(404, "not found")


def snapshots_of(ctx, ns, drive):
    out = []
    for sn in ctx.k.items("volumesnapshots", ns=ns, selector=f"{LABEL_DRIVE}={drive}"):
        st = sn.get("status") or {}
        out.append({"name": sn["metadata"]["name"], "namespace": ns, "drive": drive, "readyToUse": bool(st.get("readyToUse")), "restoreSize": str(st.get("restoreSize", "")),
                    "languages": meta_ann(sn).get(ANN["langs"], ""), "createdAt": st.get("creationTime") or sn["metadata"].get("creationTimestamp", ""), "error": (st.get("error") or {}).get("message", "")})
    return out


def drive_get(ctx):
    p, ns = find_drive(ctx, ctx.params["name"])
    v = drive_view(p, attachments(ctx, ns))
    snaps = snapshots_of(ctx, ns, v["name"])
    v["snapshots"] = len(snaps)
    if v["kind"] == "shared":
        v["sharedWith"] = shared_guests(ctx, ns[3:], v["name"])
    return 200, {"drive": v, "snapshots": snaps}


def drive_create(ctx):
    b, cat = ctx.body, catalog(ctx)
    name = (b.get("name") or "").strip()
    if not W.NAME_RE.match(name):
        raise bad("drive name: lowercase letters, digits, dashes")
    size = b.get("size") or "100Gi"
    if not QTY_RE.match(size):
        raise bad(f"bad size {size!r}")
    choice = W.storage_choice(cat, b.get("storage") or None, "shared")
    ns = ns_of(ctx.user)
    if b.get("namespace"):
        if not owns(ctx, b["namespace"]):
            raise bad("not your namespace")
        ns = b["namespace"]
    if ctx.k.get("persistentvolumeclaim", f"shared-{name}", ns=ns):
        raise bad(f"shared drive {name} already exists")
    obj = W.render_shared_drive(ns, name, size, choice, ns[3:])
    obj["metadata"]["labels"][W.LABEL_MANAGED] = "devspaces-portal"
    ctx.k.apply([obj])
    p = ctx.k.get("persistentvolumeclaim", f"shared-{name}", ns=ns)
    return 201, drive_view(p, {})


def drive_delete(ctx):
    p, ns = find_drive(ctx, ctx.params["name"])
    name = p["metadata"]["name"]
    users = attachments(ctx, ns).get(name, [])
    if users:
        raise ApiError(409, f"{name} is in use by workspace {users[0]} — delete that workspace first (the drive stays), or delete it with purge")
    if meta_labels(p).get(LABEL_DRIVE) == "shared" and shared_guests(ctx, ns[3:], name):
        raise ApiError(409, "the drive is still shared with other developers — revoke the shares first")
    ctx.k.annotate("persistentvolumeclaim", name, ns, **{ANN["allow"]: "true"})
    ctx.k.delete("persistentvolumeclaim", name, ns=ns)
    return 200, {"deleted": name}


def drive_resize(ctx):
    size = (ctx.body.get("size") or "").strip()
    if not QTY_RE.match(size):
        raise bad(f"bad size {size!r}")
    p, ns = find_drive(ctx, ctx.params["name"])
    ctx.k.patch("persistentvolumeclaim", p["metadata"]["name"], {"spec": {"resources": {"requests": {"storage": size}}}}, ns=ns)
    return 200, {"name": p["metadata"]["name"], "size": size, "note": "grows online if the StorageClass allows expansion; some drivers need a stop/start"}


def snapshots_get(ctx):
    p, ns = find_drive(ctx, ctx.params["name"])
    return 200, {"snapshots": snapshots_of(ctx, ns, p["metadata"]["name"])}


def snapshot_create(ctx):
    p, ns = find_drive(ctx, ctx.params["name"])
    drive = p["metadata"]["name"]
    snap = (ctx.body.get("name") or "").strip() or f"{drive}-{time.strftime('%Y%m%d-%H%M%S', time.gmtime())}"
    if not re.match(r"^[a-z0-9]([-a-z0-9.]{0,61}[a-z0-9])?$", snap):
        raise bad(f"bad snapshot name {snap!r}")
    cat = catalog(ctx)
    obj = {"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshot",
           "metadata": {"name": snap, "namespace": ns, "labels": {LABEL_DRIVE: drive, W.LABEL_MANAGED: "devspaces-portal"},
                        "annotations": {ANN["langs"]: meta_ann(p).get(ANN["langs"], ""), ANN["lastws"]: meta_ann(p).get(ANN["lastws"], "")}},
           "spec": {"source": {"persistentVolumeClaimName": drive}}}
    sc = (cat.get("storage") or {}).get("snapshotClass")
    if sc:
        obj["spec"]["volumeSnapshotClassName"] = sc
    try:
        ctx.k.apply([obj])
    except KubeError as e:
        raise ApiError(e.status, f"snapshot: {e} (is the CSI snapshot controller installed? CONTRACT C15)")
    return 201, {"name": snap, "namespace": ns, "drive": drive, "languages": meta_ann(p).get(ANN["langs"], "")}


def snapshot_delete(ctx):
    name = ctx.params["name"]
    cands = [target_ns(ctx)] if target_ns(ctx) else namespaces_for(ctx)
    for ns in cands:
        try:
            if ctx.k.get("volumesnapshot", name, ns=ns):
                ctx.k.delete("volumesnapshot", name, ns=ns)
                return 200, {"deleted": name}
        except KubeError:
            continue
    raise ApiError(404, "not found")


def drive_shares_get(ctx):
    p, ns = find_drive(ctx, ctx.params["name"])
    return 200, {"shares": [{"user": g, "kind": "drive", "resource": p["metadata"]["name"]} for g in shared_guests(ctx, ns[3:], p["metadata"]["name"])]}


def drive_share_add(ctx):
    need_priv(ctx)
    guest = (ctx.body.get("user") or "").strip()
    if not guest or any(c in guest for c in " \t\n\"'"):
        raise bad("user is required")
    p, ns = find_drive(ctx, ctx.params["name"])
    name = p["metadata"]["name"]
    if not owns(ctx, ns) or meta_labels(p).get(LABEL_DRIVE) != "shared":
        raise bad("only your own shared drives can be shared (home drives are single-writer disks: share the workspace instead)")
    if "ReadWriteMany" not in ((p.get("spec") or {}).get("accessModes") or []):
        raise bad("this drive is not ReadWriteMany; its StorageClass cannot be mounted by two namespaces")
    pv_name = (p.get("spec") or {}).get("volumeName")
    if not pv_name:
        raise bad("the drive has no volume yet (mount it in a workspace once so the storage driver provisions it)")
    pv = ctx.priv.get("persistentvolume", pv_name, ns=NO_NS)
    guest_ns = f"ws-{W.sanitize_user(guest)}"
    if not ctx.priv.get("namespace", guest_ns, ns=NO_NS):
        raise bad(f"{guest} is not onboarded (namespace {guest_ns} missing)")
    objs = W.render_shared_mount(pv, ns[3:], name.removeprefix("shared-"), guest_ns, bool(ctx.body.get("readOnly")))
    objs[1]["metadata"]["annotations"].update({"devspaces.io/source-drive": name, ANN["guest"]: guest, "devspaces.io/read-only": str(bool(ctx.body.get("readOnly"))).lower()})
    ctx.priv.apply(objs)
    return 201, {"user": guest, "kind": "drive", "resource": name, "readOnly": bool(ctx.body.get("readOnly"))}


def drive_share_delete(ctx):
    need_priv(ctx)
    p, ns = find_drive(ctx, ctx.params["name"])
    if not owns(ctx, ns):
        raise bad("only the owner can revoke")
    guest = ctx.params["user"]
    guest_ns = f"ws-{W.sanitize_user(guest)}"
    pvc = f"shared-{ns[3:]}-{p['metadata']['name'].removeprefix('shared-')}"
    ctx.priv.delete("persistentvolumeclaim", pvc, ns=guest_ns)
    ctx.priv.run(["delete", "persistentvolume", f"{pvc}-{guest_ns[3:]}", "--ignore-not-found"], ns=NO_NS, check=False)
    return 200, {"revoked": guest}


def projects(ctx):
    return 200, {"projects": projects_for(ctx)}


def artifactory_images(ctx):
    cat = catalog(ctx)
    images = {v["image"]: [cat.get("tag")] for v in cat["languages"].values() if isinstance(v, dict) and v.get("image")}
    return 200, {"registry": cat.get("registry"), "defaultTag": cat.get("tag"), "source": "catalog", "images": images}


def artifactory_tools(ctx):
    cat = catalog(ctx)
    tools = [{"name": n, "category": t.get("category", "other"), "description": t.get("description", ""), "docs": t.get("docs"), "builtin": bool(t.get("builtin")),
              "source": "artifactory" if t.get("artifact") else "image-feature", "artifact": t.get("artifact")}
             for n, t in cat["tools"].items() if not n.startswith("$") and isinstance(t, dict)]
    tools.sort(key=lambda x: (x["category"], x["name"]))
    return 200, {"tools": tools, "artifactory": False, "artifactoryUrl": (cat.get("artifactory") or {}).get("url", "")}
