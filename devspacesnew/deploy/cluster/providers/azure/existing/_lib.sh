#!/usr/bin/env bash
# Shared helpers for deploy/cluster/providers/azure/existing/*.sh (sourced, not run).
set -Eeuo pipefail
EX_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$EX_DIR/../../../../.." && pwd)
DRY_RUN=0
for _a in "$@"; do [ "$_a" = --dry-run ] && DRY_RUN=1; done
if [ -f "$EX_DIR/env.sh" ]; then . "$EX_DIR/env.sh"
elif [ "$DRY_RUN" = 1 ]; then . "$EX_DIR/env.example"
else echo "missing $EX_DIR/env.sh — cp env.example env.sh and fill in the first block (or pass --dry-run)"; exit 1; fi
[ -f "$EX_DIR/detected.env" ] && . "$EX_DIR/detected.env"   # written by 00-detect.sh
CHART=${CHART:-$REPO_ROOT/charts/devspaces}
KCTX=${KUBE_CONTEXT:-$CLUSTER_NAME}

step()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
ok()    { printf '  \033[32m[ok]\033[0m   %s\n' "$*"; }
skip()  { printf '  \033[2m[skip]\033[0m %s\n' "$*"; }
warn()  { printf '  \033[33m[warn]\033[0m %s\n' "$*"; }
fail()  { printf '  \033[31m[fail]\033[0m %s\n' "$*"; exit 1; }
have()  { command -v "$1" >/dev/null 2>&1; }
run()   { if [ "$DRY_RUN" = 1 ]; then printf '  \033[36m$\033[0m %s\n' "$*"; else "$@"; fi; }
k()     { kubectl --context "$KCTX" "$@"; }
# kq: read-only kubectl that must not abort the script; empty output under --dry-run
kq()    { if [ "$DRY_RUN" = 1 ]; then return 0; fi; kubectl --context "$KCTX" "$@" 2>/dev/null || true; }
need_detect() { [ -f "$EX_DIR/detected.env" ] || [ "$DRY_RUN" = 1 ] || fail "run ./00-detect.sh first (it writes detected.env)"; }
