"""stub.py — `api.py --stub`: answer the API from the static JSON in portal/web/stub/ with a
small in-memory state (create, start/stop, drives, snapshots, shares, links), so the UI, wsctl
and curl can be exercised against a real HTTP server without a cluster. Same fixtures and
response shapes as the browser stub (portal/web/stub.js) and api/openapi.yaml."""
from __future__ import annotations

import base64
import copy
import json
import re
import threading
import time
from pathlib import Path

NAME_RE = re.compile(r"^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$")


def now_iso():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


class StubError(Exception):
    def __init__(self, status, message):
        super().__init__(message)
        self.status = status


class StubStore:
    def __init__(self, fixtures_dir: Path, catalog: dict):
        self.dir = fixtures_dir
        self.cat = catalog
        self.lock = threading.Lock()
        self.fx = {}
        for name in ["me", "profile", "workspaces", "drives", "snapshots", "network", "shares", "projects"]:
            p = fixtures_dir / f"{name}.json"
            self.fx[name] = json.loads(p.read_text()) if p.is_file() else {}
        self.me = copy.deepcopy(self.fx.get("me") or {"user": {"name": "demo@example.com", "short": "demo", "groups": []}, "namespace": "ws-demo", "labNamespace": "ws-demo-lab"})
        self.profile = copy.deepcopy(self.fx.get("profile") or {"sshPublicKeys": [], "gitName": "", "gitEmail": ""})
        self.workspaces = {}
        for w in (self.fx.get("workspaces") or {}).get("workspaces", []):
            w = copy.deepcopy(w)
            w["_keys"] = list(self.profile.get("sshPublicKeys", []))
            w["_hostKey"] = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubHostKey000000000000000000000000000000"
            w["_deployKey"] = f"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubDeployKey0000000000000000000000000000 workspace:{w['namespace']}/ws-{w['name']}" if (w.get("spec") or {}).get("gitAuth") == "deploy-key" else ""
            self.workspaces[w["name"]] = w
        self.shared_with_me = copy.deepcopy((self.fx.get("workspaces") or {}).get("sharedWithMe", []))
        self.drives = {d["name"]: copy.deepcopy(d) for d in (self.fx.get("drives") or {}).get("drives", [])}
        self.snapshots = copy.deepcopy((self.fx.get("snapshots") or {}).get("snapshots", []))
        self.shares = {"api": copy.deepcopy((self.fx.get("shares") or {}).get("shares", []))}
        self.links = {}
        for l in (self.fx.get("network") or {}).get("links", []):
            self.links.setdefault(l.get("workspace", "api"), []).append(copy.deepcopy(l))
        self.projects = copy.deepcopy((self.fx.get("projects") or {}).get("projects", []))

    # ---- helpers
    def tick(self):
        t = time.time()
        for w in self.workspaces.values():
            if w["status"] in ("Pending", "Starting") and t - w.get("_since", 0) > 5:
                w.update(status="Running", ready=True, lastActive=now_iso(), startedAt=w.get("startedAt") or now_iso(), node="node-1")
        for s in self.snapshots:
            if not s.get("readyToUse") and t - s.get("_since", 0) > 3:
                s["readyToUse"] = True

    @staticmethod
    def view(w):
        return {k: v for k, v in w.items() if not k.startswith("_")}

    def attached_to(self, drive):
        return next((w["name"] for w in self.workspaces.values() if w.get("drive") == drive), "")

    @staticmethod
    def fp(k):
        return "SHA256:" + base64.b64encode((k or "").encode()).decode()[:43]

    def find_drive(self, ref):
        return self.drives.get(ref) or self.drives.get((self.workspaces.get(ref) or {}).get("drive", ""))

    def network_of(self, w):
        base = copy.deepcopy(self.fx.get("network") or {})
        same = [x["name"] for x in self.workspaces.values() if x["namespace"] == w["namespace"] and x["name"] != w["name"]]
        base.update(dns=w.get("dns", ""), podIP=base.get("podIP", "10.244.1.23") if w["status"] == "Running" else "", namespace=w["namespace"], sameNamespace=same, links=self.links.get(w["name"], []))
        base.setdefault("multicast", {"mode": "off"}); base.setdefault("mesh", "none"); base.setdefault("linksEnabled", True)
        return base

    def boot_log(self, w):
        spec = w.get("spec") or {}
        lines = [f"[workspace 10:00:01] workspace '{w['name']}' starting: lang={spec.get('lang')} profile={spec.get('profile')} tools={','.join(spec.get('tools') or []) or 'none'}",
                 "[workspace 10:00:01] first boot: seeding /home/dev from /opt/workspace/skel", "[workspace 10:00:02] sshd listening on 127.0.0.1:2222 (pid 42)"]
        s = 4
        for t in spec.get("tools") or []:
            lines.append(f"[workspace 10:00:{s:02d}] installing tool: {t}"); s += 3; lines.append(f"[workspace 10:00:{s:02d}] tool ready: {t}")
        if spec.get("repo"):
            lines.append(f"[workspace 10:00:{s:02d}] cloning {spec['repo']}"); s += 4; lines.append(f"[workspace 10:00:{s:02d}] clone complete")
        lines.append(f"[workspace 10:00:{s + 1:02d}] ready")
        return "\n".join(lines)

    # ---- the handler
    def handle(self, method, path, query, body):
        with self.lock:
            self.tick()
            return self._handle(method, path, query or {}, body or {})

    def _handle(self, method, path, q, body):  # noqa: C901 - a router
        cat = self.cat
        m = lambda rx: re.match(rx, path)  # noqa: E731
        if path == "/api/me":
            return 200, {**self.me, "projects": self.projects, "catalogSource": "stub", "auth": "stub", "version": "stub"}
        if path == "/api/projects":
            return 200, {"projects": self.projects}
        if path == "/api/catalog":
            out = copy.deepcopy(cat)
            for k in ("languages", "tools", "profiles", "isolation", "devices"):
                out[k + "Order"] = sorted(x for x in (out.get(k) or {}) if not x.startswith("$"))
            out["source"] = "stub"
            return 200, out
        if path == "/api/artifactory/images":
            images = {l["image"]: [cat.get("tag"), "2026.09"] for l in (cat.get("languages") or {}).values() if isinstance(l, dict) and l.get("image")}
            return 200, {"registry": cat.get("registry"), "defaultTag": cat.get("tag"), "source": "stub", "images": images}
        if path == "/api/artifactory/tools":
            tools = []
            for name, t in (cat.get("tools") or {}).items():
                if name.startswith("$") or not isinstance(t, dict):
                    continue
                tools.append({"name": name, "category": t.get("category", "other"), "description": t.get("description", ""), "docs": t.get("docs"), "builtin": bool(t.get("builtin")),
                              "source": "artifactory" if t.get("artifact") else "image-feature", "artifact": t.get("artifact"),
                              "availability": {"available": True, "url": "", "versions": [t["artifact"].get("version")] if t.get("artifact") else []} if t.get("artifact") else None})
            tools.sort(key=lambda x: (x["category"], x["name"]))
            return 200, {"tools": tools, "artifactory": False, "artifactoryUrl": (cat.get("artifactory") or {}).get("url", "")}
        if path == "/api/profile":
            if method == "GET":
                return 200, self.profile
            self.profile.update(sshPublicKeys=body.get("sshPublicKeys") or [], gitName=body.get("gitName", ""), gitEmail=body.get("gitEmail", ""),
                                artifactoryUser=body.get("artifactoryUser", ""), hasArtifactoryToken=self.profile.get("hasArtifactoryToken") or bool(body.get("artifactoryToken")))
            return 200, {"ok": True, "keys": len(self.profile["sshPublicKeys"])}

        # ---- workspaces
        if path == "/api/workspaces" and method == "GET":
            out = {"workspaces": [self.view(w) for w in self.workspaces.values()]}
            if q.get("shared") == "true":
                out["sharedWithMe"] = self.shared_with_me
            return 200, out
        if path == "/api/workspaces" and method == "POST":
            return self.create(body)
        mm = m(r"^/api/workspaces/([^/]+)$")
        if mm:
            name = mm.group(1)
            if q.get("owner"):
                sw = next((w for w in self.shared_with_me if w["name"] == name), None)
                if not sw:
                    raise StubError(404, "not found")
                return 200, {"workspace": sw, "ssh": {"alias": "ws-" + name, "user": "dev", "port": 2222, "hostKeyFingerprint": "SHA256:stub", "knownHostsLine": "", "authorizedKeys": []}, "network": {**self.network_of(sw), "dns": sw.get("dns", "")}}
            w = self.workspaces.get(name)
            if not w:
                raise StubError(404, "not found")
            if method == "DELETE":
                purge = q.get("purge") == "true"
                del self.workspaces[name]; self.shares.pop(name, None); self.links.pop(name, None)
                if purge:
                    self.drives.pop(w.get("drive", ""), None)
                elif w.get("drive") in self.drives:
                    self.drives[w["drive"]]["attachedTo"] = ""
                return 200, {"ok": True, "purged": purge, "deleted": name, "drive": w.get("drive"), "driveKept": not purge}
            keys = []
            for k in w["_keys"]:
                f = k.split()
                keys.append({"type": f[0], "fingerprint": self.fp(f[1] if len(f) > 1 else ""), "comment": " ".join(f[2:])})
            out = {"workspace": self.view(w), "ssh": {"hostKeyFingerprint": self.fp(w["_hostKey"]), "knownHostsLine": f"ws-{name} {w['_hostKey']}", "alias": "ws-" + name, "user": "dev", "port": 2222, "authorizedKeys": keys}, "network": self.network_of(w)}
            if w.get("_deployKey"):
                out["deployKey"] = w["_deployKey"]
            if w.get("expose"):
                out["publicAddr"] = "203.0.113.42"
            return 200, out
        mm = m(r"^/api/workspaces/([^/]+)/(start|stop|idle|keys|logs|network|shares|links|tunnel)$")
        if mm:
            name, action = mm.groups()
            w = self.workspaces.get(name)
            if not w:
                raise StubError(404, "not found")
            if action == "start":
                if w["status"] == "Stopped":
                    w.update(status="Pending", ready=False, _since=time.time())
                return 200, {"ok": True, "replicas": 1}
            if action == "stop":
                w.update(status="Stopped", ready=False, startedAt="", node=""); return 200, {"ok": True, "replicas": 0}
            if action == "idle":
                w["idleTimeout"] = body.get("idleTimeout"); (w.get("spec") or {})["idleTimeout"] = w["idleTimeout"]; return 200, {"ok": True, "idleTimeout": w["idleTimeout"]}
            if action == "keys":
                k = (body.get("publicKey") or "").strip()
                if not re.match(r"^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp\d+) [A-Za-z0-9+/=]+", k):
                    raise StubError(400, "that does not look like an SSH public key")
                added = k not in w["_keys"]
                if added:
                    w["_keys"].append(k)
                return 200, {"ok": True, "added": added}
            if action == "logs":
                return (200, {"logs": "", "note": "workspace is stopped; start it to see boot logs"}) if w["status"] == "Stopped" else (200, {"logs": self.boot_log(w)})
            if action == "network":
                return 200, self.network_of(w)
            if action == "tunnel":
                raise StubError(501, "the stub has no SSH tunnel (use the Go portal: -mock bridges to an echo server)")
            if action == "shares":
                if method == "GET":
                    return 200, {"shares": self.shares.get(name, [])}
                u = (body.get("user") or "").strip()
                if not u:
                    raise StubError(400, "user is required")
                self.shares[name] = [s for s in self.shares.get(name, []) if s["user"] != u] + [{"user": u, "kind": "workspace", "resource": name, "createdAt": now_iso()}]
                if not any(k.endswith("guest:" + u) for k in w["_keys"]):
                    w["_keys"].append("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubGuestKey00000000000000000000000000000 guest:" + u)
                return 201, {"user": u, "kind": "workspace", "resource": name}
            if action == "links":
                if method == "GET":
                    return 200, {"links": self.links.get(name, [])}
                peer = (body.get("peer") or "").strip()
                if "/" not in peer:
                    raise StubError(400, "peer must be OWNER/WORKSPACE")
                owner, pws = peer.rsplit("/", 1)
                short = re.sub(r"[^a-z0-9-]", "-", owner.split("@")[0].lower())
                link = {"workspace": name, "peer": f"{short}/{pws}", "peerDNS": f"ws-{pws}.ws-{short}.svc.cluster.local", "ports": body.get("ports") or [], "status": "linked" if short == "bob" else "pending-peer"}
                self.links[name] = [l for l in self.links.get(name, []) if l["peer"] != link["peer"]] + [link]
                return 201, link
        mm = m(r"^/api/workspaces/([^/]+)/shares/(.+)$")
        if mm and method == "DELETE":
            name, u = mm.groups()
            self.shares[name] = [s for s in self.shares.get(name, []) if s["user"] != u]
            if name in self.workspaces:
                self.workspaces[name]["_keys"] = [k for k in self.workspaces[name]["_keys"] if not k.endswith("guest:" + u)]
            return 200, {"revoked": u}
        mm = m(r"^/api/workspaces/([^/]+)/links/(.+)$")
        if mm and method == "DELETE":
            name, peer = mm.groups()
            self.links[name] = [l for l in self.links.get(name, []) if l["peer"] != peer]
            return 200, {"removed": peer}

        # ---- drives & snapshots
        if path == "/api/drives" and method == "GET":
            ds = []
            for d in self.drives.values():
                d = dict(d)
                if d.get("kind") == "home":
                    d["attachedTo"] = self.attached_to(d["name"])
                d["snapshots"] = sum(1 for s in self.snapshots if s["drive"] == d["name"])
                ds.append(d)
            return 200, {"drives": ds}
        if path == "/api/drives" and method == "POST":
            n = body.get("name") or ""
            if not NAME_RE.match(n):
                raise StubError(400, "drive name: lowercase letters, digits, dashes")
            name = "shared-" + n
            if name in self.drives:
                raise StubError(400, f"shared drive {n} already exists")
            self.drives[name] = {"name": name, "namespace": self.me["namespace"], "kind": "shared", "owner": self.me["user"]["short"], "size": body.get("size") or "100Gi", "storage": body.get("storage") or "shared",
                                 "storageClass": "", "accessModes": ["ReadWriteMany"], "phase": "Bound", "mountedBy": [], "sharedWith": [], "languages": [], "createdAt": now_iso(), "protected": False, "snapshots": 0}
            return 201, self.drives[name]
        mm = m(r"^/api/drives/([^/]+)$")
        if mm:
            d = self.find_drive(mm.group(1))
            if not d:
                raise StubError(404, "not found")
            if method == "DELETE":
                if d.get("kind") == "home" and self.attached_to(d["name"]):
                    raise StubError(409, f"{d['name']} is in use by workspace {self.attached_to(d['name'])} — delete that workspace first (the drive stays)")
                if d.get("sharedWith"):
                    raise StubError(409, "the drive is still shared with other developers — revoke the shares first")
                del self.drives[d["name"]]
                return 200, {"deleted": d["name"]}
            dv = dict(d)
            if dv.get("kind") == "home":
                dv["attachedTo"] = self.attached_to(d["name"])
            return 200, {"drive": dv, "snapshots": [s for s in self.snapshots if s["drive"] == d["name"]]}
        mm = m(r"^/api/drives/([^/]+)/resize$")
        if mm:
            d = self.find_drive(mm.group(1))
            if not d:
                raise StubError(404, "not found")
            d["size"] = body.get("size")
            return 200, {"name": d["name"], "size": d["size"]}
        mm = m(r"^/api/drives/([^/]+)/snapshots$")
        if mm:
            d = self.find_drive(mm.group(1))
            if not d:
                raise StubError(404, "not found")
            if method == "GET":
                return 200, {"snapshots": [s for s in self.snapshots if s["drive"] == d["name"]]}
            sn = {"name": body.get("name") or f"{d['name']}-{time.strftime('%Y%m%d-%H%M%S', time.gmtime())}", "namespace": d["namespace"], "drive": d["name"], "readyToUse": False,
                  "restoreSize": d["size"], "languages": ",".join(d.get("languages") or []), "createdAt": now_iso(), "_since": time.time()}
            self.snapshots.append(sn)
            return 201, {k: v for k, v in sn.items() if not k.startswith("_")}
        mm = m(r"^/api/drives/([^/]+)/shares$")
        if mm:
            d = self.find_drive(mm.group(1))
            if not d:
                raise StubError(404, "not found")
            if method == "GET":
                return 200, {"shares": [{"user": u, "kind": "drive", "resource": d["name"]} for u in d.get("sharedWith", [])]}
            if d.get("kind") != "shared":
                raise StubError(400, "only your own shared drives can be shared (home drives are single-writer disks: share the workspace instead)")
            u = (body.get("user") or "").strip()
            if u and u not in d.setdefault("sharedWith", []):
                d["sharedWith"].append(u)
            return 201, {"user": u, "kind": "drive", "resource": d["name"], "readOnly": bool(body.get("readOnly"))}
        mm = m(r"^/api/drives/([^/]+)/shares/(.+)$")
        if mm and method == "DELETE":
            d = self.find_drive(mm.group(1))
            if not d:
                raise StubError(404, "not found")
            d["sharedWith"] = [u for u in d.get("sharedWith", []) if u != mm.group(2)]
            return 200, {"revoked": mm.group(2)}
        mm = m(r"^/api/snapshots/([^/]+)$")
        if mm and method == "DELETE":
            self.snapshots = [s for s in self.snapshots if s["name"] != mm.group(1)]
            return 200, {"deleted": mm.group(1)}
        raise StubError(404, "no such endpoint (stub)")

    def create(self, body):
        cat = self.cat
        langs = [l for l in (body.get("languages") or []) if l]
        if not langs:
            raise StubError(400, "pick at least one language (or 'base' for plain Linux)")
        name = (body.get("name") or langs[0]).strip()
        if not NAME_RE.match(name):
            raise StubError(400, f'name "{name}" must be lowercase letters, digits or dashes (max 40)')
        keys = [k for k in (body.get("sshPublicKeys") or []) + self.profile.get("sshPublicKeys", []) if k]
        if not keys:
            raise StubError(400, "add an SSH public key first (Profile page, or paste one in the Connect step) — without one nobody can log in")
        profile = body.get("profile") or "standard"
        tier = ((cat.get("profiles") or {}).get(profile) or {}).get("tier", "standard")
        tools = list(body.get("tools") or [])
        for l in langs[1:]:
            if l != "base" and l not in tools:
                tools.append(l)
        choices = [c for c in ((cat.get("storage") or {}).get("choices") or []) if c.get("kind", "home") == "home"]
        storage = body.get("storage") or next((c["name"] for c in choices if c.get("default")), choices[0]["name"] if choices else "")
        drive = body.get("fromDrive") or f"ws-{name}-home"
        if body.get("fromDrive"):
            if drive not in self.drives:
                raise StubError(400, f"drive {drive} not found (see Drives)")
            user = self.attached_to(drive)
            if user and user != name:
                raise StubError(400, f"drive {drive} is attached to workspace {user} — delete that workspace first (the drive stays)")
        for mt in body.get("mounts") or []:
            dn = mt.split(":", 1)[1] if ":" in mt else mt
            owner, _, base = dn.rpartition("/")
            pvc = f"shared-{owner}-{base}" if owner else f"shared-{base}"
            if pvc not in self.drives:
                raise StubError(400, f"shared drive {base} ({pvc}) is not in your namespace — {'ask ' + owner + ' to share it' if owner else 'create it on the Drives page'}")
        d = cat.get("defaults") or {}
        lang_img = ((cat.get("languages") or {}).get(langs[0]) or {}).get("image", "workspace-" + langs[0])
        spec = {"lang": langs[0], "languages": langs, "tools": tools, "profile": profile, "isolation": body.get("isolation") or ((cat.get("profiles") or {}).get(profile) or {}).get("isolation") or "userns",
                "image": body.get("image") or f"{cat.get('registry')}/{lang_img}:{body.get('tag') or cat.get('tag')}", "repo": body.get("repo", ""), "branch": body.get("branch", ""), "dotfiles": body.get("dotfiles", ""),
                "gitAuth": body.get("gitAuth") or "deploy-key", "gitName": body.get("gitName", ""), "gitEmail": body.get("gitEmail", ""), "size": body.get("size") or d.get("size", "30Gi"), "storage": storage, "drive": drive,
                "fromSnapshot": body.get("fromSnapshot", ""), "mounts": body.get("mounts") or [], "cpu": body.get("cpu") or d.get("cpu", "4"), "memory": body.get("memory") or d.get("memory", "8Gi"),
                "cpuRequest": body.get("cpuRequest") or d.get("cpuRequest", "1"), "memoryRequest": body.get("memoryRequest") or d.get("memoryRequest", "2Gi"), "multicast": bool(body.get("multicast")),
                "devices": body.get("devices") or {}, "hostPaths": body.get("hostPaths") or [], "caps": body.get("caps") or [], "expose": bool(body.get("expose")), "idleTimeout": body.get("idleTimeout") or d.get("idleTimeout", "4h"),
                "tz": body.get("tz", ""), "createdAt": now_iso(), "createdBy": self.me["user"]["name"]}
        ns = body.get("namespace") or (self.me.get("labNamespace") if tier == "lab" else self.me["namespace"])
        old = self.workspaces.get(name) or {}
        self.workspaces[name] = {"name": name, "namespace": ns, "tier": tier, "status": "Pending", "ready": False, "_since": time.time(), "languages": langs, "tools": tools, "profile": profile, "image": spec["image"],
                                 "idleTimeout": spec["idleTimeout"], "expose": spec["expose"], "drive": drive, "storage": storage, "mounts": spec["mounts"], "dns": f"ws-{name}.{ns}.svc.cluster.local", "spec": spec,
                                 "_keys": list(dict.fromkeys(old.get("_keys", []) + keys)), "_hostKey": old.get("_hostKey") or "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubHostKey00000000000000000000000000000000",
                                 "_deployKey": (old.get("_deployKey") or f"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubDeployKey0000000000000000000000000000 workspace:{ns}/ws-{name}") if spec["gitAuth"] == "deploy-key" else "",
                                 "createdAt": old.get("createdAt") or now_iso(), "startedAt": ""}
        self.drives.setdefault(drive, {"name": drive, "namespace": ns, "kind": "home", "owner": self.me["user"]["short"], "size": spec["size"], "storage": storage, "storageClass": "", "accessModes": ["ReadWriteOnce"], "phase": "Bound",
                                       "mountedBy": [], "sharedWith": [], "createdAt": now_iso(), "protected": True, "snapshots": 0})
        self.drives[drive].update(attachedTo=name, lastWorkspace=name, languages=langs)
        w = self.workspaces[name]
        return 201, {"name": name, "namespace": ns, "deployKey": w["_deployKey"], "spec": spec, "hostKeyFingerprint": self.fp(w["_hostKey"])}
