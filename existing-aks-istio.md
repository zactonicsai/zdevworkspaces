# Workspaces on an AKS cluster you already run (with Istio) — step by step

**Who this is for.** You have an AKS cluster — maybe the platform team's shared one — and
Istio is already on it (the AKS *Istio add-on*, or Istio you installed yourself). You want
the developer-workspaces portal and a couple of dev pods on it this afternoon, without
building a new cluster. If you *don't* have a cluster yet, use `azure-aks-setup.md`.

**The picture.** Think of the cluster as a school building that is already open, with hall
monitors (Istio) checking badges on every corridor. We are not building a new wing; we are
setting up one office (the `devspaces-system` namespace) for the front desk (the portal),
giving each developer a classroom (`ws-<name>`), and letting the hall monitors know about
them (the mesh label). Their lockers (home drives) are real Azure disks that stay even if a
classroom is cleared out.

Time: about 30 minutes, most of it waiting for images to pull.

---

## 0. What you need on your laptop

* `az` (logged in: `az login`), `kubectl`, `helm` ≥ 3.12, `python3` ≥ 3.9 (for the admin CLI).
* `ssh-keygen` if you want the scripts to start sample dev pods (they create your SSH key).
* Rights on the cluster: *Azure Kubernetes Service RBAC Cluster Admin* (or the kubeconfig
  of someone who has it) — the install creates ClusterRoles and admission policies.
* The workspace images in a registry the cluster can pull from. Fastest: build them into
  your ACR (`LANGS="go python" REGISTRY=<acr>.azurecr.io/devspaces ./images/build.sh --push`)
  with the ACR attached to the cluster (`az aks update --attach-acr`). Artifactory works too.

> **Background — why Helm?** Helm is a package manager for Kubernetes: one command installs
> ~30 objects (Deployments, ConfigMaps, RBAC, policies) from a *chart* with your *values*
> on top, and `helm upgrade` later changes only what differs. Our chart is
> `charts/devspaces`; the scripts write the values for you.

## 1. Tell the scripts about your cluster (2 minutes)

```bash
cd deploy/cluster/providers/azure/existing
cp env.example env.sh
$EDITOR env.sh
```

Only four lines matter for a trial:

```bash
RESOURCE_GROUP=my-aks-rg
CLUSTER_NAME=my-aks
REGISTRY=myacr.azurecr.io/devspaces     # where workspace-go, workspace-python, workspace-portal images are
DEV_USER=you@corp.example               # AUTH=dev: the one identity the trial portal runs as
```

Leave `AUTH=dev`, `MESH_MODE=auto`, `ISTIO_REVISION=auto` for now.

## 2. Look before you touch: `./00-detect.sh`

Read-only. It connects (`az aks get-credentials`) and prints what it found:

```
==> Istio
  [ok]   AKS Istio add-on (namespace aks-istio-system), revision asm-1-25
  [ok]   istio-cni present → sidecar injection works under Pod Security 'baseline'
  [ok]   ingress gateway: aks-istio-ingress (istio: aks-istio-ingressgateway-external) → 20.51.7.14
  [ok]   mesh mode for tenant namespaces: istio-sidecar (istio.io/rev=asm-1-25)
==> Storage
  [ok]   fast home drives → managed-csi-premium
  [ok]   standard home drives → managed-csi
  [ok]   shared drives (RWX) → azurefile-csi-premium
  [ok]   snapshots: VolumeSnapshotClass csi-azuredisk-vsc
```

Things it may warn about and what they mean:

| Warning | Meaning | What to do |
|---|---|---|
| *no Istio found* | fine — the platform does not need a mesh | nothing; or `az aks mesh enable` |
| *sidecar mode without istio-cni* | the classic sidecar init needs `NET_ADMIN`, which developer namespaces (Pod Security baseline) refuse | use ambient (`az aks mesh enable --revision … ` with ambient, or self-managed ambient), or `MESH_MODE=none` |
| *no ingress gateway* | nowhere to publish the portal | `az aks mesh enable-ingress-gateway --ingress-gateway-type external`, or stay on `AUTH=dev` with port-forward |
| *managed-csi is Delete* | the built-in class deletes the disk with the PVC | step 3 creates the platform's Retain classes |
| *no NetworkPolicy engine* | tenant isolation would not be enforced | `az aks update --network-policy cilium` (or azure) |

> **Background — revisions.** The AKS add-on installs Istio under a *revision* name such as
> `asm-1-25`. A namespace joins the mesh by carrying the label `istio.io/rev=asm-1-25`; when
> Microsoft upgrades the add-on you move namespaces to the new revision label. The scripts
> detect the revision and use it everywhere. Self-managed Istio usually uses
> `istio-injection=enabled` instead; that is detected too.

## 3. Namespace, storage, snapshots: `./10-namespaces.sh`

Creates `devspaces-system` (Pod Security *restricted*, mesh label), applies the
`workspace-home-premium` / `workspace-home-standard` / `workspace-shared-files` classes
with `reclaimPolicy: Retain` + expansion (so a purged drive's disk still exists for an admin
to rescue), and a `VolumeSnapshotClass` for Azure Disk if the cluster had none.

Set `CREATE_STORAGE_CLASSES=false` in `env.sh` to keep the built-in classes instead.

## 4. Install the portal with Helm: `./20-helm.sh`

