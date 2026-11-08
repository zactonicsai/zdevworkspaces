#!/usr/bin/env bash
# Shared helpers for deploy/cluster/providers/azure/*.sh (sourced, not run).
# Loads env.sh (this folder), then the cloud-agnostic deploy/cluster/env.sh if present
# (azure/env.sh wins), supports --dry-run, and gives every script the same vocabulary.
set -Eeuo pipefail
AZ_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$AZ_DIR/../../../.." && pwd)
DRY_RUN=0
for _a in "$@"; do [ "$_a" = --dry-run ] && DRY_RUN=1; done
[ -f "$REPO_ROOT/deploy/cluster/env.sh" ] && . "$REPO_ROOT/deploy/cluster/env.sh"
if [ -f "$AZ_DIR/env.sh" ]; then . "$AZ_DIR/env.sh"
elif [ "$DRY_RUN" = 1 ]; then . "$AZ_DIR/env.example"
else echo "missing $AZ_DIR/env.sh — cp env.example env.sh and fill it in (or pass --dry-run)"; exit 1; fi

step()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
ok()    { printf '  \033[32m[ok]\033[0m   %s\n' "$*"; }
skip()  { printf '  \033[2m[skip]\033[0m %s\n' "$*"; }
warn()  { printf '  \033[33m[warn]\033[0m %s\n' "$*"; }
fail()  { printf '  \033[31m[fail]\033[0m %s\n' "$*"; exit 1; }
have()  { command -v "$1" >/dev/null 2>&1; }
# run: execute (or print under --dry-run). Use for anything that changes Azure.
run()   { if [ "$DRY_RUN" = 1 ]; then printf '  \033[36m$\033[0m %s\n' "$*"; else "$@"; fi; }
# probe: a read-only az call; under --dry-run it "fails" so the create path is printed.
probe() { if [ "$DRY_RUN" = 1 ]; then return 1; fi; "$@" >/dev/null 2>&1; }
# q: read-only query that must return a value; under --dry-run returns a placeholder.
q()     { if [ "$DRY_RUN" = 1 ]; then case "$1" in *://*|/*) echo "$1";; *) echo "<${1:-value}>";; esac; else shift; "$@"; fi; }
tags()  { echo ${TAGS:-}; }
sub_args() { [ -n "${SUBSCRIPTION:-}" ] && echo "--subscription $SUBSCRIPTION" || true; }
