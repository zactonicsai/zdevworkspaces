#!/usr/bin/env bash
# Phase 0 on Azure: is this machine and this subscription ready? Changes nothing.
#   ./deploy/cluster/providers/azure/00-prereqs.sh [--dry-run]
. "$(dirname "$0")/_lib.sh"

step "Tools"
for t in az kubectl helm jq; do
  if have "$t"; then ok "$t: $($t version 2>/dev/null | head -1 | cut -c1-60 || $t --version 2>/dev/null | head -1)"; else
    case $t in jq) warn "jq missing (optional; brew install jq / apt install jq)";; *) fail "$t missing — see docs/azure-aks-setup.md Phase 0";; esac; fi
done
azv=$(q 2.70.0 az version --query '"azure-cli"' -o tsv 2>/dev/null || echo 0)
ok "az CLI $azv (need >= 2.61 for --network-dataplane cilium, Application Gateway for Containers, AKS 1.3x)"
have az && [ "$DRY_RUN" = 0 ] && { az extension show -n aks-preview >/dev/null 2>&1 && ok "aks-preview extension present" || warn "aks-preview extension not installed (only needed for preview features; 'az extension add -n aks-preview')"; }

step "Login and subscription"
if [ "$DRY_RUN" = 1 ]; then skip "az account show (dry-run)"; else
  az account show >/dev/null 2>&1 || fail "not logged in: az login --tenant <tenant>"
  [ -n "${SUBSCRIPTION:-}" ] && az account set --subscription "$SUBSCRIPTION"
  ok "subscription: $(az account show --query '[name,id]' -o tsv | tr '\n' ' ')"
  ok "tenant: $(az account show --query tenantId -o tsv)"
  ok "signed in as: $(az ad signed-in-user show --query userPrincipalName -o tsv 2>/dev/null || echo '(service principal)')"
fi

step "Resource providers (one-time per subscription; registration takes a few minutes)"
for p in Microsoft.ContainerService Microsoft.Network Microsoft.Compute Microsoft.Storage Microsoft.ContainerRegistry \
         Microsoft.KeyVault Microsoft.ManagedIdentity Microsoft.ApiManagement Microsoft.ServiceNetworking Microsoft.OperationalInsights Microsoft.Insights; do
  st=$(q Registered az provider show -n "$p" --query registrationState -o tsv 2>/dev/null || echo Unknown)
  if [ "$st" = Registered ]; then ok "$p"; else warn "$p is $st"; run az provider register -n "$p" --wait; fi
done
# AKS features worth having on
for f in Microsoft.ContainerService/EnableAPIServerVnetIntegrationPreview; do :; done   # none required at GA for this design

step "Quota in $LOCATION (vCPUs for the sizes in env.sh)"
if [ "$DRY_RUN" = 1 ]; then skip "az vm list-usage (dry-run)"; else
  az vm list-usage -l "$LOCATION" -o tsv --query "[?contains(name.value, 'Dsv5') || contains(name.value, 'Family')].[name.localizedValue,currentValue,limit]" | head -12
  echo "  Need roughly: system $SYSTEM_COUNT×$SYSTEM_SIZE + workspaces $WORKSPACES_MAX×$WORKSPACES_SIZE + lab $LAB_MAX×$LAB_SIZE vCPUs at full scale."
  echo "  Raise with: az quota create … or Portal → Quotas (takes minutes to days)."
fi

step "Names that must be globally unique"
if [ -n "${ACR_NAME:-}" ]; then
  r=$(q true az acr check-name -n "$ACR_NAME" --query nameAvailable -o tsv 2>/dev/null || echo false)
  if [ "$r" = true ]; then ok "ACR name '$ACR_NAME' available"; else
    probe az acr show -n "$ACR_NAME" && ok "ACR '$ACR_NAME' already yours" || warn "ACR name '$ACR_NAME' taken by someone else — pick another"; fi
fi
kv=$(q true az keyvault check-name -n "$KEYVAULT_NAME" --query nameAvailable -o tsv 2>/dev/null || echo false)
[ "$kv" = true ] && ok "Key Vault name '$KEYVAULT_NAME' available" || { probe az keyvault show -n "$KEYVAULT_NAME" && ok "Key Vault '$KEYVAULT_NAME' already yours" || warn "Key Vault name '$KEYVAULT_NAME' unavailable (also check soft-deleted vaults: az keyvault list-deleted)"; }
ok "APIM name '$APIM_NAME' — checked at deploy time (az apim check-name)"

step "Kubernetes version"
if [ "$DRY_RUN" = 1 ]; then skip "az aks get-versions (dry-run)"; else
  if az aks get-versions -l "$LOCATION" -o tsv --query "values[].patchVersions.keys(@)[]" | grep -q "^${K8S_VERSION}"; then
    ok "AKS offers $K8S_VERSION.x in $LOCATION ($(az aks get-versions -l "$LOCATION" -o tsv --query "values[].patchVersions.keys(@)[]" | grep "^${K8S_VERSION}" | sort -V | tail -1) latest)"
  else fail "AKS in $LOCATION does not offer $K8S_VERSION (contract C1 needs >= 1.36): az aks get-versions -l $LOCATION -o table"; fi
fi

step "Repository"
for f in api/openapi.yaml charts/devspaces/Chart.yaml deploy/cluster/CONTRACT.md deploy/cluster/check-contract.sh; do
  [ -f "$REPO_ROOT/$f" ] && ok "$f" || fail "$f missing — run from a full checkout"
done
echo; echo "All good. Next: ./deploy/cluster/providers/azure/10-aks.sh"
