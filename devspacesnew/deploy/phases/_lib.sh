#!/usr/bin/env bash
# Shared helpers for deploy/phases/*.sh (sourced). Reads env.sh and detected.env; defines
# step/ok/skip/warn/fail and `run` (which only prints under --dry-run).
set -Eeuo pipefail
PH_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$PH_DIR/../.." && pwd)
DRY_RUN=0; for _a in "$@"; do [ "$_a" = --dry-run ] && DRY_RUN=1; done
if [ -f "$PH_DIR/env.sh" ]; then . "$PH_DIR/env.sh"
elif [ "$DRY_RUN" = 1 ]; then . "$PH_DIR/env.example"
else echo "missing $PH_DIR/env.sh — run: cp env.example env.sh  (then edit it) — or pass --dry-run"; exit 1; fi
[ -f "$PH_DIR/detected.env" ] && . "$PH_DIR/detected.env"
# defaults for everything detected.env may provide, so a phase can run alone (set -u safe)
: "${K8S_VERSION:=}" "${DEFAULT_SC:=}" "${PROVISIONER:=}" "${SNAP_CRD:=false}" "${SNAP_CLASS:=${SNAPSHOT_CLASS:-}}" "${MESH:=none}" "${ISTIO_REV:=}"
: "${INGRESS_CLASS_DETECTED:=}" "${GW_NS:=}" "${GW_SELECTOR:=}" "${GW_IP:=}" "${HOME_SC:=}" "${SHARED_SC:=}"
CHART=${CHART:-$REPO_ROOT/charts/devspaces}
KCTX=${KUBE_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}
KCTX_FLAG=${KCTX:+--context $KCTX}          # empty when no context is known: plain kubectl/helm
step()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
ok()    { printf '  \033[32m[ok]\033[0m   %s\n' "$*"; }
skip()  { printf '  \033[2m[skip]\033[0m %s\n' "$*"; }
warn()  { printf '  \033[33m[warn]\033[0m %s\n' "$*"; }
fail()  { printf '  \033[31m[fail]\033[0m %s\n' "$*"; exit 1; }
have()  { command -v "$1" >/dev/null 2>&1; }
cmd()   { printf '  \033[36m$\033[0m %s\n' "$*" >&2; }          # every command is shown before it runs
run()   { local c=$1; shift; case "$c" in k) c=kubectl; set -- $KCTX_FLAG "$@";; h) c=helm; set -- ${KCTX:+--kube-context "$KCTX"} "$@";; esac
          cmd "$c $*"; [ "$DRY_RUN" = 1 ] || "$c" "$@"; }
k()     { cmd "kubectl $KCTX_FLAG $*"; kubectl $KCTX_FLAG "$@"; }
kq()    { [ "$DRY_RUN" = 1 ] && return 0; kubectl $KCTX_FLAG "$@" 2>/dev/null || true; }   # read-only, never aborts
h()     { cmd "helm ${KCTX:+--kube-context $KCTX} $*"; helm ${KCTX:+--kube-context "$KCTX"} "$@"; }
read -r -d '' POD_DIAG_PY <<'PY' || true
import json, sys
for p in json.load(sys.stdin)["items"]:
    n = p["metadata"]["name"]; st = p["status"]
    if st.get("phase") == "Pending" and not st.get("containerStatuses"):
        for c in st.get("conditions", []):
            if c.get("reason") == "Unschedulable":
                print("    ! %s: unschedulable - %s" % (n, c.get("message", "")))
                print("      -> not enough CPU/memory, a taint, or a PVC that cannot bind: kubectl -n NS describe pod %s" % n)
    for c in st.get("containerStatuses", []) + st.get("initContainerStatuses", []):
        w = (c.get("state") or {}).get("waiting") or {}
        r = w.get("reason", ""); cn = c["name"]; msg = w.get("message", "")[:160]
        if r in ("ImagePullBackOff", "ErrImagePull"):
            print("    ! %s/%s: %s - %s" % (n, cn, r, msg))
            print("      -> the node cannot pull %s: wrong REGISTRY/TAG in env.sh, image not pushed/loaded, or no pull secret (PULL_SECRET_DOCKERCONFIG)" % c.get("image"))
        elif r == "CrashLoopBackOff":
            print("    ! %s/%s: %s - kubectl -n NS logs %s -c %s --previous" % (n, cn, r, n, cn))
        elif r and r != "ContainerCreating":
            print("    ! %s/%s: %s - %s" % (n, cn, r, msg))
PY
# wait_ready NAMESPACE deploy/NAME SECONDS — like `kubectl rollout status`, but talks: prints the pods
# every 10 s and, when a pod is stuck (image pull, pending, crash), the reason and what to do.
wait_ready() {
  local ns=$1 obj=$2 limit=${3:-600} t=0 sel
  sel=$(kubectl $KCTX_FLAG -n "$ns" get "$obj" -o jsonpath='{range .spec.selector.matchLabels}{end}{.spec.selector.matchLabels}' 2>/dev/null | python3 -c 'import sys,ast;d=ast.literal_eval(sys.stdin.read() or "{}");print(",".join(f"{k}={v}" for k,v in d.items()))' 2>/dev/null || true)
  cmd "kubectl $KCTX_FLAG -n $ns rollout status $obj   (watching pods${sel:+ -l $sel}; Ctrl-C stops watching, the release stays)"
  while :; do
    if kubectl $KCTX_FLAG -n "$ns" rollout status "$obj" --timeout=1s >/dev/null 2>&1; then
      kubectl $KCTX_FLAG -n "$ns" get pods ${sel:+-l "$sel"} --no-headers 2>/dev/null | sed 's/^/    /'; return 0; fi
    local line; line=$(kubectl $KCTX_FLAG -n "$ns" get pods ${sel:+-l "$sel"} --no-headers 2>/dev/null | awk '{printf "%s %s(%s) ", $1, $3, $2}')
    printf '    %3ds  %s\n' "$t" "${line:-no pods yet (the controller has not created them: kubectl -n $ns describe $obj)}"
    if [ $t -ge 30 ]; then
      kubectl $KCTX_FLAG -n "$ns" get pods ${sel:+-l "$sel"} -o json 2>/dev/null | python3 -c "$POD_DIAG_PY" 2>/dev/null | sed "s/NS/$ns/g"
      kubectl $KCTX_FLAG -n "$ns" get events --field-selector type=Warning --sort-by=.lastTimestamp 2>/dev/null | tail -3 | sed 's/^/    event: /'
    fi
    [ $t -ge "$limit" ] && { warn "still not ready after ${limit}s — the release is installed; fix the cause above and re-run this phase"; return 1; }
    sleep 10; t=$((t+10))
  done
}
need_detect() { [ -f "$PH_DIR/detected.env" ] || [ "$DRY_RUN" = 1 ] || fail "run ./00-check.sh first (it writes detected.env)"; }
short_name()  { printf '%s' "$1" | cut -d@ -f1 | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9-]/-/g'; }
scheme()      { [ -n "${TLS_SECRET:-}" ] && echo https || echo http; }
