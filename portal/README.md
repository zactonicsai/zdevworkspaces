# devspaces portal

A web front end for developer workspaces: pick languages and tools, create, connect,
manage — with guides written for someone who has never used a cloud workspace.
One Go binary (standard library only) serves the JSON API and the embedded site.

```
portal/
  main.go          flags, routes, security headers, static serving (embed web/)
  identity.go      who is calling: X-Forwarded-* from the auth proxy, or -dev-user
  kube.go          minimal Kubernetes REST client, impersonating the developer
  mock.go          in-memory cluster for demos and tests (-mock)
  catalog.go       catalog from the ConfigMap, embedded fallback, validation helpers
  render.go        request → Kubernetes objects (same output as the CLI; tested against it)
  sshkeys.go       Ed25519 keys in OpenSSH formats, without external modules
  artifactory.go   image/tag listing and tool availability via Artifactory's REST API
  handlers.go      /api/* handlers, CSRF rule, logging
  portal_test.go   unit + HTTP tests (go test ./...)
  web/             index.html, app.js (UI), content.js (guides + glossary), demo.js, theme.js,
                   simulation.html (standalone static simulation; `npm run build:sim` re-inlines its CSS),
                   tailwind.src.css → tailwind.css (compiled), catalog.js (generated)
  tailwind.config.js, package.json      Tailwind build (npm run build:css)
  Dockerfile       3-stage build → distroless image
```

## Run it

```bash
# demo: no cluster, in-memory data, user demo@example.com
go run . -mock
# against a real cluster from your laptop (uses your kubeconfig identity via kubectl proxy)
kubectl proxy &
go run . -dev-user alice@corp.example -kube-url http://127.0.0.1:8001
# tests (includes a render-parity test against ../cli/workspace when python3 is present)
go test ./...
```
Open http://localhost:8080. Opening `web/index.html` straight from disk also works:
the page notices there is no API and runs a browser-only demo (`demo.js`).

In the cluster: `kubectl apply -k deploy/portal` (after `deploy/platform`). The
Deployment runs behind oauth2-proxy (example included); any proxy that sets an identity
header works (`-user-header`, `-groups-header`, `-trusted-proxies`).

## How identity and authorization work

The portal never has its own power over workspaces. The auth proxy proves who the
developer is; the portal then talks to the API server **as that developer**
(Kubernetes impersonation). Its ServiceAccount may only `impersonate` users and groups.
So a developer can do exactly what `kubectl` lets them do — create in `ws-<name>`,
nothing elsewhere — and audit logs show the developer, not the portal.

Identity headers are accepted only from `-trusted-proxies` CIDRs, and a NetworkPolicy
lets only the proxy reach the portal pods. State-changing requests must carry
`X-Requested-With: devspaces-portal` (CSRF), responses carry a strict CSP
(no inline scripts or styles, no third-party scripts) plus the usual hardening headers.

## API

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/me` | identity, namespaces, onboarded?, lab access, Artifactory on? |
| GET | `/api/catalog` | the catalog (+ sorted key lists for menus) |
| GET | `/api/artifactory/images` | `workspace-*` images and tags from the Docker repo (falls back to the catalog) |
| GET | `/api/artifactory/tools` | tools with source, docs link and live availability/versions |
| GET/PUT | `/api/profile` | SSH public keys, git identity, Artifactory user/token (Secret `workspace-profile`) |
| GET | `/api/workspaces` | all workspaces in the developer's namespaces with status |
| POST | `/api/workspaces` | create/update (`WorkspaceRequest` in render.go) |
| GET | `/api/workspaces/{name}` | detail + connection info (host key, authorized keys, deploy key, public address) |
| POST | `/api/workspaces/{name}/start` · `/stop` | scale to 1 / 0 |
| PUT | `/api/workspaces/{name}/idle` | `{ "idleTimeout": "8h" }` |
| POST | `/api/workspaces/{name}/keys` | add an SSH public key (live within a minute) |
| GET | `/api/workspaces/{name}/logs?tail=300` | boot log |
| DELETE | `/api/workspaces/{name}?purge=true` | delete; purge also removes the volume |

Errors are `{ "error": "…" }` with 400 (validation), 403 (RBAC), 404 or 502.

## Multi-language workspaces

`languages: ["go","python"]` → the first picks the image (`workspace-go`), the rest become
boot-time features (`WORKSPACE_TOOLS=…,python`), exactly like `workspace create --lang go,python`.
If Artifactory holds a pre-baked combination image (`workspace-go-python`, built with
`EXTRA_FEATURES=python LANGS=go ./build.sh`), the Basics step says so and the developer
can pick it with `image:`.

## Artifactory

* **Images**: `/api/docker/{repo}/v2/_catalog` and `/v2/{image}/tags/list` fill the image
  and version menus (cached, default 5 min).
* **Tools**: catalog tools with an `artifact` block are HEAD-checked at their resolved URL
  (`{arch}`/`{goarch}` placeholders filled per node at boot); local generic repos are listed
  for other versions.
* **Developers' tokens** go into their own `workspace-profile` Secret and are injected into
  workspaces as `ARTIFACTORY_TOKEN`; the portal's own token (read-only) is only for listing.

## Front end notes

* Tailwind is compiled (`npm run build:css`) from `web/tailwind.src.css` with IBM Carbon
  tokens (`tailwind.config.js`): IBM Plex type, Blue 60 actions, square corners; light =
  white/Gray 100, dark = black/Gray 10 (`class="dark"` on `<html>`, remembered in
  localStorage, follows the OS the first time).
* Text size control: 14–36 px on the root element, so every rem-based size scales; when
  the viewport is narrower than ~60rem at the chosen size the header switches to the
  mobile layout and wide grids collapse (`html.compact`).
* Popups are one native `<dialog>`; every glossary term (`data-topic`) opens it with
  the explanation and official links. `content.js` holds 33 topics and 13 guides.
* No framework, no build step for JavaScript. `app.js` is a hash router + template
  functions; all dynamic values pass through `esc()`.
* Air-gapped: drop the Google Fonts `<link>` or vendor IBM Plex under `-vendor-dir`
  and adjust `-csp`.
