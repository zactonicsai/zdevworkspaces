/* auth.js — browser sign-in with any OpenID Connect provider (Keycloak, Entra ID, Okta …)
   using Authorization Code + PKCE, no libraries. Active only when PORTAL_CONFIG.oidc.issuer
   is set; otherwise the portal is in "session mode" (an auth proxy in front signs people in).
     AUTH.enabled()        is OIDC configured?
     AUTH.init()           finish a redirect (?code=…) if present; returns true when signed in
     AUTH.login()          redirect to the provider
     AUTH.logout()         drop tokens (and end the provider session if it offers end_session)
     AUTH.token()          a valid access token (refreshes with the refresh token when expiring)
     AUTH.user()           claims from the ID token (name, preferred_username, email)
   Tokens live in sessionStorage (per tab) under "ws.auth". Keycloak setup: a *public* client
   with PKCE (S256), Valid redirect URIs = the portal URL, Web origins = the portal origin. */
"use strict";

window.AUTH = (() => {
  const KEY = "ws.auth";
  const cfg = () => (window.PORTAL_CONFIG && window.PORTAL_CONFIG.oidc) || {};
  const enabled = () => !!cfg().issuer;
  let disc = null;
  const load = () => { try { return JSON.parse(sessionStorage.getItem(KEY)) || {}; } catch (e) { return {}; } };
  const save = (s) => { try { sessionStorage.setItem(KEY, JSON.stringify(s)); } catch (e) { /* ignore */ } };
  const b64url = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  const rand = (n = 32) => { const a = new Uint8Array(n); crypto.getRandomValues(a); return b64url(a); };
  const sha256 = async (s) => b64url(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s)));
  const redirectUri = () => location.origin + location.pathname;
  const claims = (jwt) => { try { return JSON.parse(atob(jwt.split(".")[1].replace(/-/g, "+").replace(/_/g, "/"))); } catch (e) { return {}; } };

  async function discovery() {
    if (disc) return disc;
    const url = cfg().issuer.replace(/\/$/, "") + "/.well-known/openid-configuration";
    const res = await fetch(url);
    if (!res.ok) throw new Error(`OIDC discovery failed (${res.status}) at ${url}`);
    disc = await res.json();
    return disc;
  }

  async function login() {
    const d = await discovery();
    const verifier = rand(48), state = rand(16), nonce = rand(16);
    save({ verifier, state, nonce, returnTo: location.hash });
    const p = new URLSearchParams({
      response_type: "code", client_id: cfg().clientId, redirect_uri: redirectUri(), scope: cfg().scope || "openid profile email",
      state, nonce, code_challenge: await sha256(verifier), code_challenge_method: "S256",
    });
    if (cfg().audience) p.set("audience", cfg().audience);   // Auth0-style; Keycloak uses the client's audience mapper instead
    location.assign(d.authorization_endpoint + "?" + p.toString());
  }

  async function exchange(params) {
    const d = await discovery();
    const body = new URLSearchParams({ grant_type: "authorization_code", client_id: cfg().clientId, code: params.code, redirect_uri: redirectUri(), code_verifier: params.verifier });
    const res = await fetch(d.token_endpoint, { method: "POST", headers: { "Content-Type": "application/x-www-form-urlencoded" }, body });
    if (!res.ok) throw new Error("token exchange failed: " + (await res.text()));
    return res.json();
  }

  function store(tok, prev = {}) {
    const now = Math.floor(Date.now() / 1000);
    save({ access: tok.access_token, refresh: tok.refresh_token || prev.refresh, id: tok.id_token || prev.id,
      exp: now + (tok.expires_in || 300), claims: claims(tok.id_token || prev.id || tok.access_token) });
  }

  /* init: if we are coming back from the provider, finish the dance; returns signed-in? */
  async function init() {
    if (!enabled()) return false;
    const q = new URLSearchParams(location.search);
    if (q.get("error")) throw new Error(`sign-in refused: ${q.get("error")} ${q.get("error_description") || ""}`);
    if (q.get("code")) {
      const s = load();
      if (q.get("state") !== s.state) throw new Error("sign-in state mismatch (reload and try again)");
      const tok = await exchange({ code: q.get("code"), verifier: s.verifier });
      store(tok);
      const nonce = claims(tok.id_token || "").nonce;
      if (tok.id_token && nonce && nonce !== s.nonce) { save({}); throw new Error("sign-in nonce mismatch"); }
      history.replaceState(null, "", location.pathname + (s.returnTo || "#/home"));
    }
    return !!load().access;
  }

  async function token() {
    const s = load();
    if (!s.access) return "";
    const now = Math.floor(Date.now() / 1000);
    if (s.exp - now > 30) return s.access;
    if (!s.refresh) return s.access;
    try {
      const d = await discovery();
      const body = new URLSearchParams({ grant_type: "refresh_token", client_id: cfg().clientId, refresh_token: s.refresh });
      const res = await fetch(d.token_endpoint, { method: "POST", headers: { "Content-Type": "application/x-www-form-urlencoded" }, body });
      if (!res.ok) throw new Error("refresh failed");
      store(await res.json(), s);
      return load().access;
    } catch (e) {
      return s.access; // let the API answer 401 → login()
    }
  }

  async function logout() {
    const s = load();
    save({});
    if (!enabled()) { location.reload(); return; }
    try {
      const d = await discovery();
      if (d.end_session_endpoint) {
        const p = new URLSearchParams({ post_logout_redirect_uri: redirectUri(), client_id: cfg().clientId });
        if (s.id) p.set("id_token_hint", s.id);
        location.assign(d.end_session_endpoint + "?" + p.toString());
        return;
      }
    } catch (e) { /* fall through */ }
    location.reload();
  }

  const user = () => load().claims || {};
  let redirecting = false, rejection = "";
  // 401 from the API. No token → go and sign in. A token we just sent was refused (wrong audience,
  // issuer mismatch, clock skew…) → forget it and surface the reason instead of looping back to the IdP.
  function onUnauthorized(detail) {
    if (load().access) {
      try { sessionStorage.removeItem(KEY); } catch (e) { /* ignore */ }
      if (/expired/i.test(detail || "")) { onUnauthorized(); return; } // plain expiry: just sign in again
      rejection = "You signed in, but the API rejected the token" + (detail ? ": " + detail : ".") +
        " Check that the API's issuer/audience match the identity provider (portal.oidc in the chart, PORTAL_OIDC_* in Docker).";
      return;
    }
    if (!redirecting) { redirecting = true; login().catch(() => { redirecting = false; }); }
  }

  return { enabled, init, login, logout, token, user, onUnauthorized, signedIn: () => !!load().access, rejection: () => rejection };
})();
