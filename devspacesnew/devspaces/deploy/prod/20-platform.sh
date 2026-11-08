#!/usr/bin/env bash
# Phase 3 step 2 — install or upgrade the Kubernetes layer with Helm. Pure Kubernetes:
# the same command on every provider; only the values file differs.
#   ./deploy/prod/20-platform.sh            # helm upgrade --install
#   ./deploy/prod/20-platform.sh --diff     # render and show what would change (needs helm-diff plugin, else prints the manifest)
#   ./deploy/prod/20-platform.sh --dry-run  # render + kubeconform, touch nothing
set -Eeuo pipefail
cd "$(dirname "$0")/../.."; . deploy/cluster/env.sh
for t in kubectl helm; do command -v $t >/dev/null || { echo "missing $t"; exit 1; }; done
ctx=$(kubectl config current-context); echo "context: $ctx"
args=(devspaces charts/devspaces -n devspaces-system --create-namespace -f "$VALUES_FILE"
      --set global.registry="$REGISTRY" --set global.tag="$TAG" --set ingress.host="$PORTAL_HOST" --set ingress.className="$INGRESS_CLASS"
      --set catalogOverrides.artifactory.url="$ARTIFACTORY_URL" --set admission.platformAdminGroups[0]=system:masters --set admission.platformAdminGroups[1]="$PLATFORM_ADMIN_GROUP")
[ -f "${SECRETS_VALUES_FILE:-/nonexistent}" ] && args+=(-f "$SECRETS_VALUES_FILE") || echo "note: $SECRETS_VALUES_FILE not found — oauth2-proxy needs oauth2Proxy.existingSecret or clientId/clientSecret/cookieSecret"
case "${1:-}" in
  --dry-run) helm template "${args[@]}" > /tmp/devspaces-rendered.yaml; echo "rendered /tmp/devspaces-rendered.yaml ($(grep -c '^kind:' /tmp/devspaces-rendered.yaml) objects)"; command -v kubeconform >/dev/null && kubeconform -strict -summary -ignore-missing-schemas /tmp/devspaces-rendered.yaml; exit 0;;
  --diff) helm diff upgrade "${args[@]}" 2>/dev/null || helm template "${args[@]}"; exit 0;;
esac
./deploy/cluster/check-contract.sh --no-probe || { echo "cluster contract not satisfied; fix Layer 1 first (SKIP_CONTRACT=1 to override)"; [ "${SKIP_CONTRACT:-0}" = 1 ] || exit 1; }
helm upgrade --install "${args[@]}" --wait --timeout 10m
kubectl -n devspaces-system get deploy,svc,ingress,cronjob
echo; echo "platform installed. Next: ./deploy/prod/30-onboard.sh users.csv, then ./deploy/prod/40-smoke-test.sh"
