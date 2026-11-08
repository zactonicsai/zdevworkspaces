#!/usr/bin/env bash
# Phase 00 — CHECK. Look, don't touch.
#
# WHAT   Confirms the tools and the cluster, then looks at what the cluster already offers:
#        default StorageClass (and its CSI driver), snapshot support, a service mesh, an
#        IngressClass or an Istio ingress gateway. Writes the answers to detected.env.
# WHY    Every later phase reads detected.env instead of guessing, so the same scripts work on
#        kind, k3s, EKS, GKE, AKS, OpenShift or bare metal — anything with kubectl + helm.
# NEEDS  env.sh (KUBE_CONTEXT may be empty). Read-only on the cluster.
# MAKES  detected.env
# RE-RUN any time; it only rewrites detected.env. Override any value by setting it in env.sh.
#   ./00-check.sh [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"

step "Tools"
for t in kubectl helm python3; do have "$t" && ok "$t $(case $t in kubectl) kubectl version --client -o json 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin)["clientVersion"]["gitVersion"])' 2>/dev/null;; helm) helm version --short 2>/dev/null;; python3) python3 --version 2>&1 | cut -d' ' -f2;; esac)" || { [ "$DRY_RUN" = 1 ] && warn "$t not found (dry-run continues)" || fail "$t not found — install it and run again"; }; done
have ssh-keygen && ok "ssh-keygen (needed only by 60-workspaces.sh to create dev pods from here)" || warn "ssh-keygen missing: 60-workspaces.sh will skip sample dev pods (the portal still works)"

step "Cluster (context ${KCTX:-current})"
if [ "$DRY_RUN" = 1 ]; then skip "dry-run: not contacting a cluster"; SERVER=https://example:6443; K8S=v1.31.0; else
  SERVER=$(kubectl config view --minify $KCTX_FLAG -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)
  K8S=$(kq version -o json | python3 -c 'import json,sys;print(json.load(sys.stdin)["serverVersion"]["gitVersion"])' 2>/dev/null || true)
  [ -n "$K8S" ] || fail "cannot reach the cluster with context '$KCTX' (kubectl config get-contexts)"
  ok "$SERVER — Kubernetes $K8S"
  minor=$(printf '%s' "$K8S" | sed 's/^v1\.\([0-9]*\).*/\1/'); [ "${minor:-0}" -ge 30 ] && ok "≥ 1.30: ValidatingAdmissionPolicy available (home-drive protection)" || warn "Kubernetes < 1.30: set admission.enabled=false in 30-platform.sh's values, drives are then protected by RBAC only"
  kq auth can-i create namespaces | grep -q yes && ok "you may create namespaces" || warn "you cannot create namespaces — the later phases need cluster-admin (or pre-created namespaces)"
fi

step "Storage"
DEFAULT_SC=""; PROVISIONER=""; SNAP_CLASS=${SNAPSHOT_CLASS:-}; SNAP_CRD=false
if [ "$DRY_RUN" != 1 ]; then
  DEFAULT_SC=$(kq get storageclass -o json | python3 -c '
import json,sys
for s in json.load(sys.stdin)["items"]:
    a=s["metadata"].get("annotations",{})
    if a.get("storageclass.kubernetes.io/is-default-class")=="true" or a.get("storageclass.beta.kubernetes.io/is-default-class")=="true": print(s["metadata"]["name"]); break' 2>/dev/null)
  [ -n "$DEFAULT_SC" ] && PROVISIONER=$(kq get storageclass "$DEFAULT_SC" -o jsonpath='{.provisioner}')
  [ -n "$(kq get crd volumesnapshotclasses.snapshot.storage.k8s.io -o name)" ] && SNAP_CRD=true
  [ -z "$SNAP_CLASS" ] && [ "$SNAP_CRD" = true ] && SNAP_CLASS=$(kq get volumesnapshotclass -o jsonpath='{.items[0].metadata.name}')
fi
if [ -n "${HOME_STORAGE_CLASS:-}" ]; then ok "home drives: $HOME_STORAGE_CLASS (from env.sh)"
elif [ -n "$DEFAULT_SC" ]; then ok "default StorageClass: $DEFAULT_SC (provisioner $PROVISIONER) — 20-storage.sh will clone it as 'workspace-home' with reclaimPolicy Retain"
elif [ "$DRY_RUN" = 1 ]; then skip "dry-run"
else warn "no default StorageClass: set HOME_STORAGE_CLASS in env.sh (kubectl get storageclass)"; fi
[ -n "${SHARED_STORAGE_CLASS:-}" ] && ok "shared folders (RWX): $SHARED_STORAGE_CLASS" || skip "no SHARED_STORAGE_CLASS: the 'shared folder' drive type will not be offered (set an RWX class to enable)"
if [ "$SNAP_CRD" = true ]; then [ -n "$SNAP_CLASS" ] && ok "snapshots: VolumeSnapshotClass $SNAP_CLASS" || ok "snapshot CRDs present, no class yet — 20-storage.sh creates one for $PROVISIONER"
else skip "no VolumeSnapshot CRDs — 'drives snapshot' will be unavailable (install external-snapshotter to enable)"; fi

step "Service mesh"
MESH=none; ISTIO_REV=""
if [ "$DRY_RUN" != 1 ]; then
  if [ -n "$(kq get crd virtualservices.networking.istio.io -o name)" ]; then
    ISTIO_REV=$(kq get mutatingwebhookconfiguration -l app=sidecar-injector -o jsonpath='{.items[0].metadata.labels.istio\.io/rev}')
    [ -n "$(kq get daemonset -A -l app=ztunnel -o name)" ] && MESH=istio-ambient || MESH=istio-sidecar
    [ "$ISTIO_REV" = default ] && ISTIO_REV=""
  fi
fi
case "$MESH" in
  none) skip "no Istio — plain NetworkPolicy isolation (works everywhere)";;
  *)    ok "Istio detected: tenant namespaces get mesh mode $MESH${ISTIO_REV:+ (revision $ISTIO_REV)}";;
