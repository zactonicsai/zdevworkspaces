/* config.js — runtime configuration of the portal UI. This static copy is the default;
   both API servers (portal/ in Go, portal-py/ in Python) serve a generated /config.js from
   their flags, so the same UI build works against any API URL and any OIDC provider.
     apiBase  ""            same origin (default) | "https://api.example.com" (CORS must allow this page)
     oidc     issuer ""     session mode: an auth proxy (oauth2-proxy) signs people in
              issuer set    browser PKCE login against this OIDC issuer (Keycloak realm, Entra …);
                            the API receives "Authorization: Bearer <access token>"
     stub     false         true = no API at all: static JSON from web/stub/ (UI development, demos)
   Query/localStorage overrides for developers: ?stub=1 / ?stub=0, ?api=https://… */
window.PORTAL_CONFIG = window.PORTAL_CONFIG || {
  apiBase: "",
  oidc: { issuer: "", clientId: "devspaces-portal", scope: "openid profile email", audience: "" },
  stub: false,
  allowStubToggle: true,
};
