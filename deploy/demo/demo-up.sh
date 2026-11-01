#!/usr/bin/env bash
# One-shot: the full devspaces system on a local kind cluster (Level 2 of
# docs/demo-docker.md). Every step is also explained there so you can run them
# by hand. Safe to re-run: each step is idempotent.
#
#   ./deploy/demo/demo-up.sh                 # everything
#   SKIP_BUILD=1 ./deploy/demo/demo-up.sh    # reuse images built earlier
#   LANGS="go python" ./deploy/demo/demo-up.sh
set -Eeuo pipefail
cd "$(dirname "$0")/../.."           # repo root
CLUSTER=${CLUSTER:-devspaces}
LANGS=${LANGS:-go}
DEMO_USER=${DEMO_USER:-demo@example.com}
export REGISTRY=workspace TAG=dev

step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
need() { command -v "$1" >/dev/null 2>&1 || { echo "missing: $1 — $2"; exit 1; }; }

step "0/7 Checking prerequisites"
need docker  "https://docs.docker.com/get-docker/"
need kind    "https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
need kubectl "https://kubernetes.io/docs/tasks/tools/"
need python3 "the workspace CLI is a Python 3.9+ script"
need ssh-keygen "part of the OpenSSH client"
docker info >/dev/null 2>&1 || { echo "Docker is not running."; exit 1; }
kernel=$(docker run --rm alpine:3.20 uname -r 2>/dev/null || echo "unknown")
echo "Docker's Linux kernel: $kernel  (user namespaces need >= 6.3; see the troubleshooting section otherwise)"
mem=$(docker info --format '{{.MemTotal}}' 2>/dev/null || echo 0)
[ "$mem" -ge 6000000000 ] 2>/dev/null || echo "note: Docker has $((mem / 1024 / 1024 / 1024)) GB of memory; 8 GB is comfortable for 3 nodes + workspaces"

step "1/7 Creating the kind cluster '$CLUSTER' (control-plane + worker + lab worker)"
if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  echo "cluster exists, reusing"
else
  kind create cluster --config deploy/demo/kind-config.yaml --wait 120s
fi
kubectl config use-context "kind-$CLUSTER" >/dev/null
kubectl get nodes

if [ "${SKIP_BUILD:-0}" != 1 ]; then
  step "2/7 Building images (base, ${LANGS// /, }, cli, portal) — first time takes 10-20 minutes"
  ( cd images && LANGS="$LANGS" ./build.sh )
else
  step "2/7 Skipping image build (SKIP_BUILD=1)"
fi

step "3/7 Loading images into the kind nodes (a few minutes; images are large)"
imgs=(workspace/workspace-cli:dev workspace/workspace-portal:dev)
for l in $LANGS; do imgs+=("workspace/workspace-$l:dev"); done
kind load docker-image "${imgs[@]}" --name "$CLUSTER"

step "4/7 Installing the platform + portal (demo overlay)"
kubectl apply -k deploy/demo
kubectl -n devspaces-system rollout status deploy/workspace-portal --timeout=180s

step "5/7 Onboarding $DEMO_USER (namespaces ws-demo and ws-demo-lab)"
python3 cli/workspace admin onboard "$DEMO_USER" --lab

step "6/7 Installing the CLI on your PATH (optional)"
if [ -w /usr/local/bin ]; then
  install -m 755 cli/workspace /usr/local/bin/workspace && echo "installed /usr/local/bin/workspace"
else
  echo "run:  sudo install -m 755 cli/workspace /usr/local/bin/workspace   (or use ./cli/workspace)"
fi

step "7/7 Done. Next steps"
cat <<EOF

  Portal (as $DEMO_USER):
    kubectl -n devspaces-system port-forward svc/workspace-portal 8080:80
    open http://localhost:8080  → Profile → paste your SSH public key → Create

  CLI:
    export WORKSPACE_NAMESPACE=ws-demo        # or: {"user":"demo"} in ~/.config/workspace/config.json
    workspace create --lang go --tools ripgrep,cmake --git-auth none
    workspace ssh go
    workspace code go                          # VS Code Remote-SSH on /workspace

  Tear down:  ./deploy/demo/demo-down.sh
EOF
