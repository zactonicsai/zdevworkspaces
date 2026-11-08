#!/usr/bin/env bash
# Deploy the developer-pods portal + API onto the CURRENT kubectl context with Helm.
#
#   scripts/deploy.sh [--build] [--load] [--push] [--dry-run-api] [--yes]
#
#   --build        build the images from images/*/Dockerfile (needs docker)
#   --push         push them to $REGISTRY after building
#   --load         load them into a local cluster (kind / minikube / k3d) instead of pushing
#   --dry-run-api  deploy the API with WS_DRY_RUN=1 (UI demo; no real pods are created)
#   --yes          don't ask for confirmation of the context
#
# Environment (all optional):
#   REGISTRY=registry.example.com/dev   where images live (empty = local images, local cluster)
#   NAMESPACE=dev-workspaces  RELEASE=dev-pods  API_TAG=0.1.0
#   HOST=devpods.example.com  INGRESS_CLASS=nginx  TLS_SECRET=devpods-tls
#   STORAGE_CLASS=gp3         PULL_SECRET=regcred   (or REGISTRY_USER/REGISTRY_PASS to create one)
#   VALUES_FILE=my-values.yaml   extra values merged last
#
# Examples
#   # kind / minikube, everything local:
#   kind create cluster && scripts/deploy.sh --build --load
#   # existing cluster with a registry and ingress:
#   REGISTRY=registry.example.com/dev HOST=devpods.example.com TLS_SECRET=devpods-tls scripts/deploy.sh --build --push
#   # already-built images, just (re)install the chart:
#   REGISTRY=registry.example.com/dev HOST=devpods.example.com scripts/deploy.sh
source "$(dirname "$0")/lib.sh"

BUILD=0 PUSH=0 LOAD=0 DRY=0
for a in "$@"; do case "$a" in
  --build) BUILD=1 ;; --push) PUSH=1 ;; --load) LOAD=1 ;; --dry-run-api) DRY=1 ;; --yes|-y) YES=1 ;;
  -h|--help) sed -n '2,30p' "$0"; exit 0 ;; *) die "unknown flag $a" ;;
esac; done

need kubectl helm
[ "$PUSH" = 1 ] && [ -z "$REGISTRY" ] && die "--push needs REGISTRY=…"
confirm_context
KIND="$(cluster_kind)"
PREFIX="$(image_prefix)"

if [ -z "$REGISTRY" ] && [ "$KIND" = "remote" ] && [ "$DRY" = 0 ]; then
  die "REGISTRY is empty but the context looks like a remote cluster: set REGISTRY=… (and --push), or use --dry-run-api"
fi

# ---- images -------------------------------------------------------------------------------------
IMAGES=(dev-pod-base:24.04 dev-pod-python:3.12 dev-pod-python:3.11 dev-pod-java:21 dev-pod-java:17 "dev-pods-api:${API_TAG}")
if [ "$BUILD" = 1 ]; then
  need docker
  info "building images ${PREFIX:+for $PREFIX}"
  REGISTRY="${REGISTRY:-local}" API_TAG="$API_TAG" "$ROOT/images/build.sh" $([ "$PUSH" = 1 ] && echo --push)
  [ -z "$REGISTRY" ] && PREFIX="local/"
fi
[ -z "$REGISTRY" ] && PREFIX="local/"

if [ "$LOAD" = 1 ]; then
  info "loading images into the local $KIND cluster"
  for i in "${IMAGES[@]}"; do
    case "$KIND" in
      kind)     run kind load docker-image "${PREFIX}${i}" --name "${KIND_CLUSTER:-$(current_context | sed 's/^kind-//')}" ;;
      minikube) run minikube image load "${PREFIX}${i}" ;;
      k3d)      run k3d image import "${PREFIX}${i}" -c "${K3D_CLUSTER:-$(current_context | sed 's/^k3d-//')}" ;;
      docker)   ok "${PREFIX}${i} (docker-desktop shares the daemon; nothing to load)" ;;
      *)        die "--load only works with kind, minikube, k3d or docker-desktop contexts" ;;
    esac
  done
