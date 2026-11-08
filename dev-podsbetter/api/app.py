"""
Developer Pods API
==================

A small FastAPI service that starts/stops developer pods on Kubernetes by
shelling out to ``kubectl`` and ``helm``.  Every external command goes through
``run()`` so it is logged in full (argv, cwd, exit code, duration, stdout,
stderr) to:

  * stdout             — one JSON object per line (for `kubectl logs` / Loki)
  * logs/ws-api.jsonl  — rotating file, same JSON lines
  * GET /api/audit     — in-memory ring buffer the portal shows in "API log"

Environment
-----------
WS_NAMESPACE   Kubernetes namespace for the pods          (default: dev-workspaces)
WS_CHART       Path to the Helm chart                     (default: ../helm/dev-pod)
WS_KUBECONFIG  kubeconfig passed to kubectl/helm          (default: inherit)
WS_KUBECTL     kubectl binary                             (default: kubectl)
WS_HELM        helm binary                                (default: helm)
WS_LOG_DIR     where ws-api.jsonl is written              (default: ./logs)
WS_DRY_RUN     1 = don't call kubectl/helm, fake a cluster in memory (UI demo)
WS_CREATE_NAMESPACE  pass --create-namespace to helm (default 1; set 0 in-cluster)
WS_CATALOG     JSON file with runtimes/sizes/registry (see catalog.example.json)
WS_REGISTRY    registry prefix for bare image names in the default catalog
WS_TIMEOUT     seconds before a kubectl/helm call is killed (default: 120)

Run:  uvicorn app:app --host 0.0.0.0 --port 8080
"""
from __future__ import annotations

import collections
import json
import logging
import logging.handlers
import os
import re
import shlex
import subprocess
import time
import uuid
from contextvars import ContextVar
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Literal, Optional

from fastapi import FastAPI, HTTPException, Query, Request
from fastapi.responses import FileResponse, JSONResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field

# --------------------------------------------------------------------------- config
HERE = Path(__file__).resolve().parent
NAMESPACE = os.environ.get("WS_NAMESPACE", "dev-workspaces")
CHART = str(Path(os.environ.get("WS_CHART", HERE.parent / "helm" / "dev-pod")).resolve())
KUBECONFIG = os.environ.get("WS_KUBECONFIG")
KUBECTL = os.environ.get("WS_KUBECTL", "kubectl")
HELM = os.environ.get("WS_HELM", "helm")
LOG_DIR = Path(os.environ.get("WS_LOG_DIR", HERE / "logs"))
DRY_RUN = os.environ.get("WS_DRY_RUN", "0") in ("1", "true", "yes")
TIMEOUT = int(os.environ.get("WS_TIMEOUT", "120"))
# In-cluster the service account is namespace-scoped and cannot create namespaces: set WS_CREATE_NAMESPACE=0.
CREATE_NS = os.environ.get("WS_CREATE_NAMESPACE", "1") in ("1", "true", "yes")
PART_OF = "dev-workspaces"           # label every chart object carries

# A deliberately small catalog: a few Python and Java runtimes only.  The images are the pre-built
# developer images from images/*/Dockerfile.  Override everything with WS_CATALOG=/path/catalog.json
# (the dev-pods-api chart mounts one from values.catalog) — see catalog.example.json.
DEFAULT_CATALOG: dict[str, Any] = {
    "registry": os.environ.get("WS_REGISTRY", "registry.example.com/dev"),
    "runtimes": {
        "python312": {"label": "Python 3.12", "family": "python", "image": "dev-pod-python:3.12", "tools": "git, uv, poetry, pytest, ruff, black, mypy, ipython"},
        "python311": {"label": "Python 3.11", "family": "python", "image": "dev-pod-python:3.11", "tools": "git, uv, poetry, pytest, ruff, black, mypy, ipython"},
        "java21":    {"label": "Java 21 (Temurin JDK)", "family": "java", "image": "dev-pod-java:21", "tools": "git, maven 3.9, gradle 8"},
        "java17":    {"label": "Java 17 (Temurin JDK)", "family": "java", "image": "dev-pod-java:17", "tools": "git, maven 3.9, gradle 8"},
    },
    "sizes": {
        "small":  {"cpu": "1", "memory": "2Gi", "disk": "10Gi"},
        "medium": {"cpu": "2", "memory": "4Gi", "disk": "20Gi"},
        "large":  {"cpu": "4", "memory": "8Gi", "disk": "40Gi"},
    },
    "imagePullSecret": "",
    "storageClass": "",
    "sshd": False,
}


