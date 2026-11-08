/* stub.js — answers API calls in the browser from the static JSON in web/stub/ (plus small
   in-memory changes so create/start/stop/drives feel real). Used when API.stubEnabled():
   ?stub=1, the footer "stub data" toggle, PORTAL_CONFIG.stub, or any file:// open.
   The real APIs (portal/ Go, portal-py/ Python) are authoritative; this only mirrors their
   response shapes (api/openapi.yaml). State persists in localStorage under "ws.stub.state". */
"use strict";

window.STUB = (() => {
  const KEY = "ws.stub.state";
  const now = () => new Date().toISOString().replace(/\.\d+Z$/, "Z");
  const json = (status, body) => ({ status, body });
  const CATALOG = () => window.DEMO_CATALOG || window.STUB_CATALOG || { languages: {}, tools: {}, profiles: {}, isolation: {}, devices: {}, defaults: {}, artifactory: { url: "", repos: {} }, registry: "registry", tag: "dev", storage: { choices: [] }, networking: {} };

  // ---- fixtures: fetched from ./stub/*.json when served; embedded minimums for file://
  const FX = {
    me: { user: { name: "demo@example.com", short: "demo", groups: ["system:authenticated"] }, namespace: "ws-demo", labNamespace: "ws-demo-lab", namespaces: [], projects: [], auth: "stub", onboarded: true, canLab: true, catalogSource: "stub", artifactory: false, artifactoryUrl: "", sharing: true, links: true, mesh: "none", version: "stub", mock: true, login: { mode: "stub" } },
    profile: { sshPublicKeys: [], gitName: "", gitEmail: "", artifactoryUser: "", hasArtifactoryToken: false },
    workspaces: { workspaces: [], sharedWithMe: [] }, drives: { drives: [] }, snapshots: { snapshots: [] },
    network: { dns: "", podIP: "", namespace: "ws-demo", sameNamespace: [], links: [], multicast: { mode: "off" }, mesh: "none", linksEnabled: true },
    shares: { shares: [] }, projects: { projects: [] },
  };
  let ready = null;
  async function loadFixtures() {
    if (ready) return ready;
    ready = (async () => {
      if (location.protocol === "file:") return;
      await Promise.all(Object.keys(FX).map(async (k) => {
        try { const r = await fetch(`stub/${k}.json`, { cache: "no-store" }); if (r.ok) FX[k] = await r.json(); } catch (e) { /* keep embedded */ }
      }));
    })();
    return ready;
  }

  // ---- state: fixtures + what the user did in this browser
  let S = null;
  function state() {
    if (S) return S;
    try { S = JSON.parse(localStorage.getItem(KEY)); } catch (e) { S = null; }
    if (!S || S.seed !== JSON.stringify(FX.workspaces).length) {
      S = { seed: JSON.stringify(FX.workspaces).length, profile: FX.profile, workspaces: {}, drives: {}, snapshots: FX.snapshots.snapshots.slice(), shares: { api: FX.shares.shares.slice() }, links: {} };
      for (const w of FX.workspaces.workspaces) S.workspaces[w.name] = { ...w, keys: FX.profile.sshPublicKeys.slice(), hostKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubHostKey000000000000000000000000000000", deployKey: w.spec && w.spec.gitAuth === "deploy-key" ? "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubDeployKey0000000000000000000000000000 workspace:ws-demo/ws-" + w.name : "" };
      for (const d of FX.drives.drives) S.drives[d.name] = { ...d };
      for (const l of FX.network.links || []) (S.links[l.workspace] = S.links[l.workspace] || []).push(l);
    }
    return S;
  }
  const save = () => { try { localStorage.setItem(KEY, JSON.stringify(S)); } catch (e) { /* ignore */ } };
  function tick() {
    for (const w of Object.values(state().workspaces)) {
      if ((w.status === "Starting" || w.status === "Pending") && Date.now() - (w._since || 0) > 5000) { w.status = "Running"; w.ready = true; w.lastActive = now(); w.startedAt = w.startedAt || now(); w.node = "node-1"; }
    }
    for (const sn of state().snapshots) if (!sn.readyToUse && Date.now() - (sn._since || 0) > 3000) sn.readyToUse = true;
  }
  const view = (w) => { const { keys, hostKey, deployKey, _since, ...v } = w; return v; };
  const attachedTo = (drive) => (Object.values(state().workspaces).find((w) => w.drive === drive) || {}).name || "";
  const fp = (k) => "SHA256:" + btoa(k || "").slice(0, 43);

  async function handle(method, path, body) {
    await loadFixtures();
    tick();
    const q = new URLSearchParams(path.includes("?") ? path.slice(path.indexOf("?") + 1) : "");
    path = path.replace(/\?.*$/, "");
    const m = (re) => path.match(re);
    const cat = CATALOG();
    const st = state();
    let mm;

    if (path === "/api/me") return json(200, { ...FX.me, projects: FX.projects.projects, catalogSource: "stub" });
    if (path === "/api/projects") return json(200, FX.projects);
    if (path === "/api/catalog") {
      const out = JSON.parse(JSON.stringify(cat));
      for (const k of ["languages", "tools", "profiles", "isolation", "devices"]) out[k + "Order"] = Object.keys(out[k] || {}).filter((x) => !x.startsWith("$")).sort();
      out.source = "stub";
      return json(200, out);
    }
    if (path === "/api/artifactory/images") {
      const images = {};
      for (const l of Object.values(cat.languages || {})) if (l.image) images[l.image] = [cat.tag, "2026.09", "2026.08"];
      return json(200, { registry: cat.registry, defaultTag: cat.tag, source: "stub", images });
    }
    if (path === "/api/artifactory/tools") {
      const tools = Object.entries(cat.tools || {}).filter(([n]) => !n.startsWith("$")).map(([name, t]) => ({
        name, category: t.category || "other", description: t.description || "", docs: t.docs, builtin: !!t.builtin,
        source: t.artifact ? "artifactory" : "image-feature", artifact: t.artifact,
        availability: t.artifact ? { available: true, url: (cat.artifactory && cat.artifactory.url ? cat.artifactory.url + "/" : "") + (t.artifact.path || "").replace("{version}", t.artifact.version || ""), versions: [t.artifact.version].filter(Boolean) } : undefined,
      })).sort((a, b) => a.category.localeCompare(b.category) || a.name.localeCompare(b.name));
      return json(200, { tools, artifactory: false, artifactoryUrl: cat.artifactory && cat.artifactory.url });
    }
    if (path === "/api/profile") {
      if (method === "GET") return json(200, st.profile);
      st.profile = { ...st.profile, sshPublicKeys: body.sshPublicKeys || [], gitName: body.gitName || "", gitEmail: body.gitEmail || "", artifactoryUser: body.artifactoryUser || "", hasArtifactoryToken: st.profile.hasArtifactoryToken || !!body.artifactoryToken };
      save(); return json(200, { ok: true, keys: st.profile.sshPublicKeys.length });
    }

    /* ---- workspaces */
    if (path === "/api/workspaces" && method === "GET") {
      const out = { workspaces: Object.values(st.workspaces).map(view) };
      if (q.get("shared") === "true") out.sharedWithMe = FX.workspaces.sharedWithMe || [];
      return json(200, out);
    }
    if (path === "/api/workspaces" && method === "POST") {
      const langs = (body.languages || []).filter(Boolean);
      if (!langs.length) return json(400, { error: "pick at least one language (or 'base' for plain Linux)" });
      const name = (body.name || langs[0]).trim();
      if (!/^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$/.test(name)) return json(400, { error: `name "${name}" must be lowercase letters, digits or dashes (max 40)` });
      const keys = [...(body.sshPublicKeys || []), ...st.profile.sshPublicKeys].filter(Boolean);
      if (!keys.length) return json(400, { error: "add an SSH public key first (Profile page, or paste one in the Connect step) — without one nobody can log in" });
      const profile = body.profile || "standard";
      const tier = ((cat.profiles || {})[profile] || {}).tier || "standard";
      const tools = [...(body.tools || [])];
      for (const l of langs.slice(1)) if (l !== "base" && !tools.includes(l)) tools.push(l);
      const choices = ((cat.storage || {}).choices || []).filter((c) => (c.kind || "home") === "home");
      const storage = body.storage || (choices.find((c) => c.default) || choices[0] || { name: "" }).name;
      const drive = body.fromDrive || `ws-${name}-home`;
      if (body.fromDrive) {
        const d = st.drives[body.fromDrive];
        if (!d) return json(400, { error: `drive ${body.fromDrive} not found (see Drives)` });
        const user = attachedTo(body.fromDrive);
        if (user && user !== name) return json(400, { error: `drive ${body.fromDrive} is attached to workspace ${user} — delete that workspace first (the drive stays)` });
      }
      for (const mt of body.mounts || []) {
        const dn = mt.split(":")[1] || ""; const owner = dn.includes("/") ? dn.split("/")[0] : ""; const base = dn.split("/").pop();
        const pvc = owner ? `shared-${owner}-${base}` : `shared-${base}`;
        if (!st.drives[pvc]) return json(400, { error: `shared drive ${base} (${pvc}) is not in your namespace — ${owner ? "ask " + owner + " to share it" : "create it on the Drives page"}` });
      }
      const spec = {
        lang: langs[0], languages: langs, tools, profile, isolation: body.isolation || ((cat.profiles || {})[profile] || {}).isolation || "userns",
        image: body.image || `${cat.registry}/${((cat.languages || {})[langs[0]] || {}).image || "workspace-" + langs[0]}:${body.tag || cat.tag}`,
        repo: body.repo || "", branch: body.branch || "", dotfiles: body.dotfiles || "", gitAuth: body.gitAuth || "deploy-key", gitName: body.gitName || "", gitEmail: body.gitEmail || "",
        size: body.size || (cat.defaults || {}).size || "30Gi", storage, drive, fromSnapshot: body.fromSnapshot || "", mounts: body.mounts || [],
        cpu: body.cpu || (cat.defaults || {}).cpu || "4", memory: body.memory || (cat.defaults || {}).memory || "8Gi",
        cpuRequest: body.cpuRequest || (cat.defaults || {}).cpuRequest || "1", memoryRequest: body.memoryRequest || (cat.defaults || {}).memoryRequest || "2Gi",
        multicast: !!body.multicast, devices: body.devices || {}, hostPaths: body.hostPaths || [], caps: body.caps || [], expose: !!body.expose,
        idleTimeout: body.idleTimeout || (cat.defaults || {}).idleTimeout || "4h", tz: body.tz || "", createdAt: now(), createdBy: FX.me.user.name,
      };
      const ns = body.namespace || (tier === "lab" ? FX.me.labNamespace : FX.me.namespace);
      const existing = st.workspaces[name];
      st.workspaces[name] = {
        name, namespace: ns, tier, status: "Pending", ready: false, _since: Date.now(), languages: langs, tools, profile, image: spec.image, idleTimeout: spec.idleTimeout, expose: spec.expose,
        drive, storage, mounts: spec.mounts, dns: `ws-${name}.${ns}.svc.cluster.local`, spec, keys: [...new Set([...(existing ? existing.keys : []), ...keys])],
        hostKey: existing ? existing.hostKey : "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubHostKey" + Math.random().toString(36).slice(2, 10) + "00000000000000000000000000",
        deployKey: spec.gitAuth === "deploy-key" ? (existing && existing.deployKey) || "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubDeployKey" + Math.random().toString(36).slice(2, 10) + "0000000000000000000000 workspace:" + ns + "/ws-" + name : "",
        createdAt: existing ? existing.createdAt : now(), startedAt: "",
      };
      if (!st.drives[drive]) st.drives[drive] = { name: drive, namespace: ns, kind: "home", owner: FX.me.user.short, size: spec.size, storage, storageClass: "", accessModes: ["ReadWriteOnce"], phase: "Bound", attachedTo: name, mountedBy: [], sharedWith: [], lastWorkspace: name, languages: langs, createdAt: now(), protected: true, snapshots: 0 };
      st.drives[drive].attachedTo = name; st.drives[drive].lastWorkspace = name; st.drives[drive].languages = langs;
      save();
      return json(201, { name, namespace: ns, deployKey: st.workspaces[name].deployKey, spec, hostKeyFingerprint: fp(st.workspaces[name].hostKey) });
    }
    if ((mm = m(/^\/api\/workspaces\/([^/]+)$/))) {
      const owner = q.get("owner");
      if (owner) {
        const sw = (FX.workspaces.sharedWithMe || []).find((w) => w.name === mm[1]);
        return sw ? json(200, { workspace: sw, ssh: { alias: "ws-" + sw.name, user: "dev", port: 2222, hostKeyFingerprint: "SHA256:stub", knownHostsLine: "", authorizedKeys: [] }, network: { ...FX.network, dns: sw.dns } }) : json(404, { error: "not found" });
      }
      const w = st.workspaces[mm[1]];
      if (!w) return json(404, { error: "not found" });
      if (method === "DELETE") {
        const purge = q.get("purge") === "true";
        delete st.workspaces[mm[1]]; delete st.shares[mm[1]]; delete st.links[mm[1]];
        if (purge) delete st.drives[w.drive]; else if (st.drives[w.drive]) st.drives[w.drive].attachedTo = "";
        save(); return json(200, { ok: true, purged: purge, deleted: w.name, drive: w.drive, driveKept: !purge });
      }
      return json(200, {
        workspace: view(w),
        ssh: { hostKeyFingerprint: fp(w.hostKey), knownHostsLine: "ws-" + w.name + " " + w.hostKey, alias: "ws-" + w.name, user: "dev", port: 2222,
          authorizedKeys: w.keys.map((k) => { const f = k.split(/\s+/); return { type: f[0], fingerprint: fp(f[1]), comment: f.slice(2).join(" ") }; }) },
        deployKey: w.deployKey || undefined, publicAddr: w.expose ? "203.0.113.42" : undefined,
        network: networkOf(w),
      });
    }
    if ((mm = m(/^\/api\/workspaces\/([^/]+)\/(start|stop|idle|keys|logs|network|shares|links)$/))) {
      const w = st.workspaces[mm[1]];
      if (!w) return json(404, { error: "not found" });
      switch (mm[2]) {
        case "start": if (w.status === "Stopped") { w.status = "Pending"; w.ready = false; w._since = Date.now(); } save(); return json(200, { ok: true, replicas: 1 });
        case "stop": w.status = "Stopped"; w.ready = false; w.startedAt = ""; w.node = ""; save(); return json(200, { ok: true, replicas: 0 });
        case "idle": w.idleTimeout = body.idleTimeout; w.spec.idleTimeout = body.idleTimeout; save(); return json(200, { ok: true, idleTimeout: body.idleTimeout });
        case "keys": if (!/^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp\d+) [A-Za-z0-9+/=]+/.test(body.publicKey || "")) return json(400, { error: "that does not look like an SSH public key" });
          { const added = !w.keys.includes(body.publicKey.trim()); if (added) w.keys.push(body.publicKey.trim()); save(); return json(200, { ok: true, added }); }
        case "logs": return w.status === "Stopped" ? json(200, { logs: "", note: "workspace is stopped; start it to see boot logs" }) : json(200, { logs: bootLog(w) });
        case "network": return json(200, networkOf(w));
        case "shares":
          if (method === "GET") return json(200, { shares: st.shares[w.name] || [] });
          if (!body.user) return json(400, { error: "user is required" });
          st.shares[w.name] = [...(st.shares[w.name] || []).filter((s) => s.user !== body.user), { user: body.user, kind: "workspace", resource: w.name, createdAt: now() }];
          if (!w.keys.some((k) => k.endsWith("guest:" + body.user))) w.keys.push("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubGuestKey00000000000000000000000000000 guest:" + body.user);
          save(); return json(201, { user: body.user, kind: "workspace", resource: w.name });
        case "links":
          if (method === "GET") return json(200, { links: st.links[w.name] || [] });
          { const peer = (body.peer || "").trim(); if (!/\//.test(peer)) return json(400, { error: "peer must be OWNER/WORKSPACE" });
            const [owner, pws] = [peer.slice(0, peer.lastIndexOf("/")), peer.slice(peer.lastIndexOf("/") + 1)];
            const short = owner.replace(/@.*$/, "").toLowerCase().replace(/[^a-z0-9-]/g, "-");
            const l = { workspace: w.name, peer: short + "/" + pws, peerDNS: `ws-${pws}.ws-${short}.svc.cluster.local`, ports: body.ports || [], status: short === "bob" ? "linked" : "pending-peer" };
            st.links[w.name] = [...(st.links[w.name] || []).filter((x) => x.peer !== l.peer), l]; save(); return json(201, l); }
      }
    }
    if ((mm = m(/^\/api\/workspaces\/([^/]+)\/shares\/(.+)$/)) && method === "DELETE") {
      const w = st.workspaces[mm[1]]; const u = decodeURIComponent(mm[2]);
      if (!w) return json(404, { error: "not found" });
      st.shares[w.name] = (st.shares[w.name] || []).filter((s) => s.user !== u); w.keys = w.keys.filter((k) => !k.endsWith("guest:" + u)); save(); return json(200, { revoked: u });
    }
    if ((mm = m(/^\/api\/workspaces\/([^/]+)\/links\/(.+)$/)) && method === "DELETE") {
      const w = st.workspaces[mm[1]]; const p = decodeURIComponent(mm[2]);
      if (!w) return json(404, { error: "not found" });
      st.links[w.name] = (st.links[w.name] || []).filter((x) => x.peer !== p); save(); return json(200, { removed: p });
    }

    /* ---- drives & snapshots */
    if (path === "/api/drives" && method === "GET") return json(200, { drives: Object.values(st.drives).map((d) => ({ ...d, attachedTo: d.kind === "home" ? attachedTo(d.name) : undefined, snapshots: st.snapshots.filter((s) => s.drive === d.name).length })) });
    if (path === "/api/drives" && method === "POST") {
      if (!/^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$/.test(body.name || "")) return json(400, { error: "drive name: lowercase letters, digits, dashes" });
      const name = "shared-" + body.name;
      if (st.drives[name]) return json(400, { error: `shared drive ${body.name} already exists` });
      st.drives[name] = { name, namespace: FX.me.namespace, kind: "shared", owner: FX.me.user.short, size: body.size || "100Gi", storage: body.storage || "shared", storageClass: "", accessModes: ["ReadWriteMany"], phase: "Bound", mountedBy: [], sharedWith: [], languages: [], createdAt: now(), protected: false, snapshots: 0 };
      save(); return json(201, st.drives[name]);
    }
    const findDrive = (ref) => st.drives[ref] || st.drives[(st.workspaces[ref] || {}).drive];
    if ((mm = m(/^\/api\/drives\/([^/]+)$/))) {
      const d = findDrive(mm[1]);
      if (!d) return json(404, { error: "not found" });
      if (method === "DELETE") {
        if (d.kind === "home" && attachedTo(d.name)) return json(409, { error: `${d.name} is in use by workspace ${attachedTo(d.name)} — delete that workspace first (the drive stays)` });
        if ((d.sharedWith || []).length) return json(409, { error: "the drive is still shared with other developers — revoke the shares first" });
        delete st.drives[d.name]; save(); return json(200, { deleted: d.name });
      }
      return json(200, { drive: { ...d, attachedTo: d.kind === "home" ? attachedTo(d.name) : undefined }, snapshots: st.snapshots.filter((s) => s.drive === d.name) });
    }
    if ((mm = m(/^\/api\/drives\/([^/]+)\/resize$/))) { const d = findDrive(mm[1]); if (!d) return json(404, { error: "not found" }); d.size = body.size; save(); return json(200, { name: d.name, size: d.size }); }
    if ((mm = m(/^\/api\/drives\/([^/]+)\/snapshots$/))) {
      const d = findDrive(mm[1]); if (!d) return json(404, { error: "not found" });
      if (method === "GET") return json(200, { snapshots: st.snapshots.filter((s) => s.drive === d.name) });
      const sn = { name: (body && body.name) || `${d.name}-${now().replace(/[-:TZ]/g, "").slice(0, 15).replace(/^(\d{8})(\d{6})$/, "$1-$2")}`, namespace: d.namespace, drive: d.name, readyToUse: false, restoreSize: d.size, languages: (d.languages || []).join(","), createdAt: now(), _since: Date.now() };
      st.snapshots.push(sn); save(); return json(201, sn);
    }
    if ((mm = m(/^\/api\/drives\/([^/]+)\/shares$/))) {
      const d = findDrive(mm[1]); if (!d) return json(404, { error: "not found" });
      if (method === "GET") return json(200, { shares: (d.sharedWith || []).map((u) => ({ user: u, kind: "drive", resource: d.name })) });
      if (d.kind !== "shared") return json(400, { error: "only your own shared drives can be shared (home drives are single-writer disks: share the workspace instead)" });
      d.sharedWith = [...new Set([...(d.sharedWith || []), body.user])]; save(); return json(201, { user: body.user, kind: "drive", resource: d.name, readOnly: !!body.readOnly });
    }
    if ((mm = m(/^\/api\/drives\/([^/]+)\/shares\/(.+)$/)) && method === "DELETE") { const d = findDrive(mm[1]); if (!d) return json(404, { error: "not found" }); d.sharedWith = (d.sharedWith || []).filter((u) => u !== decodeURIComponent(mm[2])); save(); return json(200, { revoked: mm[2] }); }
    if ((mm = m(/^\/api\/snapshots\/([^/]+)$/)) && method === "DELETE") { st.snapshots = st.snapshots.filter((s) => s.name !== mm[1]); save(); return json(200, { deleted: mm[1] }); }
    if ((mm = m(/^\/api\/workspaces\/([^/]+)\/tunnel$/))) return json(501, { error: "the stub has no SSH tunnel — run a real API (portal -mock bridges to an echo server)" });
    return json(404, { error: "no such endpoint (stub)" });
  }

  function networkOf(w) {
    const same = Object.values(state().workspaces).filter((x) => x.namespace === w.namespace && x.name !== w.name).map((x) => x.name);
    return { ...FX.network, dns: w.dns, podIP: w.status === "Running" ? FX.network.podIP || "10.244.1.23" : "", namespace: w.namespace, sameNamespace: same, links: state().links[w.name] || [] };
  }
  function bootLog(w) {
    const t = (s) => `[workspace 10:00:${String(s).padStart(2, "0")}]`;
    const lines = [`${t(1)} workspace '${w.name}' starting: lang=${w.spec.lang} profile=${w.spec.profile} tools=${(w.spec.tools || []).join(",") || "none"}`, `${t(1)} first boot: seeding /home/dev from /opt/workspace/skel`, `${t(2)} sshd listening on 127.0.0.1:2222 (pid 42)`];
    let s = 4;
    for (const tool of w.spec.tools || []) { lines.push(`${t(s)} installing tool: ${tool}`); s += 3; lines.push(`${t(s)} tool ready: ${tool}`); }
    if (w.spec.repo) { lines.push(`${t(s)} cloning ${w.spec.repo} into /home/dev/workspace/${w.spec.repo.replace(/\.git$/, "").split(/[/:]/).pop()}`); s += 4; lines.push(`${t(s)} clone complete`); }
    lines.push(`${t(s + 1)} ready`);
    return lines.join("\n");
  }
  function reset() { try { localStorage.removeItem(KEY); } catch (e) { /* ignore */ } S = null; }
  return { handle, reset, fixtures: FX };
})();
