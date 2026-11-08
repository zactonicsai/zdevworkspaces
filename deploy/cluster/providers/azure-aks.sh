#!/usr/bin/env bash
# Layer 1 on Azure: an AKS cluster that keeps deploy/cluster/CONTRACT.md.
# Needs: az CLI (logged in), kubectl, helm. Reads deploy/cluster/env.sh.
#   ./deploy/cluster/providers/azure-aks.sh | --delete
# Notes: Azure CNI "powered by Cilium" enforces NetworkPolicy (C8). Azure Linux / Ubuntu 24.04
# node images carry 6.x kernels and containerd 2.x on AKS 1.36 (C2). Identity: Entra ID
# integration gives e-mail (UPN) usernames (C7). Multicast: VNets do not forward it; use
# mode "cilium" with BYO OSS Cilium, or keep multicast workspaces on-prem.
set -Eeuo pipefail
cd "$(dirname "$0")/../../.."; . deploy/cluster/env.sh
for t in az kubectl helm; do command -v $t >/dev/null || { echo "missing $t"; exit 1; }; done
RG=${RESOURCE_GROUP:-${CLUSTER_NAME}-rg}
if [ "${1:-}" = --delete ]; then az group delete -n "$RG" --yes --no-wait; exit 0; fi
az group create -n "$RG" -l "$REGION" >/dev/null
if ! az aks show -g "$RG" -n "$CLUSTER_NAME" >/dev/null 2>&1; then
  az aks create -g "$RG" -n "$CLUSTER_NAME" -l "$REGION" --kubernetes-version "$K8S_VERSION" \
    --network-plugin azure --network-plugin-mode overlay --network-dataplane cilium --network-policy cilium \
    --enable-aad --aad-tenant-id "$(az account show --query tenantId -o tsv)" --enable-azure-rbac \
    --enable-managed-identity --enable-oidc-issuer --enable-workload-identity --enable-app-routing \
    --nodepool-name system --node-count 2 --node-vm-size "$SYSTEM_INSTANCE" --os-sku AzureLinux \
    --nodepool-labels devspaces.io/pool=system --nodepool-taints CriticalAddonsOnly=true:NoSchedule \
    --generate-ssh-keys
fi
az aks nodepool show -g "$RG" --cluster-name "$CLUSTER_NAME" -n standard >/dev/null 2>&1 || \
az aks nodepool add -g "$RG" --cluster-name "$CLUSTER_NAME" -n standard --node-vm-size "$STANDARD_INSTANCE" --os-sku Ubuntu \
  --enable-cluster-autoscaler --min-count "$STANDARD_MIN" --max-count "$STANDARD_MAX" --node-count "$STANDARD_MIN" --node-osdisk-size 128 \
  --labels devspaces.io/pool=standard --mode User
az aks nodepool show -g "$RG" --cluster-name "$CLUSTER_NAME" -n lab >/dev/null 2>&1 || \
az aks nodepool add -g "$RG" --cluster-name "$CLUSTER_NAME" -n lab --node-vm-size "$LAB_INSTANCE" --os-sku Ubuntu \
  --enable-cluster-autoscaler --min-count "$LAB_MIN" --max-count "$LAB_MAX" --node-count "$LAB_MIN" --node-osdisk-size 256 \
  --labels devspaces.io/pool=lab devspaces.io/lab=true --node-taints devspaces.io/lab=true:NoSchedule --mode User
az aks get-credentials -g "$RG" -n "$CLUSTER_NAME" --overwrite-existing
# C4: managed-csi-premium default with expansion (AKS built-in classes allow expansion)
kubectl annotate storageclass default storageclass.kubernetes.io/is-default-class- 2>/dev/null || true
kubectl annotate storageclass managed-csi-premium storageclass.kubernetes.io/is-default-class=true --overwrite
echo; echo "AKS ready. Next: deploy/cluster/addons.sh, then deploy/cluster/check-contract.sh"
