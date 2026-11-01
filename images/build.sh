#!/usr/bin/env bash
# Build and (optionally) push the workspace image set.
#   ./build.sh                       # build base + every language locally, tag :dev
#   LANGS="go" ./build.sh            # just base + Go (+ cli + portal) — the demo set
#   REGISTRY=ghcr.io/acme/devspaces TAG=2026.10 PUSH=1 ./build.sh
#   LANGS="rust go" EXTRA_FEATURES="cmake,gdb" ./build.sh   # bake tools into variants (-cmake-gdb suffix)
#   PLATFORMS=linux/amd64,linux/arm64 PUSH=1 ./build.sh     # multi-arch via buildx
#   PORTAL=0 ./build.sh              # skip the portal image
# Works with bash 3.2 (macOS) and newer.
set -Eeuo pipefail
cd "$(dirname "$0")"
REGISTRY=${REGISTRY:-workspace}
TAG=${TAG:-dev}
LANGS=${LANGS:-"python go rust java node cpp"}
EXTRA_FEATURES=${EXTRA_FEATURES:-}
PLATFORMS=${PLATFORMS:-}
PUSH=${PUSH:-0}
PORTAL=${PORTAL:-1}
SKIP_CSS=${SKIP_CSS:-0}

command -v docker >/dev/null 2>&1 || { echo "docker not found: https://docs.docker.com/get-docker/" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "Docker is installed but not running (or you lack permission to use it)." >&2; exit 1; }

# build NAME CONTEXT DOCKERFILE [extra docker build args...]
build() {
  local name=$1 ctx=$2 df=$3
  shift 3
  local ref="$REGISTRY/$name:$TAG"
  local args
  args=(--file "$df" --tag "$ref" ${1+"$@"})   # ${1+"$@"}: no "unbound variable" on bash < 4.4 when there are no extras
  echo
  echo "### building $ref"
  if [ -n "$PLATFORMS" ]; then
    local out=--load
    if [ "$PUSH" = 1 ]; then out=--push; fi
    docker buildx build --platform "$PLATFORMS" "$out" "${args[@]}" "$ctx"
  else
    docker build "${args[@]}" "$ctx"
    if [ "$PUSH" = 1 ]; then docker push "$ref"; fi
  fi
  echo "### built $ref"
}

echo "registry=$REGISTRY tag=$TAG languages: $LANGS  (set LANGS=\"go\" for a quick first build)"
start=$(date +%s)
build workspace-base . base/Dockerfile

suffix=""
if [ -n "$EXTRA_FEATURES" ]; then suffix="-${EXTRA_FEATURES//,/-}"; fi
for lang in $LANGS; do
  if [ ! -f "features/$lang/install.sh" ]; then
    echo "unknown language '$lang' (no features/$lang/install.sh)" >&2
    exit 1
  fi
  build "workspace-${lang}${suffix}" . langs/Dockerfile \
    --build-arg "BASE_IMAGE=$REGISTRY/workspace-base:$TAG" \
    --build-arg "LANG_FEATURE=$lang" \
    --build-arg "EXTRA_FEATURES=$EXTRA_FEATURES"
done

build workspace-cli .. cli/Dockerfile

if [ "$PORTAL" = 1 ]; then
  # Portal: keep its embedded catalog copy in sync, regenerate the demo catalog, and
  # rebuild Tailwind when npm is available (the committed CSS is used otherwise; the
  # Dockerfile also rebuilds it in its own stage unless SKIP_CSS=1).
  cp ../deploy/platform/catalog.json ../portal/catalog.default.json
  if command -v npm >/dev/null 2>&1; then
    ( cd ../portal && npm install --no-audit --no-fund >/dev/null && npm run -s build:css && npm run -s gen:catalog )
  else
    echo "npm not found: using the committed portal/web/tailwind.css"
  fi
  build workspace-portal .. ../portal/Dockerfile --build-arg "SKIP_CSS=$SKIP_CSS"
fi

echo
echo "done in $(( $(date +%s) - start ))s:"
docker images --format '  {{.Repository}}:{{.Tag}}  {{.Size}}' "$REGISTRY/*" 2>/dev/null || true
