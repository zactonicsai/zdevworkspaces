#!/usr/bin/env bash
# 1/5 — Create the platform namespace (mesh-enrolled), a VolumeSnapshotClass if missing, and
# — when the cluster only has Delete-policy disk classes — the Retain/expandable
# workspace-home-* classes from ../20-storage.yaml. Idempotent.
#   ./10-namespaces.sh [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"; need_detect

mesh_label() {   # label a namespace for the detected mesh mode
  case "$MESH" in
    istio-ambient) run k label ns "$1" istio.io/dataplane-mode=ambient --overwrite ;;
    istio-sidecar) if [ -n "$ISTIO_REV" ]; then run k label ns "$1" istio.io/rev="$ISTIO_REV" istio-injection- --overwrite
                   else run k label ns "$1" istio-injection=enabled --overwrite; fi ;;
    *) : ;;
  esac
}

step "Platform namespace $SYSTEM_NS"
if [ -n "$(kq get ns "$SYSTEM_NS" -o name)" ]; then skip "exists"; else run k create namespace "$SYSTEM_NS"; fi
run k label ns "$SYSTEM_NS" app.kubernetes.io/part-of=devspaces pod-security.kubernetes.io/enforce=restricted --overwrite
mesh_label "$SYSTEM_NS"
ok "$SYSTEM_NS ready (mesh: $MESH)"

step "Storage classes"
if [ -n "$(kq get storageclass workspace-home-premium -o name)" ]; then
  skip "workspace-home-* classes already exist (from ../20-storage.sh)"
else
  echo "  The cluster's built-in classes ($HOME_FAST / $HOME_STD) delete the disk when a PVC goes;"
  echo "  the platform's own classes keep it (reclaimPolicy Retain) so a purged drive can still be recovered."
  if [ "${CREATE_STORAGE_CLASSES:-true}" = true ]; then
    run k apply -f "$EX_DIR/../20-storage.yaml"
    HOME_FAST=workspace-home-premium; HOME_STD=workspace-home-standard
    [ -n "$(kq get storageclass workspace-shared-files -o name)" ] && SHARED=workspace-shared-files || true
    ok "applied ../20-storage.yaml → using workspace-home-premium / workspace-home-standard"
    sed -i.bak -e "s/^HOME_FAST=.*/HOME_FAST=\"$HOME_FAST\"/" -e "s/^HOME_STD=.*/HOME_STD=\"$HOME_STD\"/" -e "s/^SHARED=.*/SHARED=\"${SHARED}\"/" "$EX_DIR/detected.env" 2>/dev/null || true; rm -f "$EX_DIR/detected.env.bak"
  else
    warn "CREATE_STORAGE_CLASSES=false: keeping $HOME_FAST / $HOME_STD (Delete policy)"
  fi
fi
if [ -z "$SNAP_CLASS" ] && { [ -n "$(kq get crd volumesnapshotclasses.snapshot.storage.k8s.io -o name)" ] || [ "$DRY_RUN" = 1 ]; }; then
  run k apply -f - <<'YAML'
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotClass
metadata:
  name: csi-azuredisk-vsc
driver: disk.csi.azure.com
deletionPolicy: Delete
parameters:
  incremental: "true"
YAML
  SNAP_CLASS=csi-azuredisk-vsc
  sed -i.bak "s/^SNAP_CLASS=.*/SNAP_CLASS=\"$SNAP_CLASS\"/" "$EX_DIR/detected.env" 2>/dev/null || true; rm -f "$EX_DIR/detected.env.bak"
  ok "VolumeSnapshotClass csi-azuredisk-vsc (incremental Azure Disk snapshots)"
else
  skip "snapshot class: ${SNAP_CLASS:-unavailable}"
fi

step "Istio for the portal namespace"
case "$MESH" in
  istio-ambient) ok "ambient: the portal pods are in the mesh with no sidecar; PeerAuthentication STRICT is applied by the chart (istioAuth.strictMTLS)";;
  istio-sidecar) ok "sidecar: portal and oauth2-proxy pods get an istio-proxy on their next start";;
  none)          skip "no mesh";;
esac
echo; echo "Next:  ./20-helm.sh"
