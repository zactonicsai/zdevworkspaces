#!/usr/bin/env bash
# Phase 1 on Azure: the building. Resource group, VNet, AKS with Azure CNI (overlay) powered by
# Cilium, OIDC issuer + workload identity, Entra ID sign-in + Azure RBAC, node pools
# system / workspaces / lab (/ spot), ACR attached, kubeconfig.
#   ./deploy/cluster/providers/azure/10-aks.sh [--dry-run]
#   ./deploy/cluster/providers/azure/10-aks.sh --delete      # removes the whole resource group
# Idempotent: every resource is checked before it is created. Re-run after changing env.sh.
# Contract: C1 (version), C2 (Ubuntu 22.04+/containerd 2 → user namespaces), C3 (lab pool label +
# taint), C7 (Entra ID identities = e-mails), C8 (Cilium enforces NetworkPolicy), C9 (ACR/egress).
. "$(dirname "$0")/_lib.sh"

if [ "${1:-}" = --delete ]; then
  read -r -p "Delete resource group $RESOURCE_GROUP and EVERYTHING in it (cluster, disks, vault)? type the group name: " ans
  [ "$ans" = "$RESOURCE_GROUP" ] || exit 1
  run az group delete -n "$RESOURCE_GROUP" --yes --no-wait; echo "deleting in the background"; exit 0
fi

TENANT=$(q tenant-id az account show --query tenantId -o tsv)
step "1/8 Resource group $RESOURCE_GROUP in $LOCATION"
if probe az group show -n "$RESOURCE_GROUP"; then skip "exists"; else
  run az group create -n "$RESOURCE_GROUP" -l "$LOCATION" --tags $(tags) -o none; ok "created"; fi

step "2/8 Network: VNet $VNET_NAME ($VNET_CIDR) with subnets $NODES_SUBNET, $APPGW_SUBNET"
if probe az network vnet show -g "$RESOURCE_GROUP" -n "$VNET_NAME"; then skip "vnet exists"; else
  run az network vnet create -g "$RESOURCE_GROUP" -n "$VNET_NAME" --address-prefixes "$VNET_CIDR" \
      --subnet-name "$NODES_SUBNET" --subnet-prefixes "$NODES_CIDR" --tags $(tags) -o none; ok "vnet + nodes subnet"; fi
if probe az network vnet subnet show -g "$RESOURCE_GROUP" --vnet-name "$VNET_NAME" -n "$APPGW_SUBNET"; then skip "appgw subnet exists"; else
  run az network vnet subnet create -g "$RESOURCE_GROUP" --vnet-name "$VNET_NAME" -n "$APPGW_SUBNET" --address-prefixes "$APPGW_CIDR" \
      --delegations Microsoft.ServiceNetworking/trafficControllers -o none; ok "appgw subnet (delegated to Application Gateway for Containers)"; fi
NODES_SUBNET_ID=$(q subnet-id az network vnet subnet show -g "$RESOURCE_GROUP" --vnet-name "$VNET_NAME" -n "$NODES_SUBNET" --query id -o tsv)

step "3/8 Container registry $ACR_NAME"
if [ -z "${ACR_NAME:-}" ]; then skip "ACR_NAME empty → images come from Artifactory only";
elif probe az acr show -n "$ACR_NAME"; then skip "exists"; else
  run az acr create -g "$RESOURCE_GROUP" -n "$ACR_NAME" --sku Premium --admin-enabled false --tags $(tags) -o none; ok "created (Premium: private link + geo-replication possible)"; fi

