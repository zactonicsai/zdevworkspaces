#!/usr/bin/env bash
# 0/5 — Look at the AKS cluster you already have and write detected.env: Istio flavour
# (AKS add-on or self-managed), revision, ambient vs sidecar, ingress gateway, storage classes,
# snapshot support, NetworkPolicy enforcement, user-namespace readiness. Read-only.
#   ./00-detect.sh [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"

step "Connecting to $CLUSTER_NAME (resource group $RESOURCE_GROUP)"
if [ -z "${KUBE_CONTEXT:-}" ]; then
  have az || fail "az CLI not found (https://learn.microsoft.com/cli/azure/install-azure-cli)"
  run az aks get-credentials ${SUBSCRIPTION:+--subscription "$SUBSCRIPTION"} -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --overwrite-existing
fi
have kubectl || fail "kubectl not found"
have helm || warn "helm not found — 20-helm.sh needs it (https://helm.sh/docs/intro/install/)"
SERVER=$(kq config view -o jsonpath="{.clusters[?(@.name==\"$KCTX\")].cluster.server}")
K8S=$(kq version -o json | python3 -c 'import json,sys;print(json.load(sys.stdin)["serverVersion"]["gitVersion"])' 2>/dev/null || echo "?")
ok "context $KCTX → ${SERVER:-?} (Kubernetes ${K8S:-?})"

step "Istio"
ISTIO_NS=""; ISTIO_FLAVOUR=none; REV=""; AMBIENT=false; GW_NS=""; GW_SELECTOR=""; GW_IP=""
if [ "$DRY_RUN" = 1 ]; then
  ISTIO_NS=aks-istio-system; ISTIO_FLAVOUR=aks-addon; REV=asm-1-25; GW_NS=aks-istio-ingress; GW_SELECTOR="istio: aks-istio-ingressgateway-external"; GW_IP="<gateway-ip>"
else
  if k get ns aks-istio-system >/dev/null 2>&1; then ISTIO_NS=aks-istio-system; ISTIO_FLAVOUR=aks-addon
  elif k get ns istio-system >/dev/null 2>&1; then ISTIO_NS=istio-system; ISTIO_FLAVOUR=self-managed; fi
  if [ -n "$ISTIO_NS" ]; then
    # revision: from the istiod deployments' labels (asm-1-25 for the add-on, "default" or a tag otherwise)
    REV=$(kq -n "$ISTIO_NS" get deploy -l app=istiod -o jsonpath='{.items[0].metadata.labels.istio\.io/rev}')
    [ -z "$REV" ] && REV=$(kq -n "$ISTIO_NS" get deploy -l app=istiod -o jsonpath='{.items[0].metadata.labels.istio\.io/rev}')
    [ -n "$(kq -n "$ISTIO_NS" get ds ztunnel -o name)" ] && AMBIENT=true
    [ -n "$(kq -n "$ISTIO_NS" get ds -l k8s-app=istio-cni-node -o name)" ] && ISTIO_CNI=true || ISTIO_CNI=false
    # ingress gateway: add-on (aks-istio-ingress) or classic istio-ingressgateway
    if [ -n "$(kq -n aks-istio-ingress get svc aks-istio-ingressgateway-external -o name)" ]; then
      GW_NS=aks-istio-ingress; GW_SELECTOR="istio: aks-istio-ingressgateway-external"
      GW_IP=$(kq -n aks-istio-ingress get svc aks-istio-ingressgateway-external -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    elif [ -n "$(kq -n "$ISTIO_NS" get svc istio-ingressgateway -o name)" ]; then
      GW_NS=$ISTIO_NS; GW_SELECTOR="istio: ingressgateway"
      GW_IP=$(kq -n "$ISTIO_NS" get svc istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    fi
  fi
fi
case "$ISTIO_FLAVOUR" in
  aks-addon)    ok "AKS Istio add-on (namespace $ISTIO_NS), revision ${REV:-?}";;
  self-managed) ok "self-managed Istio (namespace $ISTIO_NS), revision ${REV:-default}";;
  none)         warn "no Istio found — the platform works without it; MESH_MODE will be 'none' (enable later: az aks mesh enable)";;
esac
[ "$AMBIENT" = true ] && ok "ambient dataplane (ztunnel) present → namespaces can be enrolled with one label, no sidecars"
[ "${ISTIO_CNI:-false}" = true ] && ok "istio-cni present → sidecar injection works under Pod Security 'baseline'"
if [ -n "$GW_SELECTOR" ]; then ok "ingress gateway: $GW_NS ($GW_SELECTOR) ${GW_IP:+→ $GW_IP}"; else [ "$ISTIO_FLAVOUR" != none ] && warn "no ingress gateway — AKS add-on: az aks mesh enable-ingress-gateway --ingress-gateway-type external; or set AUTH=dev and use port-forward"; fi
# decide the mesh mode
if [ "${MESH_MODE:-auto}" = auto ]; then
  if [ "$ISTIO_FLAVOUR" = none ]; then MESH=none; elif [ "$AMBIENT" = true ]; then MESH=istio-ambient; else MESH=istio-sidecar; fi
else MESH=$MESH_MODE; fi
[ "${ISTIO_REVISION:-auto}" = auto ] && ISTIO_REV=$REV || ISTIO_REV=$ISTIO_REVISION
[ "$ISTIO_REV" = default ] && ISTIO_REV=""
ok "mesh mode for tenant namespaces: $MESH${ISTIO_REV:+ (istio.io/rev=$ISTIO_REV)}"
if [ "$MESH" = istio-sidecar ] && [ "${ISTIO_CNI:-false}" != true ] && [ "$DRY_RUN" != 1 ]; then
  warn "sidecar mode without istio-cni: the istio-init container needs NET_ADMIN, which Pod Security 'baseline' in developer namespaces rejects. Prefer ambient, or set MESH_MODE=none and keep Istio for the portal only."
