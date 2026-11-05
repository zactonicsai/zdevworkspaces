#!/usr/bin/env python3
"""api.py — the Workspaces portal in Python: the same HTTP API as portal/ (Go), serving the
same web UI. Standard library server; PyJWT only for bearer tokens.

  python3 api.py -stub                       # no cluster: answers from portal/web/stub/*.json
  python3 api.py -dev-user alice@corp.example  # local development against your kubeconfig (kubectl)
  python3 api.py -oidc-issuer https://keycloak.example/realms/dev -oidc-audience devspaces-api \\
                 -ui-oidc-issuer https://keycloak.example/realms/dev -ui-oidc-client-id devspaces-portal

Flags mirror the Go portal (single dash, env fallback), so the Helm chart can run either image
with the same args. Every Kubernetes call impersonates the caller (kubectl --as); only sharing
uses the pod's own ServiceAccount."""
from __future__ import annotations

import argparse
import ipaddress
import json
import mimetypes
import os
import sys
import traceback
import urllib.parse
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

VERSION = "1.1.0-py"
HERE = Path(__file__).resolve().parent


def env(name, default=""):
    return os.environ.get(name, default)


def parse_args(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter, prefix_chars="-")
    a = p.add_argument
    a("-addr", default=env("PORTAL_ADDR", ":8080"), help="listen address (host:port or :port)")
    a("-stub", action="store_true", default=env("PORTAL_STUB", "") == "true", help="answer from static JSON fixtures; no cluster needed")
    a("-dev-user", default=env("PORTAL_DEV_USER", ""), help="act as this user without any sign-in (development only)")
    a("-user-header", default=env("PORTAL_USER_HEADER", "X-Forwarded-User"))
    a("-groups-header", default=env("PORTAL_GROUPS_HEADER", "X-Forwarded-Groups"))
    a("-trusted-proxies", default=env("PORTAL_TRUSTED_PROXIES", "127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"))
    a("-oidc-issuer", default=env("PORTAL_OIDC_ISSUER", ""), help="accept bearer JWTs from this issuer (Keycloak realm URL, Entra v2.0 …)")
    a("-oidc-discovery-url", default=env("PORTAL_OIDC_DISCOVERY_URL", ""), help="fetch discovery/JWKS here when the issuer is reachable under another name internally (http://keycloak:8180/realms/x/.well-known/openid-configuration)")
    a("-oidc-audience", default=env("PORTAL_OIDC_AUDIENCE", "api://devspaces"))
    a("-oidc-user-claim", default=env("PORTAL_OIDC_USER_CLAIM", "preferred_username"))
    a("-oidc-groups-claim", default=env("PORTAL_OIDC_GROUPS_CLAIM", "groups"))
    a("-cli-tenant", default=env("PORTAL_CLI_TENANT", "")), a("-cli-client-id", default=env("PORTAL_CLI_CLIENT_ID", "")), a("-cli-scope", default=env("PORTAL_CLI_SCOPE", ""))
    a("-ui-api-base", default=env("PORTAL_UI_API_BASE", ""), help="API URL the browser should call (default: same origin)")
    a("-ui-oidc-issuer", default=env("PORTAL_UI_OIDC_ISSUER", ""), help="browser PKCE login: OIDC issuer (Keycloak realm)")
    a("-ui-oidc-client-id", default=env("PORTAL_UI_OIDC_CLIENT_ID", "devspaces-portal"))
    a("-ui-oidc-scope", default=env("PORTAL_UI_OIDC_SCOPE", "openid profile email"))
    a("-ui-oidc-audience", default=env("PORTAL_UI_OIDC_AUDIENCE", ""))
    a("-ui-stub", action="store_true", default=env("PORTAL_UI_STUB", "") == "true", help="tell the UI to use its own stub data")
    a("-ui-stub-toggle", default=env("PORTAL_UI_STUB_TOGGLE", "true"), help="show the live/stub switch in the footer (true|false)")
    a("-cors-origins", default=env("PORTAL_CORS_ORIGINS", ""), help="comma-separated origins allowed cross-origin (UI hosted elsewhere)")
    a("-web-dir", default=env("PORTAL_WEB_DIR", str(HERE / "web") if (HERE / "web").is_dir() else str(HERE.parent / "portal" / "web")))
    a("-kube-context", default=env("KUBE_CONTEXT", ""))
    a("-lab-group", default=env("PORTAL_LAB_GROUP", ""))
    a("-sharing", default=env("PORTAL_SHARING", "true"), help="true|false")
    a("-csp", default=env("PORTAL_CSP", None))
    return p.parse_args(argv)