def load_catalog() -> dict[str, Any]:
    path = os.environ.get("WS_CATALOG")
    cat = json.loads(json.dumps(DEFAULT_CATALOG))
    if path:
        with open(path) as f:
            user = json.load(f)
        for k, v in user.items():
            cat[k] = v if k not in ("runtimes", "sizes") or not isinstance(v, dict) else v   # file replaces whole sections
    reg = (cat.get("registry") or "").rstrip("/")
    for rt in cat["runtimes"].values():
        if "/" not in rt["image"] and reg:          # bare name -> prefix with the registry
            rt["image"] = f"{reg}/{rt['image']}"
    return cat


CATALOG = load_catalog()
RUNTIMES: dict[str, dict[str, str]] = CATALOG["runtimes"]
SIZES: dict[str, dict[str, str]] = CATALOG["sizes"]
NAME_RE = re.compile(r"^[a-z0-9]([-a-z0-9]{0,30}[a-z0-9])?$")
OWNER_RE = re.compile(r"^[A-Za-z0-9._@+-]{1,64}$")

# --------------------------------------------------------------------------- logging
request_id_var: ContextVar[str] = ContextVar("request_id", default="-")


class JsonFormatter(logging.Formatter):
    """One JSON object per line; extra fields passed via ``extra={"data": {...}}``."""

    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "ts": datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(timespec="milliseconds"),
            "level": record.levelname,
            "logger": record.name,
            "request_id": request_id_var.get(),
            "msg": record.getMessage(),
        }
        data = getattr(record, "data", None)
        if isinstance(data, dict):
            payload.update(data)
        if record.exc_info:
            payload["exc"] = self.formatException(record.exc_info)
        return json.dumps(payload, default=str)


def setup_logging() -> logging.Logger:
    LOG_DIR.mkdir(parents=True, exist_ok=True)
    root = logging.getLogger()
    root.setLevel(logging.INFO)
    root.handlers.clear()
    stream = logging.StreamHandler()
    stream.setFormatter(JsonFormatter())
    root.addHandler(stream)
    fileh = logging.handlers.RotatingFileHandler(LOG_DIR / "ws-api.jsonl", maxBytes=10_000_000, backupCount=5)
    fileh.setFormatter(JsonFormatter())
    root.addHandler(fileh)
    logging.getLogger("uvicorn.access").disabled = True   # our middleware logs requests instead
    return logging.getLogger("ws-api")


log = setup_logging()
cmdlog = logging.getLogger("ws-api.cmd")

# --------------------------------------------------------------------------- audit buffer
AUDIT: collections.deque[dict[str, Any]] = collections.deque(maxlen=1000)
_seq = 0


def _truncate(s: str, n: int = 8000) -> str:
    return s if len(s) <= n else s[:n] + f"\n… [{len(s) - n} more bytes truncated]"


# --------------------------------------------------------------------------- command runner
class CommandError(Exception):
    def __init__(self, entry: dict[str, Any]):
        super().__init__(entry.get("stderr") or entry.get("stdout") or "command failed")
        self.entry = entry


