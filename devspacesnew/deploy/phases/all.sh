#!/usr/bin/env bash
# Run every phase in order. Each one is independent and re-runnable; stop after any of them.
#   cp env.example env.sh && $EDITOR env.sh && ./all.sh [--dry-run]
set -Eeuo pipefail; cd "$(dirname "$0")"
for p in 00-check 10-namespace 20-storage 30-platform 40-route 50-developers 60-workspaces 70-verify; do ./$p.sh "$@"; done
