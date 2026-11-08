#!/usr/bin/env bash
# Optional (contract C10): eBPF multicast for --multicast workspaces on AKS.
#   ./deploy/cluster/providers/azure/60-multicast-cilium.sh [--dry-run]
# Azure VNets never forward multicast, so Multus/macvlan cannot work in Azure. Cilium's multicast
# datapath replicates packets over the VXLAN overlay instead — but it needs knobs the MANAGED
# "Azure CNI powered by Cilium" does not expose (multicast-enabled, tunnel mode). Two options:
#   A) BYO (self-managed OSS) Cilium: create the cluster with --network-plugin none and install
#      cilium via Helm with routingMode=tunnel, tunnelProtocol=vxlan, then run the generic script.
#   B) Keep multicast workspaces on an on-prem lab cluster (Multus mode) and everything else on AKS.
# This script detects which situation you are in and either runs the generic enabler or explains.
. "$(dirname "$0")/_lib.sh"
step "Detecting the Cilium flavour"
if [ "$DRY_RUN" = 1 ]; then
  run "$REPO_ROOT/deploy/cluster/multicast-cilium.sh" $MULTICAST_GROUPS; exit 0; fi
dp=$(az aks show -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --query networkProfile.networkDataplane -o tsv 2>/dev/null || echo "")
plugin=$(az aks show -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --query networkProfile.networkPlugin -o tsv 2>/dev/null || echo "")
if [ "$plugin" = none ] && kubectl -n kube-system get cm cilium-config >/dev/null 2>&1; then
  ok "BYO Cilium detected (network plugin none) — running the generic enabler"
  exec "$REPO_ROOT/deploy/cluster/multicast-cilium.sh" $MULTICAST_GROUPS
fi
if [ "$dp" = cilium ]; then
  warn "This cluster uses MANAGED Azure CNI powered by Cilium: multicast-enabled cannot be set (the config map is reconciled by AKS)."
  cat <<MSG
  Options:
   A) Separate BYO-Cilium cluster/pool for multicast work:
        az aks create ... --network-plugin none --pod-cidr $POD_CIDR   (no managed CNI)
        helm repo add cilium https://helm.cilium.io && helm upgrade --install cilium cilium/cilium -n kube-system \\
          --set aksbyocni.enabled=true --set nodeinit.enabled=true --set routingMode=tunnel --set tunnelProtocol=vxlan \\
          --set ipam.operator.clusterPoolIPv4PodCIDRList=$POD_CIDR
        ./deploy/cluster/multicast-cilium.sh $MULTICAST_GROUPS
   B) Keep catalog networking.multicast.mode=cilium but remove the 'mcast' tool and the --multicast
      option from the AKS catalog (catalogOverrides) so developers are not offered what cannot work here.
   C) Run multicast workspaces on-prem (Multus mode) — see docs/production-deployment.md Part F.
MSG
  exit 2
fi
fail "Could not detect Cilium on this cluster (dataplane=$dp plugin=$plugin); multicast needs Cilium in VXLAN tunnel mode."
