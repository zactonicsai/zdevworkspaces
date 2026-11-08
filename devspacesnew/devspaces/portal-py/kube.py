"""kube.py — the Python API's view of Kubernetes: `kubectl` driven, impersonating the caller.

Two clients exist, exactly as in the Go portal:
  * `Kube.for_user(user)`  every call carries `--as <user> --as-group <g>`: RBAC is evaluated for
    the developer, so the API can never do more than `kubectl` would let that person do;
  * `Kube.privileged()`    the pod's own ServiceAccount — used ONLY for sharing (reading a guest's
    profile key, writing a RoleBinding/PV into another namespace), under the narrowly scoped
    ClusterRole `workspace-portal-sharing`.
The rendering of manifests is borrowed from the CLI (cli/workspace, loaded as module `wscli`)
so portal and CLI create byte-identical workspaces."""
from __future__ import annotations

import importlib.machinery
import importlib.util
import json
import os
import subprocess
import sys
from pathlib import Path


def load_cli_module():
    """Load cli/workspace (no .py extension) as module `wscli`. Looks next to this file
    (image: /app/wscli.py) and in the repository (../cli/workspace)."""
    here = Path(__file__).resolve().parent
    for cand in [here / "wscli.py", here.parent / "cli" / "workspace", Path(os.environ.get("WORKSPACE_CLI", ""))]:
        if cand and cand.is_file():
            loader = importlib.machinery.SourceFileLoader("wscli", str(cand))
            spec = importlib.util.spec_from_loader("wscli", loader)
            mod = importlib.util.module_from_spec(spec)
            sys.modules["wscli"] = mod
            loader.exec_module(mod)
            return mod
    raise RuntimeError("cli/workspace not found (set WORKSPACE_CLI=/path/to/cli/workspace)")


wscli = load_cli_module()


class KubeError(Exception):
    def __init__(self, status: int, message: str):
        super().__init__(message)
        self.status = status


class Kube(wscli.Kube):
    """wscli.Kube (kubectl wrapper) plus impersonation headers."""

    def __init__(self, context=None, user=None, groups=None):
        super().__init__(context=context, namespace=None, offline=False)
        self.user = user
        self.groups = [g for g in (groups or []) if g != "system:authenticated"]

    @classmethod
    def for_user(cls, user: dict, context=None):
        return cls(context=context, user=user["name"], groups=user.get("groups") or [])

    @classmethod
    def privileged(cls, context=None):
        return cls(context=context)

    def cmd(self, args, ns=None):
        c = super().cmd(args, ns)
        if self.user:
            c += ["--as", self.user]
            for g in self.groups:
                c += ["--as-group", g]
        return c

    # ---- richer error mapping than the CLI needs
    def run(self, args, ns=None, input=None, check=True):
        if self.offline:
            raise KubeError(500, "offline")
        res = subprocess.run(self.cmd(args, ns), input=input, text=True, capture_output=True)
        if check and res.returncode != 0:
            msg = (res.stderr or res.stdout).strip()
            low = msg.lower()
            if "forbidden" in low:
                raise KubeError(403, msg)
            if "not found" in low or "notfound" in low:
                raise KubeError(404, msg)
            if "admission webhook" in low or "validatingadmissionpolicy" in low or "denied the request" in low:
                raise KubeError(400, msg)
            raise KubeError(502, msg)
        return res

    def get(self, kind, name=None, ns=None, selector=None, extra=None):
        args = ["get", kind] + ([name] if name else []) + (["-l", selector] if selector else []) + (extra or [])
        res = self.run(args + ["-o", "json"], ns=ns, check=False)
        if res.returncode != 0:
            err = (res.stderr or "").lower()
            if "not found" in err or "notfound" in err:
                return None
            if "forbidden" in err:
                raise KubeError(403, res.stderr.strip())
            raise KubeError(502, res.stderr.strip())
        return json.loads(res.stdout)

    def items(self, kind, ns=None, selector=None, all_namespaces=False):
        extra = ["-A"] if all_namespaces else None
        try:
            res = self.get(kind, ns=wscli.NO_NS if all_namespaces else ns, selector=selector, extra=extra)
        except KubeError as e:
            if e.status in (403, 404):
                return []
            raise
        return (res or {}).get("items", [])

    def apply(self, objects):
        doc = json.dumps({"apiVersion": "v1", "kind": "List", "items": objects})
        return self.run(["apply", "-f", "-"], ns=wscli.NO_NS, input=doc).stdout

    def patch(self, kind, name, patch: dict, ns=None, ptype="merge"):
        return self.run(["patch", kind, name, f"--type={ptype}", "-p", json.dumps(patch)], ns=ns)

    def annotate(self, kind, name, ns, **kv):
        return self.run(["annotate", kind, name] + [f"{k}={v}" for k, v in kv.items()] + ["--overwrite"], ns=ns, check=False)

    def delete(self, kind, name, ns=None):
        return self.run(["delete", kind, name, "--ignore-not-found"], ns=ns)

    def scale(self, name, replicas, ns):
        return self.run(["scale", "statefulset", name, f"--replicas={replicas}"], ns=ns)

    def logs(self, pod, ns, tail=200, container="workspace"):
        res = self.run(["logs", pod, "-c", container, f"--tail={tail}"], ns=ns, check=False)
        if res.returncode != 0:
            raise KubeError(404, res.stderr.strip())
        return res.stdout

    def whoami(self):
        return self.user
