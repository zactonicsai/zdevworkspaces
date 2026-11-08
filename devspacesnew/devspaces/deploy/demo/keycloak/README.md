# Keycloak + portal on Docker — login required

One `docker compose` that runs **Keycloak** with a pre-built `devspaces` realm and the **portal
API/UI container** (`portal-py/Dockerfile`, the same image the Helm chart deploys) configured so
that every call must carry a token. Three profiles:

| profile | what the API talks to | use it for |
|---|---|---|
| `stub` (default) | nothing — static fixtures from `portal/web/stub/*.json` | test the UI, the login flow and `wsctl` with no cluster |
| `kube` | the kind cluster `devspaces` (docs/demo-docker.md, Level 2) | **real developer pods**: alice logs in, creates a workspace, a StatefulSet appears in `ws-alice` |
| `oauth2-proxy` | static fixtures | the Helm chart's default *session* mode: oauth2-proxy in front, portal trusts `X-Forwarded-Email` |

```
cd deploy/demo/keycloak
./up.sh              # stub      → http://localhost:8080   sign in alice@example.com / alice
./up.sh kube         # real pods → creates/uses the kind cluster, onboards alice/bob/admin
./test.sh            # smoke test: 401 anonymous, token from Keycloak, create+delete a workspace
./down.sh [-v]       # stop (-v also drops the Keycloak database; the realm is re-imported next time)
```

What `up.sh` does: builds `workspace/workspace-portal-py:dev` if missing (`REBUILD=1` to force),
creates the `kind` Docker network if it does not exist, for `kube` runs `../demo-up.sh` when the
cluster is missing, writes `kubeconfig-internal` (`kind get kubeconfig --internal`, the API server
address that works from inside a container), onboards `alice@example.com`, `bob@example.com`,
`admin@example.com` (`workspace admin onboard … --lab`) and the project namespace `ws-proj-demo`,
then `docker compose up -d` and waits for Keycloak's discovery document and the portal.

## Users and clients in the realm (`realm-devspaces.json`)

| user | password | groups |
|---|---|---|
| `alice@example.com` | `alice` | developers |
| `bob@example.com` | `bob` | developers |
| `admin@example.com` | `admin` | developers, platform-admins |

Keycloak admin console: http://localhost:8180 — `admin` / `admin` (`KEYCLOAK_ADMIN_PASSWORD` in `.env`).

| client | type | who uses it |
|---|---|---|
| `devspaces-portal` | public, PKCE S256, redirect `http://localhost:8080/*` | the browser UI (`auth.js`) |
| `devspaces-cli` | public, device authorization grant | `wsctl login` |
| `devspaces-api` | bearer-only | the audience the API checks (`aud: devspaces-api`, added by the `devspaces-api` client scope) |
| `oauth2-proxy` | confidential, secret `oauth2-proxy-demo-secret` | the `oauth2-proxy` profile |
| `devspaces-smoke` | public, **password grant** | `test.sh` only — never enable this on a real realm |

The `groups` client scope puts group names (without the leading `/`) into a `groups` claim; the
API maps `platform-admins` to the lab/admin capabilities exactly as it does for Entra groups.

## How login works here

* **Issuer vs. back-channel.** Tokens say `iss: http://localhost:8180/realms/devspaces` (what the
  browser and `wsctl` reach). The portal container cannot reach `localhost:8180`, so it is given
  `PORTAL_OIDC_DISCOVERY_URL=http://keycloak:8180/realms/devspaces/.well-known/openid-configuration`
  and rewrites the advertised `jwks_uri` to that host. Keycloak is started with
  `KC_HOSTNAME=http://localhost:8180` and `KC_HOSTNAME_BACKCHANNEL_DYNAMIC=true` so both views
  are consistent. The Helm chart has the same knob: `portal.oidc.discoveryUrl`.
* **Browser.** `/config.js` (generated from the `PORTAL_UI_*` env) tells the UI the issuer and
  client id; `auth.js` runs the PKCE code flow against Keycloak and sends `Authorization: Bearer`
  on every API call. No cookie session, no proxy.
* **wsctl.** `wsctl login --portal http://localhost:8080` reads `/.well-known/devspaces`, sees
  `mode: oidc` with a Keycloak issuer and no Entra tenant, and runs the **device code** flow
  against Keycloak's discovery document: open the URL it prints, sign in as alice, done.
  Afterwards `wsctl create demo --lang go`, `wsctl ls`, `wsctl drives`, … work as usual.
* **Anonymous is refused.** With `-oidc-issuer` set, `-stub` no longer signs everyone in as the
  demo user: `/api/me` without a token is `401`, and in stub mode it reports *who actually logged
  in* (`auth: bearer`, `ws-alice`) over the static data.
* **Real pods (`kube`).** The API impersonates the token's user (`--as alice@example.com
  --as-group developers`) against the kind API server, so RBAC — not the portal — decides what
  alice may touch. Sharing uses the kubeconfig's own identity (the "privileged" client), as it
  does with the chart's service account.

## Files

| file | purpose |
|---|---|
| `docker-compose.yaml` | Keycloak, `portal` (stub), `portal-kube`, `oauth2-proxy` + `portal-session` |
| `realm-devspaces.json` | realm import: users, groups, client scopes (`devspaces-api` audience, `groups`), clients |
| `.env.example` → `.env` | `COMPOSE_PROFILES`, `PORTAL_IMAGE`, `KEYCLOAK_ADMIN_PASSWORD` |
| `kubeconfig-internal` | generated by `up.sh kube`; git-ignored |
| `up.sh` / `down.sh` / `test.sh` | lifecycle and smoke test |

## Troubleshooting

* **`invalid issuer` / 401 with a token** — the token's `iss` must equal `PORTAL_OIDC_ISSUER`
  byte for byte. If you reach Keycloak as `127.0.0.1:8180` instead of `localhost:8180`, the issuer
  changes; use `localhost` (or change both the compose env and `KC_HOSTNAME`).
* **`audience devspaces-api missing`** — the `devspaces-api` client scope was not applied; a
  stale Keycloak database from an older realm file: `./down.sh -v && ./up.sh`.
* **Portal says `cannot fetch jwks`** — Keycloak was not ready when the first token arrived; the
  verifier retries on the next request. `docker compose logs keycloak` if it persists.
* **`kube`: `the server could not find the requested resource` / connection refused** — the
  container is not on the `kind` network, or `kubeconfig-internal` is stale after a cluster
  re-create: `./up.sh kube` rewrites it.
* **`kube`: workspace stays `Pending`** — `kubectl -n ws-alice describe pod` — usually the
  workspace images were not loaded into kind (`deploy/demo/demo-up.sh` builds and loads them).
* **Browser login loops** — redirect URI must match `http://localhost:8080/*`; open the portal on
  `localhost`, not `127.0.0.1`.