def run(argv: list[str], *, workspace: Optional[str] = None, action: str = "", check: bool = True,
        stdin: Optional[str] = None) -> dict[str, Any]:
    """Run kubectl/helm (never through a shell), log everything, return the audit entry."""
    global _seq
    _seq += 1
    tool = "helm" if Path(argv[0]).name == HELM or argv[0] == HELM else "kubectl" if argv[0] == KUBECTL else Path(argv[0]).name
    env = os.environ.copy()
    if KUBECONFIG:
        env["KUBECONFIG"] = KUBECONFIG
    entry: dict[str, Any] = {
        "id": _seq,
        "ts": datetime.now(timezone.utc).isoformat(timespec="milliseconds"),
        "request_id": request_id_var.get(),
        "workspace": workspace,
        "action": action,
        "tool": tool,
        "argv": argv,
        "cmd": shlex.join(argv),
        "cwd": os.getcwd(),
        "kubeconfig": KUBECONFIG or "(default)",
        "dry_run": DRY_RUN,
    }
    cmdlog.info("exec %s", entry["cmd"], extra={"data": {"phase": "start", **{k: entry[k] for k in ("id", "tool", "action", "workspace", "argv")}}})
    t0 = time.perf_counter()
    if DRY_RUN:
        code, out, err = fake.execute(argv)
    else:
        try:
            p = subprocess.run(argv, capture_output=True, text=True, env=env, timeout=TIMEOUT, input=stdin)
            code, out, err = p.returncode, p.stdout, p.stderr
        except FileNotFoundError as e:
            code, out, err = 127, "", f"{argv[0]}: not found ({e})"
        except subprocess.TimeoutExpired as e:
            code, out, err = 124, (e.stdout or b"").decode() if isinstance(e.stdout, bytes) else (e.stdout or ""), f"timed out after {TIMEOUT}s"
    entry.update({
        "exit_code": code,
        "duration_ms": round((time.perf_counter() - t0) * 1000, 1),
        "stdout": _truncate(out or ""),
        "stderr": _truncate(err or ""),
        "ok": code == 0,
    })
    AUDIT.appendleft(entry)
    (cmdlog.info if code == 0 else cmdlog.error)(
        "exit %s (%sms) %s", code, entry["duration_ms"], entry["cmd"],
        extra={"data": {"phase": "end", **{k: entry[k] for k in ("id", "tool", "action", "workspace", "exit_code", "duration_ms", "stdout", "stderr")}}},
    )
    if check and code != 0:
        raise CommandError(entry)
    return entry


def kubectl(*args: str, **kw) -> dict[str, Any]:
    return run([KUBECTL, "-n", NAMESPACE, *args], **kw)


def helm(*args: str, **kw) -> dict[str, Any]:
    return run([HELM, *args], **kw)


