/* api.js — every call the UI makes to the Workspaces API, one function each, in one place.
   Shapes follow api/openapi.yaml. Transport concerns live here only: base URL
   (PORTAL_CONFIG.apiBase), auth (session cookie or OIDC bearer from auth.js), the CSRF
   header, error normalisation, and the stub switch (window.STUB answers instead of fetch). */
"use strict";

window.API = (() => {
  const cfg = () => window.PORTAL_CONFIG || {};
  const enc = encodeURIComponent;
  const qs = (params) => {
    const p = Object.entries(params || {}).filter(([, v]) => v !== undefined && v !== null && v !== "" && v !== false);
    return p.length ? "?" + p.map(([k, v]) => `${enc(k)}=${enc(v)}`).join("&") : "";
  };

  /* ---- stub switch: config, ?stub=1|0, or the footer toggle (localStorage) */
  function stubEnabled() {
    const q = new URLSearchParams(location.search).get("stub");
    if (q === "1" || q === "true") return true;
    if (q === "0" || q === "false") return false;
    try { const s = localStorage.getItem("ws.stub"); if (s === "1") return true; if (s === "0") return false; } catch (e) { /* ignore */ }
    return !!cfg().stub || location.protocol === "file:";
  }
  function setStub(on) { try { localStorage.setItem("ws.stub", on ? "1" : "0"); } catch (e) { /* ignore */ } }

  /* ---- transport */
  function base() {
    const q = new URLSearchParams(location.search).get("api");
    return (q || cfg().apiBase || "").replace(/\/$/, "");
  }
  async function request(method, path, body, opts = {}) {
    if (stubEnabled() && window.STUB) {
      const r = await window.STUB.handle(method, path, body);
      if (r.status >= 400) throw Object.assign(new Error(r.body.error || "error"), { status: r.status });
      return r.body;
    }
    const headers = { "X-Requested-With": "devspaces-portal" };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    const tok = window.AUTH && window.AUTH.enabled() ? await window.AUTH.token() : "";
    if (tok) headers.Authorization = "Bearer " + tok;
    const res = await fetch(base() + path, {
      method, headers, body: body !== undefined ? JSON.stringify(body) : undefined,
      credentials: base() ? "include" : "same-origin",
    });
    if (opts.text) {
      const t = await res.text();
      if (!res.ok) throw Object.assign(new Error(t || `${res.status} ${res.statusText}`), { status: res.status });
      return t;
    }
    let data = {};
    try { data = await res.json(); } catch (e) { /* non-JSON body */ }
    if (!res.ok) {
      const err = Object.assign(new Error(data.error || `${res.status} ${res.statusText}`), { status: res.status });
      if (res.status === 401 && window.AUTH && window.AUTH.enabled()) window.AUTH.onUnauthorized(data.error);
      throw err;
    }
    return data;
  }

  /* ---- me / catalog / profile */
  const me = () => request("GET", "/api/me");
  const catalog = () => request("GET", "/api/catalog");
  const images = () => request("GET", "/api/artifactory/images");
  const tools = () => request("GET", "/api/artifactory/tools");
  const profile = () => request("GET", "/api/profile");
  const saveProfile = (p) => request("PUT", "/api/profile", p);
  const projects = () => request("GET", "/api/projects");

  /* ---- workspaces */
  const wsPath = (name, action, params) => `/api/workspaces/${enc(name)}${action ? "/" + action : ""}${qs(params)}`;
  const workspaces = ({ shared = false, ns = "" } = {}) => request("GET", `/api/workspaces${qs({ shared, ns })}`);
  const workspace = (name, { owner = "", ns = "" } = {}) => request("GET", wsPath(name, "", { owner, ns }));
  const createWorkspace = (req) => request("POST", "/api/workspaces", req);
  const deleteWorkspace = (name, { purge = false, owner = "" } = {}) => request("DELETE", wsPath(name, "", { purge, owner }));
  const startWorkspace = (name, { owner = "" } = {}) => request("POST", wsPath(name, "start", { owner }));
  const stopWorkspace = (name) => request("POST", wsPath(name, "stop"));
  const setIdleTimeout = (name, idleTimeout) => request("PUT", wsPath(name, "idle"), { idleTimeout });
  const addKey = (name, publicKey) => request("POST", wsPath(name, "keys"), { publicKey });
  const logs = (name, { tail = 300, owner = "" } = {}) => request("GET", wsPath(name, "logs", { tail, owner }));
  const network = (name, { owner = "" } = {}) => request("GET", wsPath(name, "network", { owner }));

  /* ---- sharing & links */
  const shares = (name) => request("GET", wsPath(name, "shares"));
  const addShare = (name, user, publicKey = "") => request("POST", wsPath(name, "shares"), { user, publicKey });
  const removeShare = (name, user) => request("DELETE", wsPath(name, `shares/${enc(user)}`));
  const links = (name) => request("GET", wsPath(name, "links"));
  const addLink = (name, peer, ports = []) => request("POST", wsPath(name, "links"), { peer, ports });
  const removeLink = (name, peer) => request("DELETE", wsPath(name, `links/${enc(peer)}`));

  /* ---- drives & snapshots */
  const drivePath = (name, action, params) => `/api/drives/${enc(name)}${action ? "/" + action : ""}${qs(params)}`;
  const drives = () => request("GET", "/api/drives");
  const drive = (name, { ns = "" } = {}) => request("GET", drivePath(name, "", { ns }));
  const createDrive = ({ name, size = "100Gi", storage = "" }) => request("POST", "/api/drives", { name, size, storage });
  const deleteDrive = (name) => request("DELETE", drivePath(name));
  const resizeDrive = (name, size) => request("PUT", drivePath(name, "resize"), { size });
  const snapshots = (name) => request("GET", drivePath(name, "snapshots"));
  const createSnapshot = (name, snapshotName = "") => request("POST", drivePath(name, "snapshots"), snapshotName ? { name: snapshotName } : {});
  const deleteSnapshot = (name) => request("DELETE", `/api/snapshots/${enc(name)}`);
  const driveShares = (name) => request("GET", drivePath(name, "shares"));
  const shareDrive = (name, user, readOnly = false) => request("POST", drivePath(name, "shares"), { user, readOnly });
  const unshareDrive = (name, user) => request("DELETE", drivePath(name, `shares/${enc(user)}`));

  return {
    request, stubEnabled, setStub, base,
    me, catalog, images, tools, profile, saveProfile, projects,
    workspaces, workspace, createWorkspace, deleteWorkspace, startWorkspace, stopWorkspace, setIdleTimeout, addKey, logs, network,
    shares, addShare, removeShare, links, addLink, removeLink,
    drives, drive, createDrive, deleteDrive, resizeDrive, snapshots, createSnapshot, deleteSnapshot, driveShares, shareDrive, unshareDrive,
  };
})();
