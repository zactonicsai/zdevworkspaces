#!/usr/bin/env bash
# Removes everything the demo created: the kind cluster, the Compose containers
# and volume, and the SSH config entries the CLI wrote. Images stay (delete with
# `docker rmi $(docker images 'workspace/*' -q)` if you want the disk back).
set -Eeuo pipefail
cd "$(dirname "$0")/../.."
CLUSTER=${CLUSTER:-devspaces}
if command -v kind >/dev/null && kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  for ws in $(kubectl --context "kind-$CLUSTER" get statefulsets -A -l devspaces.io/workspace -o jsonpath='{range .items[*]}{.metadata.labels.devspaces\.io/workspace}{"\n"}{end}' 2>/dev/null || true); do
    # tidy ~/.ssh/config and known_hosts entries the CLI created
    WORKSPACE_NAMESPACE=ws-demo python3 cli/workspace delete "$ws" -y >/dev/null 2>&1 || true
  done
  kind delete cluster --name "$CLUSTER"
fi
docker compose -f deploy/demo/docker-compose.yaml down -v 2>/dev/null || true
echo "demo removed"