DEFAULT_CSP = ("default-src 'self'; script-src 'self'; style-src 'self' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com data:; "
               "img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")


def origin_of(u):
    try:
        p = urllib.parse.urlparse(u)
        return f"{p.scheme}://{p.netloc}" if p.scheme and p.netloc else ""
    except Exception:
        return ""


class Config:
    def __init__(self, args):
        self.args = args
        self.version = VERSION
        self.lab_group = args.lab_group
        self.trusted = [ipaddress.ip_network(c.strip(), strict=False) for c in args.trusted_proxies.split(",") if c.strip()]
        self.cors = [o.strip() for o in args.cors_origins.split(",") if o.strip()]
        self.ui = {"apiBase": args.ui_api_base.rstrip("/"), "oidc": {"issuer": args.ui_oidc_issuer, "clientId": args.ui_oidc_client_id, "scope": args.ui_oidc_scope, "audience": args.ui_oidc_audience},
                   "stub": bool(args.ui_stub), "allowStubToggle": args.ui_stub_toggle != "false"}
        extra = [o for o in (origin_of(args.ui_api_base), origin_of(args.ui_oidc_issuer)) if o]
        csp = args.csp if args.csp is not None else DEFAULT_CSP
        self.csp = csp.replace("connect-src 'self'", "connect-src 'self' " + " ".join(extra), 1) if extra and csp else csp
        self.web_dir = Path(args.web_dir)
        self.verifier = None
        if args.oidc_issuer:
            from oidc import OIDCVerifier
            self.verifier = OIDCVerifier(args.oidc_issuer, args.oidc_audience, args.oidc_user_claim, args.oidc_groups_claim, args.oidc_discovery_url)

    def login_config(self):
        a = self.args
        if a.stub and not a.oidc_issuer:
            return {"mode": "stub"}
        if a.oidc_issuer:
            return {"mode": "oidc", "issuer": a.oidc_issuer, "audience": a.oidc_audience, "tenant": a.cli_tenant, "clientId": a.cli_client_id,
                    "scope": a.cli_scope or (a.oidc_audience + "/.default" if a.cli_tenant else "openid profile email"), "deviceCode": bool(a.cli_client_id)}
        return {"mode": "dev" if a.dev_user else "session"}

    def config_js(self):
        return "/* generated by portal-py from its -ui-* flags */\nwindow.PORTAL_CONFIG = " + json.dumps(self.ui, indent=2) + ";\n"


class Identity:
    def __init__(self, cfg: Config):
        self.cfg = cfg

    def resolve(self, handler) -> dict:
        """bearer → dev user → proxy headers. Raises ValueError (→ 401)."""
        a = self.cfg.args
        # -stub alone means "anyone is the demo user"; with -oidc-issuer the data is still static but login is real
        anonymous_ok = a.dev_user or (a.stub and not a.oidc_issuer)
        ah = handler.headers.get("Authorization", "")
        if ah.lower().startswith("bearer "):
            if self.cfg.verifier is None:
                if anonymous_ok:
                    return self._dev()
                raise ValueError("bearer tokens are not enabled on this portal (-oidc-issuer)")
            return self.cfg.verifier.verify(ah[7:].strip())
        if anonymous_ok:
            return self._dev()
        try:
            ip = ipaddress.ip_address(handler.client_address[0])
        except ValueError:
            raise ValueError("not signed in")
        if not any(ip in n for n in self.cfg.trusted):
            raise ValueError("not signed in: the portal must run behind an authenticating proxy, accept bearer tokens (-oidc-issuer), or run with -dev-user")
        name = (handler.headers.get(a.user_header) or handler.headers.get("X-Forwarded-Email") or "").strip()
        if not name:
            raise ValueError("not signed in (no identity header from the proxy)")
        from kube import wscli
        groups = [g.strip() for g in (handler.headers.get(a.groups_header) or "").split(",") if g.strip()]
        return {"name": name, "short": wscli.sanitize_user(name), "groups": groups, "auth": "session"}

    def _dev(self):
        name = self.cfg.args.dev_user or "demo@example.com"
        short = name.split("@")[0].lower()
        try:
            from kube import wscli
            short = wscli.sanitize_user(name)
        except Exception:
            pass
        return {"name": name, "short": short, "groups": ["system:authenticated"], "auth": "dev"}


