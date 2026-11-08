// Azure API Management in front of the portal's /api — the "Workspaces API" as an Azure API.
// Deployed by 50-apim.sh:  az deployment group create -f 50-apim.bicep -p …
// What it gives you on top of the portal itself: one Azure-managed front door with Entra ID
// token validation (validate-azure-ad-token), per-user rate limits, a developer portal with the
// OpenAPI docs, subscription keys for automation if you want them, logging to App Insights.
// The portal still validates the same bearer token itself (-oidc-*), so APIM is defence in depth,
// never the only guard.

@description('Globally unique APIM name (becomes <name>.azure-api.net)')
param apimName string
param location string = resourceGroup().location
@allowed(['Consumption', 'Developer', 'Basic', 'Standard', 'Premium'])
param sku string = 'Developer'
param publisherEmail string
param publisherName string
@description('Where APIM forwards: the portal ingress, e.g. https://workspaces.corp.example')
param backendUrl string
@description('URL suffix: https://<apim>.azure-api.net/<apiPath>/api/workspaces')
param apiPath string = 'workspaces'
@description('Entra tenant id')
param tenantId string
@description('Audience of the API app registration, e.g. api://devspaces')
param audience string = 'api://devspaces'
@description('Optional Entra group object id that callers must belong to ("" = any signed-in user)')
param requiredGroup string = ''
@description('OpenAPI document content (api/openapi.yaml) — passed by 50-apim.sh')
param openApiYaml string
@description('Inbound policy XML (apim-policy.xml with placeholders replaced) — passed by 50-apim.sh')
param policyXml string
param tags object = {}

resource apim 'Microsoft.ApiManagement/service@2023-09-01-preview' = {
  name: apimName
  location: location
  tags: tags
  sku: { name: sku, capacity: sku == 'Consumption' ? 0 : 1 }
  identity: { type: 'SystemAssigned' }
  properties: {
    publisherEmail: publisherEmail
    publisherName: publisherName
    // Developers call with a bearer token; no subscription key needed (policy still enforces the JWT).
  }
}

// Named values the policy references (so the XML stays generic).
resource nvTenant 'Microsoft.ApiManagement/service/namedValues@2023-09-01-preview' = {
  parent: apim
  name: 'devspaces-tenant-id'
  properties: { displayName: 'devspaces-tenant-id', value: tenantId }
}
resource nvAudience 'Microsoft.ApiManagement/service/namedValues@2023-09-01-preview' = {
  parent: apim
  name: 'devspaces-audience'
  properties: { displayName: 'devspaces-audience', value: audience }
}
resource nvGroup 'Microsoft.ApiManagement/service/namedValues@2023-09-01-preview' = {
  parent: apim
  name: 'devspaces-required-group'
  properties: { displayName: 'devspaces-required-group', value: empty(requiredGroup) ? 'none' : requiredGroup }
}

resource backend 'Microsoft.ApiManagement/service/backends@2023-09-01-preview' = {
  parent: apim
  name: 'devspaces-portal'
  properties: {
    protocol: 'http'
    url: backendUrl
    tls: { validateCertificateChain: true, validateCertificateName: true }
  }
}

resource api 'Microsoft.ApiManagement/service/apis@2023-09-01-preview' = {
  parent: apim
  name: 'workspaces'
  properties: {
    displayName: 'Developer Workspaces API'
    description: 'Create, manage, share and connect to developer workspaces (portal /api).'
    path: apiPath
    protocols: ['https']
    subscriptionRequired: false
    serviceUrl: backendUrl
    format: 'openapi'
    value: openApiYaml
  }
}

// WebSocket tunnel (/api/workspaces/{name}/tunnel) is not an OpenAPI operation APIM can proxy on all
// SKUs; the CLI falls back to the portal host for the tunnel (see docs). Everything else goes through.
resource apiPolicy 'Microsoft.ApiManagement/service/apis/policies@2023-09-01-preview' = {
  parent: api
  name: 'policy'
  properties: { format: 'rawxml', value: policyXml }
  dependsOn: [nvTenant, nvAudience, nvGroup, backend]
}

resource product 'Microsoft.ApiManagement/service/products@2023-09-01-preview' = {
  parent: apim
  name: 'developers'
  properties: {
    displayName: 'Developers'
    description: 'Self-service developer workspaces'
    subscriptionRequired: false
    state: 'published'
  }
}
resource productApi 'Microsoft.ApiManagement/service/products/apis@2023-09-01-preview' = {
  parent: product
  name: api.name
}

output gatewayUrl string = apim.properties.gatewayUrl
output apiBaseUrl string = '${apim.properties.gatewayUrl}/${apiPath}'
output developerPortalUrl string = apim.properties.developerPortalUrl
