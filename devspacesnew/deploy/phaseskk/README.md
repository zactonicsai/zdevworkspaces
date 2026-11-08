# deploy/phases — install on any cluster, one phase at a time

Works on anything that has `kubectl` and `helm`: kind, k3s, minikube, EKS, GKE, AKS,
OpenShift, bare metal. No cloud CLI, no Istio required (used if present), no operator.

```
cp env.example env.sh      # edit ~10 lines
./all.sh                   # or run the phases one by one, in order
```

## The phases

Each phase is one script, one job, one place to look when something fails. A phase reads
**`env.sh`** (your choices) and **`detected.env`** (what phase 00 found) and nothing else
from the other phases — the cluster itself is the shared state. That is what makes them
loosely coupled: you can run one, skip one you have done another way, or re-run one after a
change without redoing the rest.

| # | Script | Job (one sentence) | Creates | Needs | Skip it when |
|---|---|---|---|---|---|
| 00 | `00-check.sh` | Look, don't touch: tools, cluster, default StorageClass, snapshot CRDs, Istio, IngressClass/gateway | `detected.env` | `env.sh` | never (it is read-only and fast) |
| 10 | `10-namespace.sh` | The room: namespace with Pod Security + mesh labels, pull Secret | ns `SYSTEM_NS` | 00 | an admin pre-created the namespace for you |
| 20 | `20-storage.sh` | Disks that survive: a Retain StorageClass cloned from the default, a VolumeSnapshotClass | SC `workspace-home`, VSC `workspace-snapshots` | 00 | you set `HOME_STORAGE_CLASS` (your own Retain class) |
| 30 | `30-platform.sh` | The software: one `helm upgrade --install` with a generated `values.yaml` — no route | Helm release `devspaces` | 10, 20 | never; this *is* the install |
| 40 | `40-route.sh` | The way in: nothing (`port-forward`), an `Ingress`, or Istio `Gateway`+`VirtualService`, plus the NetworkPolicy and the `ALLOW_CIDRS` lock | `route.yaml` | 30 | `EXPOSE=port-forward` (it just prints the command) |
| 50 | `50-developers.sh` | The people: `workspace admin onboard` per developer, project namespaces | ns `ws-<name>`, `ws-proj-<p>` | 30 | your GitOps creates tenant namespaces |
| 60 | `60-workspaces.sh` | The first dev pods: `workspace create` for `SAMPLE_WORKSPACES` | StatefulSets + PVCs | 50 | `SAMPLE_WORKSPACES=""` — people create their own |
| 70 | `70-verify.sh` | Proof: health, login rule, drive protection, a pod Ready, snapshots, mesh, route | nothing | 30 (+50/60) | never; also fine in CI |
| 90 | `90-uninstall.sh` | Reverse order; keeps home-drive PVs unless `--purge` | — | — | — |

Every script takes `--dry-run` (prints what it would do, contacts no cluster) and starts
with a header that says WHAT / WHY / NEEDS / MAKES / RE-RUN. Read the header, then run it.

## Why this order

```
00 check ──► 10 namespace ──► 20 storage ──► 30 platform ──► 40 route
                                                  │
                                                  └──► 50 developers ──► 60 workspaces ──► 70 verify
```

* **Look before you touch** (00). Everything later adapts to what 00 found, so the same
  scripts work on a laptop cluster and a cloud one.
* **Infrastructure before software** (10, 20 before 30). The namespace and the StorageClass
  are owned by the cluster admin and outlive the Helm release; uninstalling the release never
  deletes a disk.
* **Software before the route** (30 before 40). You can test the portal over `port-forward`
  while nothing is public. Switching from nginx to Istio, adding TLS, or removing the route
  touches only phase 40.
* **Platform before people** (30 before 50). Onboarding reads the catalog ConfigMap the chart
  installed (mesh mode, pull secret name). Team leads re-run 50 whenever someone joins; they
  never need 30.
* **Verify last, always** (70). It checks the result the way a developer will see it.

Dependencies go only *downward* in the table: 40 does not know how 30 was configured beyond
the Service name `workspace-portal`; 50 does not read `values.yaml`; 30 does not care whether
20 created the class or you brought your own — it only reads the name from `detected.env`.

## What goes in `env.sh`

| Setting | Means | Typical |
|---|---|---|
| `KUBE_CONTEXT` | which cluster | empty = current context |
| `REGISTRY`, `TAG` | where the images are (`images/build.sh --push`) | `ghcr.io/acme/devspaces`, `dev` |
| `HOME_STORAGE_CLASS` | your Retain RWO class, or empty to clone the default | empty |
| `SHARED_STORAGE_CLASS` | an RWX class for team folders, or empty for none | `nfs-csi`, `azurefile-csi`, `efs-sc` |
| `AUTH` | `dev` (no login, one trusted user) or `oidc` (Keycloak, Entra, Okta…) | `dev` first, `oidc` before inviting people |
| `EXPOSE` | `port-forward`, `ingress`, `istio` | `port-forward` first |
| `PORTAL_HOST`, `TLS_SECRET` | DNS name and certificate Secret for the public route | `workspaces.acme.com` |
| `ALLOW_CIDRS` | who may use a public `AUTH=dev` route (refused without it) | your office IP |
| `DEVELOPERS`, `PROJECTS`, `SAMPLE_WORKSPACES` | who gets a room, shared namespaces, first dev pods | |

## Typical paths

**Laptop trial (kind/k3s/minikube), 5 minutes**
`AUTH=dev EXPOSE=port-forward` → `./all.sh` → `kubectl -n devspaces-system port-forward svc/workspace-portal 8080:80` → http://localhost:8080.

**A team on a shared cluster with nginx ingress**
`AUTH=oidc` with your Keycloak/Entra issuer, `EXPOSE=ingress PORTAL_HOST=workspaces.acme.com TLS_SECRET=workspaces-tls` (cert-manager) → `./all.sh`. Phase 40 writes `route.yaml`: an Ingress with long WebSocket timeouts + a NetworkPolicy admitting the ingress-controller namespace.

**A cluster that already runs Istio**
Same, with `EXPOSE=istio`. 00 finds the ingress gateway (`istio: ingressgateway` or the AKS add-on's label), 10/50 label namespaces for sidecar or ambient mode, 30 turns on `istioAuth` (STRICT mTLS; with `oidc`, the sidecar also checks JWTs), 40 writes Gateway + VirtualService.

**Something changed**
New image tag → edit `TAG`, run `./30-platform.sh`. New colleague → `./50-developers.sh carol@acme.com`. Move from nginx to Istio → `./40-route.sh --remove`, set `EXPOSE=istio`, `./40-route.sh`. Turn on login → set `AUTH=oidc …`, `./30-platform.sh`, then `./40-route.sh` (drops the IP lock when `ALLOW_CIDRS` is cleared).

## Files this folder writes (all git-ignored)

`detected.env` (00), `values.yaml` (30 — the complete Helm input, worth committing), `rendered.yaml` (30 `--dry-run`), `route.yaml` (40).

## Relation to the other deploy folders

* `deploy/demo/` — Docker-only demos (Compose, kind) for a laptop; no Helm.
* `deploy/phases/` — **this**: the plain Kubernetes path for any cluster.
* `deploy/cluster/providers/<cloud>/` — builds the *cluster itself* (AKS/EKS/GKE) and cloud
  identity; after that you can use these phases or the provider's own scripts.
* `deploy/prod/` — the opinionated production playbook (images, contract gate, CSV onboarding).
