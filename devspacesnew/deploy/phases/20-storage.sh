#!/usr/bin/env bash
# Phase 20 — STORAGE. Make sure home drives survive.
#
# WHAT   Gives the platform a StorageClass whose disks are KEPT when a claim is deleted
#        (reclaimPolicy Retain, expansion allowed). If env.sh names one (HOME_STORAGE_CLASS)
#        nothing is created. Otherwise the cluster's default class is cloned as
#        "workspace-home": same provisioner and parameters, different policy. Also makes a
#        VolumeSnapshotClass for that CSI driver when the snapshot CRDs exist and no class does.
# WHY    Most clusters' default class deletes the disk with the PVC. A developer's home drive
#        must outlive the pod, the StatefulSet and even an accidental PVC delete.
# NEEDS  detected.env (DEFAULT_SC, PROVISIONER, SNAP_CRD, SNAP_CLASS).
# MAKES  StorageClass workspace-home (maybe), VolumeSnapshotClass workspace-snapshots (maybe);
#        records the final names in detected.env (HOME_SC, SNAP_CLASS) for 30-platform.sh.
# SKIP   if your cluster already has Retain classes: set HOME_STORAGE_CLASS/SNAPSHOT_CLASS in env.sh.
#   ./20-storage.sh [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"; need_detect
record() { touch "$PH_DIR/detected.env"; python3 - "$PH_DIR/detected.env" "$1" "$2" <<'PY'
import sys,re; p,k,v=sys.argv[1:]; s=open(p).read()
s=re.sub(r'(?m)^%s=.*\n'%re.escape(k),'',s)+'%s="%s"\n'%(k,v); open(p,"w").write(s)
PY
}

step "Home-drive StorageClass"
if [ -n "${HOME_STORAGE_CLASS:-}" ]; then
  pol=$(kq get storageclass "$HOME_STORAGE_CLASS" -o jsonpath='{.reclaimPolicy}')
  [ "$pol" = Delete ] && warn "$HOME_STORAGE_CLASS has reclaimPolicy Delete: a deleted PVC takes the disk with it (the admission policy still blocks accidental deletes)"
  HOME_SC=$HOME_STORAGE_CLASS; ok "using $HOME_SC from env.sh"
elif [ -n "$(kq get storageclass workspace-home -o name)" ]; then
  HOME_SC=workspace-home; skip "workspace-home exists"
elif [ -n "$DEFAULT_SC" ] || [ "$DRY_RUN" = 1 ]; then
  if [ "$DRY_RUN" = 1 ]; then DEFAULT_SC=${DEFAULT_SC:-standard}; PROVISIONER=${PROVISIONER:-example.csi.k8s.io}; fi
  # clone: keep provisioner, parameters, binding mode, mount options; change the policy
  if [ "$DRY_RUN" = 1 ]; then
    printf '  would create StorageClass workspace-home: provisioner %s (copied from %s), reclaimPolicy Retain, allowVolumeExpansion true\n' "$PROVISIONER" "$DEFAULT_SC"
  else
    k get storageclass "$DEFAULT_SC" -o json | python3 -c '
import json,sys
s=json.load(sys.stdin)
out={"apiVersion":"storage.k8s.io/v1","kind":"StorageClass",
     "metadata":{"name":"workspace-home","labels":{"app.kubernetes.io/part-of":"devspaces"},
                 "annotations":{"devspaces.io/cloned-from":s["metadata"]["name"]}},
     "provisioner":s["provisioner"],"reclaimPolicy":"Retain","allowVolumeExpansion":True,
     "volumeBindingMode":s.get("volumeBindingMode","WaitForFirstConsumer")}
for key in ("parameters","mountOptions","allowedTopologies"):
    if key in s: out[key]=s[key]
json.dump(out,sys.stdout)' | k apply -f - >/dev/null
  fi
  HOME_SC=workspace-home; ok "workspace-home = $DEFAULT_SC + reclaimPolicy Retain + allowVolumeExpansion"
else
  fail "no default StorageClass to clone — set HOME_STORAGE_CLASS in env.sh"
fi
record HOME_SC "$HOME_SC"

step "Shared folders (ReadWriteMany)"
if [ -n "${SHARED_STORAGE_CLASS:-}" ]; then
  [ -n "$(kq get storageclass "$SHARED_STORAGE_CLASS" -o name)" ] || [ "$DRY_RUN" = 1 ] || fail "StorageClass $SHARED_STORAGE_CLASS not found"
  ok "$SHARED_STORAGE_CLASS"; record SHARED_SC "$SHARED_STORAGE_CLASS"
else skip "none configured (SHARED_STORAGE_CLASS) — team folders are off, everything else works"; record SHARED_SC ""; fi

step "Snapshots"
case "$PROVISIONER" in rancher.io/local-path|kubernetes.io/*|"") NO_CSI=1;; *) NO_CSI=0;; esac
if [ "$NO_CSI" = 1 ] && [ -z "$SNAP_CLASS" ] && [ "$DRY_RUN" != 1 ]; then
  skip "provisioner '${PROVISIONER:-?}' is not a CSI driver — no snapshot class (kind/local-path: expected; 'drives snapshot' stays off)"; SNAP_CLASS=""
elif [ "$SNAP_CRD" = true ] || [ "$DRY_RUN" = 1 ]; then
  if [ -n "$SNAP_CLASS" ]; then ok "VolumeSnapshotClass $SNAP_CLASS"
  else
    run k apply -f - <<EOT
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotClass
metadata:
  name: workspace-snapshots
  labels: { app.kubernetes.io/part-of: devspaces }
driver: ${PROVISIONER:-example.csi.k8s.io}
deletionPolicy: Delete
EOT
    SNAP_CLASS=workspace-snapshots; ok "VolumeSnapshotClass workspace-snapshots for driver $PROVISIONER (the driver must run the csi-snapshotter sidecar — 70-verify.sh checks)"
  fi
else skip "no snapshot CRDs: 'drives snapshot' stays disabled"; SNAP_CLASS=""; fi
record SNAP_CLASS "$SNAP_CLASS"
echo; echo "Next:  ./30-platform.sh"
