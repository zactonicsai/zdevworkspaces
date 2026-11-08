#!/usr/bin/env bash
# Pre-step — a small AKS cluster on Azure, ready for ./all.sh. Not a phase: phases need a
# cluster; this makes one. (The full production build with VNet, node pools, Entra and Key
# Vault is deploy/cluster/providers/azure/10-aks.sh — this is the simple version.)
#
# WHAT   1. resource group + AKS: 2 nodes, Azure CNI overlay with network policy (so the
#           phases' NetworkPolicies are enforced), managed identity, OIDC issuer
#        2. a way in:  INGRESS=approuting (default) → the managed nginx "app routing" add-on
#                      INGRESS=istio               → the Istio add-on + external ingress gateway
#                      INGRESS=none                → nothing; the phases use port-forward
#        3. an Azure Container Registry attached to the cluster; builds and pushes the
#           workspace images there (skip with NO_IMAGES=1 and point REGISTRY elsewhere)
#        4. writes env.sh for the phases: KUBE_CONTEXT, REGISTRY, EXPOSE, PORTAL_HOST as
#           workspaces.<ingress-ip>.nip.io (a DNS name that needs no DNS), ALLOW_CIDRS = your IP
# WHY    Same phases as on kind or any other cluster; only the cluster and the registry differ.
# NEEDS  az (logged in: az login), kubectl, helm, docker (for the image build). Costs money:
#        two Standard_D4s_v5 nodes ≈ a few dollars a day — ./aks-up.sh --delete when done.
#   ./aks-up.sh [--dry-run]          ./aks-up.sh --delete      # deletes the whole resource group
set -Eeuo pipefail
cd "$(dirname "$0")"; REPO_ROOT=$(cd ../.. && pwd)
RESOURCE_GROUP=${RESOURCE_GROUP:-devspaces-trial-rg}; CLUSTER=${CLUSTER:-devspaces-trial}; LOCATION=${LOCATION:-eastus2}
NODE_SIZE=${NODE_SIZE:-Standard_D4s_v5}; NODE_COUNT=${NODE_COUNT:-2}
ACR_NAME=${ACR_NAME:-}                      # empty = devspaces<6 random chars>; must be globally unique, letters+digits
INGRESS=${INGRESS:-approuting}; LANGS=${LANGS:-go node}; TAG=${TAG:-trial}
DRY_RUN=0; for a in "$@"; do [ "$a" = --dry-run ] && DRY_RUN=1; done
step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }; ok() { printf '  \033[32m[ok]\033[0m   %s\n' "$*"; }
skip() { printf '  \033[2m[skip]\033[0m %s\n' "$*"; }; warn() { printf '  \033[33m[warn]\033[0m %s\n' "$*"; }
fail() { printf '  \033[31m[fail]\033[0m %s\n' "$*"; exit 1; }
run() { printf '  \033[36m$\033[0m %s\n' "$*"; [ "$DRY_RUN" = 1 ] || "$@"; }     # every command is shown before it runs
have() { command -v "$1" >/dev/null 2>&1; }
azq() { [ "$DRY_RUN" = 1 ] && return 0; az "$@" 2>/dev/null || true; }     # read-only az, never aborts

if [ "${1:-}" = --delete ]; then
  step "Deleting resource group $RESOURCE_GROUP (cluster, registry, disks — everything)"
  printf 'Type the resource group name to confirm: '; read -r a; [ "$a" = "$RESOURCE_GROUP" ] || exit 1
  az group delete -n "$RESOURCE_GROUP" --yes --no-wait; kubectl config delete-context "$CLUSTER" >/dev/null 2>&1 || true
  ok "deletion started (runs in the background; env.sh kept)"; exit 0
fi

step "Tools and login"
for t in az kubectl helm; do have "$t" && ok "$t" || { [ "$DRY_RUN" = 1 ] && skip "$t missing (dry-run)" || fail "$t not found"; }; done
[ "${NO_IMAGES:-0}" = 1 ] || have docker || [ "$DRY_RUN" = 1 ] || fail "docker is needed to build the images (or NO_IMAGES=1 and set REGISTRY to images you already have)"
SUB=$(azq account show --query '[name,id]' -o tsv | paste -sd' ' -); [ -n "$SUB" ] || [ "$DRY_RUN" = 1 ] || fail "not logged in: az login"
ok "subscription: ${SUB:-<dry-run>}"
for p in Microsoft.ContainerService Microsoft.ContainerRegistry; do [ "$(azq provider show -n $p --query registrationState -o tsv)" = Registered ] || run az provider register -n $p --wait >/dev/null; done

