"""Tests: `python3 -m unittest -v` (or `make test`). Two layers:
  * HTTP tests against `api.py -stub` (fixtures, config.js, CORS, auth modes);
  * handler tests against an in-memory fake Kubernetes (no kubectl): create → list → drives →
    delete keeps the drive → links → shares, rendering through the shared CLI module.
Needs ssh-keygen on PATH for the create test (skipped otherwise)."""
from __future__ import annotations

import json
import os
import shutil
import sys
import threading
import unittest
import urllib.request
from http.server import ThreadingHTTPServer
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
os.environ.setdefault("WORKSPACE_CONFIG_DIR", "/tmp/wscfg-test")

import api  # noqa: E402


def start(argv):
    cfg = api.Config(api.parse_args(argv))
    srv = api.Server(cfg)
    httpd = ThreadingHTTPServer(("127.0.0.1", 0), api.make_handler(srv))
    httpd.srv = srv
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    return httpd, f"http://127.0.0.1:{httpd.server_address[1]}"


def call(base, method, path, body=None, headers=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(base + path, data=data, method=method, headers={"X-Requested-With": "devspaces-portal", "Content-Type": "application/json", **(headers or {})})
    try:
        with urllib.request.urlopen(req) as r:
            raw = r.read()
            return r.status, (json.loads(raw) if raw.startswith(b"{") or raw.startswith(b"[") else raw)
    except urllib.error.HTTPError as e:
        raw = e.read()
        return e.code, (json.loads(raw) if raw.startswith(b"{") else raw)


class StubHTTP(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.httpd, cls.base = start(["-stub", "-ui-oidc-issuer", "https://kc.example/realms/dev", "-cors-origins", "https://ui.example"])

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()

    def test_wellknown_and_config(self):
        code, wk = call(self.base, "GET", "/.well-known/devspaces")
        self.assertEqual((code, wk["login"]["mode"], wk["backend"]), (200, "stub", "python"))
        code, js = call(self.base, "GET", "/config.js")
        self.assertEqual(code, 200)
        self.assertIn(b'"issuer": "https://kc.example/realms/dev"', js)
        req = urllib.request.Request(self.base + "/", headers={"Origin": "https://ui.example"})
        with urllib.request.urlopen(req) as r:
            csp = r.headers["Content-Security-Policy"]
            self.assertIn("connect-src 'self' https://kc.example", csp)
            self.assertEqual(r.headers["Access-Control-Allow-Origin"], "https://ui.example")

    def test_fixtures_and_mutations(self):
        code, me = call(self.base, "GET", "/api/me")
        self.assertEqual((code, me["user"]["short"], me["auth"]), (200, "demo", "stub"))
        code, ws = call(self.base, "GET", "/api/workspaces?shared=true")
        self.assertEqual([w["name"] for w in ws["workspaces"]], ["api", "web"])
        self.assertEqual(ws["sharedWithMe"][0]["owner"], "alice")
        code, out = call(self.base, "POST", "/api/workspaces", {"name": "t1", "languages": ["go", "python"], "tools": ["ripgrep"], "mounts": ["shared:team-data"]})
        self.assertEqual(code, 201, out)
        self.assertTrue(out["deployKey"].startswith("ssh-ed25519"))
        code, d = call(self.base, "GET", "/api/workspaces/t1")
        self.assertEqual(d["workspace"]["mounts"], ["shared:team-data"])
        self.assertEqual(d["network"]["dns"], "ws-t1.ws-demo.svc.cluster.local")
        code, _ = call(self.base, "POST", "/api/workspaces/t1/stop")
        code, out = call(self.base, "DELETE", "/api/workspaces/t1")
        self.assertTrue(out["driveKept"])
        code, dr = call(self.base, "GET", "/api/drives")
        mine = {x["name"]: x for x in dr["drives"]}
        self.assertEqual(mine["ws-t1-home"]["attachedTo"], "")
        code, out = call(self.base, "POST", "/api/drives/ws-t1-home/snapshots", {"name": "s1"})
        self.assertEqual((code, out["drive"]), (201, "ws-t1-home"))
        code, out = call(self.base, "DELETE", "/api/drives/ws-api-home")
        self.assertEqual(code, 409)
        code, out = call(self.base, "POST", "/api/workspaces/api/links", {"peer": "bob@corp.example/web", "ports": [8080]})
        self.assertEqual((code, out["status"]), (201, "linked"))
        code, out = call(self.base, "POST", "/api/workspaces/api/shares", {"user": "carol@corp.example"})
        self.assertEqual(code, 201)
        code, out = call(self.base, "GET", "/api/workspaces/api/shares")
        self.assertIn("carol@corp.example", [s["user"] for s in out["shares"]])
        code, out = call(self.base, "GET", "/api/workspaces/api/tunnel")
        self.assertEqual(code, 501)
        code, out = call(self.base, "GET", "/api/nope")
        self.assertEqual(code, 404)

    def test_csrf_and_bearer(self):
        req = urllib.request.Request(self.base + "/api/workspaces/api/stop", data=b"{}", method="POST", headers={"Content-Type": "application/json"})
        with self.assertRaises(urllib.error.HTTPError) as cm:
            urllib.request.urlopen(req)
        self.assertEqual(cm.exception.code, 403)
        # a bearer token on a stub/dev portal is accepted as the dev user (same as the Go portal)
        code, me = call(self.base, "GET", "/api/me", headers={"Authorization": "Bearer x"})
        self.assertEqual((code, me["user"]["short"]), (200, "demo"))


class StubWithLogin(unittest.TestCase):
    """-stub plus -oidc-issuer: static data, but login is mandatory (deploy/demo/keycloak)."""
    @classmethod
    def setUpClass(cls):
        cls.httpd, cls.base = start(["-stub", "-oidc-issuer", "http://localhost:8180/realms/devspaces", "-oidc-audience", "devspaces-api", "-cli-client-id", "devspaces-cli"])
        # swap the real verifier (it would fetch JWKS) for one that accepts exactly one token
        class FakeVerifier:
            def verify(self, tok):
                if tok != "good":
                    raise ValueError("bad token")
                return {"name": "alice@example.com", "short": "alice", "groups": ["developers"], "auth": "bearer"}
        cls.httpd.srv.cfg.verifier = FakeVerifier()

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()

    def test_login_required(self):
        code, wk = call(self.base, "GET", "/.well-known/devspaces")
        self.assertEqual((wk["login"]["mode"], wk["login"]["issuer"], wk["login"]["scope"], wk["login"]["deviceCode"]),
                         ("oidc", "http://localhost:8180/realms/devspaces", "openid profile email", True))
        code, out = call(self.base, "GET", "/api/me")
        self.assertEqual(code, 401, out)
        code, out = call(self.base, "GET", "/api/me", headers={"Authorization": "Bearer nope"})
        self.assertEqual(code, 401, out)
        code, me = call(self.base, "GET", "/api/me", headers={"Authorization": "Bearer good"})
        self.assertEqual(code, 200, me)
        self.assertEqual((me["user"]["name"], me["user"]["short"], me["namespace"], me["auth"]), ("alice@example.com", "alice", "ws-alice", "bearer"))
        self.assertEqual(me["user"]["groups"], ["developers"])
        code, ws = call(self.base, "GET", "/api/workspaces", headers={"Authorization": "Bearer good"})
        self.assertEqual(code, 200)
        self.assertTrue(ws["workspaces"])


class SessionMode(unittest.TestCase):
    def test_proxy_headers_required(self):
        httpd, base = start(["-stub", "-trusted-proxies", "127.0.0.1/32"])
        # -stub without -dev-user still answers as the demo user (fixtures), but a plain session portal needs headers:
        httpd.shutdown()
        httpd, base = start(["-trusted-proxies", "192.0.2.0/24"])  # real mode, our 127.0.0.1 is not trusted
        try:
            code, out = call(base, "GET", "/api/me")
            self.assertEqual(code, 401)
            self.assertIn("not signed in", out["error"])
        finally:
            httpd.shutdown()


# ---------------------------------------------------------------- handler tests with a fake cluster
class FakeKube:
    """Enough of kube.Kube for handlers.py: objects by (kind, ns, name) with label selectors."""
    KINDS = {"statefulset": "statefulsets", "pod": "pods", "persistentvolumeclaim": "persistentvolumeclaims", "secret": "secrets", "service": "services",
             "namespace": "namespaces", "networkpolicy": "networkpolicies", "rolebinding": "rolebindings", "role": "roles", "volumesnapshot": "volumesnapshots",
             "configmap": "configmaps", "persistentvolume": "persistentvolumes"}

    def __init__(self, store, user=None):
        self.store, self.user = store, user
        self.offline = False

    @staticmethod
    def kind_of(k):
        k = k.lower()
        return FakeKube.KINDS.get(k, k)

    def get(self, kind, name=None, ns=None, selector=None, extra=None):
        kind = self.kind_of(kind)
        if not isinstance(ns, str):
            ns = None  # wscli.NO_NS sentinel → cluster-scoped
        if name:
            o = self.store.get((kind, ns, name))
            return json.loads(json.dumps(o)) if o is not None else None
        return {"items": self.items(kind, ns=ns, selector=selector)}

    def items(self, kind, ns=None, selector=None, all_namespaces=False):
        kind = self.kind_of(kind)
        want = {}
        keys = []
        for part in (selector or "").split(","):
            if "=" in part:
                k, v = part.split("=", 1); want[k] = v
            elif part:
                keys.append(part)
        out = []
        if not isinstance(ns, str):
            ns = None
        for (kd, n, name), o in self.store.items():
            if kd != kind or (not all_namespaces and ns is not None and n != ns and kd != "namespaces"):
                continue
            l = (o.get("metadata") or {}).get("labels") or {}
            if all(l.get(k) == v for k, v in want.items()) and all(k in l for k in keys):
                out.append(json.loads(json.dumps(o)))
        return out

    def apply(self, objects):
        for o in objects:
            kind = self.kind_of(o["kind"])
            ns = o["metadata"].get("namespace")
            o = json.loads(json.dumps(o))
            if "stringData" in o:
                import base64
                o.setdefault("data", {}).update({k: base64.b64encode(str(v).encode()).decode() for k, v in o.pop("stringData").items()})
            if kind == "persistentvolumeclaims":
                o["status"] = {"phase": "Bound", "capacity": {"storage": o["spec"]["resources"]["requests"]["storage"]}}
            if kind == "statefulsets":
                o["status"] = {"readyReplicas": 1}
                self.store[("pods", ns, o["metadata"]["name"] + "-0")] = {"metadata": {"name": o["metadata"]["name"] + "-0", "namespace": ns, "labels": o["spec"]["template"]["metadata"]["labels"], "annotations": {}},
                                                                            "spec": {"nodeName": "node-1"}, "status": {"phase": "Running", "podIP": "10.244.1.5", "conditions": [{"type": "Ready", "status": "True"}]}}
            self.store[(kind, ns, o["metadata"]["name"])] = o
        return "applied"

    def patch(self, kind, name, patch, ns=None, ptype="merge"):
        o = self.store[(self.kind_of(kind), ns, name)]
        if "stringData" in patch:
            import base64
            o.setdefault("data", {}).update({k: base64.b64encode(str(v).encode()).decode() for k, v in patch["stringData"].items()})
        for k, v in patch.items():
            if k != "stringData":
                o.setdefault(k, {}).update(v)

    def annotate(self, kind, name, ns, **kv):
        o = self.store.get((self.kind_of(kind), ns, name))
        if o:
            o["metadata"].setdefault("annotations", {}).update(kv)

    def delete(self, kind, name, ns=None):
        self.store.pop((self.kind_of(kind), ns, name), None)

    def scale(self, name, replicas, ns):
        self.store[("statefulsets", ns, name)]["spec"]["replicas"] = replicas

    def logs(self, pod, ns, tail=200, container="workspace"):
        return "[workspace] ready"

    def run(self, args, ns=None, input=None, check=True):
        if args[:2] == ["delete", "statefulset,service,secret,networkpolicy,role,rolebinding"]:
            sel = args[3].split("=")
            for key in [k for k, o in self.store.items() if k[1] == ns and k[0] in ("statefulsets", "services", "secrets", "networkpolicies", "roles", "rolebindings", "pods") and ((o.get("metadata") or {}).get("labels") or {}).get(sel[0]) == sel[1]]:
                del self.store[key]
        class R:  # noqa: N801
            returncode, stdout, stderr = 0, "", ""
        return R()


class Handlers(unittest.TestCase):
    def setUp(self):
        import handlers
        self.H = handlers
        cat = json.loads((HERE.parent / "deploy" / "platform" / "catalog.json").read_text())
        self.store = {("namespaces", None, "ws-alice"): {"metadata": {"name": "ws-alice", "labels": {"devspaces.io/tenant": "alice"}}},
                      ("namespaces", None, "ws-bob"): {"metadata": {"name": "ws-bob", "labels": {"devspaces.io/tenant": "bob"}}},
                      ("configmaps", "devspaces-system", "workspace-catalog"): {"metadata": {"name": "workspace-catalog"}, "data": {"catalog.json": json.dumps(cat)}}}
        self.user = {"name": "alice@corp.example", "short": "alice", "groups": [], "auth": "bearer"}
        self.k = FakeKube(self.store, self.user)
        self.priv = FakeKube(self.store)

        class Cfg:
            lab_group = ""
            version = "test"

            def login_config(self):
                return {"mode": "oidc"}
        self.ctx = lambda **kw: handlers.Ctx(user=self.user, k=self.k, priv=self.priv, cfg=Cfg(), **kw)
        import kube
        kube.wscli.SYSTEM_NS = "devspaces-system"

    @unittest.skipUnless(shutil.which("ssh-keygen"), "ssh-keygen needed")
    def test_lifecycle(self):
        H = self.ctx
        code, me = self.H.me(H())
        self.assertEqual((code, me["onboarded"], me["auth"]), (200, True, "bearer"))
        code, out = self.H.profile_put(H(body={"sshPublicKeys": ["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGm7gPZQ0cYQhh3v4u5n9Vd2D5rR0f7w3q1Yx2cZbWvK a@l"], "gitName": "Alice"}))
        self.assertEqual(code, 200)
        code, out = self.H.ws_create(H(body={"name": "api", "languages": ["go", "python"], "tools": ["ripgrep"], "storage": "fast", "repo": "git@github.com:acme/api.git"}))
        self.assertEqual(code, 201, out)
        self.assertEqual(out["spec"]["drive"], "ws-api-home")
        self.assertTrue(out["deployKey"].startswith("ssh-ed25519"))
        st = self.store[("statefulsets", "ws-alice", "ws-api")]
        self.assertEqual(st["spec"]["persistentVolumeClaimRetentionPolicy"]["whenDeleted"], "Retain")
        self.assertEqual(self.store[("persistentvolumeclaims", "ws-alice", "ws-api-home")]["metadata"]["labels"]["devspaces.io/drive"], "home")
        code, lst = self.H.ws_list(H())
        self.assertEqual([w["name"] for w in lst["workspaces"]], ["api"])
        self.assertEqual(lst["workspaces"][0]["status"], "Running")
        code, det = self.H.ws_get(H(params={"name": "api"}))
        self.assertEqual(det["ssh"]["alias"], "ws-api")
        self.assertEqual(len(det["ssh"]["authorizedKeys"]), 1)
        self.assertEqual(det["network"]["dns"], "ws-api.ws-alice.svc.cluster.local")
        # recreating stealing another drive is refused
        with self.assertRaises(self.H.ApiError):
            self.H.ws_create(H(body={"name": "thief", "languages": ["go"], "fromDrive": "ws-api-home"}))
        # link + share
        code, link = self.H.links_add(H(params={"name": "api"}, body={"peer": "bob@corp.example/web", "ports": [8080]}))
        self.assertEqual((code, link["status"]), (201, "pending-peer"))
        self.assertIn(("networkpolicies", "ws-alice", "peer-api-bob-web"), self.store)
        self.store[("secrets", "ws-bob", "workspace-profile")] = {"metadata": {"name": "workspace-profile", "namespace": "ws-bob"}, "data": {"authorized_keys": "c3NoLWVkMjU1MTkgQUFBQUMzTnphQzFsWkRJMU5URTVBQUFBSU9NcXFua1Z6cm0wU2RHNlVPb3FLTHNhYmdINUM5b2tXaTBkaDJsOUdLSmwgYm9i"}}
        code, sh = self.H.shares_add(H(params={"name": "api"}, body={"user": "bob@corp.example"}))
        self.assertEqual(code, 201)
        self.assertIn(("rolebindings", "ws-alice", "share-api-bob"), self.store)
        import base64
        ak = base64.b64decode(self.store[("secrets", "ws-alice", "ws-api-ssh")]["data"]["authorized_keys"]).decode()
        self.assertIn("guest:bob@corp.example", ak)
        # delete keeps the drive; drives list shows it detached
        code, out = self.H.ws_delete(H(params={"name": "api"}))
        self.assertTrue(out["driveKept"])
        self.assertIn(("persistentvolumeclaims", "ws-alice", "ws-api-home"), self.store)
        code, dr = self.H.drives_list(H())
        self.assertEqual(dr["drives"][0]["attachedTo"], "")
        # re-attach to a new workspace with other languages
        code, out = self.H.ws_create(H(body={"name": "engine", "languages": ["rust"], "fromDrive": "ws-api-home"}))
        self.assertEqual(code, 201, out)
        self.assertEqual(self.store[("statefulsets", "ws-alice", "ws-engine")]["spec"]["template"]["spec"]["volumes"][0]["persistentVolumeClaim"]["claimName"], "ws-api-home")
        # shared drive + snapshot
        code, d = self.H.drive_create(H(body={"name": "team-data", "size": "10Gi"}))
        self.assertEqual((code, d["kind"]), (201, "shared"))
        code, sn = self.H.snapshot_create(H(params={"name": "engine"}, body={"name": "s1"}))
        self.assertEqual((code, sn["drive"]), (201, "ws-api-home"))
        with self.assertRaises(self.H.ApiError) as cm:  # attached to engine → 409
            self.H.drive_delete(H(params={"name": "ws-api-home"}))
        self.assertEqual(cm.exception.status, 409)


if __name__ == "__main__":
    unittest.main(verbosity=2)