fi

# ---- namespace + pull secret --------------------------------------------------------------------
info "namespace $NAMESPACE"
kubectl get ns "$NAMESPACE" >/dev/null 2>&1 || run kubectl create namespace "$NAMESPACE"
if [ -z "$PULL_SECRET" ] && [ -n "${REGISTRY_USER:-}" ] && [ -n "${REGISTRY_PASS:-}" ]; then
  PULL_SECRET=regcred
  info "creating pull secret $PULL_SECRET for ${REGISTRY%%/*}"
  kubectl -n "$NAMESPACE" create secret docker-registry "$PULL_SECRET" \
    --docker-server="${REGISTRY%%/*}" --docker-username="$REGISTRY_USER" --docker-password="$REGISTRY_PASS" \
    --dry-run=client -o yaml | kubectl apply -f -
fi

# ---- helm ---------------------------------------------------------------------------------------
SET=(
  --set "image.repository=${PREFIX}dev-pods-api"
  --set "image.tag=${API_TAG}"
  --set "catalog.registry=${PREFIX%/}"
  --set "catalog.storageClass=${STORAGE_CLASS}"
  --set "api.dryRun=$([ "$DRY" = 1 ] && echo true || echo false)"
)
if [ -z "$REGISTRY" ]; then SET+=(--set image.pullPolicy=IfNotPresent); fi   # local images: never pull
if [ -n "$PULL_SECRET" ]; then SET+=(--set "imagePullSecrets[0].name=${PULL_SECRET}" --set "catalog.imagePullSecret=${PULL_SECRET}"); fi
if [ -n "$HOST" ]; then
  SET+=(--set ingress.enabled=true --set "ingress.host=${HOST}" --set "ingress.className=${INGRESS_CLASS}")
  if [ -n "$TLS_SECRET" ]; then SET+=(--set ingress.tls=true --set "ingress.tlsSecret=${TLS_SECRET}"); else SET+=(--set ingress.tls=false); fi
else
  SET+=(--set ingress.enabled=false)
fi
[ -n "$VALUES_FILE" ] && SET+=(-f "$VALUES_FILE")

info "helm lint"
run helm lint "$ROOT/helm/dev-pods" "${SET[@]}" >/dev/null && ok "chart ok"
run helm lint "$ROOT/helm/dev-pod" >/dev/null && ok "dev-pod chart ok"

info "helm upgrade --install $RELEASE"
run helm upgrade --install "$RELEASE" "$ROOT/helm/dev-pods" -n "$NAMESPACE" "${SET[@]}" --wait --timeout 5m

info "waiting for rollout"
run kubectl -n "$NAMESPACE" rollout status "deploy/${RELEASE}" --timeout=180s

# ---- smoke --------------------------------------------------------------------------------------
info "smoke test: /api/health"
POD="$(kubectl -n "$NAMESPACE" get pod -l "app.kubernetes.io/instance=${RELEASE}" -o jsonpath='{.items[0].metadata.name}')"
kubectl -n "$NAMESPACE" exec "$POD" -- python3 -c "import urllib.request;print(urllib.request.urlopen('http://127.0.0.1:8080/api/health').read().decode())" \
  && ok "API is up"

echo
info "done"
if [ -n "$HOST" ]; then
  echo "    portal:   http$([ -n "$TLS_SECRET" ] && echo s)://${HOST}/"
  echo "    swagger:  http$([ -n "$TLS_SECRET" ] && echo s)://${HOST}/api/docs"
else
  echo "    no ingress configured; open a tunnel with:"
  echo "      scripts/port-forward.sh          # then http://localhost:${PORT}/"
fi
echo "    api log:  kubectl -n ${NAMESPACE} logs deploy/${RELEASE} -f | jq ."
echo "    dev pods: kubectl -n ${NAMESPACE} get statefulsets,pods,pvc -l app.kubernetes.io/part-of=dev-workspaces"