step "Resource group $RESOURCE_GROUP in $LOCATION"
[ "$(azq group exists -n "$RESOURCE_GROUP")" = true ] && skip "exists" || run az group create -n "$RESOURCE_GROUP" -l "$LOCATION" --tags purpose=devspaces-trial -o none

step "Container registry"
if [ -z "$ACR_NAME" ]; then
  ACR_NAME=$(azq acr list -g "$RESOURCE_GROUP" --query '[0].name' -o tsv)
  [ -n "$ACR_NAME" ] || ACR_NAME="devspaces$(head -c 300 /dev/urandom | LC_ALL=C tr -dc a-z0-9 | head -c 6)"
fi
if [ -n "$(azq acr show -n "$ACR_NAME" -g "$RESOURCE_GROUP" --query name -o tsv)" ]; then skip "$ACR_NAME exists"
else run az acr create -n "$ACR_NAME" -g "$RESOURCE_GROUP" --sku Basic -o none; ok "$ACR_NAME.azurecr.io (Basic)"; fi
REGISTRY="$ACR_NAME.azurecr.io/devspaces"

step "AKS cluster $CLUSTER ($NODE_COUNT × $NODE_SIZE)"
if [ -n "$(azq aks show -n "$CLUSTER" -g "$RESOURCE_GROUP" --query name -o tsv)" ]; then skip "exists"
else
  run az aks create -n "$CLUSTER" -g "$RESOURCE_GROUP" -l "$LOCATION" \
    --node-count "$NODE_COUNT" --node-vm-size "$NODE_SIZE" --os-sku Ubuntu2204 \
    --network-plugin azure --network-plugin-mode overlay --network-policy cilium --network-dataplane cilium \
    --enable-managed-identity --enable-oidc-issuer --enable-workload-identity \
    --attach-acr "$ACR_NAME" --generate-ssh-keys --tier free -o none
  ok "created (Azure CNI overlay + Cilium network policy, managed identity, OIDC issuer)"
fi
run az aks get-credentials -n "$CLUSTER" -g "$RESOURCE_GROUP" --overwrite-existing -o none
run kubectl label nodes --all devspaces.io/pool=standard --overwrite >/dev/null
ok "kubectl context: $CLUSTER"

INGRESS_IP=""
case "$INGRESS" in
  approuting)
    step "Ingress: the AKS 'app routing' add-on (managed nginx)"
    if [ "$(azq aks show -n "$CLUSTER" -g "$RESOURCE_GROUP" --query ingressProfile.webAppRouting.enabled -o tsv)" = true ]; then skip "enabled"
    else run az aks approuting enable -n "$CLUSTER" -g "$RESOURCE_GROUP" -o none; ok "enabled: IngressClass webapprouting.kubernetes.azure.com"; fi
    if [ "$DRY_RUN" != 1 ]; then printf '  waiting for the load-balancer IP (kubectl -n app-routing-system get svc nginx)'; for i in $(seq 1 30); do INGRESS_IP=$(kubectl -n app-routing-system get svc nginx -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true); [ -n "$INGRESS_IP" ] && break; printf .; sleep 10; done; echo; fi
    INGRESS_CLASS=webapprouting.kubernetes.azure.com; EXPOSE=ingress; CTRL_NS=app-routing-system;;
  istio)
    step "Ingress: the AKS Istio add-on + external ingress gateway"
    if [ "$(azq aks show -n "$CLUSTER" -g "$RESOURCE_GROUP" --query serviceMeshProfile.mode -o tsv)" = Istio ]; then skip "mesh enabled"
    else run az aks mesh enable -n "$CLUSTER" -g "$RESOURCE_GROUP" -o none; ok "Istio add-on enabled (takes a few minutes)"; fi
    if [ "$(azq aks show -n "$CLUSTER" -g "$RESOURCE_GROUP" --query 'serviceMeshProfile.istio.components.ingressGateways[?mode==`External`].enabled | [0]' -o tsv)" = true ]; then skip "external gateway enabled"
    else run az aks mesh enable-ingress-gateway -n "$CLUSTER" -g "$RESOURCE_GROUP" --ingress-gateway-type external -o none; ok "external ingress gateway"; fi
    if [ "$DRY_RUN" != 1 ]; then printf '  waiting for the load-balancer IP (kubectl -n aks-istio-ingress get svc aks-istio-ingressgateway-external)'; for i in $(seq 1 30); do INGRESS_IP=$(kubectl -n aks-istio-ingress get svc aks-istio-ingressgateway-external -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true); [ -n "$INGRESS_IP" ] && break; printf .; sleep 10; done; echo; fi
    INGRESS_CLASS=""; EXPOSE=istio; CTRL_NS=aks-istio-ingress;;
  none) step "Ingress: none (port-forward)"; EXPOSE=port-forward; INGRESS_CLASS="";;
  *) fail "INGRESS must be approuting, istio or none";;