# ---------------------------------------------------------------- routing
ROUTES = []  # (method, compiled regex, handler name)


def route(method, pattern, name):
    import re
    ROUTES.append((method, re.compile("^" + pattern + "$"), name))


route("GET", r"/api/me", "me"); route("GET", r"/api/catalog", "catalog"); route("GET", r"/api/projects", "projects")
route("GET", r"/api/artifactory/images", "artifactory_images"); route("GET", r"/api/artifactory/tools", "artifactory_tools")
route("GET", r"/api/profile", "profile_get"); route("PUT", r"/api/profile", "profile_put")
route("GET", r"/api/workspaces", "ws_list"); route("POST", r"/api/workspaces", "ws_create")
route("GET", r"/api/workspaces/(?P<name>[^/]+)", "ws_get"); route("DELETE", r"/api/workspaces/(?P<name>[^/]+)", "ws_delete")
route("POST", r"/api/workspaces/(?P<name>[^/]+)/start", "ws_start"); route("POST", r"/api/workspaces/(?P<name>[^/]+)/stop", "ws_stop")
route("PUT", r"/api/workspaces/(?P<name>[^/]+)/idle", "ws_idle"); route("POST", r"/api/workspaces/(?P<name>[^/]+)/keys", "ws_keys")
route("GET", r"/api/workspaces/(?P<name>[^/]+)/logs", "ws_logs"); route("GET", r"/api/workspaces/(?P<name>[^/]+)/network", "ws_network")
route("GET", r"/api/workspaces/(?P<name>[^/]+)/tunnel", "ws_tunnel")
route("GET", r"/api/workspaces/(?P<name>[^/]+)/shares", "shares_get"); route("POST", r"/api/workspaces/(?P<name>[^/]+)/shares", "shares_add")
route("DELETE", r"/api/workspaces/(?P<name>[^/]+)/shares/(?P<user>.+)", "shares_delete")
route("GET", r"/api/workspaces/(?P<name>[^/]+)/links", "links_get"); route("POST", r"/api/workspaces/(?P<name>[^/]+)/links", "links_add")
route("DELETE", r"/api/workspaces/(?P<name>[^/]+)/links/(?P<peer>.+)", "links_delete")
route("GET", r"/api/drives", "drives_list"); route("POST", r"/api/drives", "drive_create")
route("GET", r"/api/drives/(?P<name>[^/]+)", "drive_get"); route("DELETE", r"/api/drives/(?P<name>[^/]+)", "drive_delete")
route("PUT", r"/api/drives/(?P<name>[^/]+)/resize", "drive_resize")
route("GET", r"/api/drives/(?P<name>[^/]+)/snapshots", "snapshots_get"); route("POST", r"/api/drives/(?P<name>[^/]+)/snapshots", "snapshot_create")
route("DELETE", r"/api/snapshots/(?P<name>[^/]+)", "snapshot_delete")
route("GET", r"/api/drives/(?P<name>[^/]+)/shares", "drive_shares_get"); route("POST", r"/api/drives/(?P<name>[^/]+)/shares", "drive_share_add")
route("DELETE", r"/api/drives/(?P<name>[^/]+)/shares/(?P<user>.+)", "drive_share_delete")


