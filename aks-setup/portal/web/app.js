/* app.js — Developer Workspaces portal. Vanilla JavaScript, no build step.
   Structure: state → api → ui helpers (toast, modal) → theme/text-size →
   router → views → wizard → event delegation. Every dynamic value goes
   through esc() before it is placed in HTML. */
"use strict";

(() => {
  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
  const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  const T = (id, label) => window.CONTENT.t(id, label);
  const NAME_RE = /^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$/;
  const STEPS = ["Basics", "Tools", "Code", "Size", "Advanced", "Connect", "Review"];
  const SIZE_PRESETS = {
    small: { label: "Small", cpu: "2", memory: "4Gi", size: "20Gi", cpuRequest: "500m", memoryRequest: "1Gi", hint: "scripts, small services, learning" },
    medium: { label: "Medium", cpu: "4", memory: "8Gi", size: "30Gi", cpuRequest: "1", memoryRequest: "2Gi", hint: "most projects (default)" },
    large: { label: "Large", cpu: "8", memory: "16Gi", size: "50Gi", cpuRequest: "2", memoryRequest: "4Gi", hint: "Java, C++, Android, big monorepos" },
    xl: { label: "Extra large", cpu: "16", memory: "32Gi", size: "100Gi", cpuRequest: "4", memoryRequest: "8Gi", hint: "data work, heavy builds" },
    custom: { label: "Custom", hint: "type your own values" },
  };

  const state = {
    me: null, catalog: null, tools: null, images: null, workspaces: [], profile: null,
    demo: false, route: { page: "home" }, draft: loadDraft(), poll: null, detail: null, createdName: null,
    drives: [], snapshots: [], sharedWithMe: [],
  };

  /* ------------------------------------------------------------------ api */
  async function api(method, path, body) {
    if (state.demo) {
      const r = window.DEMO.handle(method, path, body);
      if (r.status >= 400) throw Object.assign(new Error(r.body.error || "error"), { status: r.status });
      return r.body;
    }
    const res = await fetch(path, {
      method, headers: { "X-Requested-With": "devspaces-portal", ...(body ? { "Content-Type": "application/json" } : {}) },
      body: body ? JSON.stringify(body) : undefined, credentials: "same-origin",
    });
    let data = {};
    try { data = await res.json(); } catch (e) { /* non-JSON */ }
    if (!res.ok) throw Object.assign(new Error(data.error || `${res.status} ${res.statusText}`), { status: res.status });
    return data;
  }

  async function bootstrap() {
    try {
      if (location.protocol === "file:") throw new Error("file");
      state.me = await api("GET", "/api/me");
    } catch (e) {
      if (e.status === 401) { renderSignedOut(); return; }
      // No API behind this page (static preview / file://): run the in-browser demo.
      state.demo = true;
      state.me = await api("GET", "/api/me");
    }
    state.catalog = await api("GET", "/api/catalog");
    renderUser();
    renderBanner();
    $("#footer-version").textContent = state.demo ? "demo mode" : "v" + (state.me.version || "");
    window.addEventListener("hashchange", route);
    route();
  }

  /* ------------------------------------------------------------------ ui helpers */
  function toast(msg, type = "info", ms = 4500, key = "") {
    let el = key ? $(`#toasts [data-key="${key}"]`) : null;
    if (!el) {
      el = document.createElement("div");
      el.setAttribute("role", "status");
      if (key) el.dataset.key = key;
      $("#toasts").appendChild(el);
    }
    el.className = `toast toast-${type}`;
    el.textContent = msg;
    clearTimeout(el._t);
    el._t = setTimeout(() => el.remove(), ms);
  }

  const modal = $("#modal");
  function openModal({ title, html, links = [], actions = [] }) {
    $("#modal-title").textContent = title;
    $("#modal-body").innerHTML = html;
    $("#modal-links").innerHTML = links.map(([label, url]) => `<a href="${esc(url)}" target="_blank" rel="noopener">${esc(label)}</a>`).join("");
    $("#modal-actions").innerHTML = actions.map((a, i) => `<button type="button" class="btn ${a.cls || "btn-secondary"}" data-action="modal-action" data-arg="${i}">${esc(a.label)}</button>`).join("");
    modal._actions = actions;
    if (!modal.open) modal.showModal();
    $("#modal-body").scrollTop = 0;
  }
  function closeModal() { if (modal.open) modal.close(); }
  function openTopic(id) {
    const t = window.CONTENT.topics[id];
    if (!t) return;
    openModal({ title: t.title, html: t.body, links: t.links });
  }
  function confirmDialog({ title, html, okLabel = "Confirm", okCls = "btn-primary" }) {
    return new Promise((resolve) => {
      openModal({ title, html, actions: [
        { label: "Cancel", cls: "btn-secondary", run: () => resolve(false) },
        { label: okLabel, cls: okCls, run: () => resolve(true) },
      ] });
      modal.addEventListener("close", () => resolve(false), { once: true });
    });
  }
  async function copy(text, what = "Copied") {
    try { await navigator.clipboard.writeText(text); toast(what + " to clipboard", "ok", 2000); }
    catch (e) { openModal({ title: "Copy by hand", html: `<pre class="cmd"><code>${esc(text)}</code></pre>` }); }
  }
  const copyBtn = (text, label = "Copy") => `<button type="button" class="btn btn-ghost btn-sm" data-action="copy" data-arg="${esc(text)}">${esc(label)}</button>`;
  const ago = (iso) => {
    if (!iso) return "";
    const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
    if (s < 60) return "just now";
    if (s < 3600) return `${Math.floor(s / 60)} min ago`;
    if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
    return `${Math.floor(s / 86400)} d ago`;
  };
  const statusPill = (st) => {
    const cls = { Running: "pill-ok", Stopped: "pill-muted", Starting: "pill-warn", Pending: "pill-warn", Failed: "pill-danger" }[st] || "pill-info";
    const spin = st === "Starting" || st === "Pending" ? '<span class="spinner" aria-hidden="true"></span>' : "";
    return `<span class="pill pill-dot ${cls}">${spin}${esc(st)}</span>`;
  };
  const langLabel = (id) => (state.catalog.languages[id] || {}).label || id;

  /* ------------------------------------------------------------------ theme & text size */
  function applyTheme(theme) {
    document.documentElement.classList.toggle("dark", theme === "dark");
    document.documentElement.setAttribute("data-theme", theme);
    try { localStorage.setItem("ws.theme", theme); } catch (e) { /* ignore */ }
    $("#icon-moon").classList.toggle("hidden", theme === "dark");
    $("#icon-sun").classList.toggle("hidden", theme !== "dark");
    $("#theme-toggle").setAttribute("aria-label", theme === "dark" ? "Switch to light mode" : "Switch to dark mode");
  }
  function currentFontSize() { return parseInt(getComputedStyle(document.documentElement).fontSize, 10) || 16; }
  function setFontSize(px, announce = true) {
    px = Math.min(36, Math.max(14, px));
    document.documentElement.style.fontSize = px + "px";
    try { localStorage.setItem("ws.fontSize", String(px)); } catch (e) { /* ignore */ }
    $("#font-size-label").textContent = px;
    $("#font-dec").disabled = px <= 14;
    $("#font-inc").disabled = px >= 36;
    if (announce) toast(`Text size ${px}px`, "info", 1200, "fontsize");
    updateCompact();
  }
  // The desktop navigation needs ~60rem of width at the current text size; below that
  // (narrow screens, or large text on a laptop) the header uses the hamburger layout.
  function updateCompact() {
    const compact = window.innerWidth / currentFontSize() < 60;
    document.documentElement.classList.toggle("compact", compact);
    if (!compact) { $("#site-nav-mobile").classList.add("hidden"); $("#nav-toggle").setAttribute("aria-expanded", "false"); }
  }
  function initControls() {
    applyTheme(document.documentElement.classList.contains("dark") ? "dark" : "light");
    setFontSize(currentFontSize(), false);
    $("#theme-toggle").addEventListener("click", () => applyTheme(document.documentElement.classList.contains("dark") ? "light" : "dark"));
    $("#font-inc").addEventListener("click", () => setFontSize(currentFontSize() + 2));
    $("#font-dec").addEventListener("click", () => setFontSize(currentFontSize() - 2));
    $("#font-reset").addEventListener("click", () => setFontSize(16));
    window.addEventListener("resize", updateCompact);
    $("#nav-toggle").addEventListener("click", () => {
      const nav = $("#site-nav-mobile");
      const open = nav.classList.toggle("hidden") === false;
      $("#nav-toggle").setAttribute("aria-expanded", String(open));
    });
    modal.addEventListener("click", (e) => { if (e.target === modal) closeModal(); });
  }

  /* ------------------------------------------------------------------ router */
  function parseHash() {
    const h = location.hash.replace(/^#\/?/, "");
    const [path, query = ""] = h.split("?");
    const [page = "home", arg = ""] = path.split("/");
    return { page, arg: decodeURIComponent(arg), query: Object.fromEntries(new URLSearchParams(query)) };
  }
  async function route() {
    state.route = parseHash();
    clearInterval(state.poll); state.poll = null;
    $("#site-nav-mobile").classList.add("hidden"); $("#nav-toggle").setAttribute("aria-expanded", "false");
    $$("[data-nav]").forEach((a) => { if (a.dataset.nav === state.route.page) a.setAttribute("aria-current", "page"); else a.removeAttribute("aria-current"); });
    const view = $("#view");
    view.setAttribute("aria-busy", "true");
    try {
      const pages = { home: viewHome, create: viewCreate, workspaces: state.route.arg ? viewWorkspace : viewWorkspaces, drives: viewDrives, tools: viewTools, guides: state.route.arg ? viewGuide : viewGuides, glossary: viewGlossary, profile: viewProfile };
      const fn = pages[state.route.page] || viewNotFound;
      view.innerHTML = await fn(state.route.arg);
    } catch (e) {
      view.innerHTML = errorBox(e);
    }
    view.setAttribute("aria-busy", "false");
    window.scrollTo({ top: 0 });
    const h1 = $("h1", view); if (h1) { h1.setAttribute("tabindex", "-1"); h1.focus({ preventScroll: true }); }
    if (state.route.page === "workspaces" && state.route.arg) loadLogs(state.route.arg);
  }
  const errorBox = (e) => `<div class="callout callout-danger"><strong>Something went wrong.</strong> ${esc(e.message || e)}</div>`;

  function renderUser() {
    const u = state.me.user;
    $("#user-name").textContent = u.name;
    $("#user-initial").textContent = (u.short || "?")[0].toUpperCase();
  }
  function renderBanner() {
    const b = $("#banner");
    const msgs = [];
    if (state.demo) msgs.push(`<strong>Demo mode.</strong> No server is connected, so this page runs a small make-believe cluster in your browser. Everything works, nothing is real.`);
    if (!state.me.onboarded) msgs.push(`<strong>Your account is not set up yet.</strong> Ask a platform admin to run <code>workspace admin onboard ${esc(state.me.user.name)}</code>. Until then you can read the guides.`);
    if (state.me.catalogSource === "embedded" && !state.demo) msgs.push(`The catalog could not be read from the cluster; showing the built-in copy.`);
    b.innerHTML = msgs.map((m) => `<div class="page-width py-2 text-sm">${m}</div>`).join("");
    b.className = msgs.length ? "bg-yellow-20 text-gray-100 border-b border-yellow-30" : "hidden";
  }
  function renderSignedOut() {
    $("#view").innerHTML = `<div class="max-w-xl mx-auto py-16"><h1 class="text-2xl font-semibold mb-3">Please sign in</h1><p class="mb-4">This portal must be opened through the company sign-in page. If you reached it directly, go back to the address your platform team gave you.</p><p class="text-sm text-gray-70 dark:text-gray-30">Administrators: the portal expects an authenticating proxy that sets <code>X-Forwarded-User</code>, or <code>-dev-user</code> for local development.</p></div>`;
  }

  /* ------------------------------------------------------------------ views */
  async function viewNotFound() { return `<h1 class="text-2xl font-semibold mb-2">Page not found</h1><p><a href="#/home">Back to Home</a></p>`; }

  async function viewHome() {
    let running = 0, total = 0;
    try { const r = await api("GET", "/api/workspaces"); state.workspaces = r.workspaces; total = r.workspaces.length; running = r.workspaces.filter((w) => w.status === "Running").length; } catch (e) { /* not onboarded */ }
    const me = state.me;
    return `
<section class="grid lg:grid-cols-5 gap-8 items-start">
  <div class="lg:col-span-3">
    <p class="text-sm text-blue-60 dark:text-blue-40 font-medium mb-2">Developer Workspaces</p>
    <h1 class="text-3xl md:text-4xl font-light leading-tight mb-4">Your own Linux computer in the cloud, <span class="font-semibold">ready in about a minute.</span></h1>
    <p class="text-lg text-gray-70 dark:text-gray-30 max-w-2xl mb-6">Pick the programming languages and tools you want, point it at your code, and connect with VS Code. Your files stay between sessions. When you stop working, it switches itself off.</p>
    <div class="flex flex-wrap gap-3">
      <a href="#/create" class="btn btn-primary">Create a workspace</a>
      <a href="#/guides/start" class="btn btn-outline">Start here: a 3-minute guide</a>
    </div>
  </div>
  <div class="lg:col-span-2 card p-5">
    <h2 class="font-semibold mb-3">Your account</h2>
    <dl class="text-sm grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
      <dt class="text-gray-70 dark:text-gray-30">Signed in as</dt><dd class="truncate">${esc(me.user.name)}</dd>
      <dt class="text-gray-70 dark:text-gray-30">Your area</dt><dd><code>${esc(me.namespace)}</code> ${T("namespace", "")}</dd>
      <dt class="text-gray-70 dark:text-gray-30">Set up</dt><dd>${me.onboarded ? '<span class="pill pill-ok">yes</span>' : '<span class="pill pill-warn">not yet</span>'}</dd>
      <dt class="text-gray-70 dark:text-gray-30">Lab access</dt><dd>${me.canLab ? '<span class="pill pill-ok">yes</span> hardware &amp; kernel profiles' : '<span class="pill pill-muted">no</span> <button type="button" class="link-btn text-sm" data-topic="privileged">what is this?</button>'}</dd>
      <dt class="text-gray-70 dark:text-gray-30">Workspaces</dt><dd>${total} total, ${running} running · <a href="#/workspaces">manage</a></dd>
      <dt class="text-gray-70 dark:text-gray-30">Downloads</dt><dd>${me.artifactory ? `Artifactory connected` : `Artifactory (catalog)`} · <button type="button" class="link-btn text-sm" data-topic="artifactory">what is Artifactory?</button></dd>
    </dl>
  </div>
</section>

<section class="mt-12">
  <h2 class="text-xl font-semibold mb-4">How it works, in four steps</h2>
  <div class="grid sm:grid-cols-2 lg:grid-cols-4 gap-3">
    ${[
      ["1", "Choose", "Languages, tools, size — from a catalog the platform team keeps in " + T("artifactory", "Artifactory") + ".", "image"],
      ["2", "Create", "The portal writes a few Kubernetes objects into your private area and a " + T("pod") + " starts.", "yaml"],
      ["3", "Connect", "VS Code or a terminal over " + T("ssh", "SSH") + ". Your laptop key opens it; nothing is on the public internet.", "proxycommand"],
      ["4", "Keep working", "Files live on your " + T("volume", "home volume") + ". Stop, start, or let the " + T("idle", "idle timeout") + " do it.", "volume"],
    ].map(([n, h, p, topic]) => `<div class="tile"><div class="step-num mb-3">${n}</div><h3 class="font-semibold mb-1">${h}</h3><p class="text-sm text-gray-70 dark:text-gray-30">${p}</p><button type="button" class="link-btn text-sm mt-2" data-topic="${topic}">Learn more</button></div>`).join("")}
  </div>
</section>

<section class="mt-12 grid md:grid-cols-3 gap-3">
  <a href="#/create" class="tile tile-link"><h3 class="font-semibold mb-1">Create a workspace →</h3><p class="text-sm text-gray-70 dark:text-gray-30">Seven short steps. Nothing happens until the last one.</p></a>
  <a href="#/workspaces" class="tile tile-link"><h3 class="font-semibold mb-1">My workspaces →</h3><p class="text-sm text-gray-70 dark:text-gray-30">Start, stop, connect, add keys, read boot logs.</p></a>
  <a href="#/guides" class="tile tile-link"><h3 class="font-semibold mb-1">Guides &amp; tutorials →</h3><p class="text-sm text-gray-70 dark:text-gray-30">Step by step, in plain words, with links to the official docs.</p></a>
</section>`;
  }

  /* ---------------------------------------------------------- workspaces list */
  async function viewWorkspaces() {
    const r = await api("GET", "/api/workspaces?shared=true");
    state.workspaces = r.workspaces; state.sharedWithMe = r.sharedWithMe || [];
    state.poll = setInterval(async () => {
      if (state.route.page !== "workspaces" || state.route.arg) return;
      try { const r2 = await api("GET", "/api/workspaces"); state.workspaces = r2.workspaces; const el = $("#ws-list"); if (el) el.innerHTML = workspaceCards(); } catch (e) { /* keep last */ }
    }, 8000);
    return `
<div class="flex flex-wrap items-end justify-between gap-3 mb-6">
  <div><h1 class="text-2xl md:text-3xl font-semibold">My workspaces</h1><p class="text-gray-70 dark:text-gray-30 text-sm mt-1">In <code>${esc(state.me.namespace)}</code>${state.me.canLab ? ` and <code>${esc(state.me.labNamespace)}</code>` : ""} · refreshes every few seconds</p></div>
  <a href="#/create" class="btn btn-primary">Create a workspace</a>
</div>
<div id="ws-list">${workspaceCards()}</div>
${state.sharedWithMe.length ? `<h2 class="text-xl font-semibold mt-10 mb-1">Shared with me ${T("share", "")}</h2><p class="text-sm text-gray-70 dark:text-gray-30 mb-4">Workspaces other developers gave you SSH access to. Connect with <code>wsctl ssh OWNER/NAME</code> or <code>workspace ssh OWNER/NAME</code>.</p>
<div class="grid md:grid-cols-2 gap-4">${state.sharedWithMe.map((w) => `<article class="card p-5 flex flex-col gap-2"><div class="flex items-start justify-between gap-3"><div><a href="#/workspaces/${esc(w.name)}?owner=${esc(w.owner)}" class="text-lg font-semibold no-underline hover:underline">${esc(w.owner)}/${esc(w.name)}</a><div class="text-xs text-gray-70 dark:text-gray-30 mt-0.5">${esc(w.namespace)}</div></div>${statusPill(w.status)}</div><div class="flex flex-wrap gap-1">${(w.languages || []).map((l) => `<span class="chip">${esc(langLabel(l))}</span>`).join("")}</div><pre class="cmd text-xs mt-1"><code>wsctl ssh ${esc(w.owner)}/${esc(w.name)}</code></pre></article>`).join("")}</div>` : ""}`;
  }
  function workspaceCards() {
    if (!state.workspaces.length) return `<div class="card p-8 text-center"><p class="text-lg mb-2">No workspaces yet.</p><p class="text-gray-70 dark:text-gray-30 mb-4">Create one in a couple of minutes — the <a href="#/guides/first-workspace">first-workspace guide</a> walks you through it.</p><a href="#/create" class="btn btn-primary">Create a workspace</a></div>`;
    return `<div class="grid md:grid-cols-2 gap-4">${state.workspaces.map((w) => `
<article class="card p-5 flex flex-col gap-3">
  <div class="flex items-start justify-between gap-3">
    <div><a href="#/workspaces/${esc(w.name)}" class="text-lg font-semibold no-underline hover:underline">${esc(w.name)}</a><div class="text-xs text-gray-70 dark:text-gray-30 mt-0.5">${esc(w.namespace)}${w.tier === "lab" ? ' · <span class="pill pill-info">lab</span>' : ""}</div></div>
    ${statusPill(w.status)}
  </div>
  <div class="flex flex-wrap gap-1">${(w.languages || []).map((l, i) => `<span class="chip">${esc(langLabel(l))}${i === 0 && w.languages.length > 1 ? " (main)" : ""}</span>`).join("")}${(w.tools || []).filter((t) => !(w.languages || []).includes(t)).slice(0, 6).map((t) => `<span class="chip opacity-80">${esc(t)}</span>`).join("")}${(w.tools || []).length > 6 ? `<span class="chip opacity-60">+${w.tools.length - 6}</span>` : ""}</div>
  <dl class="text-sm grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-gray-70 dark:text-gray-30">
    <dt>Profile</dt><dd class="text-gray-100 dark:text-gray-10">${esc(w.profile)}</dd>
    <dt>Idle timeout</dt><dd class="text-gray-100 dark:text-gray-10">${esc(w.idleTimeout || "-")}</dd>
    <dt>Last active</dt><dd class="text-gray-100 dark:text-gray-10">${w.lastActive ? esc(ago(w.lastActive)) : "—"}</dd>
    <dt>Home drive</dt><dd class="text-gray-100 dark:text-gray-10"><a href="#/drives">${esc(w.drive || "")}</a>${w.storage ? ` · ${esc(storageLabel(w.storage))}` : ""}${(w.mounts || []).length ? ` · +${w.mounts.length} shared` : ""}</dd>
    ${w.waiting ? `<dt>Waiting</dt><dd class="text-red-60 dark:text-red-50">${esc(w.waiting)}</dd>` : ""}
  </dl>
  <div class="flex flex-wrap gap-2 mt-auto pt-2">
    <a href="#/workspaces/${esc(w.name)}" class="btn btn-secondary btn-sm">Open &amp; connect</a>
    ${w.status === "Stopped" ? `<button type="button" class="btn btn-primary btn-sm" data-action="ws-start" data-arg="${esc(w.name)}">Start</button>` : `<button type="button" class="btn btn-outline btn-sm" data-action="ws-stop" data-arg="${esc(w.name)}">Stop</button>`}
    <button type="button" class="btn btn-ghost btn-sm" data-action="ws-delete" data-arg="${esc(w.name)}">Delete…</button>
  </div>
</article>`).join("")}</div>`;
  }

  /* ---------------------------------------------------------- workspace detail */
  async function viewWorkspace(name) {
    const owner = state.route.query.owner || "";
    const qs = owner ? `?owner=${encodeURIComponent(owner)}` : "";
    const d = await api("GET", `/api/workspaces/${encodeURIComponent(name)}${qs}`);
    state.detail = d;
    const w = d.workspace;
    const ref = owner ? `${owner}/${name}` : name;
    const guest = !!w.owner;
    const isNew = state.route.query.new === "1";
    if (w.status === "Starting" || w.status === "Pending") {
      state.poll = setInterval(async () => {
        if (state.route.page !== "workspaces" || state.route.arg !== name) return;
        try { const d2 = await api("GET", `/api/workspaces/${encodeURIComponent(name)}${qs}`); if (d2.workspace.status !== w.status) { route(); } } catch (e) { /* ignore */ }
      }, 5000);
    }
    const spec = w.spec || {};
    if (!guest) { try { state.detail.shares = (await api("GET", `/api/workspaces/${encodeURIComponent(name)}/shares`)).shares; } catch (e) { state.detail.shares = []; } }
    const sshBlock = `Host ws-${name}
    User dev
    HostName ws-${name}
    HostKeyAlias ws-${name}
    ProxyCommand workspace --namespace ${w.namespace} proxy ${name}
    IdentityFile ~/.ssh/id_ed25519
    IdentitiesOnly yes
    StrictHostKeyChecking yes
    ConnectTimeout 240`;
    const connectTabs = [
      ["wsctl", "wsctl (no kubectl)", `
<p class="mb-2">The <code>wsctl</code> command talks only to this portal — no kubeconfig, no <code>kubectl</code>. ${T("wsctl", "What is wsctl?")}</p>
<pre class="cmd"><code>wsctl login --portal ${esc(location.origin)}
wsctl ssh ${esc(ref)}          # starts it if stopped, then a shell
wsctl code ${esc(ref)}         # VS Code Remote-SSH on /workspace</code></pre>
<p class="text-sm">Behind the scenes <code>wsctl proxy</code> opens a tunnel through the portal to the workspace's SSH server, so the same SSH keys and host key apply.</p>`],
      ["vscode", "VS Code", `
<ol class="prose">
  <li>On your laptop, once per workspace: <pre class="cmd"><code>workspace ssh-config ${esc(name)}</code></pre> This writes the SSH entry <code>ws-${esc(name)}</code>, registers this laptop's key and pins the ${T("hostkey", "host key")}.</li>
  <li>In VS Code set <code>remote.SSH.connectTimeout</code> to <strong>180</strong> (a stopped workspace needs up to a minute to start).</li>
  <li><kbd>F1</kbd> → <em>Remote-SSH: Connect to Host…</em> → <code>ws-${esc(name)}</code>, then open <code>/workspace</code>. Or from a terminal: <pre class="cmd"><code>code --remote ssh-remote+ws-${esc(name)} /workspace</code></pre></li>
</ol>
<p class="text-sm"><a href="#/guides/connect-vscode">Full guide</a> · no <code>workspace</code> command yet? <button type="button" class="link-btn" data-topic="cli">What it is and where to get it</button>.</p>`],
      ["terminal", "Terminal", `
<pre class="cmd"><code>workspace ssh ${esc(name)}        # starts it if stopped
ssh ws-${esc(name)}               # after ssh-config</code></pre>
<p class="text-sm mb-2">Prefer to edit <code>~/.ssh/config</code> yourself? Paste this block (uses your default key):</p>
<pre class="cmd"><code>${esc(sshBlock)}</code></pre>${copyBtn(sshBlock, "Copy SSH config block")}
${d.ssh && d.ssh.knownHostsLine ? `<p class="text-sm mt-3 mb-1">Host key (add to <code>~/.ssh/known_hosts</code> to skip the first-connection question; fingerprint <code>${esc(d.ssh.hostKeyFingerprint)}</code>):</p><pre class="cmd"><code>${esc(d.ssh.knownHostsLine)}</code></pre>${copyBtn(d.ssh.knownHostsLine, "Copy known_hosts line")}` : ""}`],
      ["public", "Public address", w.expose ? `<p class="mb-2">This workspace also listens on a public load-balancer address (key authentication only):</p><pre class="cmd"><code>ssh -p 22 dev@${esc(d.publicAddr || "<pending>")}</code></pre><p class="text-sm">Compare the host key fingerprint on first connect: <code>${esc(d.ssh ? d.ssh.hostKeyFingerprint : "")}</code>.</p>` : `<p>This workspace has no public address (the default). It is reachable only through your Kubernetes login — which is what VS Code and the terminal tabs use. To add one, recreate the workspace with <em>Public SSH address</em> switched on in the Advanced step.</p>`],
    ];
    return `
<nav class="text-sm mb-3"><a href="#/workspaces">My workspaces</a> › <span>${esc(name)}</span></nav>
<div class="flex flex-wrap items-start justify-between gap-3 mb-6">
  <div><h1 class="text-2xl md:text-3xl font-semibold flex items-center gap-3">${esc(name)} ${statusPill(w.status)}</h1>
  <p class="text-sm text-gray-70 dark:text-gray-30 mt-1"><code>${esc(w.namespace)}</code> · ${esc(w.image)}${w.node ? ` · node ${esc(w.node)}` : ""}${w.startedAt ? ` · started ${esc(ago(w.startedAt))}` : ""}</p></div>
  <div class="flex flex-wrap gap-2">
    ${w.status === "Stopped" ? `<button type="button" class="btn btn-primary" data-action="ws-start" data-arg="${esc(ref)}">Start</button>` : guest ? "" : `<button type="button" class="btn btn-outline" data-action="ws-stop" data-arg="${esc(name)}">Stop</button>`}
    ${guest ? `<span class="pill pill-info self-center">shared with you by ${esc(w.owner)}</span>` : `<button type="button" class="btn btn-secondary" data-action="ws-recreate" data-arg="${esc(name)}">Recreate with changes</button>
    <button type="button" class="btn btn-ghost" data-action="ws-delete" data-arg="${esc(name)}">Delete…</button>`}
  </div>
</div>
${isNew ? `<div class="callout callout-ok mb-6"><strong>Your workspace is ${w.status === "Running" ? "ready" : "being created"}.</strong> ${w.status === "Running" ? "Connect using one of the tabs below." : "The first start takes about a minute: creating the disk, unpacking the image, installing tools, cloning your code. This page refreshes by itself."}${d.deployKey ? ` <br><strong>One more thing for Git:</strong> add the deploy key shown below to your repository so the workspace can clone and push.` : ""}</div>` : ""}
${w.waiting ? `<div class="callout callout-warn mb-6"><strong>Waiting:</strong> ${esc(w.waiting)} — <a href="#/guides/troubleshooting">what this means</a></div>` : ""}

<div class="grid lg:grid-cols-3 gap-6">
  <section class="lg:col-span-2 card p-5">
    <h2 class="text-lg font-semibold mb-3">Connect</h2>
    <div class="flex gap-1 border-b border-gray-20 dark:border-gray-80 mb-4" role="tablist">
      ${connectTabs.map(([id, label], i) => `<button type="button" role="tab" aria-selected="${i === 0}" class="nav-link ${i === 0 ? "" : ""}" data-action="tab" data-arg="${id}" ${i === 0 ? 'aria-current="page"' : ""}>${label}</button>`).join("")}
    </div>
    ${connectTabs.map(([id, , html], i) => `<div data-tab="${id}" class="${i === 0 ? "" : "hidden"} prose max-w-none">${html}</div>`).join("")}
  </section>

  <aside class="flex flex-col gap-6">
    ${d.deployKey ? `<section class="card p-5"><h2 class="font-semibold mb-2">Deploy key for Git ${T("deploykey", "")}</h2><p class="text-sm text-gray-70 dark:text-gray-30 mb-2">Add this public key to <code>${esc(spec.repo || "your repository")}</code> (deploy keys, allow write) or to your Git account.</p><pre class="cmd text-xs break-all whitespace-pre-wrap"><code>${esc(d.deployKey)}</code></pre>${copyBtn(d.deployKey, "Copy deploy key")}</section>` : ""}
    <section class="card p-5">
      <h2 class="font-semibold mb-2">Details</h2>
      <dl class="text-sm grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
        <dt class="text-gray-70 dark:text-gray-30">Languages</dt><dd>${(w.languages || []).map(langLabel).map(esc).join(", ")}</dd>
        <dt class="text-gray-70 dark:text-gray-30">Tools</dt><dd>${(w.tools || []).map(esc).join(", ") || "none"}</dd>
        <dt class="text-gray-70 dark:text-gray-30">Profile</dt><dd>${esc(spec.profile)} · ${esc(spec.isolation)}${spec.multicast ? " · multicast" : ""}</dd>
        <dt class="text-gray-70 dark:text-gray-30">Size</dt><dd>${esc(spec.cpuRequest)}→${esc(spec.cpu)} CPU · ${esc(spec.memoryRequest)}→${esc(spec.memory)} · disk ${esc(spec.size)} ${T("cpu", "")}</dd>
        ${Object.keys(spec.devices || {}).length ? `<dt class="text-gray-70 dark:text-gray-30">Devices</dt><dd>${Object.entries(spec.devices).map(([k, v]) => `${esc(k)}=${esc(v)}`).join(", ")}</dd>` : ""}
        <dt class="text-gray-70 dark:text-gray-30">Home drive ${T("drive", "")}</dt><dd><a href="#/drives">${esc(w.drive || "")}</a>${w.storage ? ` · ${esc(storageLabel(w.storage))}` : ""}<span class="block text-xs text-gray-70 dark:text-gray-30">kept when the workspace is stopped or deleted</span></dd>
        ${(w.mounts || []).length ? `<dt class="text-gray-70 dark:text-gray-30">Shared drives</dt><dd>${w.mounts.map((m) => `<code>/shared/${esc(m.split(":")[1].split("/").pop())}</code>`).join(", ")}</dd>` : ""}
        <dt class="text-gray-70 dark:text-gray-30">Repo</dt><dd class="break-all">${esc(spec.repo || "—")}${spec.branch ? ` @ ${esc(spec.branch)}` : ""}</dd>
        <dt class="text-gray-70 dark:text-gray-30">Created</dt><dd>${esc(spec.createdAt || "")}${spec.createdBy ? ` by ${esc(spec.createdBy)}` : ""}</dd>
      </dl>
    </section>
    <section class="card p-5">
      <h2 class="font-semibold mb-2">Idle timeout ${T("idle", "")}</h2>
      <form class="flex gap-2" data-action="idle-form" data-arg="${esc(name)}">
        <select class="select" name="idle" aria-label="Idle timeout">${["30m", "1h", "2h", "4h", "8h", "12h", "1d", "off"].map((v) => `<option ${v === (w.idleTimeout || "4h") ? "selected" : ""}>${v}</option>`).join("")}</select>
        <button class="btn btn-secondary btn-sm" type="submit">Save</button>
      </form>
    </section>
    <section class="card p-5">
      <h2 class="font-semibold mb-2">SSH keys that can log in ${T("sshkey", "")}</h2>
      <ul class="text-sm mb-3 space-y-1">${((d.ssh && d.ssh.authorizedKeys) || []).map((k) => `<li><code class="text-xs">${esc(k.fingerprint)}</code> <span class="text-gray-70 dark:text-gray-30">${esc(k.comment || k.type)}</span></li>`).join("") || "<li>none</li>"}</ul>
      <form data-action="addkey-form" data-arg="${esc(name)}">
        <textarea class="textarea" name="key" rows="2" placeholder="ssh-ed25519 AAAA… you@laptop" aria-label="Public key to add"></textarea>
        <button class="btn btn-secondary btn-sm mt-2" type="submit">Add key</button>
        <p class="help">Takes effect within a minute, no restart. <button type="button" class="link-btn" data-topic="sshkey">How do I get my public key?</button></p>
      </form>
    </section>
  </aside>
</div>

${guest ? "" : `<div class="grid lg:grid-cols-2 gap-6 mt-6">
  <section class="card p-5">
    <h2 class="text-lg font-semibold mb-1">Network ${T("link", "")}</h2>
    <p class="text-sm text-gray-70 dark:text-gray-30 mb-3">Other programs reach this workspace at this name. Your own workspaces in the same namespace need nothing else.</p>
    <dl class="text-sm grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 mb-3">
      <dt class="text-gray-70 dark:text-gray-30">DNS name</dt><dd><code class="break-all">${esc((d.network || {}).dns || "")}</code> ${copyBtn((d.network || {}).dns || "", "Copy")}</dd>
      <dt class="text-gray-70 dark:text-gray-30">Pod IP</dt><dd>${esc((d.network || {}).podIP || "— (stopped)")}</dd>
      <dt class="text-gray-70 dark:text-gray-30">Same namespace</dt><dd>${((d.network || {}).sameNamespace || []).map((n) => `<a href="#/workspaces/${esc(n)}">${esc(n)}</a>`).join(", ") || "<span class='text-gray-70 dark:text-gray-30'>no other workspaces</span>"} <span class="text-xs text-gray-70 dark:text-gray-30">— already reachable on every port</span></dd>
      <dt class="text-gray-70 dark:text-gray-30">Multicast</dt><dd>${(d.network || {}).multicast && d.network.multicast.mode !== "off" ? `on · ${esc(d.network.multicast.mode)} · ${esc(d.network.multicast.iface)}` : "off"}</dd>
      <dt class="text-gray-70 dark:text-gray-30">Mesh</dt><dd>${esc((d.network || {}).mesh || "none")}</dd>
    </dl>
    <h3 class="font-semibold text-sm mb-1">Links to other developers' workspaces</h3>
    <ul class="text-sm mb-3 space-y-1">${((d.network || {}).links || []).map((l) => `<li class="flex items-center gap-2 flex-wrap"><code>${esc(l.peer)}</code> ${l.status === "linked" ? '<span class="pill pill-ok">linked</span>' : '<span class="pill pill-warn">waiting for them to link back</span>'} <span class="text-xs text-gray-70 dark:text-gray-30">ports ${(l.ports || []).join(", ") || "all"} · ${esc(l.peerDNS)}</span> <button type="button" class="link-btn text-xs" data-action="link-rm" data-arg="${esc(name)}|${esc(l.peer)}">remove</button></li>`).join("") || "<li class='text-gray-70 dark:text-gray-30'>none</li>"}</ul>
    ${(d.network || {}).linksEnabled === false ? "" : `<form class="flex flex-wrap gap-2" data-action="link-form" data-arg="${esc(name)}">
      <input class="input flex-1 min-w-[12rem]" name="peer" placeholder="bob@corp.example/api" aria-label="Peer OWNER/WORKSPACE" required>
      <input class="input !w-28" name="ports" placeholder="ports" aria-label="Ports, comma separated">
      <button class="btn btn-secondary btn-sm" type="submit">Link</button>
      <p class="help w-full">Allows traffic between this workspace and theirs once <em>they</em> link back to <code>${esc(state.me.user.name)}/${esc(name)}</code>. <a href="#/guides/networking">How workspaces talk to each other</a></p>
    </form>`}
  </section>
  <section class="card p-5">
    <h2 class="text-lg font-semibold mb-1">Sharing ${T("share", "")}</h2>
    <p class="text-sm text-gray-70 dark:text-gray-30 mb-3">Give a teammate SSH access to this exact workspace (pair programming, debugging together). They log in as <code>dev</code>, see everything you see.</p>
    <ul class="text-sm mb-3 space-y-1" id="share-list">${(state.detail.shares || []).map((sh) => `<li class="flex items-center gap-2"><span>${esc(sh.user)}</span> <button type="button" class="link-btn text-xs" data-action="share-rm" data-arg="${esc(name)}|${esc(sh.user)}">revoke</button></li>`).join("") || "<li class='text-gray-70 dark:text-gray-30'>not shared</li>"}</ul>
    ${state.me.sharing === false ? `<p class="text-sm text-gray-70 dark:text-gray-30">Sharing is switched off on this portal.</p>` : `<form class="flex flex-wrap gap-2" data-action="share-form" data-arg="${esc(name)}">
      <input class="input flex-1 min-w-[12rem]" name="user" placeholder="bob@corp.example" aria-label="Teammate" required>
      <button class="btn btn-secondary btn-sm" type="submit">Share</button>
      <textarea class="textarea w-full" name="key" rows="2" placeholder="Their SSH public key — only needed if they have no profile yet"></textarea>
      <p class="help w-full">They connect with <code>wsctl ssh ${esc(state.me.user.short)}/${esc(name)}</code>. Revoke any time; the key is removed within a minute.</p>
    </form>`}
  </section>
</div>`}

<section class="card p-5 mt-6">
  <div class="flex items-center justify-between mb-2"><h2 class="text-lg font-semibold">Boot log</h2><button type="button" class="btn btn-ghost btn-sm" data-action="logs" data-arg="${esc(name)}">Refresh</button></div>
  <p class="text-sm text-gray-70 dark:text-gray-30 mb-2">What happened when the workspace started: tools installed, repository cloned, and any failures.</p>
  <pre id="logbox" class="cmd text-xs max-h-80 overflow-auto"><code>Loading…</code></pre>
</section>`;
  }
  async function loadLogs(name) {
    const box = $("#logbox code"); if (!box) return;
    try { const r = await api("GET", `/api/workspaces/${encodeURIComponent(name)}/logs?tail=300`); box.textContent = r.logs || r.note || "(empty)"; $("#logbox").scrollTop = 1e9; }
    catch (e) { box.textContent = "Could not load logs: " + e.message; }
  }

  /* ---------------------------------------------------------- tools */
  /* ------------------------------------------------------------------ storage helpers */
  function storageChoices(kind) { return (((state.me || {}).storage || {}).choices || state.catalog.storage && state.catalog.storage.choices || []).filter((c) => (c.kind || "home") === kind); }
  function storageLabel(name) { const c = storageChoices("home").concat(storageChoices("shared")).find((x) => x.name === name); return c ? c.label || c.name : (name || "default"); }
  function storageStep(d) {
    const field = (label, inner, help) => `<label class="field"><span class="lbl">${label}</span>${inner}${help ? `<p class="help">${help}</p>` : ""}</label>`;
    const home = storageChoices("home");
    const drives = state.drives || [];
    const detached = drives.filter((x) => x.kind === "home" && !x.attachedTo);
    const snaps = state.snapshots || [];
    const mine = drives.filter((x) => x.kind === "shared"), theirs = drives.filter((x) => x.kind === "shared-mount");
    const sel = d.fromDrive ? `drive:${d.fromDrive}` : d.fromSnapshot ? `snap:${d.fromSnapshot}` : "new";
    return `
<h3 class="font-semibold mb-1 mt-2">Home drive ${T("drive", "")}</h3>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-3">Your files live on a drive that outlives the workspace: stopping, deleting, even a broken pod never touch it. Only <em>Delete drive</em> on the Drives page does.</p>
${home.length ? `<div class="grid sm:grid-cols-3 gap-2 mb-4">${home.map((c) => `<label class="check-card ${(d.storage || home.find((h) => h.default) && home.find((h) => h.default).name) === c.name ? "selected" : ""}"><input type="radio" name="storage" data-field="storage" value="${esc(c.name)}" ${(d.storage || (home.find((h) => h.default) || {}).name) === c.name ? "checked" : ""}><span><span class="font-semibold">${esc(c.label || c.name)}</span><span class="block text-sm text-gray-70 dark:text-gray-30">${esc(c.description || "")}</span></span></label>`).join("")}</div>` : ""}
${d.recreating ? `<div class="callout callout-info mb-4">Recreating keeps <code>ws-${esc(d.recreating)}-home</code> exactly as it is.</div>` : `${field("Start from", `<select class="select" data-field="startFrom"><option value="new" ${sel === "new" ? "selected" : ""}>A new, empty drive</option>${detached.map((x) => `<option value="drive:${esc(x.name)}" ${sel === "drive:" + x.name ? "selected" : ""}>Re-attach drive ${esc(x.name)} (${esc(x.size)}, last used by ${esc(x.lastWorkspace || "?")}${x.languages && x.languages.length ? ", " + x.languages.join("+") : ""})</option>`).join("")}${snaps.filter((x) => x.readyToUse).map((x) => `<option value="snap:${esc(x.name)}" ${sel === "snap:" + x.name ? "selected" : ""}>New drive from snapshot ${esc(x.name)} (${esc(x.drive)})</option>`).join("")}</select>`, detached.length || snaps.length ? "Detached drives are the files of workspaces you deleted; snapshots are copies you took on the Drives page." : "You have no detached drives or snapshots yet — they appear here when you do.")}`}
${mine.length || theirs.length ? `<h3 class="font-semibold mb-1">Shared drives to mount ${T("shared-drive", "")}</h3><div class="grid sm:grid-cols-2 gap-2 mb-4">${mine.map((x) => { const v = `shared:${x.name.replace(/^shared-/, "")}`; const on = (d.mounts || []).includes(v); return `<label class="check-card ${on ? "selected" : ""}"><input type="checkbox" data-field="mount" value="${esc(v)}" ${on ? "checked" : ""}><span><span class="font-mono text-sm font-semibold">/shared/${esc(x.name.replace(/^shared-/, ""))}</span><span class="block text-sm text-gray-70 dark:text-gray-30">yours · ${esc(x.size)}${x.mountedBy && x.mountedBy.length ? " · also in " + x.mountedBy.join(", ") : ""}</span></span></label>`; }).join("")}${theirs.map((x) => { const nm = x.name.replace(new RegExp("^shared-" + x.sharedBy + "-"), ""); const v = `shared:${x.sharedBy}/${nm}`; const on = (d.mounts || []).includes(v); return `<label class="check-card ${on ? "selected" : ""}"><input type="checkbox" data-field="mount" value="${esc(v)}" ${on ? "checked" : ""}><span><span class="font-mono text-sm font-semibold">/shared/${esc(nm)}</span><span class="block text-sm text-gray-70 dark:text-gray-30">shared by ${esc(x.sharedBy)} · ${esc(x.size)}</span></span></label>`; }).join("")}</div>` : `<p class="text-sm text-gray-70 dark:text-gray-30 mb-4">No shared drives yet — create one on the <a href="#/drives">Drives page</a> to have a folder several workspaces (or teammates) see at the same time.</p>`}`;
  }

  /* ------------------------------------------------------------------ drives page */
  async function viewDrives() {
    const r = await api("GET", "/api/drives");
    state.drives = r.drives;
    const home = r.drives.filter((x) => x.kind === "home"), shared = r.drives.filter((x) => x.kind === "shared"), mounts = r.drives.filter((x) => x.kind === "shared-mount");
    const details = {};
    for (const x of home) { try { const d = await api("GET", `/api/drives/${encodeURIComponent(x.name)}?ns=${encodeURIComponent(x.namespace)}`); details[x.name] = d.snapshots || []; } catch (e) { details[x.name] = []; } }
    state.snapshots = Object.values(details).flat();
    const sharedChoices = storageChoices("shared");
    const row = (x) => `<tr class="border-t border-gray-20 dark:border-gray-80 align-top"><td class="py-2 pr-3 font-mono text-sm">${esc(x.name)}<div class="text-xs text-gray-70 dark:text-gray-30 font-sans">${esc(x.namespace)}</div></td><td class="py-2 pr-3 text-sm">${esc(x.size)}<div class="text-xs text-gray-70 dark:text-gray-30">${esc(storageLabel(x.storage))}</div></td><td class="py-2 pr-3 text-sm">${x.attachedTo ? `attached to <a href="#/workspaces/${esc(x.attachedTo)}">${esc(x.attachedTo)}</a>` : `<span class="pill pill-warn">detached</span><div class="text-xs text-gray-70 dark:text-gray-30 mt-1">last used by ${esc(x.lastWorkspace || "?")}${x.languages && x.languages.length ? " · " + x.languages.join(", ") : ""}</div>`}</td><td class="py-2 text-sm"><div class="flex flex-wrap gap-2">${x.attachedTo ? "" : `<button type="button" class="btn btn-primary btn-sm" data-action="drive-use" data-arg="${esc(x.name)}">New workspace on it</button>`}<button type="button" class="btn btn-secondary btn-sm" data-action="drive-snapshot" data-arg="${esc(x.name)}">Snapshot</button><form class="inline-flex gap-1" data-action="drive-resize-form" data-arg="${esc(x.name)}"><input class="input !w-24 !py-1" name="size" placeholder="100Gi" aria-label="New size"><button class="btn btn-ghost btn-sm" type="submit">Grow</button></form>${x.attachedTo ? "" : `<button type="button" class="btn btn-ghost btn-sm text-red-60" data-action="drive-delete" data-arg="${esc(x.name)}">Delete drive…</button>`}</div>
      ${(details[x.name] || []).length ? `<ul class="mt-2 text-xs space-y-1">${details[x.name].map((sn) => `<li class="flex flex-wrap items-center gap-2"><span class="font-mono">${esc(sn.name)}</span> ${sn.readyToUse ? `<span class="pill pill-ok">ready</span> ${esc(sn.restoreSize || "")}` : sn.error ? `<span class="pill pill-danger">failed</span> ${esc(sn.error)}` : '<span class="pill pill-info">in progress</span>'} <span class="text-gray-70 dark:text-gray-30">${esc(ago(sn.createdAt || ""))}</span>${sn.readyToUse ? `<button type="button" class="link-btn" data-action="snapshot-use" data-arg="${esc(sn.name)}">restore into a new workspace</button>` : ""}<button type="button" class="link-btn" data-action="snapshot-delete" data-arg="${esc(sn.name)}">delete</button></li>`).join("")}</ul>` : ""}</td></tr>`;
    return `
<div class="flex flex-wrap items-end justify-between gap-3 mb-2"><div><h1 class="text-2xl md:text-3xl font-semibold">Drives ${T("drive", "")}</h1><p class="text-sm text-gray-70 dark:text-gray-30 mt-1">Where your files actually live. A workspace is a computer you can throw away; its drive is the thing worth keeping — and it is kept, always, unless you delete it here.</p></div></div>
<div class="callout callout-info mb-6"><strong>The rule:</strong> stop, delete, crash, upgrade — none of these touch a home drive. A deleted workspace leaves a <em>detached</em> drive you can put a new workspace on (even with different languages). Snapshots are extra copies; Delete drive is the only thing that erases files. ${T("snapshot", "About snapshots")}</div>

<section class="card p-5 mb-6"><h2 class="text-lg font-semibold mb-2">Home drives</h2>
${home.length ? `<div class="overflow-x-auto"><table class="w-full text-left"><thead><tr class="text-xs uppercase text-gray-70 dark:text-gray-30"><th class="pb-2 pr-3">Drive</th><th class="pb-2 pr-3">Size</th><th class="pb-2 pr-3">State</th><th class="pb-2">Actions · snapshots</th></tr></thead><tbody>${home.map(row).join("")}</tbody></table></div>` : `<p class="text-gray-70 dark:text-gray-30">No home drives yet — every workspace you create gets one.</p>`}</section>

<div class="grid lg:grid-cols-2 gap-6">
<section class="card p-5"><h2 class="text-lg font-semibold mb-1">Shared drives ${T("shared-drive", "")}</h2>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-3">A folder several workspaces mount at the same time under <code>/shared/&lt;name&gt;</code> — yours, or your teammates' if you share it.</p>
${shared.length ? `<ul class="space-y-3 mb-4">${shared.map((x) => `<li class="border-t border-gray-20 dark:border-gray-80 pt-3"><div class="flex flex-wrap items-center gap-2"><span class="font-mono text-sm font-semibold">/shared/${esc(x.name.replace(/^shared-/, ""))}</span><span class="text-xs text-gray-70 dark:text-gray-30">${esc(x.size)} · ${esc(x.name)}${x.mountedBy && x.mountedBy.length ? " · mounted by " + x.mountedBy.map(esc).join(", ") : ""}</span>${x.mountedBy && x.mountedBy.length ? "" : `<button type="button" class="btn btn-ghost btn-sm text-red-60 ml-auto" data-action="drive-delete" data-arg="${esc(x.name)}">Delete…</button>`}</div>
  <div class="text-sm mt-1">Shared with: ${(x.sharedWith || []).map((g) => `<span class="chip">${esc(g)} <button type="button" class="link-btn text-xs" data-action="drive-share-rm" data-arg="${esc(x.name)}|${esc(g)}">×</button></span>`).join(" ") || "<span class='text-gray-70 dark:text-gray-30'>nobody</span>"}</div>
  ${state.me.sharing === false ? "" : `<form class="flex flex-wrap gap-2 mt-2" data-action="drive-share-form" data-arg="${esc(x.name)}"><input class="input flex-1 min-w-[10rem] !py-1" name="user" placeholder="bob@corp.example" aria-label="Share with" required><label class="inline-flex items-center gap-1 text-sm"><input type="checkbox" name="ro"> read-only</label><button class="btn btn-secondary btn-sm" type="submit">Share</button></form>`}</li>`).join("")}</ul>` : `<p class="text-sm text-gray-70 dark:text-gray-30 mb-4">None yet.</p>`}
${sharedChoices.length ? `<form class="flex flex-wrap gap-2 items-end" data-action="drive-create-form"><div class="flex-1 min-w-[10rem]"><label class="label">Name</label><input class="input" name="name" placeholder="team-data" required></div><div><label class="label">Size</label><input class="input !w-28" name="size" value="100Gi"></div>${sharedChoices.length > 1 ? `<div><label class="label">Storage</label><select class="select" name="storage">${sharedChoices.map((c) => `<option value="${esc(c.name)}" ${c.default ? "selected" : ""}>${esc(c.label || c.name)}</option>`).join("")}</select></div>` : ""}<button class="btn btn-primary" type="submit">Create shared drive</button></form>` : `<p class="text-sm text-gray-70 dark:text-gray-30">This cluster has no ReadWriteMany storage configured for shared drives (ask the platform team; on Azure it is Azure Files).</p>`}
</section>
<section class="card p-5"><h2 class="text-lg font-semibold mb-1">Shared with me</h2>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-3">Drives teammates let you mount. Tick them in the Size step when creating a workspace.</p>
${mounts.length ? `<ul class="space-y-2">${mounts.map((x) => { const nm = x.name.replace(new RegExp("^shared-" + x.sharedBy + "-"), ""); return `<li class="text-sm"><span class="font-mono font-semibold">/shared/${esc(nm)}</span> <span class="text-gray-70 dark:text-gray-30">from ${esc(x.sharedBy)} · ${esc(x.size)}</span><pre class="cmd text-xs mt-1"><code>wsctl create api --lang go --mount shared:${esc(x.sharedBy)}/${esc(nm)}</code></pre></li>`; }).join("")}</ul>` : `<p class="text-sm text-gray-70 dark:text-gray-30">Nothing shared with you yet.</p>`}
</section>
</div>
<p class="text-sm mt-6"><a href="#/guides/drives">Guide: your files, drives and snapshots</a> · CLI: <code>wsctl drives list</code> / <code>workspace drives list</code></p>`;
  }

  async function viewTools() {
    state.tools = state.tools || (await api("GET", "/api/artifactory/tools"));
    const groups = {};
    for (const t of state.tools.tools) (groups[t.category] = groups[t.category] || []).push(t);
    return `
<h1 class="text-2xl md:text-3xl font-semibold mb-2">Tools catalog</h1>
<p class="text-gray-70 dark:text-gray-30 max-w-3xl mb-4">Everything you can tick in the <em>Tools</em> step. Tools marked <span class="tag-src">artifactory</span> are downloaded from ${T("artifactory", "Artifactory")} when the workspace boots; the dot shows whether the file is there right now. Tools marked <span class="tag-src">image</span> are installed by a script that comes with the image. Each card links to the tool's official documentation.</p>
<div class="flex flex-wrap gap-3 items-center mb-6"><input id="tool-search" type="search" class="input max-w-sm" placeholder="Search tools…" aria-label="Search tools"><span class="text-sm text-gray-70 dark:text-gray-30">${state.tools.tools.length} tools · <a href="${esc(state.tools.artifactoryUrl || "#")}" target="_blank" rel="noopener">open Artifactory ↗</a></span></div>
<div id="tool-groups">${Object.keys(groups).sort().map((g) => `
<section class="mb-8" data-group="${esc(g)}"><h2 class="text-lg font-semibold capitalize mb-3">${esc(g)}</h2>
<div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-3">${groups[g].map(toolCard).join("")}</div></section>`).join("")}</div>
<p class="text-sm text-gray-70 dark:text-gray-30">Missing something? <a href="#/guides/languages-tools">How tools are added to the catalog.</a></p>`;
  }
  function toolCard(t) {
    const av = t.availability;
    const dot = t.source === "artifactory" ? (av ? (av.available ? '<span class="pill pill-ok" title="file found in Artifactory">available</span>' : `<span class="pill pill-danger" title="${esc(av.error || "")}">not found</span>`) : '<span class="pill pill-muted">not checked</span>') : "";
    const ver = t.artifact && t.artifact.version ? `<span class="chip">v${esc(t.artifact.version)}</span>` : "";
    const vers = av && av.versions && av.versions.length > 1 ? `<span class="text-xs text-gray-70 dark:text-gray-30">also: ${av.versions.slice(0, 4).map(esc).join(", ")}</span>` : "";
    const inDraft = state.draft.tools.includes(t.name);
    return `<article class="card p-4 flex flex-col gap-2" data-tool="${esc(t.name)}">
  <div class="flex items-start justify-between gap-2"><h3 class="font-semibold font-mono text-sm">${esc(t.name)}</h3><span class="tag-src">${t.source === "artifactory" ? "artifactory" : t.builtin ? "built in" : "image"}</span></div>
  <p class="text-sm text-gray-70 dark:text-gray-30 flex-1">${esc(t.description)}</p>
  <div class="flex flex-wrap items-center gap-2">${dot}${ver}${vers}</div>
  <div class="flex items-center gap-2 pt-1">
    ${t.docs ? `<a class="text-sm" href="${esc(t.docs)}" target="_blank" rel="noopener">Official docs ↗</a>` : ""}
    ${t.builtin ? "" : `<button type="button" class="btn btn-ghost btn-sm ml-auto" data-action="draft-tool" data-arg="${esc(t.name)}">${inDraft ? "Remove from new workspace" : "Add to new workspace"}</button>`}
  </div></article>`;
  }

  /* ---------------------------------------------------------- guides */
  async function viewGuides() {
    const g = window.CONTENT.guides;
    return `
<h1 class="text-2xl md:text-3xl font-semibold mb-2">Guides &amp; tutorials</h1>
<p class="text-gray-70 dark:text-gray-30 max-w-3xl mb-6">Written for someone who has never used a cloud workspace. Every unfamiliar word is a link to a short explanation with the official documentation. Start at the top and go down.</p>
<ol class="grid md:grid-cols-2 gap-3">${g.map((x, i) => `<li><a href="#/guides/${x.id}" class="tile tile-link h-full"><div class="flex items-center gap-3 mb-2"><span class="text-2xl" aria-hidden="true">${x.icon}</span><span class="text-xs uppercase tracking-wide text-gray-70 dark:text-gray-30">${i + 1} · ${esc(x.level)} · ${x.minutes} min</span></div><h2 class="font-semibold">${esc(x.title)}</h2><p class="text-sm text-gray-70 dark:text-gray-30 mt-1">${esc(x.summary)}</p></a></li>`).join("")}</ol>
<section class="mt-10 card p-5"><h2 class="font-semibold mb-2">Official references</h2><ul class="grid sm:grid-cols-2 gap-x-6 gap-y-1 text-sm">
${[["VS Code Remote-SSH", "https://code.visualstudio.com/docs/remote/ssh"], ["OpenSSH manual pages", "https://www.openssh.com/manual.html"], ["Pro Git (free book)", "https://git-scm.com/book/en/v2"], ["Kubernetes concepts", "https://kubernetes.io/docs/concepts/"], ["JFrog Artifactory", "https://jfrog.com/help/r/jfrog-artifactory-documentation"], ["Ubuntu command line basics", "https://ubuntu.com/tutorials/command-line-for-beginners"], ["Linux kernel docs", "https://www.kernel.org/doc/html/latest/"], ["IP multicast (RFC 1112)", "https://www.rfc-editor.org/rfc/rfc1112"]].map(([l, u]) => `<li><a href="${u}" target="_blank" rel="noopener">${l} ↗</a></li>`).join("")}</ul></section>`;
  }
  async function viewGuide(id) {
    const all = window.CONTENT.guides;
    const i = all.findIndex((g) => g.id === id);
    if (i < 0) return viewNotFound();
    const g = all[i], prev = all[i - 1], next = all[i + 1];
    return `
<nav class="text-sm mb-3"><a href="#/guides">Guides</a> › <span>${esc(g.title)}</span></nav>
<div class="flex items-center gap-3 mb-1"><span class="text-3xl" aria-hidden="true">${g.icon}</span><span class="text-xs uppercase tracking-wide text-gray-70 dark:text-gray-30">${esc(g.level)} · about ${g.minutes} minutes</span></div>
<h1 class="text-2xl md:text-3xl font-semibold mb-2">${esc(g.title)}</h1>
<p class="text-lg text-gray-70 dark:text-gray-30 mb-6 max-w-3xl">${esc(g.summary)}</p>
<div class="grid lg:grid-cols-[1fr_16rem] gap-8">
  <div>${g.sections.map((s, n) => `<section class="step-card" id="s${n + 1}"><div class="step-num" aria-hidden="true">${n + 1}</div><div class="prose max-w-none"><h2 class="text-lg font-semibold mb-2">${s.h}</h2>${s.body}</div></section>`).join("")}
  <div class="flex flex-wrap justify-between gap-3 mt-8 pt-6 border-t border-gray-20 dark:border-gray-80">${prev ? `<a href="#/guides/${prev.id}" class="btn btn-outline">← ${esc(prev.title)}</a>` : "<span></span>"}${next ? `<a href="#/guides/${next.id}" class="btn btn-primary">${esc(next.title)} →</a>` : `<a href="#/create" class="btn btn-primary">Create a workspace →</a>`}</div></div>
  <aside class="hidden lg:block"><div class="sticky top-20 text-sm"><p class="font-semibold mb-2">On this page</p><ol class="space-y-1 text-gray-70 dark:text-gray-30">${g.sections.map((s, n) => `<li><a href="#/guides/${g.id}#s${n + 1}" data-action="jump" data-arg="s${n + 1}" class="no-underline hover:underline">${n + 1}. ${s.h.replace(/<[^>]+>/g, "")}</a></li>`).join("")}</ol><p class="mt-6"><a href="#/create" class="btn btn-secondary btn-sm">Try it: create a workspace</a></p></div></aside>
</div>`;
  }
  async function viewGlossary() {
    const t = window.CONTENT.topics;
    return `<h1 class="text-2xl md:text-3xl font-semibold mb-2">Glossary</h1><p class="text-gray-70 dark:text-gray-30 mb-6 max-w-3xl">Short explanations of every term used in this portal, each with links to the official documentation. Click a card.</p>
<div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-3">${Object.entries(t).map(([id, x]) => `<button type="button" class="tile tile-link text-left" data-topic="${id}"><h2 class="font-semibold">${esc(x.title)}</h2><p class="text-sm text-gray-70 dark:text-gray-30 mt-1">${esc(x.short)}</p></button>`).join("")}</div>`;
  }

  /* ---------------------------------------------------------- profile */
  async function viewProfile() {
    state.profile = await api("GET", "/api/profile");
    const p = state.profile;
    return `
<h1 class="text-2xl md:text-3xl font-semibold mb-2">Profile</h1>
<p class="text-gray-70 dark:text-gray-30 mb-6 max-w-3xl">Settings that every new workspace picks up. Saved as a Kubernetes Secret in your own area (<code>${esc(state.me.namespace)}/workspace-profile</code>) — nobody else can read it.</p>
<form class="grid lg:grid-cols-2 gap-8" data-action="profile-form">
  <section class="card p-5">
    <h2 class="font-semibold mb-1">SSH public keys ${T("sshkey", "")}</h2>
    <p class="text-sm text-gray-70 dark:text-gray-30 mb-3">One per line. These keys may log in to every workspace you create. Get yours with <code>cat ~/.ssh/id_ed25519.pub</code> (<a href="#/guides/ssh-key">guide</a>).</p>
    <textarea class="textarea" name="keys" rows="5" placeholder="ssh-ed25519 AAAA… you@laptop">${esc(p.sshPublicKeys.join("\n"))}</textarea>
    <p class="help">${p.sshPublicKeys.length} key(s) saved.</p>
  </section>
  <section class="card p-5">
    <h2 class="font-semibold mb-1">Git identity</h2>
    <p class="text-sm text-gray-70 dark:text-gray-30 mb-3">Written into <code>~/.gitconfig</code> on first boot, so your commits carry your name.</p>
    <label class="field"><span class="lbl">Name</span><input class="input" name="gitName" value="${esc(p.gitName)}" autocomplete="name"></label>
    <label class="field"><span class="lbl">E-mail</span><input class="input" name="gitEmail" value="${esc(p.gitEmail)}" autocomplete="email" type="email"></label>
    <h2 class="font-semibold mb-1 mt-6">Artifactory login (optional) ${T("artifactory", "")}</h2>
    <p class="text-sm text-gray-70 dark:text-gray-30 mb-3">Only needed if some repositories require sign-in. Create an <em>identity token</em> in Artifactory (your avatar → Edit profile → Generate). The token is stored write-only: it is never shown again here.</p>
    <label class="field"><span class="lbl">Artifactory user</span><input class="input" name="afUser" value="${esc(p.artifactoryUser)}" autocomplete="off"></label>
    <label class="field"><span class="lbl">Token ${p.hasArtifactoryToken ? '<span class="pill pill-ok">saved</span>' : ""}</span><input class="input" name="afToken" type="password" autocomplete="new-password" placeholder="${p.hasArtifactoryToken ? "leave empty to keep the saved token" : "paste a token"}"></label>
    <button class="btn btn-primary mt-2" type="submit">Save profile</button>
  </section>
</form>`;
  }

  /* ================================================================== wizard */
  function emptyDraft() {
    return { step: 0, name: "", languages: [], tools: [], tag: "", image: "", repo: "", branch: "", gitAuth: "deploy-key", gitToken: "", gitName: "", gitEmail: "", dotfiles: "",
      sizePreset: "medium", cpu: "", memory: "", size: "", cpuRequest: "", memoryRequest: "", idleTimeout: "4h", tz: (Intl.DateTimeFormat().resolvedOptions().timeZone || ""),
      profile: "standard", isolation: "", multicast: false, devices: {}, hostPaths: "", caps: "", expose: false, extraKeys: "", recreating: "",
      storage: "", fromDrive: "", fromSnapshot: "", mounts: [], project: "" };
  }
  function loadDraft() {
    try { const d = JSON.parse(localStorage.getItem("ws.draft")); if (d && typeof d === "object") return { ...emptyDraft(), ...d }; } catch (e) { /* ignore */ }
    return emptyDraft();
  }
  function saveDraft() { try { localStorage.setItem("ws.draft", JSON.stringify(state.draft)); } catch (e) { /* ignore */ } }
  function resetDraft() { state.draft = emptyDraft(); saveDraft(); }

  function draftToRequest() {
    const d = state.draft, preset = SIZE_PRESETS[d.sizePreset] || {};
    const custom = d.sizePreset === "custom";
    const req = {
      name: d.name.trim(), languages: d.languages, tools: d.tools, profile: d.profile, isolation: d.isolation || undefined,
      image: d.image || undefined, tag: d.tag || undefined, repo: d.repo.trim(), branch: d.branch.trim(), dotfiles: d.dotfiles.trim(),
      gitAuth: d.repo.trim() ? d.gitAuth : "none", gitToken: d.gitAuth === "token" ? d.gitToken : undefined, gitName: d.gitName.trim(), gitEmail: d.gitEmail.trim(),
      size: custom ? d.size : preset.size, cpu: custom ? d.cpu : preset.cpu, memory: custom ? d.memory : preset.memory,
      cpuRequest: custom ? d.cpuRequest : preset.cpuRequest, memoryRequest: custom ? d.memoryRequest : preset.memoryRequest,
      multicast: d.multicast, devices: d.devices, hostPaths: d.hostPaths.split(/[\s,]+/).filter(Boolean), caps: d.caps.split(/[\s,]+/).filter(Boolean),
      expose: d.expose, idleTimeout: d.idleTimeout, tz: d.tz, sshPublicKeys: d.extraKeys.split("\n").map((s) => s.trim()).filter(Boolean),
      storage: d.storage || undefined, fromDrive: d.fromDrive || undefined, fromSnapshot: d.fromSnapshot || undefined,
      mounts: d.mounts && d.mounts.length ? d.mounts : undefined, namespace: d.project ? `ws-proj-${d.project}` : undefined,
    };
    for (const k of Object.keys(req)) if (req[k] === undefined || req[k] === "") delete req[k];
    return req;
  }
  function stepErrors(step) {
    const d = state.draft, errs = [];
    if (step === 0) {
      if (!d.languages.length) errs.push("Tick at least one language (or 'No language' for plain Linux).");
      const n = d.name.trim() || d.languages[0] || "";
      if (!NAME_RE.test(n)) errs.push("The name may contain lowercase letters, digits and dashes (max 40), and must start and end with a letter or digit.");
    }
    if (step === 2 && d.repo.trim() && d.gitAuth === "token" && !d.gitToken.trim()) errs.push("Paste a token, or choose Deploy key.");
    if (step === 3 && d.sizePreset === "custom") for (const [k, v] of [["cpu", d.cpu], ["memory", d.memory], ["size", d.size]]) if (!/^[0-9]+(\.[0-9]+)?(m|Mi|Gi|Ti)?$/.test(v)) errs.push(`Custom ${k}: use a number with a unit, e.g. 4, 500m, 8Gi.`);
    if (step === 4) {
      const lab = (state.catalog.profiles[d.profile] || {}).tier === "lab";
      if (lab && !state.me.canLab) errs.push("Hardware and kernel profiles need lab access. Ask the platform team.");
      if (!lab && (Object.keys(d.devices).length || d.hostPaths.trim() || d.caps.trim())) errs.push("Devices, host paths and capabilities need the Hardware lab or Kernel lab profile.");
    }
    if (step === 5) {
      const profileKeys = state.profile ? state.profile.sshPublicKeys.length : 0;
      const extra = d.extraKeys.split("\n").map((s) => s.trim()).filter(Boolean);
      if (!profileKeys && !extra.length) errs.push("Add at least one SSH public key — without it nobody can log in to the workspace.");
      for (const k of extra) if (!/^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) [A-Za-z0-9+/]+=*( .*)?$/.test(k)) errs.push(`This does not look like a public key: "${k.slice(0, 30)}…"`);
    }
    return errs;
  }

  async function viewCreate() {
    if (!state.me.onboarded) return `<h1 class="text-2xl font-semibold mb-3">Create a workspace</h1><div class="callout callout-warn">Your account is not set up yet. Ask a platform admin to run <code>workspace admin onboard ${esc(state.me.user.name)}</code>, then come back. Meanwhile, read <a href="#/guides/start">Start here</a>.</div>`;
    if (!state.profile) { try { state.profile = await api("GET", "/api/profile"); } catch (e) { state.profile = { sshPublicKeys: [] }; } }
    if (!state.tools) { try { state.tools = await api("GET", "/api/artifactory/tools"); } catch (e) { state.tools = { tools: [] }; } }
    if (!state.images) { try { state.images = await api("GET", "/api/artifactory/images"); } catch (e) { state.images = { images: {} }; } }
    try { const dr = await api("GET", "/api/drives"); state.drives = dr.drives; const snaps = []; for (const x of dr.drives.filter((y) => y.kind === "home" && y.snapshots > 0)) { try { snaps.push(...((await api("GET", `/api/drives/${encodeURIComponent(x.name)}?ns=${encodeURIComponent(x.namespace)}`)).snapshots || [])); } catch (e) { /* ignore */ } } state.snapshots = snaps; } catch (e) { state.drives = state.drives || []; state.snapshots = state.snapshots || []; }
    const d = state.draft;
    if (!d.gitName && state.profile.gitName) d.gitName = state.profile.gitName;
    if (!d.gitEmail && state.profile.gitEmail) d.gitEmail = state.profile.gitEmail;
    return `
<div class="flex flex-wrap items-end justify-between gap-3 mb-4">
  <div><h1 class="text-2xl md:text-3xl font-semibold">${d.recreating ? `Recreate <span class="font-mono">${esc(d.recreating)}</span>` : "Create a workspace"}</h1>
  <p class="text-sm text-gray-70 dark:text-gray-30 mt-1">${d.recreating ? "Your disk and files are kept; the running machine is replaced with the new settings." : "Seven short steps. Nothing is created until you press the button on the last one."} Your answers are saved in this browser as you go.</p></div>
  <button type="button" class="btn btn-ghost btn-sm" data-action="draft-reset">Start over</button>
</div>
<div class="progress mb-2" role="list" aria-label="Steps">${STEPS.map((s, i) => `<button type="button" role="listitem" class="progress-step ${i < d.step ? "done" : ""} ${i === d.step ? "current" : ""}" data-action="wiz-goto" data-arg="${i}" ${i > d.step ? "disabled" : ""} aria-current="${i === d.step ? "step" : "false"}" title="${s}">${i + 1}<span class="lbl">. ${s}</span></button>`).join("")}</div>
<p class="sm:hidden text-sm font-semibold mb-4">Step ${d.step + 1} of ${STEPS.length}: ${STEPS[d.step]}</p><div class="hidden sm:block mb-4"></div>
<div id="wiz-errors" class="mb-4"></div>
<form id="wiz" class="card p-5 md:p-6" novalidate>${wizardStep(d.step)}</form>
<div class="flex flex-wrap justify-between gap-3 mt-4">
  <button type="button" class="btn btn-secondary" data-action="wiz-prev" ${d.step === 0 ? "disabled" : ""}>← Back</button>
  ${d.step < STEPS.length - 1 ? `<button type="button" class="btn btn-primary" data-action="wiz-next">Next: ${STEPS[d.step + 1]} →</button>` : `<button type="button" class="btn btn-primary" data-action="wiz-create" id="create-btn">${d.recreating ? "Recreate workspace" : "Create workspace"}</button>`}
</div>`;
  }

  function wizardStep(step) {
    const d = state.draft, cat = state.catalog;
    const field = (label, inner, help) => `<label class="field"><span class="lbl">${label}</span>${inner}${help ? `<p class="help">${help}</p>` : ""}</label>`;
    switch (step) {
      case 0: {
        const primary = d.languages[0];
        const image = primary ? `${cat.registry}/${cat.languages[primary].image}` : "";
        const comboName = d.languages.length > 1 ? "workspace-" + [...d.languages].sort().join("-") : "";
        const combo = comboName && state.images.images[comboName] ? comboName : "";
        const tags = primary ? (state.images.images[cat.languages[primary].image] || [cat.tag]) : [];
        return `
<h2 class="text-lg font-semibold mb-1">Basics</h2>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-4">A short name, and the languages you want. Tick as many as you like — the <strong>first one you tick is the main language</strong> and decides the starting ${T("image")}; the others are added when the workspace boots.</p>
${field("Workspace name", `<input class="input" data-field="name" value="${esc(d.name)}" placeholder="${esc(primary || "my-project")}" autocomplete="off" spellcheck="false" ${d.recreating ? "readonly" : ""}>`, "Lowercase letters, digits and dashes. Shows up as <code>ws-&lt;name&gt;</code> in SSH and VS Code. Leave empty to use the main language's name.")}
${(state.me.projects || []).length ? field(`Where does it live? ${T("project", "")}`, `<select class="select" data-field="project" ${d.recreating ? "disabled" : ""}><option value="" ${!d.project ? "selected" : ""}>My own namespace (${esc(state.me.namespace)})</option>${state.me.projects.map((p) => `<option value="${esc(p.name)}" ${d.project === p.name ? "selected" : ""}>Project ${esc(p.name)} (${esc(p.namespace)}) — shared with ${p.members.length + p.groups.length} member${p.members.length + p.groups.length === 1 ? "" : "s"}</option>`).join("")}</select>`, "In a project namespace every member sees this workspace and their workspaces can talk to it directly.") : ""}
<fieldset class="mb-4"><legend class="text-sm text-gray-70 dark:text-gray-30 mb-2">Languages</legend>
<div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-2">${[...cat.languagesOrder.filter((l) => l !== "base"), ...cat.languagesOrder.filter((l) => l === "base")].map((id) => { const l = cat.languages[id]; const on = d.languages.includes(id); const main = d.languages[0] === id; return `
<label class="check-card ${on ? "selected" : ""}"><input type="checkbox" data-field="lang" value="${id}" ${on ? "checked" : ""}><span class="flex-1"><span class="font-semibold">${esc(l.label || id)}</span> ${main && d.languages.length > 1 ? '<span class="pill pill-info">main</span>' : ""}<span class="block text-sm text-gray-70 dark:text-gray-30">${esc(l.description || "")}</span><span class="block mt-1 text-sm">${l.docs ? `<a href="${esc(l.docs)}" target="_blank" rel="noopener">docs ↗</a>` : ""}${l.learn ? ` · <a href="${esc(l.learn)}" target="_blank" rel="noopener">learn ↗</a>` : ""}${on && !main ? ` · <button type="button" class="link-btn" data-action="lang-main" data-arg="${id}">make main</button>` : ""}</span></span></label>`; }).join("")}</div></fieldset>
${primary ? `<div class="callout callout-info text-sm"><strong>Image:</strong> <code>${esc(combo ? cat.registry + "/" + combo : image)}:${esc(d.tag || cat.tag)}</code>${combo ? " — a pre-built image with exactly these languages exists in Artifactory, so it will be used." : d.languages.length > 1 ? ` — plus ${d.languages.slice(1).map(langLabel).map(esc).join(", ")} installed at boot.` : ""}
<label class="inline-flex items-center gap-2 ml-2">version <select class="select !w-auto !py-1" data-field="tag">${tags.map((t) => `<option ${t === (d.tag || cat.tag) ? "selected" : ""}>${esc(t)}</option>`).join("")}</select></label></div>` : ""}`;
      }
      case 1: {
        const tools = state.tools.tools.filter((t) => !t.builtin && !cat.languagesOrder.includes(t.name));
        const groups = {}; for (const t of tools) (groups[t.category] = groups[t.category] || []).push(t);
        return `
<h2 class="text-lg font-semibold mb-1">Tools</h2>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-4">Optional. Tick what you use; they are installed while the workspace boots and come back after every restart. <span class="tag-src">artifactory</span> tools are files downloaded from ${T("artifactory", "Artifactory")}; <span class="tag-src">image</span> tools run an install script. Git is always included.</p>
<div class="flex flex-wrap gap-3 items-center mb-3"><input type="search" class="input max-w-xs" id="tool-filter" placeholder="Filter…" aria-label="Filter tools"><span class="text-sm">${d.tools.length} selected${d.tools.length ? ": " + d.tools.map(esc).join(", ") : ""}</span></div>
${Object.keys(groups).sort().map((g) => `<h3 class="text-sm font-semibold uppercase tracking-wide text-gray-70 dark:text-gray-30 mt-4 mb-2">${esc(g)}</h3><div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-2">${groups[g].map((t) => { const on = d.tools.includes(t.name); const av = t.availability; return `
<label class="check-card ${on ? "selected" : ""}" data-tool-card="${esc(t.name)} ${esc(t.description)}"><input type="checkbox" data-field="tool" value="${esc(t.name)}" ${on ? "checked" : ""}><span class="flex-1 min-w-0"><span class="font-mono text-sm font-semibold">${esc(t.name)}</span> <span class="tag-src">${t.source === "artifactory" ? "artifactory" : "image"}</span>${t.artifact && t.artifact.version ? ` <span class="chip">v${esc(t.artifact.version)}</span>` : ""}${av && !av.available ? ' <span class="pill pill-danger">not in Artifactory</span>' : ""}<span class="block text-sm text-gray-70 dark:text-gray-30">${esc(t.description)}</span>${t.docs ? `<a class="text-sm" href="${esc(t.docs)}" target="_blank" rel="noopener">docs ↗</a>` : ""}</span></label>`; }).join("")}</div>`).join("")}`;
      }
      case 2:
        return `
<h2 class="text-lg font-semibold mb-1">Code</h2>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-4">Optional. Give a ${T("git", "Git")} repository and it is cloned into <code>/workspace</code> on the first start.</p>
${field("Repository URL", `<input class="input" data-field="repo" value="${esc(d.repo)}" placeholder="git@github.com:org/repo.git" autocomplete="off" spellcheck="false">`, "Use the SSH form (<code>git@…</code>) so you can push later. HTTPS works with an access token.")}
<div class="grid sm:grid-cols-2 gap-4">${field("Branch (optional)", `<input class="input" data-field="branch" value="${esc(d.branch)}" placeholder="main">`)}${field("Dotfiles repository (optional)", `<input class="input" data-field="dotfiles" value="${esc(d.dotfiles)}" placeholder="https://github.com/you/dotfiles">`, `Your personal shell/editor settings; <code>install.sh</code> runs on first boot. ${T("dotfiles", "")}`)}</div>
<fieldset class="mb-4 ${d.repo.trim() ? "" : "opacity-60"}"><legend class="text-sm text-gray-70 dark:text-gray-30 mb-2">How should Git log in?</legend>
<div class="grid sm:grid-cols-3 gap-2">${[["deploy-key", "Deploy key (recommended)", "The workspace gets its own key; you add the public half to the repo. Nothing from your laptop goes to the cluster.", "deploykey"], ["token", "Access token", "For https:// URLs. Stored as a Secret, handed to Git only when asked.", "token"], ["none", "No login", "Public repositories, or you will log in yourself later."]].map(([v, l, h, topic]) => `<label class="check-card ${d.gitAuth === v ? "selected" : ""}"><input type="radio" name="gitAuth" data-field="gitAuth" value="${v}" ${d.gitAuth === v ? "checked" : ""}><span><span class="font-semibold">${l}</span><span class="block text-sm text-gray-70 dark:text-gray-30">${h} ${topic ? T(topic, "") : ""}</span></span></label>`).join("")}</div></fieldset>
${d.gitAuth === "token" ? field("Access token", `<input class="input" type="password" data-field="gitToken" value="${esc(d.gitToken)}" autocomplete="off">`, "Needs read and write access to the repository. It is sent once, over HTTPS, and stored only in your namespace.") : ""}
<div class="grid sm:grid-cols-2 gap-4">${field("Your name for commits", `<input class="input" data-field="gitName" value="${esc(d.gitName)}" autocomplete="name">`)}${field("Your e-mail for commits", `<input class="input" data-field="gitEmail" value="${esc(d.gitEmail)}" autocomplete="email">`)}</div>`;
      case 3:
        return `
<h2 class="text-lg font-semibold mb-1">Size ${T("cpu", "")}</h2>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-4">How much computer you get. <em>Medium</em> suits most projects. You can always recreate with a bigger size; the disk can grow but not shrink.</p>
<div class="grid sm:grid-cols-2 lg:grid-cols-5 gap-2 mb-4">${Object.entries(SIZE_PRESETS).map(([k, p]) => `<label class="check-card ${d.sizePreset === k ? "selected" : ""}"><input type="radio" name="size" data-field="sizePreset" value="${k}" ${d.sizePreset === k ? "checked" : ""}><span><span class="font-semibold">${p.label}</span><span class="block text-sm text-gray-70 dark:text-gray-30">${p.cpu ? `${p.cpu} CPU · ${p.memory} RAM · ${p.size} disk` : "your values"}</span><span class="block text-xs text-gray-70 dark:text-gray-30 mt-1">${p.hint}</span></span></label>`).join("")}</div>
${d.sizePreset === "custom" ? `<div class="grid sm:grid-cols-3 gap-4">${field("CPU limit", `<input class="input" data-field="cpu" value="${esc(d.cpu)}" placeholder="4">`, "cores, e.g. 2 or 500m")}${field("Memory limit", `<input class="input" data-field="memory" value="${esc(d.memory)}" placeholder="8Gi">`)}${field("Disk", `<input class="input" data-field="size" value="${esc(d.size)}" placeholder="30Gi">`)}${field("CPU request", `<input class="input" data-field="cpuRequest" value="${esc(d.cpuRequest)}" placeholder="1">`, "guaranteed share")}${field("Memory request", `<input class="input" data-field="memoryRequest" value="${esc(d.memoryRequest)}" placeholder="2Gi">`)}</div>` : ""}
${storageStep(d)}
<div class="grid sm:grid-cols-2 gap-4">${field(`Idle timeout ${T("idle", "")}`, `<select class="select" data-field="idleTimeout">${["30m", "1h", "2h", "4h", "8h", "12h", "1d", "off"].map((v) => `<option ${v === d.idleTimeout ? "selected" : ""}>${v}</option>`).join("")}</select>`, "Switch off after this long with nobody connected. Your files are kept.")}${field("Time zone", `<input class="input" data-field="tz" value="${esc(d.tz)}" placeholder="UTC">`, "For timestamps inside the workspace.")}</div>`;
      case 4: {
        const profiles = cat.profilesOrder.filter((p) => state.me.canLab || (cat.profiles[p].tier || "standard") !== "lab");
        const lab = (cat.profiles[d.profile] || {}).tier === "lab";
        return `
<h2 class="text-lg font-semibold mb-1">Advanced</h2>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-4">Most people skip this page. It is for multicast networking, hardware and driver work, and a public SSH address.</p>
<fieldset class="mb-4"><legend class="text-sm text-gray-70 dark:text-gray-30 mb-2">Profile ${T("privileged", "")}</legend>
<div class="grid sm:grid-cols-3 gap-2">${profiles.map((p) => { const pr = cat.profiles[p]; return `<label class="check-card ${d.profile === p ? "selected" : ""}"><input type="radio" name="profile" data-field="profile" value="${p}" ${d.profile === p ? "checked" : ""}><span><span class="font-semibold">${esc(pr.label || p)}</span> ${pr.tier === "lab" ? '<span class="pill pill-info">lab</span>' : ""}<span class="block text-sm text-gray-70 dark:text-gray-30">${esc(pr.description || "")}</span></span></label>`; }).join("")}</div>
${!state.me.canLab ? `<p class="help">Hardware and kernel profiles are hidden because your account has no lab access. <button type="button" class="link-btn" data-topic="driver">What is the lab tier?</button></p>` : ""}</fieldset>
<div class="grid sm:grid-cols-2 gap-4">
${field(`Sandbox ${T("sandbox", "")}`, `<select class="select" data-field="isolation"><option value="">profile default</option>${cat.isolationOrder.map((i) => `<option value="${i}" ${d.isolation === i ? "selected" : ""}>${i} — ${esc((cat.isolation[i].description || "").split(".")[0])}</option>`).join("")}</select>`)}
<div class="field"><span class="lbl">Network &amp; access</span>
<label class="check-card ${d.multicast ? "selected" : ""} mb-2"><input type="checkbox" data-field="multicast" ${d.multicast ? "checked" : ""}><span><span class="font-semibold">Multicast network</span> ${T("multicast", "")}<span class="block text-sm text-gray-70 dark:text-gray-30">Adds a second interface (<code>net1</code>) that carries multicast. Tick the <strong>mcast</strong> tool to test it.</span></span></label>
<label class="check-card ${d.expose ? "selected" : ""}"><input type="checkbox" data-field="expose" ${d.expose ? "checked" : ""}><span><span class="font-semibold">Public SSH address</span><span class="block text-sm text-gray-70 dark:text-gray-30">Also publish SSH on a load balancer (key login only). Needed only for devices or CI runners that cannot use your Kubernetes login.</span></span></label></div></div>
<fieldset class="mb-2 ${lab ? "" : "opacity-60"}"><legend class="text-sm text-gray-70 dark:text-gray-30 mb-2">Hardware ${T("deviceplugin", "")} ${lab ? "" : "— choose a lab profile first"}</legend>
<div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-2">${cat.devicesOrder.map((r) => { const dv = cat.devices[r]; const on = r in d.devices; return `<label class="check-card ${on ? "selected" : ""}"><input type="checkbox" data-field="device" value="${esc(r)}" ${on ? "checked" : ""} ${lab ? "" : "disabled"}><span class="min-w-0"><span class="font-semibold">${esc(dv.label || r)}</span><span class="block text-xs font-mono text-gray-70 dark:text-gray-30 truncate">${esc(r)} = ${esc(dv.quantity || "1")}</span>${dv.docs ? `<a class="text-sm" href="${esc(dv.docs)}" target="_blank" rel="noopener">docs ↗</a>` : ""}</span></label>`; }).join("")}</div>
<div class="grid sm:grid-cols-2 gap-4 mt-3">${field("Extra host paths", `<input class="input" data-field="hostPaths" value="${esc(d.hostPaths)}" placeholder="/dev /sys/bus/pci" ${lab ? "" : "disabled"}>`, "Space-separated. Allowed: /dev, /sys, /lib/modules, /usr/src, /lib/firmware, /run/udev.")}${field("Extra capabilities (kernel profile)", `<input class="input" data-field="caps" value="${esc(d.caps)}" placeholder="SYS_RAWIO" ${lab ? "" : "disabled"}>`)}</div></fieldset>`;
      }
      case 5: {
        const keys = state.profile.sshPublicKeys || [];
        return `
<h2 class="text-lg font-semibold mb-1">Connect: who may log in ${T("sshkey", "")}</h2>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-4">SSH uses keys instead of passwords. The workspace will accept the keys saved in your profile plus any you paste here. <strong>Without at least one key, nobody can log in.</strong></p>
<div class="card p-4 mb-4"><h3 class="font-semibold text-sm mb-2">Keys from your profile (${keys.length})</h3>${keys.length ? `<ul class="text-sm space-y-1">${keys.map((k) => `<li class="font-mono text-xs break-all">${esc(k.slice(0, 32))}… <span class="text-gray-70 dark:text-gray-30 font-sans">${esc(k.split(/\s+/).slice(2).join(" "))}</span></li>`).join("")}</ul>` : `<p class="text-sm">None yet — paste one below, and save it in your <a href="#/profile">Profile</a> so you do not have to do this again.</p>`}</div>
${field("Additional public keys (one per line)", `<textarea class="textarea" data-field="extraKeys" rows="3" placeholder="ssh-ed25519 AAAA… you@laptop">${esc(d.extraKeys)}</textarea>`, `Get yours with <code>cat ~/.ssh/id_ed25519.pub</code>. No key yet? <a href="#/guides/ssh-key">Make one in 5 minutes</a>.`)}
<div class="callout callout-info text-sm">After creating, run <code>workspace ssh-config &lt;name&gt;</code> once on each laptop you use; it writes the SSH settings VS Code needs. ${T("proxycommand", "Why?")}</div>`;
      }
      case 6: {
        const r = draftToRequest();
        const rows = [["Name", r.name || d.languages[0]], ["Languages", d.languages.map(langLabel).join(", ") + (d.languages.length > 1 ? ` (main: ${langLabel(d.languages[0])})` : "")], ["Tools", d.tools.join(", ") || "git only"],
          ["Image version", d.tag || cat.tag], ["Repository", r.repo ? `${r.repo}${r.branch ? " @ " + r.branch : ""} · login: ${r.gitAuth}` : "none"], ["Size", `${r.cpu} CPU · ${r.memory} RAM · ${r.size} disk`], ["Idle timeout", r.idleTimeout],
          ["Profile", `${d.profile}${r.isolation ? " · " + r.isolation : ""}${r.multicast ? " · multicast" : ""}${r.expose ? " · public SSH" : ""}`],
          ...(Object.keys(r.devices || {}).length ? [["Devices", Object.entries(r.devices).map(([k, v]) => `${k}=${v}`).join(", ")]] : []),
          ["Storage", `${storageLabel(r.storage)}${r.fromDrive ? ` · re-attach drive ${r.fromDrive}` : r.fromSnapshot ? ` · new drive from snapshot ${r.fromSnapshot}` : " · new drive"}${(r.mounts || []).length ? ` · mounts ${r.mounts.map((m) => "/shared/" + m.split(":")[1].split("/").pop()).join(", ")}` : ""}`],
          ...(r.namespace ? [["Namespace", `${r.namespace} (project)`]] : []),
          ["SSH keys", `${(state.profile.sshPublicKeys || []).length} from profile + ${(r.sshPublicKeys || []).length} pasted`]];
        return `
<h2 class="text-lg font-semibold mb-1">Review</h2>
<p class="text-sm text-gray-70 dark:text-gray-30 mb-4">Check the summary, then press the button below. ${d.recreating ? "The disk of the existing workspace is kept." : "The first start takes about a minute."}</p>
<table class="tbl mb-4"><tbody>${rows.map(([k, v]) => `<tr><th class="w-40">${esc(k)}</th><td>${esc(v)}</td></tr>`).join("")}</tbody></table>
<details class="text-sm"><summary class="cursor-pointer">What will be sent to the API</summary><pre class="cmd mt-2"><code>${esc(JSON.stringify({ ...r, gitToken: r.gitToken ? "••••" : undefined }, null, 2))}</code></pre></details>
<div id="create-progress" class="mt-4"></div>`;
      }
    }
    return "";
  }
  function showStepErrors(errs) {
    const box = $("#wiz-errors"); if (!box) return;
    box.innerHTML = errs.length ? `<div class="callout callout-danger"><ul class="list-disc pl-5">${errs.map((e) => `<li>${e}</li>`).join("")}</ul></div>` : "";
    if (errs.length) box.scrollIntoView({ block: "nearest" });
  }
  async function wizardCreate() {
    const errs = [0, 1, 2, 3, 4, 5].flatMap(stepErrors);
    if (errs.length) { showStepErrors(errs); return; }
    const btn = $("#create-btn"); btn.disabled = true; btn.innerHTML = '<span class="spinner"></span> Creating…';
    try {
      const r = await api("POST", "/api/workspaces", draftToRequest());
      toast(`Workspace ${r.name} is being created`, "ok");
      resetDraft();
      state.workspaces = [];
      location.hash = `#/workspaces/${encodeURIComponent(r.name)}?new=1`;
    } catch (e) {
      showStepErrors([esc(e.message)]);
      btn.disabled = false; btn.textContent = "Create workspace";
    }
  }
  function loadDraftFromWorkspace(w) {
    const spec = w.spec || {}, d = emptyDraft();
    d.recreating = w.name; d.name = w.name; d.languages = spec.languages || [spec.lang]; d.tools = (spec.tools || []).filter((t) => !d.languages.includes(t));
    d.repo = spec.repo || ""; d.branch = spec.branch || ""; d.dotfiles = spec.dotfiles || ""; d.gitAuth = spec.gitAuth === "ssh" ? "deploy-key" : (spec.gitAuth || "deploy-key"); d.gitName = spec.gitName || ""; d.gitEmail = spec.gitEmail || "";
    const preset = Object.entries(SIZE_PRESETS).find(([, p]) => p.cpu === spec.cpu && p.memory === spec.memory && p.size === spec.size);
    if (preset) d.sizePreset = preset[0]; else { d.sizePreset = "custom"; d.cpu = spec.cpu || ""; d.memory = spec.memory || ""; d.size = spec.size || ""; d.cpuRequest = spec.cpuRequest || ""; d.memoryRequest = spec.memoryRequest || ""; }
    d.idleTimeout = w.idleTimeout || spec.idleTimeout || "4h"; d.tz = spec.tz || d.tz; d.profile = spec.profile || "standard"; d.isolation = spec.isolation && spec.isolation !== (state.catalog.profiles[d.profile] || {}).isolation ? spec.isolation : "";
    d.multicast = !!spec.multicast; d.devices = spec.devices || {}; d.hostPaths = (spec.hostPaths || []).join(" "); d.caps = (spec.caps || []).join(" "); d.expose = !!spec.expose;
    const m = /:([^:/]+)$/.exec(spec.image || ""); if (m && m[1] !== state.catalog.tag) d.tag = m[1];
    d.storage = spec.storage || ""; d.mounts = spec.mounts || []; d.fromDrive = ""; d.fromSnapshot = "";
    d.project = /^ws-proj-(.+)$/.test(w.namespace || "") ? w.namespace.replace(/^ws-proj-/, "") : "";
    state.draft = d; saveDraft();
  }

  /* ================================================================== events */
  document.addEventListener("click", async (e) => {
    const topicBtn = e.target.closest("[data-topic]");
    if (topicBtn) { e.preventDefault(); openTopic(topicBtn.dataset.topic); return; }
    const el = e.target.closest("[data-action]");
    if (!el) return;
    const act = el.dataset.action, arg = el.dataset.arg;
    const d = state.draft;
    try {
      switch (act) {
        case "modal-close": closeModal(); break;
        case "modal-action": { const a = modal._actions[+arg]; closeModal(); if (a && a.run) a.run(); break; }
        case "copy": await copy(arg); break;
        case "tab": {
          const sec = el.closest("section");
          $$("[role=tab]", sec).forEach((b) => { const on = b.dataset.arg === arg; b.setAttribute("aria-selected", on); if (on) b.setAttribute("aria-current", "page"); else b.removeAttribute("aria-current"); });
          $$("[data-tab]", sec).forEach((p) => p.classList.toggle("hidden", p.dataset.tab !== arg));
          break;
        }
        case "jump": { e.preventDefault(); const t = document.getElementById(arg); if (t) t.scrollIntoView({ behavior: "smooth", block: "start" }); break; }
        case "logs": await loadLogs(arg); break;
        case "ws-start": { el.disabled = true; const [o, n] = arg.includes("/") ? arg.split("/") : ["", arg]; await api("POST", `/api/workspaces/${encodeURIComponent(n)}/start${o ? `?owner=${encodeURIComponent(o)}` : ""}`); toast(`Starting ${n} — about a minute`, "ok"); route(); break; }
        case "share-rm": { const [n, usr] = arg.split("|"); if (!confirm(`Revoke ${usr}'s access to ${n}?`)) break; await api("DELETE", `/api/workspaces/${encodeURIComponent(n)}/shares/${encodeURIComponent(usr)}`); toast(`Revoked ${usr}`, "ok"); route(); break; }
        case "link-rm": { const [n, peer] = arg.split("|"); await api("DELETE", `/api/workspaces/${encodeURIComponent(n)}/links/${encodeURIComponent(peer)}`); toast("Link removed", "ok"); route(); break; }
        case "drive-snapshot": { const r = await api("POST", `/api/drives/${encodeURIComponent(arg)}/snapshots`, {}); toast(`Snapshot ${r.name} requested`, "ok"); route(); break; }
        case "drive-delete": {
          openModal({ title: `Delete drive ${arg} for good?`, html: `<p>Everything on it that was not pushed to Git is lost. This is the <strong>only</strong> action that deletes files; stopping or deleting a workspace never does.</p>`,
            actions: [{ label: "Keep it" }, { label: "Delete drive", cls: "btn-danger", run: async () => { try { await api("DELETE", `/api/drives/${encodeURIComponent(arg)}`); toast(`Deleted ${arg}`, "ok"); route(); } catch (err) { toast(err.message, "err", 7000); } } }] });
          break;
        }
        case "snapshot-delete": { if (!confirm(`Delete snapshot ${arg}?`)) break; await api("DELETE", `/api/snapshots/${encodeURIComponent(arg)}`); toast("Snapshot deleted", "ok"); route(); break; }
        case "drive-share-rm": { const [dr, usr] = arg.split("|"); await api("DELETE", `/api/drives/${encodeURIComponent(dr)}/shares/${encodeURIComponent(usr)}`); toast(`${usr} can no longer mount ${dr}`, "ok"); route(); break; }
        case "drive-use": { const dr = emptyDraft(); dr.fromDrive = arg; dr.step = 0; const dv = (state.drives || []).find((x) => x.name === arg); if (dv && dv.languages && dv.languages.length) dr.languages = dv.languages.slice(); state.draft = dr; saveDraft(); toast(`New workspace will sit on ${arg}`, "ok"); location.hash = "#/create"; break; }
        case "snapshot-use": { const dr = emptyDraft(); dr.fromSnapshot = arg; state.draft = dr; saveDraft(); toast(`New workspace will start from snapshot ${arg}`, "ok"); location.hash = "#/create"; break; }
        case "ws-stop": {
          if (!(await confirmDialog({ title: `Stop ${arg}?`, html: `<p>Running programs and open terminals end. Your files are kept, and the workspace starts again the next time you connect.</p>`, okLabel: "Stop workspace" }))) break;
          await api("POST", `/api/workspaces/${encodeURIComponent(arg)}/stop`); toast(`${arg} stopped`, "ok"); route(); break;
        }
        case "ws-delete": {
          openModal({ title: `Delete ${arg}?`, html: `<p>This removes the workspace, its SSH host key and its Git key. <strong>Your disk (code, history, caches) is kept</strong> unless you tick the box — a new workspace with the same name gets it back.</p><label class="check-card mt-3"><input type="checkbox" id="purge-box"><span><span class="font-semibold">Also delete my files</span><span class="block text-sm">Permanently removes the home volume. There is no undo.</span></span></label>`,
            actions: [{ label: "Cancel" }, { label: "Delete", cls: "btn-danger", run: async () => {
              const purge = $("#purge-box") ? $("#purge-box").checked : false;
              try { await api("DELETE", `/api/workspaces/${encodeURIComponent(arg)}?purge=${purge}`); toast(`Deleted ${arg}${purge ? " and its files" : " (files kept)"}`, "ok"); location.hash = "#/workspaces"; if (state.route.page === "workspaces" && !state.route.arg) route(); }
              catch (err) { toast(err.message, "err"); }
            } }] });
          break;
        }
        case "ws-recreate": {
          const detail = state.detail && state.detail.workspace && state.detail.workspace.name === arg ? state.detail : await api("GET", `/api/workspaces/${encodeURIComponent(arg)}`);
          loadDraftFromWorkspace(detail.workspace);
          location.hash = "#/create";
          break;
        }
        case "draft-tool": {
          if (d.tools.includes(arg)) d.tools = d.tools.filter((t) => t !== arg); else d.tools.push(arg);
          saveDraft(); toast(d.tools.includes(arg) ? `${arg} will be in your next workspace` : `${arg} removed`, "ok", 2000);
          el.textContent = d.tools.includes(arg) ? "Remove from new workspace" : "Add to new workspace";
          break;
        }
        case "draft-reset": resetDraft(); route(); break;
        case "lang-main": d.languages = [arg, ...d.languages.filter((l) => l !== arg)]; saveDraft(); rerenderWizard(); break;
        case "wiz-goto": if (+arg <= d.step) { d.step = +arg; saveDraft(); route(); } break;
        case "wiz-prev": d.step = Math.max(0, d.step - 1); saveDraft(); route(); break;
        case "wiz-next": {
          const errs = stepErrors(d.step);
          if (errs.length) { showStepErrors(errs); break; }
          if (d.step === 0 && !d.name.trim()) d.name = d.languages[0];
          d.step = Math.min(STEPS.length - 1, d.step + 1); saveDraft(); route(); break;
        }
        case "wiz-create": await wizardCreate(); break;
      }
    } catch (err) {
      toast(err.message, "err", 7000);
      if (el) el.disabled = false;
    }
  });

  function rerenderWizard() { const f = $("#wiz"); if (f) f.innerHTML = wizardStep(state.draft.step); }

  document.addEventListener("change", (e) => {
    const el = e.target.closest("[data-field]");
    if (!el) return;
    const d = state.draft, f = el.dataset.field;
    switch (f) {
      case "lang": d.languages = el.checked ? [...d.languages, el.value] : d.languages.filter((l) => l !== el.value); saveDraft(); rerenderWizard(); return;
      case "tool": d.tools = el.checked ? [...new Set([...d.tools, el.value])] : d.tools.filter((t) => t !== el.value); saveDraft(); rerenderWizard(); applyToolFilter(); return;
      case "device": { const q = (state.catalog.devices[el.value] || {}).quantity || "1"; if (el.checked) d.devices = { ...d.devices, [el.value]: q }; else { const c = { ...d.devices }; delete c[el.value]; d.devices = c; } saveDraft(); rerenderWizard(); return; }
      case "multicast": case "expose": d[f] = el.checked; saveDraft(); rerenderWizard(); return;
      case "mount": d.mounts = el.checked ? [...new Set([...(d.mounts || []), el.value])] : (d.mounts || []).filter((m) => m !== el.value); saveDraft(); rerenderWizard(); return;
      case "startFrom": d.fromDrive = ""; d.fromSnapshot = ""; if (el.value.startsWith("drive:")) d.fromDrive = el.value.slice(6); if (el.value.startsWith("snap:")) d.fromSnapshot = el.value.slice(5); saveDraft(); rerenderWizard(); return;
      case "storage": case "project": d[f] = el.value; saveDraft(); rerenderWizard(); return;
      case "gitAuth": case "sizePreset": case "profile": d[f] = el.value; if (f === "profile") { const lab = (state.catalog.profiles[el.value] || {}).tier === "lab"; if (!lab) { d.devices = {}; d.hostPaths = ""; d.caps = ""; } d.isolation = ""; } saveDraft(); rerenderWizard(); return;
      default: d[f] = el.value; saveDraft();
    }
  });
  document.addEventListener("input", (e) => {
    const el = e.target.closest("[data-field]");
    if (el && !["lang", "tool", "device", "multicast", "expose", "gitAuth", "sizePreset", "profile", "mount", "startFrom", "storage", "project"].includes(el.dataset.field)) { state.draft[el.dataset.field] = el.value; saveDraft(); }
    if (e.target.id === "tool-filter") applyToolFilter();
    if (e.target.id === "tool-search") {
      const q = e.target.value.toLowerCase();
      $$("[data-tool]").forEach((c) => c.classList.toggle("hidden", !c.dataset.tool.includes(q) && !c.textContent.toLowerCase().includes(q)));
      $$("[data-group]").forEach((g) => g.classList.toggle("hidden", !$$("[data-tool]:not(.hidden)", g).length));
    }
  });
  function applyToolFilter() { const inp = $("#tool-filter"); if (!inp) return; const q = inp.value.toLowerCase(); $$("[data-tool-card]").forEach((c) => c.classList.toggle("hidden", !c.dataset.toolCard.toLowerCase().includes(q))); }

  document.addEventListener("submit", async (e) => {
    const form = e.target.closest("form[data-action]");
    if (!form) return;
    e.preventDefault();
    const fd = new FormData(form), act = form.dataset.action, arg = form.dataset.arg;
    try {
      if (act === "share-form") { const r = await api("POST", `/api/workspaces/${encodeURIComponent(arg)}/shares`, { user: String(fd.get("user")).trim(), publicKey: String(fd.get("key") || "").trim() }); toast(`Shared with ${r.user}`, "ok"); route(); }
      if (act === "link-form") { const ports = String(fd.get("ports") || "").split(/[\s,]+/).filter(Boolean).map(Number); const r = await api("POST", `/api/workspaces/${encodeURIComponent(arg)}/links`, { peer: String(fd.get("peer")).trim(), ports }); toast(r.status === "linked" ? "Linked both ways" : "Linked on your side — ask them to link back", "ok"); route(); }
      if (act === "drive-create-form") { const r = await api("POST", "/api/drives", { name: String(fd.get("name")).trim(), size: String(fd.get("size")).trim(), storage: String(fd.get("storage") || "") }); toast(`Shared drive ${r.name} created`, "ok"); route(); }
      if (act === "drive-share-form") { const r = await api("POST", `/api/drives/${encodeURIComponent(arg)}/shares`, { user: String(fd.get("user")).trim(), readOnly: fd.get("ro") === "on" }); toast(`${r.user} can now mount ${arg}`, "ok"); route(); }
      if (act === "drive-resize-form") { await api("PUT", `/api/drives/${encodeURIComponent(arg)}/resize`, { size: String(fd.get("size")).trim() }); toast("Resize requested", "ok"); route(); }
      if (act === "idle-form") { await api("PUT", `/api/workspaces/${encodeURIComponent(arg)}/idle`, { idleTimeout: fd.get("idle") }); toast(`Idle timeout set to ${fd.get("idle")}`, "ok"); }
      if (act === "addkey-form") { const r = await api("POST", `/api/workspaces/${encodeURIComponent(arg)}/keys`, { publicKey: String(fd.get("key")).trim() }); toast(r.added ? "Key added" : "That key was already there", "ok"); route(); }
      if (act === "profile-form") {
        const keys = String(fd.get("keys")).split("\n").map((s) => s.trim()).filter(Boolean);
        const body = { sshPublicKeys: keys, gitName: String(fd.get("gitName")), gitEmail: String(fd.get("gitEmail")), artifactoryUser: String(fd.get("afUser")) };
        if (fd.get("afToken")) body.artifactoryToken = String(fd.get("afToken"));
        await api("PUT", "/api/profile", body); state.profile = null; toast("Profile saved", "ok"); route();
      }
    } catch (err) { toast(err.message, "err", 7000); }
  });

  document.addEventListener("keydown", (e) => { if (e.key === "Escape" && modal.open) closeModal(); });

  /* ================================================================== go */
  initControls();
  bootstrap().catch((e) => { $("#view").innerHTML = errorBox(e); });
})();
