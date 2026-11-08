# shellcheck shell=bash
# Shared helpers for scripts/*.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# ---- defaults (override with env vars or flags)
NAMESPACE="${NAMESPACE:-dev-workspaces}"
RELEASE="${RELEASE:-dev-pods}"
REGISTRY="${REGISTRY:-}"                 # e.g. registry.example.com/dev ; empty = local images (kind/minikube/k3d)
API_TAG="${API_TAG:-0.1.0}"
HOST="${HOST:-}"                         # ingress host; empty = no ingress, use port-forward
INGRESS_CLASS="${INGRESS_CLASS:-nginx}"
TLS_SECRET="${TLS_SECRET:-}"             # existing TLS secret for HOST; empty = plain http
STORAGE_CLASS="${STORAGE_CLASS:-}"       # "" = cluster default
PULL_SECRET="${PULL_SECRET:-}"           # name of an existing docker-registry secret, or created from REGISTRY_USER/REGISTRY_PASS
VALUES_FILE="${VALUES_FILE:-}"           # extra -f values file
PORT="${PORT:-8080}"

c_bold=$'\e[1m'; c_dim=$'\e[2m'; c_ok=$'\e[32m'; c_warn=$'\e[33m'; c_err=$'\e[31m'; c_off=$'\e[0m'
info()  { echo "${c_bold}==>${c_off} $*"; }
ok()    { echo "${c_ok}  ✓${c_off} $*"; }
warn()  { echo "${c_warn}  !${c_off} $*"; }
die()   { echo "${c_err}error:${c_off} $*" >&2; exit 1; }
run()   { echo "${c_dim}  \$ $*${c_off}"; "$@"; }

need() { for b in "$@"; do command -v "$b" >/dev/null 2>&1 || die "'$b' is required but not on PATH"; done; }

current_context() { kubectl config current-context 2>/dev/null || die "no current kubectl context (kubectl config use-context …)"; }

# Detect local dev clusters where images can be loaded instead of pushed.
cluster_kind() {
  local ctx; ctx="$(current_context)"
  case "$ctx" in
    kind-*)       echo kind ;;
    minikube*)    echo minikube ;;
    k3d-*)        echo k3d ;;
    docker-desktop|docker-for-desktop|orbstack|rancher-desktop) echo docker ;;
    *)            echo remote ;;
  esac
}

confirm_context() {
  local ctx; ctx="$(current_context)"
  local server; server="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)"
  info "kubectl context: ${c_bold}${ctx}${c_off}  ${c_dim}${server}${c_off}"
  if [ "${YES:-0}" != "1" ] && [ -t 0 ]; then
    read -r -p "    deploy '${RELEASE}' into namespace '${NAMESPACE}' on this cluster? [y/N] " a
    [[ "$a" =~ ^[Yy]$ ]] || die "aborted"
  fi
  kubectl cluster-info >/dev/null 2>&1 || die "cannot reach the cluster for context ${ctx}"
}

image_prefix() { if [ -n "$REGISTRY" ]; then echo "${REGISTRY%/}/"; else echo ""; fi; }
