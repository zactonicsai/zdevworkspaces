#!/usr/bin/env bash
# Phase 2b on Azure: secrets in Key Vault, delivered to pods by the Secrets Store CSI driver
# through workload identity (no secret values in Helm values or in git).
#   Key Vault $KEYVAULT_NAME  ← oauth2-client-secret, oauth2-cookie-secret (+ artifactory-token if given)
#   managed identity $PLATFORM_IDENTITY + federated credential for devspaces-system/devspaces-secrets
#   SecretProviderClass that materialises Kubernetes Secret `oauth2-proxy` (keys client-id, client-secret, cookie-secret)
#   ./deploy/cluster/providers/azure/40-keyvault.sh [--dry-run]
. "$(dirname "$0")/_lib.sh"
TENANT=${TENANT_ID:-$(q tenant-id az account show --query tenantId -o tsv)}
SUB=$(q subscription-id az account show --query id -o tsv)

step "1/6 Key Vault $KEYVAULT_NAME (RBAC mode, soft-delete + purge protection)"
if probe az keyvault show -n "$KEYVAULT_NAME"; then skip "exists"; else
  run az keyvault create -g "$RESOURCE_GROUP" -n "$KEYVAULT_NAME" -l "$LOCATION" --enable-rbac-authorization true \
      --enable-purge-protection true --retention-days 90 --tags $(tags) -o none; ok "created"; fi
KV_ID=$(q kv-id az keyvault show -n "$KEYVAULT_NAME" --query id -o tsv)
ME=$(q me-object-id az ad signed-in-user show --query id -o tsv 2>/dev/null || echo "")
[ -n "$ME" ] && { run az role assignment create --assignee-object-id "$ME" --assignee-principal-type User --role "Key Vault Secrets Officer" --scope "$KV_ID" -o none || true; }

step "2/6 Secrets"
PORTAL_APP_ID=$(q portal-app-id az ad app list --display-name "$ENTRA_PORTAL_APP" --query '[0].appId' -o tsv)
put() { # name value
  if [ "$DRY_RUN" = 0 ] && az keyvault secret show --vault-name "$KEYVAULT_NAME" -n "$1" >/dev/null 2>&1; then skip "secret $1 exists (set a new version by deleting it first or with az keyvault secret set)"; return; fi
  if [ "$DRY_RUN" = 1 ]; then run az keyvault secret set --vault-name "$KEYVAULT_NAME" -n "$1" --value "<redacted>" -o none; else
    az keyvault secret set --vault-name "$KEYVAULT_NAME" -n "$1" --value "$2" -o none; ok "secret $1"; fi
}
if [ -f "$AZ_DIR/.portal-client-secret" ]; then put oauth2-client-secret "$(cat "$AZ_DIR/.portal-client-secret")"; rm -f "$AZ_DIR/.portal-client-secret"; ok "removed .portal-client-secret from disk"
else [ "$DRY_RUN" = 1 ] && put oauth2-client-secret "<from 30-entra.sh>" || skip "no .portal-client-secret file (already moved?)"; fi
put oauth2-cookie-secret "$(python3 -c 'import os,base64;print(base64.urlsafe_b64encode(os.urandom(32)).decode())')"
[ -n "${ARTIFACTORY_TOKEN:-}" ] && put artifactory-token "$ARTIFACTORY_TOKEN" || skip "ARTIFACTORY_TOKEN not set (portal lists images/tools anonymously)"

step "3/6 Managed identity $PLATFORM_IDENTITY + Key Vault read role"
if probe az identity show -g "$RESOURCE_GROUP" -n "$PLATFORM_IDENTITY"; then skip "exists"; else
  run az identity create -g "$RESOURCE_GROUP" -n "$PLATFORM_IDENTITY" -l "$LOCATION" --tags $(tags) -o none; ok "created"; fi
