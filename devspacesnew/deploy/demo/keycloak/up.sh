#!/usr/bin/env bash
# Keycloak + portal on Docker, login required.
#   ./up.sh            # stub profile: no cluster, static data, real login
#   ./up.sh kube       # the API deploys real developer pods on the kind cluster "devspaces"
#   ./up.sh oauth2-proxy
# Idempotent; re-run after changing the realm file (docker compose down -v to re-import it).
set -Eeuo pipefail
cd "$(dirname "$0")"
REPO=$(cd ../../.. && pwd)
PROFILE=${1:-${COMPOSE_PROFILES:-stub}}
CLUSTER=${CLUSTER:-devspaces}
step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
ok()   { printf '  \033[32m[ok]\033[0m   %s\n' "$*"; }
skip() { printf '  \033[2m[skip]\033[0m %s\n' "$*"; }
have() { command -v "$1" >/dev/null 2>&1; }
have docker || { echo "docker not found"; exit 1; }
docker info >/dev/null 2>&1 || { echo "Docker is not running"; exit 1; }
[ -f .env ] || cp .env.example .env
export COMPOSE_PROFILES=$PROFILE

step "Portal image"
IMG=$(grep -E '^PORTAL_IMAGE=' .env | cut -d= -f2-); IMG=${IMG:-workspace/workspace-portal-py:dev}
if docker image inspect "$IMG" >/dev/null 2>&1 && [ "${REBUILD:-0}" != 1 ]; then skip "$IMG exists (REBUILD=1 to rebuild)"; else
  docker compose build portal; ok "built $IMG"; fi

# compose declares the kind network as external for the kube profile; make sure it exists for the others too
docker network inspect kind >/dev/null 2>&1 || { docker network create kind >/dev/null; ok "created placeholder network 'kind'"; }

if [ "$PROFILE" = kube ]; then
  step "kind cluster '$CLUSTER' for real developer pods"
  have kind && have kubectl || { echo "kube profile needs kind + kubectl (docs/demo-docker.md Level 2)"; exit 1; }
  if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
    echo "  cluster missing — running ../demo-up.sh (creates it, builds/loads images, installs the platform)"
    ( cd "$REPO" && SKIP_CHECKS=${SKIP_CHECKS:-0} ./deploy/demo/demo-up.sh )
  else skip "cluster exists"; fi
  kubectl config use-context "kind-$CLUSTER" >/dev/null
  kind get kubeconfig --internal --name "$CLUSTER" > kubeconfig-internal
  ok "kubeconfig-internal written (server reachable from containers on the 'kind' network)"
  if ! kubectl get ns devspaces-system >/dev/null 2>&1; then kubectl apply -k "$REPO/deploy/demo" >/dev/null; ok "platform installed"; else skip "platform present"; fi
  for u in alice@example.com bob@example.com admin@example.com; do
    short=${u%%@*}
    if kubectl get ns "ws-$short" >/dev/null 2>&1; then skip "$u onboarded"; else python3 "$REPO/cli/workspace" --context "kind-$CLUSTER" admin onboard "$u" --lab >/dev/null && ok "onboarded $u → ws-$short"; fi
  done
  python3 "$REPO/cli/workspace" --context "kind-$CLUSTER" admin project create demo --members alice@example.com,bob@example.com >/dev/null 2>&1 && ok "project namespace ws-proj-demo" || true
else
  [ -f kubeconfig-internal ] || echo "# unused in profile $PROFILE" > kubeconfig-internal
fi

step "docker compose up (profile $PROFILE)"
docker compose up -d
printf '  waiting for Keycloak'
for i in $(seq 1 60); do
  if curl -fsS -m 2 http://localhost:8180/realms/devspaces/.well-known/openid-configuration >/dev/null 2>&1; then echo; ok "Keycloak realm 'devspaces' ready"; break; fi
  printf '.'; sleep 3
  [ "$i" = 60 ] && { echo; echo "Keycloak did not come up: docker compose logs keycloak"; exit 1; }
done
printf '  waiting for the portal'
for i in $(seq 1 30); do
  P=8080; [ "$PROFILE" = oauth2-proxy ] && P=4180
  if curl -fsS -m 2 "http://localhost:$P/healthz" >/dev/null 2>&1 || curl -s -o /dev/null -m 2 "http://localhost:$P/" ; then echo; ok "portal answering on :$P"; break; fi
  printf '.'; sleep 2
done
code=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/api/me || true)
[ "$code" = 401 ] && ok "API refuses anonymous calls (401) — login is required" || [ "$PROFILE" = oauth2-proxy ] || echo "  [warn] /api/me returned $code without a token"
cat <<EOT

  Portal:    http://localhost:$([ "$PROFILE" = oauth2-proxy ] && echo 4180 || echo 8080)      sign in: alice@example.com / alice  (bob/bob, admin/admin)
  Keycloak:  http://localhost:8180   admin console admin / $(grep -E '^KEYCLOAK_ADMIN_PASSWORD=' .env | cut -d= -f2- || echo admin)   realm "devspaces"
  wsctl:     wsctl login --portal http://localhost:8080          (device code: open the URL it prints, log in as alice)
  smoke:     ./test.sh
  stop:      ./down.sh      (down.sh -v also forgets the Keycloak database)
EOT
[ "$PROFILE" = kube ] && echo "  Developer pods land in ws-alice / ws-bob on the kind cluster: kubectl get pods -A -l devspaces.io/workspace"