class Server:
    def __init__(self, cfg: Config):
        self.cfg = cfg
        self.ids = Identity(cfg)
        self.stub = None
        self.H = None
        if cfg.args.stub:
            from stub import StubStore
            cat_file = next((p for p in [HERE / "catalog.json", HERE.parent / "deploy" / "platform" / "catalog.json", HERE.parent / "portal" / "catalog.default.json"] if p.is_file()), None)
            cat = json.loads(cat_file.read_text()) if cat_file else {}
            self.stub = StubStore(cfg.web_dir / "stub", cat)
        else:
            import handlers
            self.H = handlers

    def dispatch(self, method, path, query, body, user):
        if self.stub:
            from stub import StubError
            try:
                status, out = self.stub.handle(method, path, query, body)
                if path == "/api/me" and user and user.get("auth") not in (None, "dev", "stub"):
                    # static data, real sign-in: show who actually logged in
                    short = user["short"]
                    out = {**out, "user": {"name": user["name"], "short": short, "groups": user.get("groups") or []},
                           "namespace": f"ws-{short}", "labNamespace": f"ws-{short}-lab", "auth": user["auth"]}
                return status, out
            except StubError as e:
                return e.status, {"error": str(e)}
        from handlers import ApiError, Ctx
        from kube import Kube, KubeError
        for m, rx, name in ROUTES:
            mm = rx.match(path)
            if not mm or m != method:
                continue
            k = Kube.for_user(user, context=self.cfg.args.kube_context or None)
            priv = Kube.privileged(context=self.cfg.args.kube_context or None) if self.cfg.args.sharing != "false" else None
            ctx = Ctx(user=user, k=k, priv=priv, query=query, body=body, params=mm.groupdict(), cfg=self.cfg)
            for k_, v in ctx.params.items():
                ctx.params[k_] = urllib.parse.unquote(v)
            try:
                if name == "ws_start":
                    return self.H.ws_scale(ctx, 1)
                if name == "ws_stop":
                    return self.H.ws_scale(ctx, 0)
                if name == "ws_tunnel":
                    return 501, {"error": "the Python API has no WebSocket SSH tunnel: use the Go portal image for wsctl ssh, or `workspace ssh` with kubectl"}
                if name == "catalog":
                    return 200, self.H.public_catalog(self.H.catalog(ctx))
                return getattr(self.H, name)(ctx)
            except ApiError as e:
                return e.status, {"error": str(e)}
            except KubeError as e:
                return e.status, {"error": str(e)}
            except Exception as e:  # wscli.CliError and friends
                if e.__class__.__name__ == "CliError":
                    return 400, {"error": str(e)}
                traceback.print_exc()
                return 500, {"error": f"internal error: {e}"}
        if any(rx.match(path) for _, rx, _ in ROUTES):
            return 405, {"error": "method not allowed"}
        return 404, {"error": "no such endpoint"}