esac
[ "$EXPOSE" = port-forward ] || ok "ingress address: ${INGRESS_IP:-<pending — re-run later to fill PORTAL_HOST>}"

if [ "${NO_IMAGES:-0}" != 1 ]; then
  step "Workspace images → $REGISTRY (LANGS=\"$LANGS\"; first build 10-20 min, push a few more)"
  run az acr login -n "$ACR_NAME" -o none
  ( cd "$REPO_ROOT/images" && run env REGISTRY="$REGISTRY" TAG="$TAG" LANGS="$LANGS" PUSH=1 ./build.sh )
  ok "pushed: workspace-portal, workspace-cli, $(for l in $LANGS; do printf 'workspace-%s ' "$l"; done):$TAG"
fi

step "env.sh for the phases"
MYIP=$(curl -fsS -m 5 https://ifconfig.me 2>/dev/null || curl -fsS -m 5 https://api.ipify.org 2>/dev/null || true)
HOST="workspaces.${INGRESS_IP:-INGRESS-IP}.nip.io"
if [ -f env.sh ] && [ "${REWRITE_ENV:-0}" != 1 ]; then
  if [ -n "$INGRESS_IP" ] && grep -q 'INGRESS-IP' env.sh; then sed -i.bak "s/INGRESS-IP/$INGRESS_IP/" env.sh && rm -f env.sh.bak; ok "filled the ingress IP into env.sh: PORTAL_HOST=$HOST"
  else skip "env.sh exists (REWRITE_ENV=1 to overwrite). Wanted: REGISTRY=$REGISTRY EXPOSE=$EXPOSE PORTAL_HOST=$HOST"; fi
else
  python3 - "$CLUSTER" "$REGISTRY" "$TAG" "$EXPOSE" "$INGRESS_CLASS" "$HOST" "${MYIP:+$MYIP/32}" "$(set -- $LANGS; echo "$1")" <<'PY'
import re, sys
cluster, registry, tag, expose, iclass, host, cidr, lang = sys.argv[1:]
s = open("env.example").read()
def put(key, val, note=""):
    global s
    s = re.sub(r'(?m)^%s=.*$' % key, '%s=%s%s' % (key, val, ("   # " + note) if note else ""), s, count=1)
put("KUBE_CONTEXT", '"%s"' % cluster)
put("REGISTRY", registry, "ACR attached to the cluster by aks-up.sh")
put("TAG", tag)
put("SHARED_STORAGE_CLASS", '"azurefile-csi"', "Azure Files: ReadWriteMany team folders")
put("EXPOSE", expose)
put("INGRESS_CLASS", '"%s"' % iclass)
put("INGRESS_CONTROLLER_NS", '"%s"' % ("app-routing-system" if expose == "ingress" else ""))
put("PORTAL_HOST", host, "nip.io resolves to the ingress IP; swap for a real name when you have one")
put("ALLOW_CIDRS", '"%s"' % cidr, "your public IP (AUTH=dev lock); widen or clear when AUTH=oidc")
put("SAMPLE_WORKSPACES", '"demo:%s"' % lang)
open("env.sh", "w").write(s)
PY
  ok "env.sh written (AUTH=dev, DEV_USER=you@example.com)"
  [ -n "$MYIP" ] || warn "could not learn your public IP: set ALLOW_CIDRS in env.sh before ./40-route.sh"
fi
cat <<EOT

  Cluster:   $CLUSTER in $RESOURCE_GROUP ($LOCATION)   kubectl get nodes
  Registry:  $REGISTRY:$TAG
  Next:      ./all.sh      then open http://$HOST
  TLS later: install cert-manager, set TLS_SECRET in env.sh, re-run ./40-route.sh
  Login:     set AUTH=oidc + your issuer (Entra: https://login.microsoftonline.com/<tenant>/v2.0) and re-run ./30-platform.sh ./40-route.sh
  Remove:    ./90-uninstall.sh ; ./aks-up.sh --delete
EOT
