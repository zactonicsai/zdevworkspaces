#!/usr/bin/env bash
# Everything in order on an existing AKS cluster (Istio or not):
#   cp env.example env.sh && $EDITOR env.sh && ./quickstart.sh
# Each step is its own idempotent script; re-run any of them alone.
#   00 look around → 10 namespaces/storage → 20 helm install → 30 mesh check
#   → 60 route on the existing Istio gateway (when there is one and it is safe to publish)
#   → 40 developers + sample dev pods → 50 verify
set -Eeuo pipefail
cd "$(dirname "$0")"
./00-detect.sh "$@" && ./10-namespaces.sh "$@" && ./20-helm.sh "$@" && ./30-istio-check.sh "$@"
# publish through the gateway: always for entra/oidc; for dev only when an IP allow-list is set
AUTH=dev; ALLOW_CIDRS=""; GW_SELECTOR=""
[ -f env.sh ] && . ./env.sh || { for a in "$@"; do [ "$a" = --dry-run ] && . ./env.example; done; }
[ -f detected.env ] && . ./detected.env
if [ -n "$GW_SELECTOR" ] || [ "${1:-}" = --dry-run ]; then
  if [ "$AUTH" != dev ] || [ -n "$ALLOW_CIDRS" ]; then ./60-virtualservice.sh "$@"; else
    printf '\n  [skip] 60-virtualservice.sh: AUTH=dev with no ALLOW_CIDRS — portal stays port-forward only (set ALLOW_CIDRS to publish it)\n'; fi
fi
./40-dev-pods.sh "$@" && ./50-verify.sh "$@"