# --------------------------------------------------------------------------- dry-run cluster
class FakeCluster:
    """Just enough state to make the UI work with WS_DRY_RUN=1. Not a Kubernetes emulator."""

    def __init__(self) -> None:
        self.releases: dict[str, dict[str, Any]] = {}   # release -> values
        self.pvcs: set[str] = set()

    def execute(self, argv: list[str]) -> tuple[int, str, str]:
        a = argv
        tool = a[0]
        if tool == HELM and "upgrade" in a:
            rel = next(x for x in a[a.index("upgrade") + 1:] if not x.startswith("--"))
            vals = {}
            for i, x in enumerate(a):
                if x == "--set" and i + 1 < len(a):
                    k, _, v = a[i + 1].partition("=")
                    vals[k] = v
            self.releases[rel] = {**self.releases.get(rel, {}), **vals, "replicas": "1", "created": self.releases.get(rel, {}).get("created", time.time()), "started": time.time()}
            self.pvcs.add(f"home-{rel}-0")
            return 0, f'Release "{rel}" has been upgraded. Happy Helming!\nNAME: {rel}\nNAMESPACE: {NAMESPACE}\nSTATUS: deployed\n', ""
        if tool == HELM and "uninstall" in a:
            rel = a[a.index("uninstall") + 1]
            if rel not in self.releases:
                return 1, "", f"Error: uninstall: Release not loaded: {rel}: release: not found"
            del self.releases[rel]
            return 0, f'release "{rel}" uninstalled\n', ""
        if tool == HELM and "list" in a:
            return 0, json.dumps([{"name": r, "namespace": NAMESPACE, "status": "deployed", "chart": "dev-pod-0.1.0"} for r in self.releases]), ""
        if tool == KUBECTL and "scale" in a:
            rel = next(x for x in a if x.startswith("ws-"))
            if rel not in self.releases:
                return 1, "", f'Error from server (NotFound): statefulsets.apps "{rel}" not found'
            n = next(x for x in a if x.startswith("--replicas=")).split("=")[1]
            self.releases[rel]["replicas"] = n
            if n != "0":
                self.releases[rel]["started"] = time.time()
            return 0, f"statefulset.apps/{rel} scaled\n", ""
        if tool == KUBECTL and a[3:5] == ["get", "statefulsets"]:
            items = [self._sts(r, v) for r, v in self.releases.items()]
            return 0, json.dumps({"items": items}), ""
        if tool == KUBECTL and a[3:5] == ["get", "pods"]:
            items = [self._pod(r, v) for r, v in self.releases.items() if v["replicas"] != "0"]
            if "-l" not in a and len(a) > 5 and a[5].startswith("ws-"):
                items = [p for p in items if p["metadata"]["name"] == a[5]]
                if not items:
                    return 1, "", f'Error from server (NotFound): pods "{a[5]}" not found'
                return 0, json.dumps(items[0]), ""
            return 0, json.dumps({"items": items}), ""
        if tool == KUBECTL and a[3] == "logs":
            rel = a[4].rsplit("-", 1)[0]
            v = self.releases.get(rel)
            if not v or v["replicas"] == "0":
                return 1, "", f'Error from server (NotFound): pods "{a[4]}" not found'
            return 0, f"[dev-pod] runtime={v.get('runtime')} image={v.get('image')}\n[dev-pod] home volume mounted at /home/dev\n[dev-pod] ready — exec in with: kubectl exec -it {a[4]} -n {NAMESPACE} -- bash\n", ""
        if tool == KUBECTL and a[3] == "exec":
            cmd = a[a.index("--") + 1:]
            rel = a[4].rsplit("-", 1)[0]
            v = self.releases.get(rel, {})
            shell_cmd = cmd[-1]
            if "version" in shell_cmd:
                return 0, ("Python 3.12.3" if v.get("runtime", "").startswith("python") else 'openjdk version "21.0.4" 2024-07-16'), ""
            if shell_cmd.strip() == "whoami":
                return 0, "dev\n", ""
            return 0, f"(dry-run) would run in {a[4]}: {shell_cmd}\n", ""
        if tool == KUBECTL and a[3:5] == ["get", "events"]:
            return 0, json.dumps({"items": [{"lastTimestamp": datetime.now(timezone.utc).isoformat(), "type": "Normal", "reason": "Started", "message": "Started container dev (dry-run)"}]}), ""
        if tool == KUBECTL and a[3:5] == ["delete", "pvc"]:
            n = len(self.pvcs)
            self.pvcs.clear()
            return 0, f"(dry-run) {n} persistentvolumeclaim(s) deleted\n", ""
        if "version" in a or "cluster-info" in a:
            return 0, "dry-run cluster\n", ""
        return 0, "(dry-run) no-op\n", ""

    def _sts(self, rel, v):
        return {"metadata": {"name": rel, "labels": {"workspace/name": v.get("name"), "workspace/runtime": v.get("runtime"), "workspace/owner": v.get("owner"), "workspace/size": v.get("size")},
                             "creationTimestamp": datetime.fromtimestamp(v["created"], tz=timezone.utc).isoformat()},
                "spec": {"replicas": int(v["replicas"]), "template": {"spec": {"containers": [{"image": v.get("image")}]}}},
                "status": {"readyReplicas": int(v["replicas"]) if time.time() - v["started"] > 4 else 0}}

    def _pod(self, rel, v):
        booting = time.time() - v["started"] < 4   # a fake boot takes 4 s so the UI shows Pending → Running
        return {"metadata": {"name": f"{rel}-0", "labels": {"workspace/name": v.get("name")}},
                "spec": {"nodeName": "node-1"},
                "status": {"phase": "Pending" if booting else "Running", "podIP": None if booting else "10.244.1.23",
                           "containerStatuses": [{"ready": not booting, "restartCount": 0}]}}


fake = FakeCluster()