ID_CLIENT=$(q identity-client-id az identity show -g "$RESOURCE_GROUP" -n "$PLATFORM_IDENTITY" --query clientId -o tsv)
ID_PRINCIPAL=$(q identity-principal-id az identity show -g "$RESOURCE_GROUP" -n "$PLATFORM_IDENTITY" --query principalId -o tsv)
run az role assignment create --assignee-object-id "$ID_PRINCIPAL" --assignee-principal-type ServicePrincipal --role "Key Vault Secrets User" --scope "$KV_ID" -o none || true
ok "identity may read secrets"

step "4/6 Federated credential (workload identity): devspaces-system/devspaces-secrets → $PLATFORM_IDENTITY"
ISSUER=$(q oidc-issuer-url az aks show -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --query oidcIssuerProfile.issuerUrl -o tsv)
if probe az identity federated-credential show -g "$RESOURCE_GROUP" --identity-name "$PLATFORM_IDENTITY" -n devspaces-secrets; then skip "exists"; else
  run az identity federated-credential create -g "$RESOURCE_GROUP" --identity-name "$PLATFORM_IDENTITY" -n devspaces-secrets \
      --issuer "$ISSUER" --subject system:serviceaccount:devspaces-system:devspaces-secrets --audiences api://AzureADTokenExchange -o none; ok "created"; fi

step "5/6 Secrets Store CSI driver + Azure provider (AKS add-on)"
if [ "$DRY_RUN" = 0 ] && az aks show -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --query 'addonProfiles.azureKeyvaultSecretsProvider.enabled' -o tsv 2>/dev/null | grep -q true; then skip "add-on enabled"; else
  run az aks enable-addons -g "$RESOURCE_GROUP" -n "$CLUSTER_NAME" --addons azure-keyvault-secrets-provider --enable-secret-rotation -o none; ok "add-on enabled (rotation every 2 min)"; fi

step "6/6 SecretProviderClass → Kubernetes Secret oauth2-proxy"
SPC=$(cat <<YAML
apiVersion: v1
kind: Namespace
metadata: { name: devspaces-system }
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: devspaces-secrets
  namespace: devspaces-system
  annotations: { azure.workload.identity/client-id: "$ID_CLIENT" }
  labels: { azure.workload.identity/use: "true" }
---
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: devspaces-keyvault
  namespace: devspaces-system
spec:
  provider: azure
  parameters:
    usePodIdentity: "false"
    clientID: "$ID_CLIENT"           # workload identity
    keyvaultName: "$KEYVAULT_NAME"
    tenantId: "$TENANT"
    objects: |
      array:
        - |
          objectName: oauth2-client-secret
          objectType: secret
        - |
          objectName: oauth2-cookie-secret
          objectType: secret
  # The CSI driver creates this Secret once a pod mounts the volume. The chart's oauth2-proxy
  # pods mount it (values: oauth2Proxy.existingSecret=oauth2-proxy, oauth2Proxy.secretProviderClass=devspaces-keyvault).
  secretObjects:
    - secretName: oauth2-proxy
      type: Opaque
      data:
        - { objectName: oauth2-client-secret, key: client-secret }
        - { objectName: oauth2-cookie-secret, key: cookie-secret }
YAML
)
if [ "$DRY_RUN" = 1 ]; then echo "$SPC" | sed 's/^/  | /'; else
  echo "$SPC" | kubectl apply -f -
  kubectl -n devspaces-system create secret generic oauth2-proxy-client-id --from-literal=client-id="$PORTAL_APP_ID" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  ok "SecretProviderClass devspaces-keyvault applied"
fi
cat <<MSG

Secrets ready. Nothing secret is in git or in values files:
  Key Vault $KEYVAULT_NAME → (CSI, workload identity $ID_CLIENT) → Secret devspaces-system/oauth2-proxy
Next:  ./deploy/cluster/addons.sh  then  ./deploy/cluster/check-contract.sh  then the Kubernetes layer.
MSG
