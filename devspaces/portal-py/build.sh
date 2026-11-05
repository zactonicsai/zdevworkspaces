#!/usr/bin/env bash
# Build (and optionally push) the Python portal image the Helm chart deploys.
#   ./portal-py/build.sh                       # workspace/workspace-portal-py:dev
#   REGISTRY=myacr.azurecr.io/devspaces TAG=2026.11 ./portal-py/build.sh --push
#   ./portal-py/build.sh --test                # run the Python tests first
#   PLATFORMS=linux/amd64,linux/arm64 ./portal-py/build.sh --push   (buildx)
# Images: ${REGISTRY}/workspace-portal-py:${TAG}  (set IMAGE_NAME=workspace-portal to replace the Go image under the same name)
set -Eeuo pipefail
cd "$(dirname "$0")/.."
REGISTRY=${REGISTRY:-workspace}; TAG=${TAG:-dev}; IMAGE_NAME=${IMAGE_NAME:-workspace-portal-py}
IMG="$REGISTRY/$IMAGE_NAME:$TAG"
PUSH=0; TEST=0
for a in "$@"; do case "$a" in --push) PUSH=1;; --test) TEST=1;; -h|--help) sed -n 2,8p "$0"; exit 0;; esac; done
if [ "$TEST" = 1 ]; then echo "==> python tests"; ( cd portal-py && python3 -W ignore -m unittest ); fi
command -v docker >/dev/null || { echo "docker not found"; exit 1; }
echo "==> building $IMG"
if [ -n "${PLATFORMS:-}" ]; then
  docker buildx build --platform "$PLATFORMS" -f portal-py/Dockerfile -t "$IMG" ${SKIP_CSS:+--build-arg SKIP_CSS=$SKIP_CSS} $([ "$PUSH" = 1 ] && echo --push || echo --load) .
else
  docker build -f portal-py/Dockerfile -t "$IMG" ${SKIP_CSS:+--build-arg SKIP_CSS=$SKIP_CSS} .
  [ "$PUSH" = 1 ] && docker push "$IMG"
fi
cat <<EOT
built $IMG

  try it (no cluster):   docker run --rm -p 8080:8080 $IMG -stub          → http://localhost:8080
  with your kubeconfig:  docker run --rm -p 8080:8080 -v ~/.kube:/tmp/portal-home/.kube:ro $IMG -dev-user you@corp.example
  Helm:                  --set portal.backend=python --set global.registry=$REGISTRY --set global.tag=$TAG
EOT