# --------------------------------------------------------------------------- app
app = FastAPI(title="Developer Pods API", version="1.0.0", docs_url="/api/docs", openapi_url="/api/openapi.json")


@app.middleware("http")
async def request_logging(request: Request, call_next):
    rid = request.headers.get("x-request-id") or uuid.uuid4().hex[:12]
    token = request_id_var.set(rid)
    t0 = time.perf_counter()
    try:
        response = await call_next(request)
    except Exception:
        log.exception("unhandled error", extra={"data": {"method": request.method, "path": request.url.path}})
        raise
    finally:
        request_id_var.reset(token)
    if request.url.path.startswith("/api"):
        log.info("%s %s -> %s", request.method, request.url.path, response.status_code,
                 extra={"data": {"method": request.method, "path": request.url.path, "query": str(request.query_params),
                                 "status": response.status_code, "duration_ms": round((time.perf_counter() - t0) * 1000, 1),
                                 "client": request.client.host if request.client else None, "request_id": rid}})
    response.headers["X-Request-ID"] = rid
    return response


@app.exception_handler(CommandError)
async def command_error_handler(_: Request, exc: CommandError):
    e = exc.entry
    return JSONResponse(status_code=502, content={"error": f"{e['tool']} exited {e['exit_code']}", "cmd": e["cmd"], "stderr": e["stderr"], "audit_id": e["id"]})


# ---- models
class CreateRequest(BaseModel):
    name: str = Field(..., examples=["my-app"])
    runtime: str = Field(..., description="a key of /api/catalog runtimes")
    size: str = Field("medium", description="a key of /api/catalog sizes")
    owner: str = Field("dev", examples=["jane.doe"])
    repo: Optional[str] = Field(None, description="git URL cloned into /home/dev/workspace on first boot")


class ExecRequest(BaseModel):
    command: str = Field(..., max_length=2000, examples=["python3 --version"])


def _validate_name(name: str) -> str:
    if not NAME_RE.match(name):
        raise HTTPException(400, "name must be lowercase letters, digits and dashes (max 32 chars)")
    return name


def release(name: str) -> str:
    return f"ws-{name}"


def pod_name(name: str) -> str:
    return f"{release(name)}-0"


# ---- helpers
def _status_from(sts: dict[str, Any], pod: Optional[dict[str, Any]]) -> str:
    if (sts.get("spec", {}).get("replicas") or 0) == 0:
        return "Stopped"
    if not pod:
        return "Pending"
    phase = pod.get("status", {}).get("phase", "Pending")
    if phase == "Running":
        ready = all(c.get("ready") for c in pod["status"].get("containerStatuses", [])) if pod["status"].get("containerStatuses") else False
        return "Running" if ready else "Starting"
    return phase


def _list_workspaces() -> list[dict[str, Any]]:
    sel = f"app.kubernetes.io/part-of={PART_OF}"
    sts = json.loads(kubectl("get", "statefulsets", "-l", sel, "-o", "json", action="list").get("stdout") or "{}").get("items", [])
    pods = json.loads(kubectl("get", "pods", "-l", sel, "-o", "json", action="list").get("stdout") or "{}").get("items", [])
    by_pod = {p["metadata"]["name"]: p for p in pods}
    out = []
    for s in sts:
        lbl = s["metadata"].get("labels", {})
        name = lbl.get("workspace/name") or s["metadata"]["name"].removeprefix("ws-")
        pod = by_pod.get(pod_name(name))
        rt = lbl.get("workspace/runtime", "")
        out.append({
            "name": name,
            "release": s["metadata"]["name"],
            "runtime": rt,
            "runtimeLabel": RUNTIMES.get(rt, {}).get("label", rt),
            "family": RUNTIMES.get(rt, {}).get("family", ""),
            "size": lbl.get("workspace/size", ""),
            "owner": lbl.get("workspace/owner", ""),
            "image": (s["spec"]["template"]["spec"]["containers"] or [{}])[0].get("image"),
            "status": _status_from(s, pod),
            "pod": pod_name(name) if pod else None,
            "node": pod.get("spec", {}).get("nodeName") if pod else None,
            "podIP": pod.get("status", {}).get("podIP") if pod else None,
            "restarts": sum(c.get("restartCount", 0) for c in pod.get("status", {}).get("containerStatuses", [])) if pod else 0,
            "createdAt": s["metadata"].get("creationTimestamp"),
            "namespace": NAMESPACE,
        })
    return sorted(out, key=lambda w: w["createdAt"] or "", reverse=True)


