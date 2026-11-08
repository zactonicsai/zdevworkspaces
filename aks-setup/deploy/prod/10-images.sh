#!/usr/bin/env bash
# Phase 3 step 1 — build the image set and push it to the production registry (Artifactory
# Docker repo, ECR, Artifact Registry, ACR…). Log in first (docker login / aws ecr
# get-login-password / gcloud auth configure-docker / az acr login).
#   ./deploy/prod/10-images.sh                       # all languages
#   LANGS="go python" ./deploy/prod/10-images.sh
#   PLATFORMS=linux/amd64,linux/arm64 ./deploy/prod/10-images.sh   # mixed-architecture node pools
set -Eeuo pipefail
cd "$(dirname "$0")/../.."; . deploy/cluster/env.sh
docker info >/dev/null 2>&1 || { echo "Docker is not running"; exit 1; }
docker manifest inspect "$REGISTRY/workspace-base:$TAG" >/dev/null 2>&1 && echo "note: $REGISTRY/workspace-base:$TAG already exists; pushing will overwrite it (use a new TAG for a new release)"
REGISTRY="$REGISTRY" TAG="$TAG" PUSH=1 PLATFORMS="${PLATFORMS:-}" LANGS="${LANGS:-python go rust java node cpp}" ./images/build.sh
echo; echo "pushed. Record the tag in your values file: global.tag: \"$TAG\""
