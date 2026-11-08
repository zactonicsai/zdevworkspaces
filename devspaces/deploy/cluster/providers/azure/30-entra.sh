#!/usr/bin/env bash
# Phase 2 on Azure: identity (contract C7). Three Entra ID app registrations + two groups.
#   devspaces-portal  web app       — oauth2-proxy signs developers into the browser portal
#   devspaces-api     resource app  — exposes api://devspaces/Workspaces.ReadWrite; bearer tokens for
#                                     the CLI and for Azure API Management are issued FOR this app
#   devspaces-cli     public client — `wsctl login` device-code flow; pre-authorised on the API app
#   groups            devspaces-platform-admins, devspaces-developers
# Writes  deploy/cluster/providers/azure/entra.values.yaml  (Helm values snippet, git-ignored by name)
#   ./deploy/cluster/providers/azure/30-entra.sh [--dry-run]
# Needs: Application Administrator (or Cloud App Admin) + Groups Administrator in the tenant.
. "$(dirname "$0")/_lib.sh"
TENANT=${TENANT_ID:-$(q tenant-id az account show --query tenantId -o tsv)}
OUT="$AZ_DIR/entra.values.yaml"

app_id() { q "$1-app-id" az ad app list --display-name "$1" --query '[0].appId' -o tsv; }
obj_id() { q "$1-object-id" az ad app list --display-name "$1" --query '[0].id' -o tsv; }
exists() { [ "$DRY_RUN" = 0 ] && [ -n "$(az ad app list --display-name "$1" --query '[0].appId' -o tsv)" ]; }

step "1/6 Groups"
for g in "$ADMIN_GROUP_NAME" "${DEVELOPERS_GROUP_NAME:-}"; do
  [ -n "$g" ] || continue
  if probe az ad group show -g "$g"; then skip "group $g exists"; else
    run az ad group create --display-name "$g" --mail-nickname "$(echo "$g" | tr -cd 'a-zA-Z0-9')" -o none; ok "group $g"; fi
done
ADMIN_GID=$(q admin-group-id az ad group show -g "$ADMIN_GROUP_NAME" --query id -o tsv)
DEV_GID=""; [ -n "${DEVELOPERS_GROUP_NAME:-}" ] && DEV_GID=$(q developers-group-id az ad group show -g "$DEVELOPERS_GROUP_NAME" --query id -o tsv)

step "2/6 API app '$ENTRA_API_APP' exposing $API_APP_ID_URI/$API_SCOPE"
SCOPE_ID=$(q scope-uuid python3 -c 'import uuid;print(uuid.uuid4())')
if exists "$ENTRA_API_APP"; then skip "exists"; else
  run az ad app create --display-name "$ENTRA_API_APP" --sign-in-audience AzureADMyOrg \
      --optional-claims '{"accessToken":[{"name":"groups"},{"name":"preferred_username"},{"name":"email"}]}' -o none
  ok "created"
fi
API_APP_ID=$(app_id "$ENTRA_API_APP"); API_OBJ=$(obj_id "$ENTRA_API_APP")
# identifier URI, the scope, group claims (security groups → `groups` claim as object ids)
run az ad app update --id "$API_APP_ID" --identifier-uris "$API_APP_ID_URI" --set groupMembershipClaims=SecurityGroup -o none
if [ "$DRY_RUN" = 0 ] && az ad app show --id "$API_APP_ID" --query "api.oauth2PermissionScopes[?value=='$API_SCOPE'] | [0].id" -o tsv | grep -q .; then skip "scope $API_SCOPE exists"; else
  run az ad app update --id "$API_APP_ID" --set api.oauth2PermissionScopes="[{\"id\":\"$SCOPE_ID\",\"adminConsentDescription\":\"Create and manage developer workspaces\",\"adminConsentDisplayName\":\"Manage workspaces\",\"isEnabled\":true,\"type\":\"User\",\"userConsentDescription\":\"Create and manage your developer workspaces\",\"userConsentDisplayName\":\"Manage your workspaces\",\"value\":\"$API_SCOPE\"}]" -o none
  ok "scope $API_SCOPE"
fi
# v2 tokens (so `aud` is api://devspaces and claims are the v2 names the portal expects)
run az ad app update --id "$API_APP_ID" --set api.requestedAccessTokenVersion=2 -o none
probe az ad sp show --id "$API_APP_ID" && skip "service principal exists" || { run az ad sp create --id "$API_APP_ID" -o none; ok "service principal"; }
SCOPE_ID=$(q scope-uuid az ad app show --id "$API_APP_ID" --query "api.oauth2PermissionScopes[?value=='$API_SCOPE'] | [0].id" -o tsv)

step "3/6 Portal web app '$ENTRA_PORTAL_APP' (oauth2-proxy)"
REDIRECT="https://$PORTAL_HOST/oauth2/callback"
if exists "$ENTRA_PORTAL_APP"; then skip "exists"; else
  run az ad app create --display-name "$ENTRA_PORTAL_APP" --sign-in-audience AzureADMyOrg --web-redirect-uris "$REDIRECT" \
      --enable-id-token-issuance true -o none
  ok "created (redirect $REDIRECT)"
fi
PORTAL_APP_ID=$(app_id "$ENTRA_PORTAL_APP")
run az ad app update --id "$PORTAL_APP_ID" --set groupMembershipClaims=SecurityGroup \
    --optional-claims '{"idToken":[{"name":"groups"},{"name":"preferred_username"},{"name":"email"}]}' -o none