# ---- endpoints
@app.get("/api/health")
def health():
    return {"ok": True, "namespace": NAMESPACE, "chart": CHART, "dryRun": DRY_RUN, "time": datetime.now(timezone.utc).isoformat()}


@app.get("/api/catalog")
def catalog():
    return {"runtimes": RUNTIMES, "sizes": SIZES, "registry": CATALOG.get("registry"), "namespace": NAMESPACE, "dryRun": DRY_RUN, "catalogFile": os.environ.get("WS_CATALOG")}


@app.get("/api/pods")
def list_pods():
    return {"items": _list_workspaces()}


@app.post("/api/pods", status_code=201)
def create_pod(req: CreateRequest):
    name = _validate_name(req.name)
    if not OWNER_RE.match(req.owner):
        raise HTTPException(400, "owner contains invalid characters")
    if req.repo and not re.match(r"^(https?://|git@|ssh://)[\w.@:/~+-]+$", req.repo):
        raise HTTPException(400, "repo must be an https:// or git@ URL")
    if req.runtime not in RUNTIMES:
        raise HTTPException(400, f"unknown runtime {req.runtime!r}; see /api/catalog")
    if req.size not in SIZES:
        raise HTTPException(400, f"unknown size {req.size!r}; see /api/catalog")
    rt, sz = RUNTIMES[req.runtime], SIZES[req.size]
    log.info("create workspace %s", name, extra={"data": {"workspace": name, "runtime": req.runtime, "size": req.size, "owner": req.owner}})
    args = [
        "upgrade", "--install", release(name), CHART,
        "-n", NAMESPACE, *(["--create-namespace"] if CREATE_NS else []),
        "--set", f"name={name}",
        "--set", f"owner={req.owner}",
        "--set", f"runtime={req.runtime}",
        "--set", f"runtimeFamily={rt['family']}",
        "--set", f"image={rt['image']}",
        "--set", f"size={req.size}",
        "--set", f"resources.cpu={sz['cpu']}",
        "--set", f"resources.memory={sz['memory']}",
        "--set", f"persistence.size={sz['disk']}",
        "--set", f"repo={req.repo or ''}",
        "--set", f"partOf={PART_OF}",
        "--set", "replicas=1",
        "--set", f"imagePullSecret={CATALOG.get('imagePullSecret', '')}",
        "--set", f"persistence.storageClass={CATALOG.get('storageClass', '')}",
        "--set", f"sshd.enabled={'true' if CATALOG.get('sshd') else 'false'}",
    ]
    if KUBECONFIG:
        args += ["--kubeconfig", KUBECONFIG]
    entry = helm(*args, workspace=name, action="create")
    return {"name": name, "release": release(name), "pod": pod_name(name), "helm": entry["stdout"], "audit_id": entry["id"]}


@app.get("/api/pods/{name}")
def get_pod(name: str):
    name = _validate_name(name)
    ws = next((w for w in _list_workspaces() if w["name"] == name), None)
    if not ws:
        raise HTTPException(404, f"workspace {name} not found")
    if ws["pod"]:
        e = kubectl("get", "pods", ws["pod"], "-o", "json", workspace=name, action="describe", check=False)
        ws["podJson"] = json.loads(e["stdout"]) if e["ok"] and e["stdout"] else None
    return ws


@app.post("/api/pods/{name}/start")
def start_pod(name: str):
    name = _validate_name(name)
    e = kubectl("scale", "statefulset", release(name), "--replicas=1", workspace=name, action="start")
    return {"name": name, "status": "Pending", "out": e["stdout"], "audit_id": e["id"]}


