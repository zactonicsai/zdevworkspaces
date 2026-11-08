#!/usr/bin/env bash
# Build and push every image.  Usage:  REGISTRY=registry.example.com/dev ./images/build.sh [--push]
set -euo pipefail
cd "$(dirname "$0")/.."
REGISTRY="${REGISTRY:?set REGISTRY, e.g. registry.example.com/dev}"
TAG_BASE="${TAG_BASE:-24.04}"
API_TAG="${API_TAG:-0.1.0}"
PUSH="${1:-}"

build() { echo "==> $1"; shift; docker build "$@"; }

build "base"        -f images/base/Dockerfile -t "$REGISTRY/dev-pod-base:$TAG_BASE" images
for py in 3.12 3.11; do
  build "python $py" --build-arg BASE="$REGISTRY/dev-pod-base:$TAG_BASE" --build-arg PY=$py -t "$REGISTRY/dev-pod-python:$py" images/python
done
for jdk in 21 17; do
  build "java $jdk"  --build-arg BASE="$REGISTRY/dev-pod-base:$TAG_BASE" --build-arg JDK=$jdk -t "$REGISTRY/dev-pod-java:$jdk" images/java
done
build "api"         -f images/api/Dockerfile -t "$REGISTRY/dev-pods-api:$API_TAG" .

if [ "$PUSH" = "--push" ]; then
  for i in dev-pod-base:$TAG_BASE dev-pod-python:3.12 dev-pod-python:3.11 dev-pod-java:21 dev-pod-java:17 dev-pods-api:$API_TAG; do
    docker push "$REGISTRY/$i"
  done
fi
echo "done. set image.registry=$REGISTRY in helm/dev-pods-api/values.yaml"
