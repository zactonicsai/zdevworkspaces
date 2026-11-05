# portal-py — the Workspaces portal in Python (API + the same web UI), as one image for Helm

The Go portal (`portal/`) and this one serve the **same UI** (`portal/web`) and the **same API**
(`api/openapi.yaml`); pick whichever your team prefers to maintain. Differences: this one is
Python standard library + `kubectl` (plus PyJWT for bearer tokens) and has **no WebSocket SSH
tunnel** — `wsctl ssh` needs the Go image; `workspace ssh` (kubectl) and VS Code via the
Python CLI work the same.

```
portal-py/
  api.py        HTTP server, routing, identity (bearer / dev / proxy headers), /config.js, static UI, CORS, CSP
  handlers.py   the endpoints on a real cluster (impersonating kubectl; manifests from cli/workspace)
  kube.py       kubectl wrapper with --as/--as-group; loads cli/workspace as module `wscli`
  oidc.py       bearer JWT validation against the issuer's JWKS (Keycloak, Entra, Okta …)
  stub.py       --stub: answers from portal/web/stub/*.json with in-memory state
  test_api.py   unit tests (stub HTTP + handlers against a fake cluster):  make test
  Dockerfile    python:3.12-slim + kubectl + openssh-client, non-root, port 8080
  build.sh      build/push the image;  Makefile  shortcuts
```

## Run it

```bash
make stub                      # http://127.0.0.1:8080 — no cluster, static JSON, every UI flow works
make run                       # against your kubeconfig as $USER@example.com (kubectl must work)
python3 api.py -h              # all flags; env PORTAL_* equivalents exist for every flag
```

Image:

```bash
./portal-py/build.sh --test                       # workspace/workspace-portal-py:dev
docker run --rm -p 8080:8080 workspace/workspace-portal-py:dev -stub
cd deploy/demo/keycloak && ./up.sh           # same image behind Keycloak, login required (deploy/demo/keycloak/README.md)
REGISTRY=myacr.azurecr.io/devspaces TAG=2026.11 ./portal-py/build.sh --push
helm upgrade --install devspaces charts/devspaces -n devspaces-system \
  --set portal.backend=python --set global.registry=myacr.azurecr.io/devspaces --set global.tag=2026.11
```

`images/build.sh` builds it too with `PORTAL_PY=1`.

## Configuration: the UI is configured at runtime, not at build time

`GET /config.js` is generated from flags and read by the UI before `api.js` loads:

| Flag | `config.js` | Meaning |
|---|---|---|
| `-ui-api-base URL` | `apiBase` | the browser calls the API there (default: same origin). The API must list the UI's origin in `-cors-origins`. |
| `-ui-oidc-issuer`, `-ui-oidc-client-id`, `-ui-oidc-scope` | `oidc.*` | browser signs in with PKCE against this OIDC issuer (Keycloak realm URL); the UI then sends `Authorization: Bearer` |
| `-ui-stub`, `-ui-stub-toggle=false` | `stub`, `allowStubToggle` | start in stub mode / hide the footer switch |

The UI module `portal/web/api.js` has one function per endpoint (`API.workspaces()`,
`API.createWorkspace(req)`, `API.addShare(name, user)` …) and is the only place that knows
about fetch, headers, auth and the stub switch. `auth.js` is the OIDC client (discovery →
authorize with S256 challenge → token exchange → refresh → logout). `stub.js` answers in the
browser from `web/stub/*.json`.

### Three ways to sign in

| Mode | Flags | How the browser authenticates | How `wsctl`/automation authenticates |
|---|---|---|---|
| session (default) | `-user-header -groups-header -trusted-proxies` | an auth proxy (oauth2-proxy) in front sets `X-Forwarded-Email` | bearer token if `-oidc-issuer` is also set |
| OIDC in the browser | `-oidc-issuer -oidc-audience` **and** `-ui-oidc-issuer -ui-oidc-client-id` | PKCE login page → bearer token on every call | same issuer, device-code flow |
| dev / stub | `-dev-user you@corp.example` or `-stub` | nothing — every call is that user | — |

### Keycloak, step by step

1. In the realm create three clients: `devspaces-portal` (**public**, Standard flow, PKCE
   S256, Valid redirect URIs `https://workspaces.corp.example/*`, Web origins
   `https://workspaces.corp.example`), `devspaces-api` (bearer-only; its client id is the
   audience), `devspaces-cli` (public, *OAuth 2.0 Device Authorization Grant* on, for `wsctl`).
2. On `devspaces-portal` and `devspaces-cli` add an **Audience** mapper for `devspaces-api`
   (so access tokens carry `aud: devspaces-api`) and a **Group Membership** mapper named
   `groups` (full path off).
3. Make sure Kubernetes knows users under the same name: API server
   `--oidc-issuer-url=https://keycloak…/realms/dev --oidc-client-id=devspaces-api
   --oidc-username-claim=preferred_username --oidc-groups-claim=groups` (or map names
   with `-oidc-user-claim email`). The portal impersonates that identity.
4. Run the portal with
   `-oidc-issuer https://keycloak.corp.example/realms/dev -oidc-audience devspaces-api
   -oidc-user-claim preferred_username -ui-oidc-issuer https://keycloak.corp.example/realms/dev
   -ui-oidc-client-id devspaces-portal -cli-client-id devspaces-cli`
   — or `charts/devspaces/examples/values-keycloak.yaml` with Helm.
5. Open the portal: *Sign in* → Keycloak → back to the page, calls carry the token;
   `wsctl login --portal https://workspaces.corp.example` reads the same settings from
   `/.well-known/devspaces`.

## Stub mode

`api.py -stub` serves `portal/web/stub/*.json` (the same fixtures the browser stub uses) with
in-memory create/start/stop/drives/snapshots/shares/links, so the UI, `wsctl` and curl can be
exercised against a real HTTP server without a cluster — including in CI. The UI's own switch
(footer *data: live | stub*, `?stub=1`, or `PORTAL_CONFIG.stub`) goes one step further and
needs no server at all (`file://` opens default to it).

## Security posture (same as the Go portal)

Impersonation for everything a developer does; the pod's ServiceAccount only for sharing
(`workspace-portal-sharing` ClusterRole, confined by admission policy); identity headers only
from trusted proxy CIDRs; bearer tokens validated locally (signature, issuer, audience, expiry);
CSRF header required for session calls; strict CSP (`connect-src` widened only for the
configured API/OIDC origins); CORS only for listed origins; non-root, read-only root fs friendly.

## Stub mode and login

`-stub` alone signs every caller in as the demo user (`auth: stub`) — handy for UI work. Add
`-oidc-issuer` (and `-oidc-audience`) and the data stays static but **login becomes mandatory**:
anonymous calls get `401`, `/.well-known/devspaces` reports `mode: oidc`, and `/api/me` shows
the user from the verified token (`auth: bearer`, `ws-<short>`). That is how
`deploy/demo/keycloak/docker-compose.yaml` runs the image: real Keycloak sign-in for the browser
(PKCE) and `wsctl` (device code), no cluster. When the issuer host is not reachable from where
the API runs, `-oidc-discovery-url` names the back-channel discovery document; the JWKS address
it advertises is rewritten to that host. The default CLI scope published to `wsctl` is
`<audience>/.default` with an Entra tenant (`-cli-tenant`) and `openid profile email` otherwise.
