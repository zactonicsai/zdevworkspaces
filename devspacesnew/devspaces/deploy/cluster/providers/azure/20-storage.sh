#!/usr/bin/env bash
# Phase 1b on Azure: storage classes for home drives + shared drives, snapshot class (contract C4).
#   ./deploy/cluster/providers/azure/20-storage.sh [--dry-run]
. "$(dirname "$0")/_lib.sh"

step "1/4 CSI drivers (built into AKS)"
if [ "$DRY_RUN" = 1 ]; then skip "kubectl checks (dry-run)"; else
  kubectl get csidriver disk.csi.azure.com >/dev/null 2>&1 && ok "disk.csi.azure.com" || fail "Azure Disk CSI driver missing: az aks update -g $RESOURCE_GROUP -n $CLUSTER_NAME --enable-disk-driver"
  kubectl get csidriver file.csi.azure.com >/dev/null 2>&1 && ok "file.csi.azure.com" || fail "Azure Files CSI driver missing: az aks update -g $RESOURCE_GROUP -n $CLUSTER_NAME --enable-file-driver"
fi

step "2/4 Snapshot controller + CRDs"
if [ "$DRY_RUN" = 1 ]; then skip "kubectl checks (dry-run)"; else
  if kubectl get crd volumesnapshotclasses.snapshot.storage.k8s.io >/dev/null 2>&1; then ok "snapshot CRDs present"; else
    warn "snapshot CRDs missing → enabling the AKS snapshot controller"
    run az aks update -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --enable-snapshot-controller -o none
    kubectl wait --for=condition=Established crd/volumesnapshots.snapshot.storage.k8s.io --timeout=120s
  fi
  kubectl -n kube-system get deploy -l app=snapshot-controller --no-headers 2>/dev/null | grep -q . && ok "snapshot-controller running" || warn "snapshot-controller deployment not found in kube-system"
fi

step "3/4 Make the built-in 'default' class no longer default"
if [ "$DRY_RUN" = 1 ]; then run kubectl annotate storageclass default storageclass.kubernetes.io/is-default-class-; else
  for sc in $(kubectl get storageclass -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{"\n"}{end}'); do
    [ "$sc" = workspace-home-premium ] || { kubectl annotate storageclass "$sc" storageclass.kubernetes.io/is-default-class- --overwrite >/dev/null; ok "$sc is no longer default"; }
  done
fi

step "4/4 Apply StorageClasses + VolumeSnapshotClass"
run kubectl apply -f "$AZ_DIR/20-storage.yaml"
if [ "$DRY_RUN" = 0 ]; then
  kubectl get storageclass workspace-home-premium workspace-home-standard workspace-shared-files \
    -o custom-columns='NAME:.metadata.name,PROVISIONER:.provisioner,RECLAIM:.reclaimPolicy,EXPAND:.allowVolumeExpansion'
  kubectl get volumesnapshotclass csi-azuredisk-vsc
fi
cat <<MSG

Storage ready. Catalog names (charts/devspaces/examples/values-azure.yaml → storage.choices):
  fast     → workspace-home-premium  (default)      standard → workspace-home-standard
  shared   → workspace-shared-files  (RWX)          snapshots → csi-azuredisk-vsc
Next:  ./deploy/cluster/providers/azure/30-entra.sh
MSG