esac

step "A way in (for 40-route.sh, EXPOSE=$EXPOSE)"
INGRESS_CLASS_DETECTED=""; GW_NS=""; GW_SELECTOR=""; GW_IP=""
if [ "$DRY_RUN" != 1 ]; then
  INGRESS_CLASS_DETECTED=$(kq get ingressclass -o json | python3 -c '
import json,sys
items=json.load(sys.stdin)["items"]
d=[i for i in items if i["metadata"].get("annotations",{}).get("ingressclass.kubernetes.io/is-default-class")=="true"]
print((d or items or [{"metadata":{"name":""}}])[0]["metadata"]["name"])' 2>/dev/null)
  gw=$(kq get svc -A -l 'istio in (ingressgateway, aks-istio-ingressgateway-external)' -o json | python3 -c '
import json,sys
for s in json.load(sys.stdin)["items"]:
    if s["spec"].get("type")=="LoadBalancer":
        ing=(s["status"].get("loadBalancer",{}).get("ingress") or [{}])[0]
        print(s["metadata"]["namespace"], "istio: "+s["spec"]["selector"].get("istio",""), ing.get("ip") or ing.get("hostname") or ""); break' 2>/dev/null)
  if [ -n "$gw" ]; then GW_NS=$(printf '%s' "$gw" | cut -d' ' -f1); GW_SELECTOR=$(printf '%s' "$gw" | cut -d' ' -f2-3); GW_IP=$(printf '%s' "$gw" | cut -d' ' -f4-); fi
fi
[ -n "$INGRESS_CLASS_DETECTED" ] && ok "IngressClass: $INGRESS_CLASS_DETECTED${INGRESS_CLASS:+ (env.sh says $INGRESS_CLASS)}" || skip "no IngressClass (EXPOSE=ingress needs one: nginx, traefik, …)"
[ -n "$GW_SELECTOR" ] && ok "Istio ingress gateway: $GW_NS ($GW_SELECTOR) → ${GW_IP:-<pending>}" || skip "no Istio ingress gateway (EXPOSE=istio needs one)"
case "$EXPOSE" in
  port-forward) ok "EXPOSE=port-forward: nothing public; fine for AUTH=dev";;
  ingress) [ -n "${INGRESS_CLASS:-$INGRESS_CLASS_DETECTED}" ] || [ "$DRY_RUN" = 1 ] || warn "EXPOSE=ingress but no IngressClass found";;
  istio)   [ -n "$GW_SELECTOR" ] || [ "$DRY_RUN" = 1 ] || warn "EXPOSE=istio but no Istio ingress gateway found";;
  *) fail "EXPOSE must be port-forward, ingress or istio";;
esac
[ "$AUTH" = dev ] && [ "$EXPOSE" != port-forward ] && [ -z "$ALLOW_CIDRS" ] && warn "AUTH=dev with a public route: 40-route.sh will refuse unless ALLOW_CIDRS is set"

step "Writing detected.env"
cat > "$PH_DIR/detected.env" <<EOT
# written by 00-check.sh on $(date -u +%Y-%m-%dT%H:%MZ) — values env.sh leaves empty are filled from here
K8S_VERSION="$K8S"
DEFAULT_SC="$DEFAULT_SC"
PROVISIONER="$PROVISIONER"
SNAP_CRD="$SNAP_CRD"
SNAP_CLASS="${SNAP_CLASS}"
MESH="$MESH"
ISTIO_REV="$ISTIO_REV"
INGRESS_CLASS_DETECTED="$INGRESS_CLASS_DETECTED"
GW_NS="$GW_NS"
GW_SELECTOR="$GW_SELECTOR"
GW_IP="$GW_IP"
EOT
ok "$PH_DIR/detected.env"; echo; echo "Next:  ./10-namespace.sh"