The script writes `values-existing.yaml` — read it, it is short — and runs
`helm upgrade --install devspaces charts/devspaces -n devspaces-system -f values-existing.yaml`.

With `AUTH=dev` you get:

* the portal (workspaces + tools catalog + guides), the reaper, RBAC and admission policies,
* **no** public address: `kubectl -n devspaces-system port-forward svc/workspace-portal 8080:80`
  and open <http://localhost:8080>. Every call runs as `DEV_USER`. Do not expose this.
* if Istio is present: `PeerAuthentication STRICT` in the namespace, the portal in the mesh.

> **Background — why not expose the trial portal?** The portal has no login of its own. In
> production the login is done by oauth2-proxy (browser) or by checking Entra bearer tokens
> (CLI). `-dev-user` skips both, which is only acceptable when the only way in is your own
> `kubectl port-forward` (which already proves who you are to the API server).

## 5. See what Istio is doing: `./30-istio-check.sh`

Prints enrolment of every platform/tenant namespace, whether the portal pod has its
`istio-proxy` (sidecar mode) or is handled by `ztunnel` (ambient), the `PeerAuthentication`,
and — in `AUTH=entra` mode — the `Gateway`/`VirtualService` and an HTTP check through the
gateway IP. `./30-istio-check.sh --apply-examples` installs the L7 examples from
`deploy/platform/mesh/` into your project namespace (a waypoint, an `AuthorizationPolicy`
that only lets same-namespace and linked workspaces talk, and an `HTTPRoute` that sends
requests carrying `x-dev: alice` to Alice's workspace).

## 6. Developers and dev pods: `./40-dev-pods.sh`

* onboards each name in `DEVELOPERS` (`AUTH=dev`: just `DEV_USER`): namespace `ws-<name>`
  with quota, the `workspace-developer` RoleBinding, default-deny NetworkPolicies and the
  mesh label; `LAB_NAMESPACES=true` adds `ws-<name>-lab` if the cluster has a lab pool;
* creates the shared project namespace `ws-proj-$PROJECT` with all of them as members;
* starts `SAMPLE_WORKSPACES` (default `go:go python:python`) for the first developer with
  `SAMPLE_TOOLS` and a short idle timeout — real dev pods, real home drives.

Then:

```bash
kubectl -n devspaces-system port-forward svc/workspace-portal 8080:80 &
open http://localhost:8080                # My workspaces → go → Connect
python3 cli/workspace --context my-aks -n ws-you ssh go     # or: wsctl login --portal http://localhost:8080 && wsctl ssh go
```

## 7. Check it: `./50-verify.sh`

Portal health through the API-server proxy, catalog served, the home-drive admission policy
(it attempts a *server-side dry-run* delete of a drive and expects **refusal**), mesh
enrolment of every tenant namespace, the first dev pod Ready, contract items C4/C7/C8/C14–C16.

## 8. From trial to real sign-in (when you are ready)

1. Create the Entra app registrations: `../30-entra.sh` (or copy IDs from the Entra portal)
   and put `ENTRA_TENANT_ID`, `ENTRA_PORTAL_CLIENT_ID`, `ENTRA_PORTAL_CLIENT_SECRET`,
   `ENTRA_CLI_CLIENT_ID` into `env.sh`; set `AUTH=entra` and `PORTAL_HOST`.
2. DNS: `PORTAL_HOST` → the gateway IP from step 2. TLS: a Secret in the gateway's namespace
   (`aks-istio-ingress`) from cert-manager or Key Vault, named in `TLS_CREDENTIAL_NAME`.
3. `./20-helm.sh` again. Now: oauth2-proxy signs people in with their company account, the
   `VirtualService` publishes the portal on the Istio gateway, the sidecar enforces
   `RequestAuthentication` for bearer tokens, and `wsctl login --portal https://PORTAL_HOST`
   does a device-code login with no other flags.
4. Add developers to `DEVELOPERS` and re-run `./40-dev-pods.sh`.

## 9. Undo

```bash
helm -n devspaces-system uninstall devspaces          # portal, policies, RBAC (tenant namespaces stay)
kubectl delete ns ws-you ws-proj-team-demo              # a developer's namespace — their drives go too (Retain keeps the disks in Azure)
kubectl delete ns devspaces-system
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| portal pod `CreateContainerConfigError` / image pull | `REGISTRY`/`TAG` wrong or no pull rights | check `values-existing.yaml`, `az aks update --attach-acr`, or `PULL_SECRET_DOCKERCONFIG` |
| workspace pod stuck `Pending` | no node with room, or PVC pending | `kubectl -n ws-you describe pod ws-go-0`; storage class exists? |
| workspace pod `CreateContainerError: hostUsers` | nodes older than kernel 6.3 / containerd 2 | `catalogOverrides.defaults.isolation: none` in `values-existing.yaml`, re-run 20 |
| sidecar never appears on portal | namespace labelled after the pod started | `kubectl -n devspaces-system rollout restart deploy` |
| `istio-init` rejected in `ws-*` | sidecar mode without istio-cni | ambient, or `MESH_MODE=none` |
| 404 from the gateway IP | DNS/Host header, or Gateway in another namespace than the VirtualService expects | `istioctl analyze -n devspaces-system`; check `Host:` header in curl |
| `wsctl login` says tokens not enabled | `AUTH=dev` portal | use `wsctl login --dev --portal …` or switch to `AUTH=entra` |
