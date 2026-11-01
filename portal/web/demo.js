/* demo.js — a browser-only stand-in for the Go API. Used automatically when the
   page is opened without a server (file://, a static preview) so the UI can be
   explored. State lives in localStorage under "ws.demo". The real API and the
   Go mock (-mock) are authoritative; this only mirrors their response shapes. */
"use strict";

window.DEMO = (() => {
  const KEY = "ws.demo";
  const now = () => new Date().toISOString().replace(/\.\d+Z$/, "Z");
  const user = { name: "demo@example.com", short: "demo", groups: ["system:authenticated"] };

  function load() {
    try { return JSON.parse(localStorage.getItem(KEY)) || {}; } catch (e) { return {}; }
  }
  function save(s) { try { localStorage.setItem(KEY, JSON.stringify(s)); } catch (e) { /* ignore */ } }
  let S = load();
  S.workspaces = S.workspaces || {};
  S.profile = S.profile || { sshPublicKeys: [], gitName: "", gitEmail: "", artifactoryUser: "", hasArtifactoryToken: false };

  // the static catalog is embedded here so the demo works from file://
  const CATALOG = window.DEMO_CATALOG;

  function tick() {
    for (const w of Object.values(S.workspaces)) {
      if (w.status === "Starting" && Date.now() - w._since > 5000) { w.status = "Running"; w.ready = true; w.lastActive = now(); w.startedAt = w.startedAt || now(); }
    }
  }
  const json = (status, body) => ({ status, body });

  function handle(method, path, body) {
    tick();
    const q = path.indexOf("?") >= 0 ? path.slice(path.indexOf("?") + 1) : "";
    path = path.replace(/\?.*$/, "");
    const m = (re) => path.match(re);
    if (path === "/api/me") return json(200, { user, namespace: "ws-demo", labNamespace: "ws-demo-lab", onboarded: true, canLab: true, catalogSource: "demo", artifactory: false, artifactoryUrl: CATALOG.artifactory.url, version: "demo", mock: true });
    if (path === "/api/catalog") {
      const out = JSON.parse(JSON.stringify(CATALOG));
      for (const k of ["languages", "tools", "profiles", "isolation", "devices"]) out[k + "Order"] = Object.keys(out[k] || {}).filter((x) => !x.startsWith("$")).sort();
      out.source = "demo";
      return json(200, out);
    }
    if (path === "/api/artifactory/images") {
      const images = {};
      for (const l of Object.values(CATALOG.languages)) images[l.image] = [CATALOG.tag, "2026.09", "2026.08"];
      images["workspace-go-python"] = [CATALOG.tag];
      return json(200, { registry: CATALOG.registry, defaultTag: CATALOG.tag, source: "demo", images });
    }
    if (path === "/api/artifactory/tools") {
      const tools = Object.entries(CATALOG.tools).filter(([n]) => !n.startsWith("$")).map(([name, t]) => ({
        name, category: t.category || "other", description: t.description || "", docs: t.docs, builtin: !!t.builtin,
        source: t.artifact ? "artifactory" : "image-feature", artifact: t.artifact,
        availability: t.artifact ? { available: name !== "duckdb", url: CATALOG.artifactory.url + "/" + (CATALOG.artifactory.repos[t.artifact.repo] || t.artifact.repo) + "/" + t.artifact.path.replace("{version}", t.artifact.version), versions: name === "ripgrep" ? ["14.1.1", "14.1.0", "13.0.0"] : [] } : undefined,
      })).sort((a, b) => a.category.localeCompare(b.category) || a.name.localeCompare(b.name));
      return json(200, { tools, artifactory: true, artifactoryUrl: CATALOG.artifactory.url });
    }
    if (path === "/api/profile") {
      if (method === "GET") return json(200, S.profile);
      S.profile = { ...S.profile, sshPublicKeys: body.sshPublicKeys || [], gitName: body.gitName || "", gitEmail: body.gitEmail || "", artifactoryUser: body.artifactoryUser || "", hasArtifactoryToken: S.profile.hasArtifactoryToken || !!body.artifactoryToken };
      save(S); return json(200, { ok: true, keys: S.profile.sshPublicKeys.length });
    }
    if (path === "/api/workspaces" && method === "GET") return json(200, { workspaces: Object.values(S.workspaces).map(view) });
    if (path === "/api/workspaces" && method === "POST") {
      const langs = (body.languages || []).filter(Boolean);
      if (!langs.length) return json(400, { error: "pick at least one language (or 'base' for plain Linux)" });
      const name = (body.name || langs[0]).trim();
      if (!/^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$/.test(name)) return json(400, { error: `name "${name}" must be lowercase letters, digits or dashes (max 40)` });
      const keys = [...(body.sshPublicKeys || []), ...S.profile.sshPublicKeys].filter(Boolean);
      if (!keys.length) return json(400, { error: "add an SSH public key first (Profile page, or paste one in the Connect step) — without one nobody can log in" });
      const profile = body.profile || "standard";
      const tier = (CATALOG.profiles[profile] || {}).tier || "standard";
      const tools = [...(body.tools || [])];
      for (const l of langs.slice(1)) if (l !== "base" && !tools.includes(l)) tools.push(l);
      const spec = {
        lang: langs[0], languages: langs, tools, profile, isolation: body.isolation || (CATALOG.profiles[profile] || {}).isolation || "userns",
        image: body.image || `${CATALOG.registry}/${CATALOG.languages[langs[0]].image}:${body.tag || CATALOG.tag}`,
        repo: body.repo || "", branch: body.branch || "", dotfiles: body.dotfiles || "", gitAuth: body.gitAuth || "deploy-key", gitName: body.gitName || "", gitEmail: body.gitEmail || "",
        size: body.size || CATALOG.defaults.size, cpu: body.cpu || CATALOG.defaults.cpu, memory: body.memory || CATALOG.defaults.memory,
        cpuRequest: body.cpuRequest || CATALOG.defaults.cpuRequest, memoryRequest: body.memoryRequest || CATALOG.defaults.memoryRequest,
        multicast: !!body.multicast, devices: body.devices || {}, hostPaths: body.hostPaths || [], caps: body.caps || [], expose: !!body.expose,
        idleTimeout: body.idleTimeout || CATALOG.defaults.idleTimeout, tz: body.tz || "", createdAt: now(), createdBy: user.name,
      };
      const existing = S.workspaces[name];
      S.workspaces[name] = {
        name, namespace: tier === "lab" ? "ws-demo-lab" : "ws-demo", tier, status: "Starting", ready: false, _since: Date.now(),
        spec, keys: [...new Set([...(existing ? existing.keys : []), ...keys])],
        hostKey: existing ? existing.hostKey : "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDEMOhostkey" + Math.random().toString(36).slice(2, 10) + "HOSTKEYdemo000000",
        deployKey: spec.gitAuth === "deploy-key" ? (existing && existing.deployKey) || "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDEMOdeploykey" + Math.random().toString(36).slice(2, 10) + "DEPLOYdemo00000 workspace:ws-demo/ws-" + name : "",
        createdAt: existing ? existing.createdAt : now(), startedAt: "",
      };
      save(S);
      return json(201, { name, namespace: S.workspaces[name].namespace, deployKey: S.workspaces[name].deployKey, spec, hostKeyFingerprint: "SHA256:demo" });
    }
    let mm;
    if ((mm = m(/^\/api\/workspaces\/([^/]+)$/))) {
      const w = S.workspaces[mm[1]];
      if (!w) return json(404, { error: "not found" });
      if (method === "DELETE") { delete S.workspaces[mm[1]]; save(S); return json(200, { ok: true, purged: /purge=true/.test(q) }); }
      return json(200, {
        workspace: view(w),
        ssh: { hostKeyFingerprint: "SHA256:" + btoa(w.hostKey).slice(0, 43), knownHostsLine: "ws-" + w.name + " " + w.hostKey, alias: "ws-" + w.name, user: "dev", port: 2222,
          authorizedKeys: w.keys.map((k) => { const f = k.split(/\s+/); return { type: f[0], fingerprint: "SHA256:" + btoa(f[1] || "").slice(0, 43), comment: f.slice(2).join(" ") }; }) },
        deployKey: w.deployKey || undefined, publicAddr: w.spec.expose ? "203.0.113.42" : undefined,
      });
    }
    if ((mm = m(/^\/api\/workspaces\/([^/]+)\/(start|stop|idle|keys|logs)$/))) {
      const w = S.workspaces[mm[1]];
      if (!w) return json(404, { error: "not found" });
      switch (mm[2]) {
        case "start": if (w.status === "Stopped") { w.status = "Starting"; w.ready = false; w._since = Date.now(); } save(S); return json(200, { ok: true, replicas: 1 });
        case "stop": w.status = "Stopped"; w.ready = false; w.startedAt = ""; save(S); return json(200, { ok: true, replicas: 0 });
        case "idle": w.spec.idleTimeout = body.idleTimeout; save(S); return json(200, { ok: true, idleTimeout: body.idleTimeout });
        case "keys": if (!/^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp\d+) [A-Za-z0-9+/=]+/.test(body.publicKey || "")) return json(400, { error: "that does not look like an SSH public key" });
          if (!w.keys.includes(body.publicKey.trim())) w.keys.push(body.publicKey.trim()); save(S); return json(200, { ok: true, added: true });
        case "logs": return w.status === "Stopped" ? json(200, { logs: "", note: "workspace is stopped; start it to see boot logs" }) : json(200, { logs: demoLogs(w) });
      }
    }
    return json(404, { error: "no such endpoint (demo)" });
  }

  function view(w) {
    tick();
    return { name: w.name, namespace: w.namespace, tier: w.tier, status: w.status, ready: w.ready, languages: w.spec.languages, tools: w.spec.tools, profile: w.spec.profile, image: w.spec.image,
      node: w.status === "Stopped" ? "" : "node-1", idleTimeout: w.spec.idleTimeout, lastActive: w.lastActive, startedAt: w.startedAt, createdAt: w.createdAt, spec: w.spec, expose: w.spec.expose, publicAddr: w.spec.expose ? "203.0.113.42" : "" };
  }
  function demoLogs(w) {
    const t = (s) => `[workspace 10:00:${String(s).padStart(2, "0")}]`;
    const lines = [`${t(1)} workspace '${w.name}' starting: lang=${w.spec.lang} profile=${w.spec.profile} tools=${w.spec.tools.join(",") || "none"}`, `${t(1)} first boot: seeding /home/dev from /opt/workspace/skel`, `${t(2)} sshd listening on 127.0.0.1:2222 (pid 42)`, `${t(2)} configuring package managers for Artifactory at ${CATALOG.artifactory.url}`];
    let s = 4;
    for (const tool of w.spec.tools) { lines.push(`${t(s)} installing ${CATALOG.tools[tool] && CATALOG.tools[tool].artifact ? "artifact tool" : "tool"}: ${tool}`); s += 3; lines.push(`${t(s)} tool ready: ${tool}`); }
    if (w.spec.repo) { lines.push(`${t(s)} cloning ${w.spec.repo} into /home/dev/workspace/${w.spec.repo.replace(/\.git$/, "").split(/[/:]/).pop()}`); s += 4; lines.push(`${t(s)} clone complete`); }
    lines.push(`${t(s + 1)} ready`);
    return lines.join("\n");
  }
  return { handle };
})();