def make_handler(srv: Server):
    cfg = srv.cfg

    class H(BaseHTTPRequestHandler):
        server_version = "devspaces-portal-py/" + VERSION
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt, *args):
            sys.stderr.write("portal: %s %s\n" % (self.command, fmt % args))

        # ---- helpers
        def headers_common(self, ctype):
            self.send_header("Content-Type", ctype)
            self.send_header("X-Content-Type-Options", "nosniff")
            self.send_header("X-Frame-Options", "DENY")
            self.send_header("Referrer-Policy", "strict-origin-when-cross-origin")
            if cfg.csp:
                self.send_header("Content-Security-Policy", cfg.csp)
            origin = self.headers.get("Origin", "")
            if origin and (origin in cfg.cors or "*" in cfg.cors):
                self.send_header("Access-Control-Allow-Origin", origin)
                self.send_header("Access-Control-Allow-Credentials", "true")
                self.send_header("Vary", "Origin")

        def send_json(self, status, body):
            data = json.dumps(body).encode()
            self.send_response(status)
            self.headers_common("application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(data)

        def send_bytes(self, status, data, ctype, cache="no-cache"):
            self.send_response(status)
            self.headers_common(ctype)
            self.send_header("Cache-Control", cache)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(data)

        def read_body(self):
            n = int(self.headers.get("Content-Length") or 0)
            if n <= 0:
                return {}
            if n > 1 << 20:
                raise ValueError("body too large")
            raw = self.rfile.read(n)
            try:
                v = json.loads(raw.decode() or "{}")
            except json.JSONDecodeError as e:
                raise ValueError(f"bad JSON: {e}")
            if not isinstance(v, dict):
                raise ValueError("expected a JSON object")
            return v

        # ---- request entry
        def do_OPTIONS(self):
            origin = self.headers.get("Origin", "")
            self.send_response(204)
            if origin and (origin in cfg.cors or "*" in cfg.cors):
                self.send_header("Access-Control-Allow-Origin", origin)
                self.send_header("Access-Control-Allow-Credentials", "true")
                self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
                self.send_header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
                self.send_header("Access-Control-Max-Age", "600")
            self.send_header("Content-Length", "0")
            self.end_headers()

        def handle_any(self):
            u = urllib.parse.urlparse(self.path)
            path = u.path
            query = {k: v[-1] for k, v in urllib.parse.parse_qs(u.query).items()}
            if path == "/healthz":
                return self.send_bytes(200, b"ok\n", "text/plain")
            if path == "/readyz":
                return self.send_bytes(200, b"ok\n", "text/plain")
            if path == "/.well-known/devspaces":
                return self.send_json(200, {"version": VERSION, "login": cfg.login_config(), "api": "/api", "openapi": "/openapi.yaml", "backend": "python"})
            if path == "/config.js":
                return self.send_bytes(200, cfg.config_js().encode(), "application/javascript; charset=utf-8", cache="no-store")
            if path.startswith("/api/"):
                return self.handle_api(path, query)
            return self.serve_static(path)

        def handle_api(self, path, query):
            try:
                user = srv.ids.resolve(self)
            except ValueError as e:
                return self.send_json(401, {"error": str(e)})
            if user.get("auth") != "bearer" and self.command not in ("GET", "HEAD") and self.headers.get("X-Requested-With") != "devspaces-portal":
                return self.send_json(403, {"error": "missing X-Requested-With header"})
            try:
                body = self.read_body() if self.command in ("POST", "PUT") else {}
            except ValueError as e:
                return self.send_json(400, {"error": str(e)})
            status, out = srv.dispatch(self.command, path, query, body, user)
            self.send_json(status, out)

        def serve_static(self, path):
            if path == "/" or (not os.path.splitext(path)[1] and not path.startswith("/stub/")):
                path = "/index.html"
            if path == "/openapi.yaml" and not (cfg.web_dir / "openapi.yaml").is_file():
                alt = HERE.parent / "api" / "openapi.yaml"
                if alt.is_file():
                    return self.send_bytes(200, alt.read_bytes(), "application/yaml")
            f = (cfg.web_dir / path.lstrip("/")).resolve()
            if not str(f).startswith(str(cfg.web_dir.resolve())) or not f.is_file():
                return self.send_bytes(404, b"not found\n", "text/plain")
            ctype = mimetypes.guess_type(str(f))[0] or "application/octet-stream"
            if f.suffix == ".js":
                ctype = "application/javascript; charset=utf-8"
            self.send_bytes(200, f.read_bytes(), ctype, cache="no-cache" if f.suffix in (".html", ".js", ".json") else "public, max-age=86400")

        do_GET = do_POST = do_PUT = do_DELETE = do_HEAD = handle_any

    return H


def main(argv=None):
    args = parse_args(argv)
    os.environ.setdefault("WORKSPACE_SYSTEM_NAMESPACE", env("WORKSPACE_SYSTEM_NAMESPACE", "devspaces-system"))
    cfg = Config(args)
    srv = Server(cfg)
    host, _, port = args.addr.rpartition(":")
    httpd = ThreadingHTTPServer((host or "0.0.0.0", int(port or 8080)), make_handler(srv))
    mode = "stub (static fixtures)" if args.stub else f"kubernetes via kubectl{' context ' + args.kube_context if args.kube_context else ''} (impersonating callers)"
    print(f"portal-py {VERSION}: {mode}; web {cfg.web_dir}; listening on {args.addr}", file=sys.stderr)
    if args.dev_user:
        print(f"WARNING: -dev-user {args.dev_user}: identity is not verified; never expose this instance", file=sys.stderr)
    if args.oidc_issuer and cfg.verifier and not cfg.verifier.available():
        print("WARNING: -oidc-issuer set but PyJWT is not installed: bearer tokens will be refused (pip install -r requirements.txt)", file=sys.stderr)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
