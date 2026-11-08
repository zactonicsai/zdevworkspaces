# Use an AKS cluster you already have (with or without Istio)

> New to this? **[docs/tutorial-aks-istio.md](../../../../../docs/tutorial-aks-istio.md)** walks
> through every script below in plain language, with what you should see after each step.

You do not need the full `../10-aks.sh` build-out to try the platform. These scripts look at
an **existing AKS cluster**, reuse what is there (the Istio add-on or a self-managed Istio,
its ingress gateway, the Azure Disk/Files storage classes, snapshot support) and install
only the Kubernetes layer with Helm: the **portal** (workspaces + tools), the catalog,
RBAC/admission, the reaper, developer namespaces and a few **dev pods** to look at.

```
cp env.example env.sh        # fill in RESOURCE_GROUP, CLUSTER_NAME, REGISTRY, DEV_USER (AUTH=dev) — 2 minutes
./quickstart.sh              # = 00 → 10 → 20 → 30 → 60 → 40 → 50 below; every step is idempotent and has --dry-run
```

| Script | Does | Changes the cluster? |
|---|---|---|
| `00-detect.sh` | `az aks get-credentials`; finds Istio (`aks-istio-system` add-on with revision `asm-1-xx`, or `istio-system`), ambient vs sidecar, istio-cni, the ingress gateway and its IP, storage classes, snapshot class, NetworkPolicy engine, lab nodes → writes `detected.env` | no |
| `10-namespaces.sh` | creates `devspaces-system` with the mesh label (`istio.io/rev=…` / `istio.io/dataplane-mode=ambient`), the Retain/expandable `workspace-home-*` StorageClasses (from `../20-storage.yaml`) and a `VolumeSnapshotClass` if missing | yes |
| `20-helm.sh` | writes `values-existing.yaml` from env + detection and runs `helm upgrade --install`. `AUTH=dev`: portal as one trusted user, port-forward only (60- can publish it IP-locked). `AUTH=oidc`: any OpenID Connect provider (Keycloak, Okta…) — browser PKCE + bearer tokens, no proxy, Istio `Gateway`/`VirtualService`, `RequestAuthentication`. `AUTH=entra`: oauth2-proxy + Entra ID, bearer tokens for `wsctl`, Istio `Gateway`/`VirtualService` on the existing ingress gateway, `PeerAuthentication STRICT`, `RequestAuthentication` + `AuthorizationPolicy` | yes |
| `30-istio-check.sh` | shows/verifies the mesh enrolment, mTLS, the route through the gateway; `--apply-examples` installs the L7 examples from `deploy/platform/mesh/` into the project namespace | only with `--apply-examples` |
| `40-dev-pods.sh` | onboards `DEVELOPERS` (namespaces, quota, RBAC, NetworkPolicies, mesh label), creates the shared `PROJECT` namespace, starts `SAMPLE_WORKSPACES` (e.g. `go` and `python`) for the first developer | yes |
| `60-virtualservice.sh` | "Istio is already set up — just add the route": Gateway (or reuse `EXISTING_GATEWAY`) + VirtualService for `PORTAL_HOST` on the existing ingress gateway, optional IP lock (`ALLOW_CIDRS`); refuses to publish an `AUTH=dev` portal without the lock; tests the route with `curl -H Host:`. In `entra`/`oidc` mode Helm already made these, so it only re-tests. `--show` prints the YAML | yes (`route-portal.yaml`) |
| `70-expose-workspace.sh DEV WS PORT` | a developer's app inside a dev pod gets `WS-DEV.apps.<PORTAL_HOST>`: Service + NetworkPolicy (gateway namespace → that port) + VirtualService; `--remove` undoes it | yes (in `ws-<dev>`) |
| `50-verify.sh` | portal health through the API-server proxy, catalog, admission protection of home drives (tries a server-side dry-run delete and expects a refusal), mesh enrolment, a dev pod becomes Ready | no |

## What the mesh gets you here

* **Sidecar mode** (`istio.io/rev=asm-1-25` on each namespace — what the AKS add-on uses): the
  portal, oauth2-proxy and workspace pods get an `istio-proxy`; traffic between them is mTLS.
  Needs `istio-cni` (the add-on has it) because developer namespaces enforce Pod Security
  *baseline*, which forbids the `NET_ADMIN` the classic `istio-init` would need.
* **Ambient mode** (`istio.io/dataplane-mode=ambient`): same, with no sidecars and no pod
  restarts; preferred when available (`MESH_MODE=auto` picks it if `ztunnel` is running).
* SSH into a workspace never crosses the mesh (it is `kubectl exec`/the portal's exec tunnel
  to `127.0.0.1:2222` inside the pod), so Remote-SSH works unchanged. Hardware/kernel
  profiles are labelled out of the mesh (`sidecar.istio.io/inject=false`).
* The portal is published through the **existing Istio ingress gateway** (no extra load
  balancer): `Gateway` selects `istio: aks-istio-ingressgateway-external`, the
  `VirtualService` routes the WebSocket tunnel path with no timeout and everything else to
  oauth2-proxy (or straight to the portal when there is no proxy).
* `istioAuth.jwt` makes the sidecar reject bearer tokens that are not Entra tokens for
  `api://devspaces` *before* the portal sees them (the portal validates them again).

## Files this creates

`detected.env` (facts about the cluster), `values-existing.yaml` (what Helm installed —
contains the oauth2-proxy client secret and cookie secret in `AUTH=entra` mode, so it is
git-ignored; move those two into a Secret and `existingSecret:` before committing), and
`env.sh`. All three are safe to delete and regenerate.

## Switching from the trial to real sign-in

1. Run `../30-entra.sh` (creates the three app registrations) or copy the IDs from the Entra
   portal into `env.sh`; set `AUTH=entra`, `PORTAL_HOST`, optionally `TLS_CREDENTIAL_NAME`.
2. Point DNS for `PORTAL_HOST` at the gateway IP printed by `00-detect.sh`.
3. `./20-helm.sh` again. Developers now sign in with their company account;
   `wsctl login --portal https://PORTAL_HOST` works with no other flags.

Everything else (`DEVELOPERS`, drives, dev pods) stays as it was.