fi

step "Storage"
SC_LIST=$(kq get storageclass -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.provisioner}{"\t"}{.reclaimPolicy}{"\t"}{.allowVolumeExpansion}{"\t"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}')
pick() { printf '%s\n' "$SC_LIST" | awk -F'\t' -v want="$1" '$1==want{print $1}' | head -1; }
HOME_FAST=$(pick workspace-home-premium); [ -z "$HOME_FAST" ] && HOME_FAST=$(pick managed-csi-premium); [ -z "$HOME_FAST" ] && HOME_FAST=$(pick managed-premium)
HOME_STD=$(pick workspace-home-standard);  [ -z "$HOME_STD" ]  && HOME_STD=$(pick managed-csi);         [ -z "$HOME_STD" ]  && HOME_STD=$(pick default)
SHARED=$(pick workspace-shared-files);     [ -z "$SHARED" ]    && SHARED=$(pick azurefile-csi-premium); [ -z "$SHARED" ]    && SHARED=$(pick azurefile-csi)
if [ "$DRY_RUN" = 1 ]; then HOME_FAST=managed-csi-premium; HOME_STD=managed-csi; SHARED=azurefile-csi-premium; fi
[ -n "$HOME_FAST" ] && ok "fast home drives → $HOME_FAST" || warn "no premium disk class found; fast = standard"
[ -n "$HOME_STD" ]  && ok "standard home drives → $HOME_STD" || fail "no managed-csi StorageClass — is the Azure Disk CSI driver enabled?"
[ -z "$HOME_FAST" ] && HOME_FAST=$HOME_STD
[ -n "$SHARED" ]    && ok "shared drives (RWX) → $SHARED" || warn "no Azure Files class: shared drives will be hidden in the portal"
for c in $HOME_FAST $HOME_STD; do
  rp=$(printf '%s\n' "$SC_LIST" | awk -F'\t' -v w="$c" '$1==w{print $3" "$4}')
  case "$rp" in "Retain true") ;; *) [ "$DRY_RUN" = 1 ] || warn "$c is '$rp' (contract C14 wants Retain + expansion). ../20-storage.sh creates proper workspace-home-* classes; the built-in AKS classes use Delete, so a purged drive's disk is removed immediately.";; esac
done
SNAP_CLASS=""
if [ -n "$(kq get crd volumesnapshotclasses.snapshot.storage.k8s.io -o name)" ] || [ "$DRY_RUN" = 1 ]; then
  SNAP_CLASS=$(kq get volumesnapshotclass -o jsonpath='{.items[?(@.driver=="disk.csi.azure.com")].metadata.name}' | awk '{print $1}')
  [ "$DRY_RUN" = 1 ] && SNAP_CLASS=csi-azuredisk-vsc
  [ -n "$SNAP_CLASS" ] && ok "snapshots: VolumeSnapshotClass $SNAP_CLASS" || warn "snapshot CRDs present but no Azure Disk VolumeSnapshotClass — 10-namespaces.sh creates csi-azuredisk-vsc"
else
  warn "no snapshot CRDs (AKS normally ships them) — drives snapshot will not work"
fi

step "Policy & isolation"
CNI=$(kq -n kube-system get ds -o jsonpath='{.items[*].metadata.name}')
case " $CNI " in *" cilium "*) ok "Cilium dataplane → NetworkPolicy enforced (C8)";; *" azure-npm "*|*" calico-node "*) ok "network policy engine present (C8)";; *) [ "$DRY_RUN" = 1 ] || warn "no NetworkPolicy engine detected (azure-npm/calico/cilium): tenant isolation policies will not be enforced — az aks update --network-policy cilium";; esac
KVER=$(kq get nodes -o jsonpath='{.items[0].status.nodeInfo.kernelVersion}'); CRT=$(kq get nodes -o jsonpath='{.items[0].status.nodeInfo.containerRuntimeVersion}')
ok "nodes: kernel ${KVER:-?}, runtime ${CRT:-?} (user namespaces need kernel ≥ 6.3 + containerd 2; otherwise set isolation default to 'none' in catalogOverrides)"
LAB=$(kq get nodes -l devspaces.io/lab=true -o name | wc -l | tr -d ' ')
[ "${LAB:-0}" -gt 0 ] && ok "$LAB lab node(s) labelled devspaces.io/lab=true" || skip "no lab pool: hardware/kernel profiles unavailable (fine for a trial)"

step "Writing detected.env"
cat > "$EX_DIR/detected.env" <<EOT
# written by 00-detect.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ) — re-run to refresh; values here win over env.sh defaults
ISTIO_FLAVOUR="$ISTIO_FLAVOUR"
ISTIO_NS="$ISTIO_NS"
MESH="$MESH"
ISTIO_REV="$ISTIO_REV"
AMBIENT="$AMBIENT"
GW_NS="$GW_NS"
GW_SELECTOR="$GW_SELECTOR"
GW_IP="$GW_IP"
HOME_FAST="$HOME_FAST"
HOME_STD="$HOME_STD"
SHARED="$SHARED"
SNAP_CLASS="$SNAP_CLASS"
LAB_NODES="${LAB:-0}"
EOT
ok "$EX_DIR/detected.env"
cat "$EX_DIR/detected.env" | sed 's/^/    /'
echo; echo "Next:  ./10-namespaces.sh"