probe az ad sp show --id "$PORTAL_APP_ID" && skip "service principal exists" || { run az ad sp create --id "$PORTAL_APP_ID" -o none; ok "service principal"; }
# the portal app may request the API scope (oauth2-proxy can pass the access token upstream if you want)
run az ad app permission add --id "$PORTAL_APP_ID" --api "$API_APP_ID" --api-permissions "$SCOPE_ID=Scope" -o none || true
if [ "$DRY_RUN" = 1 ]; then run az ad app credential reset --id "$PORTAL_APP_ID" --display-name oauth2-proxy --years 1 --query password -o tsv; PORTAL_SECRET="<generated-client-secret>"; else
  if [ -f "$AZ_DIR/.portal-client-secret" ]; then PORTAL_SECRET=$(cat "$AZ_DIR/.portal-client-secret"); skip "client secret reused from .portal-client-secret"; else
    PORTAL_SECRET=$(az ad app credential reset --id "$PORTAL_APP_ID" --display-name oauth2-proxy --years 1 --query password -o tsv)
    umask 077; echo "$PORTAL_SECRET" >"$AZ_DIR/.portal-client-secret"; ok "client secret created (saved to .portal-client-secret — move it to Key Vault with 40-keyvault.sh, then delete the file)"
  fi
fi

step "4/6 CLI public client '$ENTRA_CLI_APP' (device-code flow for wsctl)"
if exists "$ENTRA_CLI_APP"; then skip "exists"; else
  run az ad app create --display-name "$ENTRA_CLI_APP" --sign-in-audience AzureADMyOrg --is-fallback-public-client true \
      --public-client-redirect-uris "http://localhost" "https://login.microsoftonline.com/common/oauth2/nativeclient" -o none
  ok "created"
fi
CLI_APP_ID=$(app_id "$ENTRA_CLI_APP")
run az ad app permission add --id "$CLI_APP_ID" --api "$API_APP_ID" --api-permissions "$SCOPE_ID=Scope" -o none || true
# pre-authorise the CLI on the API so developers get no consent prompt
run az ad app update --id "$API_APP_ID" --set api.preAuthorizedApplications="[{\"appId\":\"$CLI_APP_ID\",\"delegatedPermissionIds\":[\"$SCOPE_ID\"]}]" -o none
probe az ad sp show --id "$CLI_APP_ID" && skip "service principal exists" || { run az ad sp create --id "$CLI_APP_ID" -o none; ok "service principal"; }

step "5/6 Admin consent (tenant-wide, so nobody sees a consent screen)"
run az ad app permission admin-consent --id "$PORTAL_APP_ID" || warn "admin consent needs a Global/Privileged Role admin: Portal → App registrations → $ENTRA_PORTAL_APP → API permissions → Grant"
run az ad app permission admin-consent --id "$CLI_APP_ID" || true
if [ -n "$DEV_GID" ]; then
  # optional: only members of the developers group may sign in (assignment required on the portal SP)
  PORTAL_SP=$(q portal-sp-id az ad sp show --id "$PORTAL_APP_ID" --query id -o tsv)
  run az ad sp update --id "$PORTAL_SP" --set appRoleAssignmentRequired=true -o none
  run az rest --method POST --uri "https://graph.microsoft.com/v1.0/servicePrincipals/$PORTAL_SP/appRoleAssignedTo" \
      --body "{\"principalId\":\"$DEV_GID\",\"resourceId\":\"$PORTAL_SP\",\"appRoleId\":\"00000000-0000-0000-0000-000000000000\"}" -o none || true
  ok "only '$DEVELOPERS_GROUP_NAME' members can sign in to the portal"
fi

step "6/6 Values snippet → $OUT"
cat >"$OUT" <<YAML
# Generated by 30-entra.sh on $(date -u +%Y-%m-%dT%H:%MZ). Merge with charts/devspaces/examples/values-azure.yaml:
#   helm upgrade --install devspaces charts/devspaces -n devspaces-system -f charts/devspaces/examples/values-azure.yaml -f $OUT -f deploy/prod/secrets.values.yaml
oauth2Proxy:
  provider: entra-id
  issuerUrl: https://login.microsoftonline.com/$TENANT/v2.0
  clientId: "$PORTAL_APP_ID"
  # clientSecret / cookieSecret: NOT here — Key Vault (40-keyvault.sh) → Secret oauth2-proxy (existingSecret)
  existingSecret: oauth2-proxy
  groupsClaim: groups
  extraArgs:
    - --oidc-groups-claim=groups
    - --allowed-group=${DEV_GID:-"<developers-group-object-id>"}
portal:
  oidc:
    issuer: https://login.microsoftonline.com/$TENANT/v2.0
    audience: "$API_APP_ID_URI"
    userClaim: preferred_username
  platformAdminGroups: ["$ADMIN_GID"]
# For wsctl (clients/wsctl) — give developers these values or put them in the portal's /api/me:
#   wsctl login --portal https://$PORTAL_HOST --tenant $TENANT --client-id $CLI_APP_ID --scope $API_APP_ID_URI/$API_SCOPE
# For kubectl (Azure RBAC on AKS, Entra identities): usernames are UPNs = e-mails (contract C7).
YAML
ok "written"
cat <<MSG

Identity ready.
  tenant            $TENANT
  portal app (web)  $PORTAL_APP_ID   redirect $REDIRECT
  api app           $API_APP_ID      $API_APP_ID_URI/$API_SCOPE   (v2 tokens, groups claim)
  cli app (public)  $CLI_APP_ID      device-code flow, pre-authorised on the API
  admins group      $ADMIN_GID ($ADMIN_GROUP_NAME)
Next:  ./deploy/cluster/providers/azure/40-keyvault.sh   (moves the client secret into Key Vault)
MSG