@app.post("/api/pods/{name}/stop")
def stop_pod(name: str):
    name = _validate_name(name)
    e = kubectl("scale", "statefulset", release(name), "--replicas=0", workspace=name, action="stop")
    return {"name": name, "status": "Stopped", "out": e["stdout"], "audit_id": e["id"]}


@app.delete("/api/pods/{name}")
def delete_pod(name: str, purge: bool = Query(False, description="also delete the home PVC")):
    name = _validate_name(name)
    e = helm("uninstall", release(name), "-n", NAMESPACE, *(["--kubeconfig", KUBECONFIG] if KUBECONFIG else []), workspace=name, action="delete")
    ids = [e["id"]]
    if purge:
        # volumeClaimTemplates PVCs are not removed by helm uninstall: delete them by label.
        p = kubectl("delete", "pvc", "-l", f"workspace/name={name}", "--ignore-not-found", workspace=name, action="purge-volume")
        ids.append(p["id"])
    return {"name": name, "deleted": True, "purged": purge, "audit_ids": ids}


@app.get("/api/pods/{name}/logs")
def pod_logs(name: str, tail: int = Query(200, ge=1, le=5000)):
    name = _validate_name(name)
    e = kubectl("logs", pod_name(name), "-c", "dev", f"--tail={tail}", workspace=name, action="logs", check=False)
    return {"name": name, "pod": pod_name(name), "ok": e["ok"], "logs": e["stdout"] if e["ok"] else e["stderr"], "audit_id": e["id"]}


@app.get("/api/pods/{name}/events")
def pod_events(name: str):
    name = _validate_name(name)
    e = kubectl("get", "events", "--field-selector", f"involvedObject.name={pod_name(name)}", "--sort-by=.lastTimestamp", "-o", "json",
                workspace=name, action="events", check=False)
    items = json.loads(e["stdout"]).get("items", []) if e["ok"] and e["stdout"] else []
    return {"name": name, "events": [{"time": i.get("lastTimestamp") or i.get("eventTime"), "type": i.get("type"), "reason": i.get("reason"), "message": i.get("message")} for i in items]}


@app.post("/api/pods/{name}/exec")
def pod_exec(name: str, req: ExecRequest):
    """Run one command inside the developer's pod (a dev convenience, like `kubectl exec`)."""
    name = _validate_name(name)
    e = kubectl("exec", pod_name(name), "-c", "dev", "--", "sh", "-lc", req.command, workspace=name, action="exec", check=False)
    return {"name": name, "command": req.command, "exit_code": e["exit_code"], "stdout": e["stdout"], "stderr": e["stderr"], "audit_id": e["id"]}


@app.get("/api/audit")
def audit(limit: int = Query(200, ge=1, le=1000), workspace: Optional[str] = None, tool: Optional[Literal["kubectl", "helm"]] = None):
    """Every kubectl / helm call this process has made, newest first."""
    items = [a for a in AUDIT if (not workspace or a["workspace"] == workspace) and (not tool or a["tool"] == tool)]
    return {"items": items[:limit], "total": len(AUDIT), "logFile": str(LOG_DIR / "ws-api.jsonl")}


@app.get("/api/audit/{audit_id}")
def audit_one(audit_id: int):
    for a in AUDIT:
        if a["id"] == audit_id:
            return a
    raise HTTPException(404, "no such audit entry (buffer keeps the last 1000; see the log file)")


# ---- static portal (web/index.html) served at /
WEB = HERE.parent / "web"
if WEB.exists():
    @app.get("/", include_in_schema=False)
    def index():
        return FileResponse(WEB / "index.html")
    app.mount("/web", StaticFiles(directory=str(WEB)), name="web")


@app.on_event("startup")
def on_startup():
    log.info("ws-api starting", extra={"data": {"namespace": NAMESPACE, "chart": CHART, "dry_run": DRY_RUN, "kubectl": KUBECTL, "helm": HELM, "log_dir": str(LOG_DIR)}})
    if not DRY_RUN:
        run([KUBECTL, "version", "--client", "-o", "json"], action="startup-check", check=False)
        run([HELM, "version", "--short"], action="startup-check", check=False)
