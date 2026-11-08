#!/usr/bin/env bash
# Phase 90 — UNINSTALL. Take it down in reverse, keeping people's disks unless told otherwise.
#
# WHAT   route (40) → Helm release (30) → developer and project namespaces (50/60) →
#        namespace SYSTEM_NS (10). StorageClass/VolumeSnapshotClass from 20 are left alone
#        (they are harmless and other things may use them). Home drives are Retain, so their
#        PersistentVolumes survive the namespace; --purge deletes those PVs too.
#   ./90-uninstall.sh [--dry-run] [--purge] [--yes]
. "$(dirname "$0")/_lib.sh" "$@"
PURGE=0; YES=0; for _a in "$@"; do case "$_a" in --purge) PURGE=1;; --yes|-y) YES=1;; esac; done
tenants=$(kq get ns -l devspaces.io/tenant -o jsonpath='{.items[*].metadata.name}')
echo "This removes: route, Helm release devspaces, namespaces: $SYSTEM_NS $tenants"
[ "$PURGE" = 1 ] && echo "and PURGES every retained home-drive PersistentVolume (data gone for good)."
if [ "$YES" != 1 ] && [ "$DRY_RUN" != 1 ]; then printf 'Type yes to continue: '; read -r a; [ "$a" = yes ] || exit 1; fi

step "Route";          [ -f "$PH_DIR/route.yaml" ] && run k delete -f "$PH_DIR/route.yaml" --ignore-not-found || skip "none"
step "Helm release";   run h -n "$SYSTEM_NS" uninstall devspaces --ignore-not-found 2>/dev/null || skip "none"
step "Developers";     for ns in $tenants; do
  # PVCs are protected by the admission policy; the policy goes with the release, but mark them anyway
  for pvc in $(kq -n "$ns" get pvc -l devspaces.io/drive=home -o name); do run k -n "$ns" annotate "$pvc" devspaces.io/allow-delete=true --overwrite >/dev/null; done
  run k delete ns "$ns" --wait=false; ok "$ns (home-drive PVs stay Released)"; done
step "Namespace";      run k delete ns "$SYSTEM_NS" --ignore-not-found --wait=false
if [ "$PURGE" = 1 ]; then step "Purging retained drives"; for pv in $(kq get pv -o json | python3 -c 'import json,sys;print(" ".join(p["metadata"]["name"] for p in json.load(sys.stdin)["items"] if (p["spec"].get("claimRef") or {}).get("namespace","").startswith("ws-")))'); do run k delete pv "$pv"; done; fi
echo; echo "Left in place: StorageClass/VolumeSnapshotClass from 20-storage.sh$([ "$PURGE" = 1 ] || echo ", Released PVs of home drives (kubectl get pv | grep Released)")."
