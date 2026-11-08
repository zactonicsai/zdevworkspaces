#!/usr/bin/env bash
# Optional: Azure API Management as the Azure front door for the Workspaces API.
#   ./deploy/cluster/providers/azure/50-apim.sh [--dry-run]
# Deploys 50-apim.bicep with api/openapi.yaml and apim-policy.xml, then prints how to test.
# Developer/Standard/Premium SKUs take 30–45 minutes to create; Consumption takes ~2 minutes.
. "$(dirname "$0")/_lib.sh"
TENANT=${TENANT_ID:-$(q tenant-id az account show --query tenantId -o tsv)}
DEV_GID=""; [ -n "${DEVELOPERS_GROUP_NAME:-}" ] && DEV_GID=$(q developers-group-id az ad group show -g "$DEVELOPERS_GROUP_NAME" --query id -o tsv 2>/dev/null || true)

step "1/3 Inputs"
[ -f "$REPO_ROOT/api/openapi.yaml" ] || fail "api/openapi.yaml missing"
ok "OpenAPI: api/openapi.yaml ($(wc -c <"$REPO_ROOT/api/openapi.yaml") bytes)"
ok "backend: $APIM_BACKEND_URL  path: /$APIM_API_PATH  audience: $API_APP_ID_URI  required group: ${DEV_GID:-none}"
[ "$DRY_RUN" = 0 ] && { az apim check-name -n "$APIM_NAME" --query nameAvailable -o tsv | grep -q true && ok "name $APIM_NAME available" || { probe az apim show -g "$RESOURCE_GROUP" -n "$APIM_NAME" && skip "APIM $APIM_NAME exists (will be updated in place)" || fail "APIM name $APIM_NAME taken"; }; }

step "2/3 Deploy (Bicep)"
PARAMS=$(mktemp "${TMPDIR:-/tmp}/apim.XXXXXX.json")
python3 - "$PARAMS" "$REPO_ROOT/api/openapi.yaml" "$AZ_DIR/apim-policy.xml" <<PY
import json,sys
p,oa,pol=sys.argv[1:4]
v=lambda x:{"value":x}
json.dump({"\$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentParameters.json#","contentVersion":"1.0.0.0","parameters":{
 "apimName":v("$APIM_NAME"),"sku":v("$APIM_SKU"),"publisherEmail":v("$APIM_PUBLISHER_EMAIL"),"publisherName":v("$APIM_PUBLISHER_NAME"),
 "backendUrl":v("$APIM_BACKEND_URL"),"apiPath":v("$APIM_API_PATH"),"tenantId":v("$TENANT"),"audience":v("$API_APP_ID_URI"),
 "requiredGroup":v("$DEV_GID"),"openApiYaml":v(open(oa).read()),"policyXml":v(open(pol).read()),
 "tags":v(dict(kv.split("=",1) for kv in "$(tags)".split() if "=" in kv))}},open(p,"w"))
PY
ok "parameters → $PARAMS"
run az deployment group create -g "$RESOURCE_GROUP" -n devspaces-apim -f "$AZ_DIR/50-apim.bicep" -p "@$PARAMS" -o none
GW=$(q "https://$APIM_NAME.azure-api.net" az deployment group show -g "$RESOURCE_GROUP" -n devspaces-apim --query properties.outputs.gatewayUrl.value -o tsv)

step "3/3 Test"
cat <<MSG
  Base URL:  $GW/$APIM_API_PATH        (OpenAPI docs: Azure portal → APIM → APIs → Developer Workspaces API)
  # 1. get a token as a developer (same one wsctl uses)
  TOKEN=\$(az account get-access-token --resource $API_APP_ID_URI --query accessToken -o tsv)
  curl -sS -H "Authorization: Bearer \$TOKEN" $GW/$APIM_API_PATH/api/me | jq .
  curl -sS -H "Authorization: Bearer \$TOKEN" $GW/$APIM_API_PATH/api/workspaces | jq .
  # 2. the Go CLI through APIM (tunnel/ssh still goes to the portal host directly: APIM does not proxy WebSockets on every SKU)
  wsctl login --portal $GW/$APIM_API_PATH --tunnel-host https://$PORTAL_HOST --az
  wsctl list
  # Without a token you must get 401; with a token from another tenant/audience 401; a non-member 403.
MSG
rm -f "$PARAMS"