step "4/8 AKS cluster $CLUSTER_NAME (Kubernetes $K8S_VERSION, Azure CNI overlay powered by Cilium)"
k8s_patch=$(q "$K8S_VERSION.x" bash -c "az aks get-versions -l '$LOCATION' -o tsv --query 'values[].patchVersions.keys(@)[]' | grep '^$K8S_VERSION' | sort -V | tail -1")
private=""; [ "${PRIVATE_CLUSTER:-false}" = true ] && private="--enable-private-cluster --disable-public-fqdn"
appgw=""; [ "${INGRESS:-}" = appgw-for-containers ] && appgw="--enable-app-routing"   # managed NGINX is a safe default; ALB controller is added in step 8
if probe az aks show -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME"; then skip "cluster exists"; else
  run az aks create -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" -l "$LOCATION" --kubernetes-version "$k8s_patch" --tier standard \
    --network-plugin azure --network-plugin-mode overlay --network-dataplane cilium --network-policy cilium \
    --pod-cidr "$POD_CIDR" --service-cidr "$SERVICE_CIDR" --dns-service-ip "$DNS_SERVICE_IP" --vnet-subnet-id "$NODES_SUBNET_ID" \
    --enable-managed-identity --enable-oidc-issuer --enable-workload-identity \
    --enable-aad --aad-tenant-id "$TENANT" --enable-azure-rbac \
    --nodepool-name system --node-count "$SYSTEM_COUNT" --node-vm-size "$SYSTEM_SIZE" --os-sku AzureLinux \
    --nodepool-labels devspaces.io/pool=system --nodepool-taints CriticalAddonsOnly=true:NoSchedule \
    --enable-addons monitoring --enable-azure-monitor-metrics \
    --auto-upgrade-channel patch --node-os-upgrade-channel NodeImage \
    --generate-ssh-keys --tags $(tags) $private $appgw -o none
  ok "created (10–15 minutes)"
fi
ISSUER=$(q oidc-issuer-url az aks show -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --query oidcIssuerProfile.issuerUrl -o tsv)
ok "OIDC issuer: $ISSUER (used by workload identity in 40-keyvault.sh)"

pool() { # name size min max osdisk labels taints extra…
  local n=$1 size=$2 min=$3 max=$4 disk=$5 labels=$6 taints=$7; shift 7
  if probe az aks nodepool show -g "$RESOURCE_GROUP" --cluster-name "$CLUSTER_NAME" -n "$n"; then skip "pool $n exists"; return; fi
  local t=""; [ -n "$taints" ] && t="--node-taints $taints"
  run az aks nodepool add -g "$RESOURCE_GROUP" --cluster-name "$CLUSTER_NAME" -n "$n" --mode User --os-sku "$NODE_OS_SKU" \
      --node-vm-size "$size" --enable-cluster-autoscaler --min-count "$min" --max-count "$max" --node-count "$min" \
      --node-osdisk-size "$disk" --node-osdisk-type Ephemeral --labels $labels $t --vnet-subnet-id "$NODES_SUBNET_ID" --tags $(tags) "$@" -o none
  ok "pool $n ($size, $min–$max nodes)"
}
step "5/8 Node pool 'workspaces' (standard tier, autoscaled)"
pool workspaces "$WORKSPACES_SIZE" "$WORKSPACES_MIN" "$WORKSPACES_MAX" "$WORKSPACES_OSDISK" "devspaces.io/pool=standard" ""
step "6/8 Node pool 'lab' (hardware tier: tainted devspaces.io/lab=true:NoSchedule, scales to 0)"
gpu=""; [ "${LAB_GPU:-false}" = true ] && gpu="--gpu-driver install"
pool lab "$LAB_SIZE" "$LAB_MIN" "$LAB_MAX" "$LAB_OSDISK" "devspaces.io/pool=lab devspaces.io/lab=true" "devspaces.io/lab=true:NoSchedule" $gpu
if [ "${SPOT_POOL:-false}" = true ]; then
  step "6b/8 Node pool 'spot' (cheap, interruptible — for throw-away workspaces)"
  pool spot "$WORKSPACES_SIZE" 0 "$WORKSPACES_MAX" "$WORKSPACES_OSDISK" "devspaces.io/pool=spot" "" --priority Spot --eviction-policy Delete --spot-max-price -1
fi

step "7/8 Attach ACR + kubeconfig"
if [ -n "${ACR_NAME:-}" ]; then run az aks update -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --attach-acr "$ACR_NAME" -o none; ok "kubelet identity may pull from $ACR_NAME (no pull secret needed)"; fi
run az aks get-credentials -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --overwrite-existing
have kubelogin && ok "kubelogin present (Entra ID sign-in for kubectl)" || warn "install kubelogin: az aks install-cli  (kubectl needs it with --enable-aad)"
# Azure RBAC: whoever runs this becomes cluster admin; developers get RBAC inside the cluster from onboarding.
ME=$(q me-object-id az ad signed-in-user show --query id -o tsv 2>/dev/null || echo "")
AKS_ID=$(q aks-id az aks show -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --query id -o tsv)
if [ -n "$ME" ]; then
  probe az role assignment list --assignee "$ME" --scope "$AKS_ID" --role "Azure Kubernetes Service RBAC Cluster Admin" --query '[0]' -o tsv \
    && skip "you are already AKS RBAC Cluster Admin" \
    || { run az role assignment create --assignee-object-id "$ME" --assignee-principal-type User --role "Azure Kubernetes Service RBAC Cluster Admin" --scope "$AKS_ID" -o none; ok "you: AKS RBAC Cluster Admin"; }
