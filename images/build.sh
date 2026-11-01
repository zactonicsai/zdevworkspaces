#!/usr/bin/env bash
# Build and (optionally) push the workspace image set.
#   ./build.sh                       # build base + every language locally, tag :dev
#   REGISTRY=ghcr.io/acme/devspaces TAG=2026.10 PUSH=1 ./build.sh
#   LANGS="rust go" EXTRA_FEATURES="cmake,gdb" ./build.sh   # bake tools into variants (-cmake-gdb suffix)
#   PLATFORMS=linux/amd64,linux/arm64 PUSH=1 ./build.sh     # multi-arch via buildx
set -Eeuo pipefail
cd "$(dirname "$0")"
REGISTRY=${REGISTRY:-workspace}
TAG=${TAG:-dev}
LANGS=${LANGS:-"python go rust java node cpp"}
EXTRA_FEATURES=${EXTRA_FEATURES:-}
PLATFORMS=${PLATFORMS:-}
PUSH=${PUSH:-0}

build() { # build NAME CONTEXT DOCKERFILE [build-args...]
  local name=$1 ctx=$2 df=$3; shift 3
  local ref="$REGISTRY/$name:$TAG" args=(--file "$df" --tag "$ref" "$@")
  if [ -n "$PLATFORMS" ]; then
    docker buildx build --platform "$PLATFORMS" $([ "$PUSH" = 1 ] && echo --push || echo --load) "${args[@]}" "$ctx"
  else
    docker build "${args[@]}" "$ctx"
    [ "$PUSH" = 1 ] && docker push "$ref"
  fi
  echo "built $ref"
}

build workspace-base . base/Dockerfile
suffix=""; [ -n "$EXTRA_FEATURES" ] && suffix="-${EXTRA_FEATURES//,/-}"
for lang in $LANGS; do
  build "workspace-${lang}${suffix}" . langs/Dockerfile \
    --build-arg "BASE_IMAGE=$REGISTRY/workspace-base:$TAG" \
    --build-arg "LANG_FEATURE=$lang" \
    --build-arg "EXTRA_FEATURES=$EXTRA_FEATURES"
done
build workspace-cli .. cli/Dockerfile

# Portal: keep its embedded catalog copy in sync, regenerate the demo catalog,
# rebuild Tailwind when npm is available (the committed CSS is used otherwise).
cp ../deploy/platform/catalog.json ../portal/catalog.default.json
( cd ../portal && if command -v npm >/dev/null 2>&1; then npm install --no-audit --no-fund >/dev/null && npm run -s build:css && npm run -s gen:catalog; else echo "npm not found: using committed web/tailwind.css"; fi )
build workspace-portal .. ../portal/Dockerfile --build-arg "SKIP_CSS=${SKIP_CSS:-0}"
