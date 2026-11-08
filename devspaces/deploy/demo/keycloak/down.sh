#!/usr/bin/env bash
# Stop the Keycloak + portal stack. `./down.sh -v` also removes the Keycloak database volume
# (the realm is re-imported from realm-devspaces.json on the next up).
set -Eeuo pipefail
cd "$(dirname "$0")"
export COMPOSE_PROFILES=stub,kube,oauth2-proxy
docker compose down ${1:-}