fi
ADMIN_GID=$(q admin-group-id az ad group show -g "$ADMIN_GROUP_NAME" --query id -o tsv 2>/dev/null || echo "")
if [ -n "$ADMIN_GID" ] && [ "$ADMIN_GID" != "<admin-group-id>" ]; then
  run az role assignment create --assignee-object-id "$ADMIN_GID" --assignee-principal-type Group --role "Azure Kubernetes Service RBAC Cluster Admin" --scope "$AKS_ID" -o none || true
  ok "group $ADMIN_GROUP_NAME: AKS RBAC Cluster Admin"
else warn "Entra group $ADMIN_GROUP_NAME not found yet (30-entra.sh creates it); re-run this script afterwards"; fi

step "8/8 Ingress controller"
case "${INGRESS:-nginx}" in
  appgw-for-containers)
    # Application Gateway for Containers = Gateway API implementation managed by Azure (ALB controller in-cluster).
    if probe helm status alb-controller -n azure-alb-system; then skip "ALB controller installed"; else
      run az aks update -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --enable-oidc-issuer --enable-workload-identity -o none
      run az identity create -g "$RESOURCE_GROUP" -n azure-alb-identity -o none || true
      ALB_CLIENT=$(q alb-client-id az identity show -g "$RESOURCE_GROUP" -n azure-alb-identity --query clientId -o tsv)
      ALB_PRINCIPAL=$(q alb-principal-id az identity show -g "$RESOURCE_GROUP" -n azure-alb-identity --query principalId -o tsv)
      NODE_RG=$(q node-rg az aks show -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --query nodeResourceGroup -o tsv)
      run az role assignment create --assignee-object-id "$ALB_PRINCIPAL" --assignee-principal-type ServicePrincipal --scope "/subscriptions/$(q sub az account show --query id -o tsv)/resourceGroups/$NODE_RG" --role "Reader" -o none || true
      run az role assignment create --assignee-object-id "$ALB_PRINCIPAL" --assignee-principal-type ServicePrincipal --scope "/subscriptions/$(q sub az account show --query id -o tsv)/resourceGroups/$RESOURCE_GROUP" --role "AppGw for Containers Configuration Manager" -o none || true
      run az identity federated-credential create -n azure-alb-identity -g "$RESOURCE_GROUP" --identity-name azure-alb-identity --issuer "$ISSUER" --subject system:serviceaccount:azure-alb-system:alb-controller-sa -o none || true
      run helm upgrade --install alb-controller oci://mcr.microsoft.com/application-lb/charts/alb-controller -n azure-alb-system --create-namespace --version 1.3.7 --set albController.namespace=azure-alb-system --set albController.podIdentity.clientID="$ALB_CLIENT"
      ok "ALB controller installed; the ApplicationLoadBalancer + Gateway objects are in docs/azure-aks-setup.md Phase 6"
    fi;;
  nginx) skip "NGINX ingress + cert-manager come from deploy/cluster/addons.sh (INSTALL_INGRESS_NGINX=true)";;
esac

cat <<EOF

AKS is up. What you have:
  cluster   $CLUSTER_NAME ($k8s_patch) in $RESOURCE_GROUP, Cilium dataplane → NetworkPolicy works (C8)
  pools     system (AzureLinux, tainted), workspaces ($WORKSPACES_MIN–$WORKSPACES_MAX × $WORKSPACES_SIZE), lab ($LAB_MIN–$LAB_MAX × $LAB_SIZE, tainted)
  identity  Entra ID sign-in for kubectl (kubelogin), OIDC issuer for workload identity
  registry  ${ACR_NAME:-"(Artifactory only)"}
Next:  ./deploy/cluster/providers/azure/20-storage.sh
EOF
