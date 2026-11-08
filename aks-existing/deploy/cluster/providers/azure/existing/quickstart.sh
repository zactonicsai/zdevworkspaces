#!/usr/bin/env bash
# Everything in order on an existing AKS cluster (Istio or not):
#   cp env.example env.sh && $EDITOR env.sh && ./quickstart.sh
# Each step is its own idempotent script; re-run any of them alone.
set -Eeuo pipefail
cd "$(dirname "$0")"
./00-detect.sh "$@" && ./10-namespaces.sh "$@" && ./20-helm.sh "$@" && ./30-istio-check.sh "$@" && ./40-dev-pods.sh "$@" && ./50-verify.sh "$@"
