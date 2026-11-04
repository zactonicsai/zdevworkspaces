# Azure AKS setup guide — the devspaces platform on Microsoft Azure, step by step

This guide builds the production version of the developer-workspaces system on **Azure
Kubernetes Service (AKS)**, from an empty subscription to developers signing in with their
company (Entra ID) accounts, creating workspaces from the portal or the `wsctl` CLI, keeping
their home drives even when pods and workspaces are deleted, sharing drives and workspaces
with teammates, and calling the same API through **Azure API Management**.

It follows the two-layer design of `docs/production-deployment.md`:

```
                 ┌──────────────────────────────────────────────────────────────────┐
  Phase 7        │  KUBERNETES LAYER  (identical on every cloud)                     │
  "the store"    │  helm chart charts/devspaces + values-azure.yaml                  │
                 ├──────────────────── CONTRACT (deploy/cluster/CONTRACT.md) ───────┤
  Phase 1–6,8–9  │  CLUSTER LAYER on AZURE  (deploy/cluster/providers/azure/)        │
  "the building" │  AKS + Cilium · node pools · disks & files · Entra ID · Key Vault │
                 │  App Gateway / NGINX · API Management · (Cilium multicast)        │
                 └──────────────────────────────────────────────────────────────────┘
```

Every step says **what to type**, **what it does**, and **what you should see**. Commands run
from the unpacked repository folder. Each phase ends with a **checkpoint**; don't move on
until it passes. The whole thing takes an afternoon the first time; the scripts in
`deploy/cluster/providers/azure/` do the typing for you and are safe to re-run.

> **How to read the boxes.** *Background* boxes explain the idea for someone who has never
> touched Azure. *Grocery store* and *school* boxes are the analogies used across all our
> guides. Skip them if you already know; read them when a step feels arbitrary.

---

> **Already have an AKS cluster (perhaps with the Istio add-on)?** Skip the build-out: `deploy/cluster/providers/azure/existing/quickstart.sh` installs the portal and dev pods onto it — see `docs/existing-aks-istio.md`.

## Phase 0 — Prerequisites, names and money (one hour)

> **Background: the words.** A *subscription* is the Azure bill (a company usually has one
> per team or environment). A *resource group* is a folder inside it; deleting the folder
> deletes everything in it, which is why we put the whole platform in one. A *region* is the
> data-centre family (`eastus2`, `westeurope`…). *Entra ID* (formerly Azure AD) is the company
> phone book: users, groups, and "apps" that are allowed to ask who someone is.

### 0.1 Tools on your laptop

| Tool | Minimum | Check | Install |
|---|---|---|---|
| Azure CLI `az` | 2.61 | `az version` | <https://learn.microsoft.com/cli/azure/install-azure-cli> |
| `kubectl` | 1.33 | `kubectl version --client` | `az aks install-cli` (installs `kubelogin` too) |
| `kubelogin` | any | `kubelogin --version` | same as above — needed because AKS uses Entra ID sign-in |
| `helm` | 3.14 | `helm version` | <https://helm.sh/docs/intro/install/> |
| `jq`, `python3` | any | `jq --version`, `python3 -V` | package manager |
| Go 1.22+ (only to build `wsctl`) | 1.22 | `go version` | <https://go.dev/dl/> |

Sign in and pick the subscription:

```bash
az login --tenant <your-tenant>.onmicrosoft.com     # a browser opens
az account list -o table                            # pick the subscription
az account set --subscription "<name or id>"
az account show -o table                            # what you should see: your subscription, IsDefault=True
```

### 0.2 Write the names down once

Copy the example and fill it in. It is the only place you type names, sizes and regions;
every script reads it.

```bash
cp deploy/cluster/providers/azure/env.example deploy/cluster/providers/azure/env.sh
$EDITOR deploy/cluster/providers/azure/env.sh
```

The decisions that matter:

| Variable | Decide | Hint |
|---|---|---|
| `LOCATION` | region close to the developers | AKS 1.36 availability: `az aks get-versions -l <region> -o table` |
| `RESOURCE_GROUP`, `CLUSTER_NAME` | naming convention | one group = one platform; `devspaces-prod-rg` |
| `WORKSPACES_SIZE` | the normal workspace node | `Standard_D8s_v5` = 8 vCPU/32 GiB → ~3 medium workspaces per node |
| `LAB_SIZE`, `LAB_GPU` | hardware-lab node (privileged, one workspace per node) | bigger, maybe N-series GPU; `LAB_MIN=0` so you pay nothing when unused |
| `ACR_NAME`, `KEYVAULT_NAME`, `APIM_NAME` | globally unique names | lowercase, no dashes for ACR |
| `PORTAL_HOST` | the DNS name developers will type | `workspaces.corp.example` |
| `INGRESS` | `appgw-for-containers` (Azure-managed, Gateway API) or `nginx` (portable) | Both work; NGINX is what the other clouds use |
| `ADMIN_GROUP_NAME`, `DEVELOPERS_GROUP_NAME` | Entra groups | admins run `workspace admin …`; developers may sign in |

> **Money, roughly (list prices, pay-as-you-go, eastus2, 2026):** 2 system nodes D4s_v5
> ≈ $280/mo; each workspace node D8s_v5 ≈ $280/mo (autoscaled, 2 minimum); Premium SSD P10
> (128 GiB) home drive ≈ $20/mo per developer; Azure Files Premium ≈ $0.16/GiB-month; APIM
> Developer tier ≈ $50/mo (Consumption: per call); Application Gateway for Containers ≈
> $0.0225/h + capacity units; Key Vault, Log Analytics: a few dollars. The reaper and the
> autoscaler are what keep the node bill proportional to *working* developers, not enrolled ones.

### 0.3 Run the checker

```bash
./deploy/cluster/providers/azure/00-prereqs.sh
```

It checks the tools, that you are logged in, registers the Azure *resource providers* the
platform uses (a one-time per-subscription switch — think "turn on the Kubernetes aisle in
this store"), looks at your vCPU **quota** in the region, and whether your ACR/Key Vault names
are free.

**What you should see:** `[ok]` lines and finally `All good. Next: 10-aks.sh`. A `[warn]` on
quota means: request more before Phase 2 (*Portal → Subscriptions → Usage + quotas*;
`Standard Dsv5 Family vCPUs` is the one you need; approvals take minutes to days).

**Checkpoint 0:** the checker passes; `env.sh` is filled in; you know who the admins and
developers groups are.

---

## Phase 1 — The network (15 minutes)

> **Background: VNet and subnets.** A *virtual network* (VNet) is your private street;
> *subnets* are the buildings on it. AKS nodes live in one subnet. The Azure load balancer /
> Application Gateway lives in another. Pods get addresses from an *overlay* range that
> exists only inside the cluster (`POD_CIDR`), which is why a VNet of `/16` is plenty.

Phase 1 and 2 are both done by `10-aks.sh`; run it once and read along:

```bash
./deploy/cluster/providers/azure/10-aks.sh --dry-run     # prints every az command it would run
./deploy/cluster/providers/azure/10-aks.sh               # does it (15–25 minutes)
```

Step 1–2 of the script create the resource group and the VNet with two subnets:

```bash
az group create -n devspaces-prod-rg -l eastus2
az network vnet create -g devspaces-prod-rg -n devspaces-vnet --address-prefixes 10.10.0.0/16 \
   --subnet-name nodes --subnet-prefixes 10.10.0.0/20
az network vnet subnet create -g devspaces-prod-rg --vnet-name devspaces-vnet -n appgw \
   --address-prefixes 10.10.16.0/24 --delegations Microsoft.ServiceNetworking/trafficControllers
```

The `appgw` subnet is *delegated* to Application Gateway for Containers (only that service may
use it). If you chose `INGRESS=nginx` it is harmless and unused.

**Private cluster?** `PRIVATE_CLUSTER=true` makes the Kubernetes API reachable only from the
VNet (VPN/ExpressRoute/Bastion). Developers never talk to the API directly — the portal does —
so this mostly affects *admins* and CI. Start public with authorized IP ranges if unsure:
`az aks update --api-server-authorized-ip-ranges <office-cidr>` later.

**Checkpoint 1:** `az network vnet subnet list -g devspaces-prod-rg --vnet-name devspaces-vnet -o table`
shows `nodes` and `appgw`.

---

## Phase 2 — The AKS cluster and its node pools (25 minutes)

> **Background: what makes this cluster special.** Three things the contract
> (`deploy/cluster/CONTRACT.md`) demands and most "default" AKS clusters do not have:
> (C8) a network plugin that **enforces NetworkPolicy** — we use *Azure CNI powered by
> Cilium*; (C2) node images with **containerd 2 and a 6.x kernel** so workspaces can run
> inside user namespaces (`hostUsers: false`) — Ubuntu 22.04+/Azure Linux node SKUs on AKS
> 1.36; (C3) a **tainted lab pool** where privileged hardware workspaces run alone.

The script's step 4:

```bash
az aks create -g devspaces-prod-rg -n devspaces-prod -l eastus2 --kubernetes-version 1.36.x --tier standard \
  --network-plugin azure --network-plugin-mode overlay --network-dataplane cilium --network-policy cilium \
  --pod-cidr 10.244.0.0/16 --service-cidr 10.96.0.0/16 --dns-service-ip 10.96.0.10 --vnet-subnet-id <nodes subnet> \
  --enable-managed-identity --enable-oidc-issuer --enable-workload-identity \
  --enable-aad --aad-tenant-id <tenant> --enable-azure-rbac \
  --nodepool-name system --node-count 2 --node-vm-size Standard_D4s_v5 --os-sku AzureLinux \
  --nodepool-labels devspaces.io/pool=system --nodepool-taints CriticalAddonsOnly=true:NoSchedule \
  --enable-addons monitoring --enable-azure-monitor-metrics \
  --auto-upgrade-channel patch --node-os-upgrade-channel NodeImage --generate-ssh-keys
```

Line by line, in plain words:

| Flag | Means |
|---|---|
| `--network-dataplane cilium --network-policy cilium` | Cilium's eBPF datapath; NetworkPolicy works (tenant isolation, links) |
| `--network-plugin-mode overlay --pod-cidr` | pods get private addresses; the VNet stays small |
| `--enable-oidc-issuer --enable-workload-identity` | pods can prove who they are to Azure (Key Vault access without secrets) |
| `--enable-aad --enable-azure-rbac` | `kubectl` users are Entra identities (e-mails) — contract C7; Azure RBAC decides who is cluster admin |
| `system` pool, `AzureLinux`, tainted `CriticalAddonsOnly` | platform pods only (portal, proxy, ingress, cert-manager); workspaces never land here |
| `--tier standard` | 99.95 % API server SLA; required for anything production |
| `--auto-upgrade-channel patch`, `--node-os-upgrade-channel NodeImage` | security patches arrive on their own (you still pick maintenance windows, Phase 10) |

Steps 5–6 add the pools developers actually run on:

```bash
# standard workspaces: autoscaled 2–20, Ubuntu 22.04 (containerd 2 → user namespaces), ephemeral OS disk
az aks nodepool add -g devspaces-prod-rg --cluster-name devspaces-prod -n workspaces --mode User --os-sku Ubuntu2204 \
  --node-vm-size Standard_D8s_v5 --enable-cluster-autoscaler --min-count 2 --max-count 20 --node-count 2 \
  --node-osdisk-size 128 --node-osdisk-type Ephemeral --labels devspaces.io/pool=standard
# hardware lab: scales to ZERO, tainted so only lab workspaces schedule here, optional GPU driver
az aks nodepool add ... -n lab --node-vm-size Standard_D16s_v5 --min-count 0 --max-count 5 --node-count 0 \
  --labels devspaces.io/pool=lab devspaces.io/lab=true --node-taints devspaces.io/lab=true:NoSchedule [--gpu-driver install]
```

> **School:** the system pool is the staff room (students never sit there); the workspaces
> pool is the classrooms (more open as more students arrive, close when empty); the lab pool
> is the chemistry lab — locked (taint), one experiment per bench (anti-affinity), and the
> lights are off when nobody has booked it (`min-count 0`).

Step 7 attaches the registry (Phase 3) and fetches credentials; step 8 installs the ingress
controller you chose (Phase 6). Then:

```bash
kubectl get nodes -L devspaces.io/pool,devspaces.io/lab -o wide
# NAME                            STATUS  POOL        LAB    OS-IMAGE             KERNEL   CONTAINER-RUNTIME
# aks-system-xxxxx-vmss000000     Ready   system             CBL-Mariner/Linux    6.6.x    containerd://2.0.x
# aks-workspaces-xxxxx-vmss000000 Ready   standard           Ubuntu 22.04.x LTS   6.8.x    containerd://2.0.x
kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints[*].key
```

`kubectl` will open a browser the first time (that is `kubelogin` doing Entra ID sign-in).
The person who ran the script is *Azure Kubernetes Service RBAC Cluster Admin*; the script
also grants it to `ADMIN_GROUP_NAME` once that group exists (Phase 5) — re-run `10-aks.sh`
afterwards; it only adds what is missing.

**Checkpoint 2:** nodes `Ready`, kernel ≥ 6.3 and containerd ≥ 2.0 on the workspaces pool, the
lab pool shows the taint (it may have 0 nodes — that is correct). Run the contract probe early:
`./deploy/cluster/check-contract.sh` should pass C1, C2, C3, C7, C8 already.

---

## Phase 3 — Registry: ACR and/or Artifactory (20 minutes)

> **Grocery store:** Artifactory is the company warehouse that supplies every store. On Azure
> you may add a *local cold room* — Azure Container Registry (ACR) — right next to the cluster:
> the same goods, faster to fetch, and the cluster's own identity may open it (no password).

Three valid setups:

| Setup | Images from | Tools/packages from | Values |
|---|---|---|---|
| A. Artifactory only (default elsewhere) | Artifactory | Artifactory | `global.registry: artifactory…/devspaces-docker`, `pullSecretName: artifactory-pull` |
| B. ACR + Artifactory (**this guide**) | ACR (attached, no secret) | Artifactory | `global.registry: <acr>.azurecr.io/devspaces`, `pullSecretName: ""` |
| C. ACR mirroring Artifactory | ACR (cache rule or import) | Artifactory | as B + `az acr cache create` |

`10-aks.sh` created the ACR and ran `az aks update --attach-acr`, which gives the kubelet
identity *AcrPull*. Build and push the images (`deploy/prod/10-images.sh` does exactly this):

```bash
az acr login -n devspacesacr
export REGISTRY=devspacesacr.azurecr.io/devspaces TAG=2026.11
LANGS="go python rust node java cpp" PUSH=1 ./images/build.sh          # ~30–60 min first time
az acr repository list -n devspacesacr -o table
# workspace-base, workspace-go, workspace-python, …, workspace-cli, workspace-portal
```

Faster alternative: build *in* Azure with `az acr build -r devspacesacr -t devspaces/workspace-base:2026.11 -f images/base/Dockerfile images`.

Optional hardening: `az acr update -n devspacesacr --public-network-enabled false` plus a
private endpoint in the `nodes` subnet; Defender for Containers image scanning; a *cache rule*
so `devspacesacr.azurecr.io/devspaces/*` pulls through from Artifactory on first use.

**Checkpoint 3:** `kubectl run t --rm -it --image=devspacesacr.azurecr.io/devspaces/workspace-cli:2026.11 --restart=Never -- workspace --version`
prints the CLI version (image pulled without any pull secret).

---

## Phase 4 — Storage: home drives that outlive pods (20 minutes)

This phase is the heart of "**save the dev home drive, and let them choose, even if they
delete their dev pod**".

> **Background: four kinds of "delete".** A developer's files live on a *PersistentVolumeClaim*
> (PVC) — in Azure, a managed disk. Four different things can be deleted, and only the last
> one touches the files:
>
> | Action | What goes away | Files? |
> |---|---|---|
> | the **pod** dies or is deleted (node upgrade, OOM, `kubectl delete pod`) | the running container | **kept** — the StatefulSet recreates the pod on the *same* disk |
> | **stop** (`wsctl stop`, or the idle reaper) | the pod, the node capacity | **kept** — PVC unchanged |
> | **delete the workspace** (`wsctl delete`) | StatefulSet, Service, Secrets, policies | **kept** — the PVC stays as a *detached home drive* and is listed under *Drives*; a new workspace can re-attach it (`--from-drive`) under any name |
> | **purge** (`wsctl delete --purge`, *Drives → Delete*) | the PVC | the Kubernetes object is gone, **but** the StorageClass says `reclaimPolicy: Retain`, so the Azure disk still exists until an admin deletes it |
>
> A ValidatingAdmissionPolicy refuses to delete a home-drive PVC unless it was first marked
> `devspaces.io/allow-delete=true` — which only purge does. There is no path by which an
> ordinary mistake erases a drive.

### 4.1 The storage classes

`deploy/cluster/providers/azure/20-storage.yaml` defines what developers choose from:

| Catalog name (what devs type) | StorageClass | Azure | Access | For |
|---|---|---|---|---|
| `fast` (default) | `workspace-home-premium` | Premium SSD managed disk (`Premium_LRS`) | ReadWriteOnce | `/home/dev`: builds, IDE indexes |
| `standard` | `workspace-home-standard` | Standard SSD (`StandardSSD_LRS`) | ReadWriteOnce | cheaper homes for light work |
| `shared` | `workspace-shared-files` | Azure Files **Premium** share (SMB, `uid=1000`) | **ReadWriteMany** | team folders at `/shared/<name>`, mountable by several workspaces and sharable with teammates |
| (snapshots) | `csi-azuredisk-vsc` | incremental disk snapshots | — | `wsctl drives snapshot`, restore to a new drive |

All three classes: `reclaimPolicy: Retain`, `allowVolumeExpansion: true` (grow in place:
`wsctl drives resize api 100Gi`), `WaitForFirstConsumer` for disks (created in the pod's zone).

```bash
./deploy/cluster/providers/azure/20-storage.sh
kubectl get storageclass
# NAME                         PROVISIONER          RECLAIMPOLICY   VOLUMEBINDINGMODE      ALLOWVOLUMEEXPANSION
# workspace-home-premium (default)  disk.csi.azure.com  Retain  WaitForFirstConsumer  true
# workspace-home-standard      disk.csi.azure.com   Retain          WaitForFirstConsumer   true
# workspace-shared-files       file.csi.azure.com   Retain          Immediate              true
kubectl get volumesnapshotclass
# csi-azuredisk-vsc   disk.csi.azure.com   Retain
```

The script also removes the "default" mark from AKS's built-in `default`/`managed-csi` classes
(contract C4 wants *our* class to be default so a plain `size: 50Gi` request lands on it; C14 asks for `Retain` + expansion, C15 for the snapshot class, C16 for the ReadWriteMany class).

> **Why SMB for shared drives, and when NFS.** SMB shares need no network setup and honour the
> `uid=1000` mount options so files belong to `dev`. NFS 4.1 shares (`protocol: nfs`) are
> faster for Linux-only use but require a private endpoint and disabling secure transfer on
> the storage account. Start with SMB; switch the `shared` class to NFS later if build
> throughput on shared data matters.

### 4.2 What developers can do (portal → *Drives*, or `wsctl drives`)

```bash
wsctl create api --lang go --storage fast --size 64Gi         # pick the class and size at creation
wsctl drives list
# NAME           KIND   SIZE   STORAGE  ATTACHED TO  LAST WORKSPACE  LANGUAGES
# ws-api-home    home   64Gi   fast     api          api             go
wsctl delete api                                              # workspace gone, drive detached
wsctl drives list                                             # ws-api-home  home  64Gi  fast  (detached)  api  go
wsctl create api2 --lang go,python --from-drive ws-api-home   # same files, new name, extra language
wsctl drives snapshot api2 before-upgrade                     # incremental Azure snapshot
wsctl create api3 --lang go --from-snapshot before-upgrade    # a clone to try something risky
wsctl drives resize api2 128Gi                                # grows online (ext4 resize at next mount/online)
wsctl drives create-shared team-data --size 200Gi             # Azure Files, RWX
wsctl create etl --lang python --mount shared:team-data       # appears at /shared/team-data
wsctl share drive team-data bob@corp.example                  # Bob mounts it as shared:alice/team-data
```

### 4.3 Recovering a retained disk (admin)

A purged home drive's PV goes to `Released` (the PVC is gone, the disk is not):

```bash
kubectl get pv | grep Released
# pvc-6f1c…   64Gi  RWO  Retain  Released  ws-alice/ws-api-home  workspace-home-premium
az disk list -g MC_devspaces-prod-rg_devspaces-prod_eastus2 --query "[?tags.\"kubernetes.io-created-for-pvc-name\"=='ws-api-home'].{name:name,size:diskSizeGb,created:timeCreated}" -o table
```

To hand it back: clear the old claim reference and let a new PVC with the same storage class
adopt it, then create the workspace from that drive:

```bash
kubectl patch pv pvc-6f1c… -p '{"spec":{"claimRef":null}}'
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ws-api-home-recovered
  namespace: ws-alice
  labels: { devspaces.io/drive: home }
  annotations: { devspaces.io/last-workspace: api }
spec:
  storageClassName: workspace-home-premium
  accessModes: [ReadWriteOnce]
  resources: { requests: { storage: 64Gi } }
  volumeName: pvc-6f1c…
EOF
# as Alice:  wsctl create api --lang go --from-drive ws-api-home-recovered
```

If even the PV was deleted, the Azure disk is still in the node resource group:
`az disk show -g MC_… -n pvc-6f1c…` → create a static PV with `csi.driver: disk.csi.azure.com`,
`volumeHandle: <disk resource id>` and proceed as above. Delete retained disks you really
do not want (`az disk delete`) on a schedule — they cost money.

### 4.4 Backups

Snapshots are per drive and live in the same region. For disaster recovery add one of:

* **Azure Backup for AKS** (vaulted backups of PVs + cluster state, cross-region restore):
  `az aks extension` *Microsoft.DataProtection.Kubernetes*, a Backup vault, a policy for
  namespaces `ws-*`. Managed; recommended.
* **Velero** with the Azure plugin (portable, same tool on every cloud): backs up
  `ws-*` namespaces and snapshots disks to a storage account.

Either way, **exclude** nothing in `ws-*`: the Secrets (host keys, deploy keys) are what make a
restored workspace reachable without developers re-adding keys.

**Checkpoint 4:** `kubectl apply` a 1 GiB PVC on each class in a test namespace → all `Bound`
(the disk ones only after a pod mounts them); `kubectl delete pvc` → PV goes `Released`, not
deleted; clean up (`kubectl delete pv …`, `az disk delete …`).

---

## Phase 5 — Identity: Entra ID, Key Vault (30 minutes)

> **Background: three app registrations.** Entra ID will issue tokens for three "apps":
> *devspaces-portal* — the website; a browser signs in here (oauth2-proxy handles it).
> *devspaces-api* — not a program, a *name for the API* (`api://devspaces`); tokens meant for
> the API say so in their `aud` (audience) claim, which is how the portal and API Management
> know a token was really meant for them. *devspaces-cli* — the `wsctl` command on laptops; it
> cannot keep a secret, so it is a *public client* using the **device-code flow** (type a code
> on a web page, like signing a TV into a streaming service).
>
> **School:** the portal app is the front desk where visitors sign in; the API app is the
> school's name printed on every visitor badge; the CLI app is the phone-in attendance line
> that gives you a one-time code.

```bash
./deploy/cluster/providers/azure/30-entra.sh
```

What it does (all idempotent, by display name):

1. Groups `devspaces-platform-admins` and `devspaces-developers` (if they do not exist).
2. `devspaces-api`: identifier URI `api://devspaces`, scope `Workspaces.ReadWrite`,
   **v2 tokens**, group claims as `groups`, a service principal.
3. `devspaces-portal`: web redirect `https://<PORTAL_HOST>/oauth2/callback`, ID tokens with
   `groups`/`preferred_username`/`email`, a 1-year client secret (saved to a local file only
   until Phase 5.2 moves it to Key Vault), permission to request the API scope.
4. `devspaces-cli`: public client, permission on `api://devspaces/Workspaces.ReadWrite`,
   **pre-authorised** on the API app so developers never see a consent prompt.
5. Admin consent for both; optionally restricts portal sign-in to `devspaces-developers`.
6. Writes `deploy/cluster/providers/azure/entra.values.yaml` — the Helm values with the ids.

**What you should see:** the summary with tenant id, three app ids, the admin group id, and
`Next: 40-keyvault.sh`. In the Azure portal: *Entra ID → App registrations → devspaces-api →
Expose an API* shows the scope and `devspaces-cli` under *Authorized client applications*.

Put people in the groups now: *Entra ID → Groups → devspaces-developers → Members*. Group
membership is what the APIM policy and oauth2-proxy check; `workspace admin onboard` creates
namespaces per person (or per group, see Phase 7).

### 5.2 Secrets in Key Vault, never in values files

```bash
./deploy/cluster/providers/azure/40-keyvault.sh
```

Creates the vault (RBAC mode, purge protection), stores `oauth2-client-secret` (moved from
the file, which is then deleted) and a fresh `oauth2-cookie-secret`, creates the
user-assigned identity `devspaces-platform-id` with *Key Vault Secrets User*, a **federated
credential** so the Kubernetes ServiceAccount `devspaces-system/devspaces-secrets` may act as
that identity (workload identity: no secret anywhere), enables the AKS *Secrets Store CSI*
add-on, and applies a `SecretProviderClass` that materialises the Kubernetes Secret
`oauth2-proxy` (keys `client-secret`, `cookie-secret`) once a pod mounts it. Rotation is
automatic every two minutes.

> **Why not just `kubectl create secret`?** You can (`deploy/prod/secrets.values.yaml`,
> git-ignored). Key Vault adds: an audit log of who read the secret, rotation without
> redeploying, and no copy of the secret on the laptop that ran Helm.

**Checkpoint 5:** `az keyvault secret list --vault-name <kv> -o table` shows two secrets;
`kubectl -n devspaces-system get secretproviderclass devspaces-keyvault` exists;
`entra.values.yaml` is written. (The `oauth2-proxy` Secret appears only after Phase 7, when a
pod mounts the CSI volume — that is normal.)

---

## Phase 6 — Front door: ingress, TLS, DNS (20 minutes)

Two good choices; the contract (C5/C6) only needs *an* IngressClass or Gateway and TLS.

### 6.1 Option A — Application Gateway for Containers (Azure-managed, Gateway API)

`10-aks.sh` with `INGRESS=appgw-for-containers` installed the ALB controller. Create the
gateway once (the chart then emits an `HTTPRoute` for the portal, values `ingress.gatewayAPI`):

```bash
cat <<EOF | kubectl apply -f -
apiVersion: alb.networking.azure.io/v1
kind: ApplicationLoadBalancer
metadata: { name: devspaces-alb, namespace: devspaces-system }
spec:
  associations: [ "$(az network vnet subnet show -g devspaces-prod-rg --vnet-name devspaces-vnet -n appgw --query id -o tsv)" ]
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: devspaces-gateway
  namespace: devspaces-system
  annotations: { alb.networking.azure.io/alb-name: devspaces-alb, alb.networking.azure.io/alb-namespace: devspaces-system }
spec:
  gatewayClassName: azure-alb-external
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      hostname: workspaces.corp.example
      tls: { mode: Terminate, certificateRefs: [ { kind: Secret, name: workspaces-tls } ] }
      allowedRoutes: { namespaces: { from: Same } }
EOF
kubectl -n devspaces-system get gateway devspaces-gateway -o jsonpath='{.status.addresses[0].value}'   # the FQDN to CNAME to
```

Internal only: use `gatewayClassName: azure-alb-internal` (private IP in the appgw subnet).

### 6.2 Option B — NGINX ingress + cert-manager (portable)

`INSTALL_INGRESS_NGINX=true INSTALL_CERT_MANAGER=true ./deploy/cluster/addons.sh` installs
both; `ingress.className: nginx` in values. Internal only: annotate the controller's Service
with `service.beta.kubernetes.io/azure-load-balancer-internal: "true"`.

### 6.3 Certificates and DNS

* **cert-manager + Let's Encrypt** (`CERT_ISSUER=letsencrypt`): the chart adds the annotation;
  works with both options (for Gateway API, cert-manager's `gateway-shim`).
* **Key Vault certificate**: import your company cert into the vault, add it to the
  `SecretProviderClass` objects (type `secret`, PEM), and point `ingress.tls.secretName` at the
  synced Secret — the renewal happens in Key Vault.
* **DNS**: `az network dns record-set cname set-record -g <dns-rg> -z corp.example -n workspaces -c <gateway FQDN>`
  (or an A record for the NGINX load balancer IP). Private DNS zones for internal setups.

**Checkpoint 6:** `curl -sI https://workspaces.corp.example/healthz` returns `404` or `503`
from the gateway with a valid certificate (the portal is not installed yet — the TLS part is
what you are testing). `./deploy/cluster/check-contract.sh` now passes C5 and C6.

---

## Phase 7 — The Kubernetes layer: install the platform (20 minutes)

Everything cloud-specific is now behind the contract. Install the chart like on any cloud,
with the Azure values file plus the generated identity snippet:

```bash
./deploy/cluster/check-contract.sh            # the gate: all required Cx must pass
helm upgrade --install devspaces charts/devspaces -n devspaces-system --create-namespace \
  -f charts/devspaces/examples/values-azure.yaml \
  -f deploy/cluster/providers/azure/entra.values.yaml
kubectl -n devspaces-system get pods
# workspace-portal-…    2/2 Running      oauth2-proxy-…   1/1 Running
kubectl -n devspaces-system get secret oauth2-proxy          # created by the CSI driver on first mount
```

The values worth understanding (`charts/devspaces/examples/values-azure.yaml`):

| Values key | Azure meaning |
|---|---|
| `global.registry: <acr>.azurecr.io/devspaces`, `pullSecretName: ""` | ACR attached → no pull secret |
| `storage.choices[]` (`fast`, `standard`, `shared`), `storage.snapshotClass` | Phase 4 classes, surfaced in the catalog and the create form |
| `storage.protectHomeDrives: true` | the admission policy that protects home PVCs |
| `catalogOverrides.networking.clusterCIDRs` | pod + service + VNet ranges excluded from egress; `blockedEgress` adds IMDS `169.254.169.254` |
| `catalogOverrides.networking.multicast.mode: cilium` | only usable with BYO Cilium (Phase 9) |
| `catalogOverrides.networking.mesh: none` | set `istio-ambient` after installing Istio (Phase 9) |
| `projects[]` | team namespaces created at install (members = UPNs or `group:<name>`) |
| `portal.oidc.{issuer,audience,userClaim}` | the portal validates Entra **bearer tokens** itself: this is what makes `wsctl` and APIM work |
| `portal.cliLogin.{tenant,clientId,scope}` | published on `/api/me` so `wsctl login --portal URL` needs nothing else |
| `oauth2Proxy.provider: entra-id`, `existingSecret: oauth2-proxy`, `secretProviderClass`, `serviceAccount` | browser sign-in via Entra; secret from Key Vault via workload identity |
| `ingress.className: azure-alb-external`, `ingress.gatewayAPI.enabled: true` | Phase 6 option A; set `nginx` + `gatewayAPI.enabled: false` for option B |
| `runtimeClasses.gvisor.enabled: false`, `kata.enabled: true` | AKS has no gVisor; Kata (pod sandboxing) on the lab pool if you need `--isolation kata` |

### 7.1 Onboard developers and teams

```bash
./deploy/prod/30-onboard.sh users.csv                 # e-mail,lab(true/false) per line
# or one at a time (as a platform admin):
workspace admin onboard alice@corp.example --lab
workspace admin onboard devspaces-developers --group   # binds the Entra group; each person still needs their own namespace (onboard them by UPN)
workspace admin project create checkout --members alice@corp.example,bob@corp.example
```

On AKS with Azure RBAC, the *username* Kubernetes sees is the UPN (`alice@corp.example`) —
exactly what oauth2-proxy forwards (`preferred_username`/`email`) and what the portal
impersonates. If your tenant's UPNs differ from e-mails, set `portal.oidc.userClaim: upn`
and `oauth2Proxy` `--oidc-email-claim=upn` so all three agree (contract C7).

### 7.2 Smoke test (as a developer)

```bash
./deploy/prod/40-smoke-test.sh                        # creates, connects, stops, deletes a test workspace
# by hand, from a laptop:
go install ./clients/wsctl && wsctl login --portal https://workspaces.corp.example   # device code in the browser
wsctl me                                              # user, namespaces, projects
wsctl create hello --lang go --storage fast
wsctl ssh hello                                       # no kubectl on the laptop: tunnel via the portal
wsctl drives list; wsctl stop hello; wsctl start hello; wsctl delete hello; wsctl drives list
```

Open `https://workspaces.corp.example` in a browser: Entra sign-in → the portal → *Create*
shows the **Storage** dropdown (fast/standard) and the **Drives** page.

**Checkpoint 7:** smoke test passes; a workspace survives `kubectl -n ws-alice delete pod
ws-hello-0` with files intact; `wsctl delete hello` leaves `ws-hello-home` listed as detached;
`wsctl delete hello --purge` deletes it and the PV shows `Released`.

---

## Phase 8 — Azure API Management: the Workspaces API as an Azure API (45 minutes)

> **Background: why put APIM in front of an API that already checks tokens?** The portal
> validates Entra tokens itself, so APIM is not required. Teams add it for what Azure gives
> on top: one company front door for all internal APIs, a developer portal with the OpenAPI
> docs and "try it", per-user rate limits, request logs in Application Insights, IP
> restrictions, versioning, and the ability to hand automation (CI, bots) subscription keys
> or managed identities without touching the portal. **Grocery store:** the store already
> checks membership cards at the till; APIM is the mall entrance that also checks cards,
> counts visitors, and keeps the directory of every shop.

```bash
./deploy/cluster/providers/azure/50-apim.sh      # Developer SKU: 30–45 minutes; Consumption: ~2 minutes
```

The Bicep (`50-apim.bicep`) creates:

* the APIM instance (`APIM_SKU`), system-assigned identity;
* **named values** `devspaces-tenant-id`, `devspaces-audience` (`api://devspaces`), `devspaces-required-group`;
* a **backend** `devspaces-portal` → `https://workspaces.corp.example` (TLS verified);
* the API `workspaces` imported from **`api/openapi.yaml`** (every operation the portal, UI
  and `wsctl` use), path `/workspaces`, no subscription key required;
* the inbound **policy** (`apim-policy.xml`):

```xml
<validate-azure-ad-token tenant-id="{{devspaces-tenant-id}}" header-name="Authorization" failed-validation-httpcode="401">
  <audiences><audience>{{devspaces-audience}}</audience></audiences>
  <required-claims><claim name="scp" match="any"><value>Workspaces.ReadWrite</value></claim></required-claims>
</validate-azure-ad-token>
<!-- 403 unless the token's groups contain devspaces-required-group (when set) -->
<rate-limit-by-key calls="300" renewal-period="60" counter-key="@(…oid claim…)" />
<set-backend-service backend-id="devspaces-portal" />   <!-- the original Authorization header is forwarded -->
```

* a published product *Developers* containing the API.

Test it exactly as a developer or a script would:

```bash
TOKEN=$(az account get-access-token --resource api://devspaces --query accessToken -o tsv)
curl -sS -H "Authorization: Bearer $TOKEN" https://devspaces-apim.azure-api.net/workspaces/api/me | jq .
curl -sS -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"from-apim","languages":["python"],"storage":"standard"}' \
  https://devspaces-apim.azure-api.net/workspaces/api/workspaces
curl -sS https://devspaces-apim.azure-api.net/workspaces/api/me              # → 401 (no token)
wsctl login --portal https://devspaces-apim.azure-api.net/workspaces --tunnel-host https://workspaces.corp.example --az
wsctl list
```

Two details to know:

* **WebSocket tunnel** (`/api/workspaces/{name}/tunnel`, used by `wsctl ssh`): APIM proxies
  WebSockets only on some SKUs and not for imported OpenAPI operations, so `wsctl` sends the
  tunnel to `--tunnel-host` (the portal) and everything else to APIM. Both validate the same token.
* **Private backend**: if the portal is internal-only, deploy APIM in *internal VNet mode*
  (Developer/Premium) or use a *Private Link* backend; the Bicep's `backendUrl` is all that changes.

Automation without a human: create an Entra *service principal* or a *managed identity*, grant
it the app role (add an app role to `devspaces-api` for daemons), onboard it like a user
(`workspace admin onboard <client-id>` — Entra app tokens carry the app id as the identity), and let it call APIM with
`az account get-access-token --resource api://devspaces` from a pipeline.

**Checkpoint 8:** the three curl results above are `200`, `201`, `401`; the APIM *Developer
portal* shows the Workspaces API with its operations from `api/openapi.yaml`.

---

## Phase 9 — Multicast and (optional) Istio ambient

### 9.1 Multicast on AKS

Azure VNets **do not forward multicast**, so the Multus/macvlan mode cannot work on AKS.
Cilium's eBPF multicast (packets replicated over the VXLAN overlay) does — but only when
**you** run Cilium ("BYO CNI"), because managed *Azure CNI powered by Cilium* does not expose
`multicast-enabled` or the tunnel-mode knob.

```bash
./deploy/cluster/providers/azure/60-multicast-cilium.sh
# on a managed-Cilium cluster it stops and prints the three options; on a BYO-Cilium cluster it enables multicast and programs MULTICAST_GROUPS
```

If multicast developers are few, the practical answer is a **second, small cluster** (or an
on-prem lab cluster in Multus mode) just for them: `az aks create … --network-plugin none`,
`helm install cilium cilium/cilium --set aksbyocni.enabled=true --set nodeinit.enabled=true
--set routingMode=tunnel --set tunnelProtocol=vxlan`, then the generic
`deploy/cluster/multicast-cilium.sh 239.255.0.0/24`. Verification is the same everywhere:
`mcast-check --iface eth0` in two workspaces (see `docs/production-deployment.md` Part F).
On the main cluster, hide the option: remove the `mcast` tool via `catalogOverrides`.

### 9.2 Istio ambient (identity, L7 routing between developers' pods)

Not needed for workspaces to talk to each other — `docs/workspace-networking.md` explains the
three NetworkPolicy tiers (own namespace, project namespace, links) that cover almost
everything. Add Istio **ambient** when a team needs mTLS identity, L7 authorization or
"route `x-dev: alice` to Alice's copy of the service":

```bash
az aks mesh enable -g devspaces-prod-rg -n devspaces-prod     # AKS Istio add-on (check ambient-mode availability in your region)
# or OSS: istioctl install --set profile=ambient --set values.cni.chained=true   (Cilium keeps enforcing NetworkPolicy)
helm upgrade devspaces charts/devspaces -n devspaces-system --reuse-values --set catalogOverrides.networking.mesh=istio-ambient
kubectl apply -f deploy/platform/mesh/waypoint.yaml -n ws-proj-checkout          # examples; adapt names
kubectl apply -f deploy/platform/mesh/httproute-dev-routing.yaml
```

**Checkpoint 9:** (multicast) `mcast-check` passes between two workspaces on the BYO-Cilium
cluster; (mesh) `kubectl get ns ws-alice -L istio.io/dataplane-mode` shows `ambient` after
re-onboarding, and `wsctl ssh` still works (the root `AuthorizationPolicy` keeps port 2222 open
for the portal).

---

## Phase 10 — Day 2: upgrades, scaling, cost, monitoring, troubleshooting

### 10.1 Upgrades

* AKS control plane: `--auto-upgrade-channel patch` keeps 1.36.x current; minor upgrades
  (`az aks upgrade --kubernetes-version 1.37.x`) you schedule. Set a **maintenance window**:
  `az aks maintenanceconfiguration add -g … --cluster-name … -n aksManagedAutoUpgradeSchedule --schedule-type Weekly --day-of-week Saturday --start-time 02:00 --duration 6`.
* Node images: `NodeImage` channel + a second window `aksManagedNodeOSUpgradeSchedule`. Pods
  are drained; a workspace pod simply comes back on another node **with the same disk**
  (that is what the StatefulSet + PVC design buys you). Developers see a 1–2 minute SSH drop.
* The platform: `helm upgrade` with a new `global.tag`; existing workspaces keep their image
  until recreated (*Recreate with changes* in the portal keeps the drive).

### 10.2 Scaling and cost controls

| Lever | Where | Effect |
|---|---|---|
| Idle reaper (`reaper.schedule`, per-workspace `idleTimeout`) | chart | stops forgotten workspaces → autoscaler removes nodes |
| Cluster autoscaler min/max per pool | `10-aks.sh` (`*_MIN/_MAX`) | lab pool at 0 when nobody books hardware |
| `SPOT_POOL=true` | `10-aks.sh` | a `spot` pool for throw-away workspaces (profile with `nodeSelector devspaces.io/pool=spot`, tolerations for `kubernetes.azure.com/scalesetpriority=spot`) |
| ResourceQuota per namespace (`admin onboard --cpu --memory --storage`) | onboarding | no single developer can take the cluster |
| `standard` storage class, retained-disk cleanup | Phase 4 | disks are the long-tail cost; list `Released` PVs monthly |
| Azure Advisor + Cost Management budgets with alerts on the resource group | Azure | the bill you expected |

### 10.3 Monitoring

`10-aks.sh` enabled **Container insights** (logs) and **Azure Monitor managed Prometheus**
(metrics). Useful queries: workspace pods per namespace (`kube_pod_info{namespace=~"ws-.*"}`),
reaper actions (`kubectl -n devspaces-system logs job/…`), PVC usage
(`kubelet_volume_stats_used_bytes{namespace=~"ws-.*"}`) to warn developers before `/home/dev`
fills. Alert on: portal `5xx` rate, `Pending` workspace pods > 10 min (quota or node pool
limits), nodes `NotReady`, APIM `401/403` spikes (misconfigured token), Key Vault access
denied (identity federation broke).

### 10.4 Troubleshooting

| Symptom | Likely cause | Check / fix |
|---|---|---|
| `kubectl` opens a browser every time | kubelogin not caching | `kubelogin convert-kubeconfig -l azurecli` to reuse `az login` |
| Workspace pod `Pending`, event *0/5 nodes available: insufficient cpu* | pool at max or quota | `az aks nodepool show … --query maxCount`; vCPU quota |
| Pod `CreateContainerError: hostUsers` / *user namespaces not supported* | node image too old (contract C2) | pool `--os-sku Ubuntu2204`+, AKS ≥ 1.36; `check-prereqs.sh` probe |
| Portal `401 not signed in` | oauth2-proxy not trusted | `portal.trustedProxies` must contain the pod CIDR; `X-Forwarded-Email` present? |
| `wsctl login` works, API says `401 invalid audience` | token for the wrong resource | scope must be `api://devspaces/Workspaces.ReadWrite`; `portal.oidc.audience` matches the API app's identifier URI |
| APIM `401` but portal direct `200` | policy tenant/audience mismatch | named values in APIM; token `aud`/`tid` with `jwt.ms` |
| Group checks fail, `groups` claim missing | > 200 groups → overage claim | use *group filtering* (assign groups to the app) or `groups` via app roles |
| `oauth2-proxy` Secret never appears | workload identity federation subject mismatch | `az identity federated-credential list`; subject `system:serviceaccount:devspaces-system:devspaces-secrets`; pod has label `azure.workload.identity/use: "true"` |
| PVC `Pending` with *WaitForFirstConsumer* | expected until the pod schedules | nothing; if the pod is Pending too, see row 2 |
| Shared drive mount: `permission denied` | missing uid/gid mount options | `workspace-shared-files` `mountOptions` (uid=1000,gid=1000) |
| Multicast test fails | managed Cilium | Phase 9.1 — BYO Cilium only |
| Node upgrade killed a long build | normal drain | shorter builds, or `kubectl annotate pod … cluster-autoscaler.kubernetes.io/safe-to-evict=false` for lab pods; the drive is intact |

---

## Final checklist

- [ ] `00-prereqs.sh` passes; `env.sh` filled; quota approved
- [ ] AKS: Cilium dataplane, OIDC issuer, Azure RBAC; pools `system` (tainted), `workspaces` (Ubuntu 22.04+, autoscaled), `lab` (tainted, min 0)
- [ ] `check-contract.sh`: C1–C9 pass (C10–C13 as chosen)
- [ ] ACR attached; images pushed with `TAG`
- [ ] StorageClasses `workspace-home-premium` (default), `workspace-home-standard`, `workspace-shared-files`; VolumeSnapshotClass; built-in default class demoted
- [ ] Entra apps `devspaces-portal`, `devspaces-api` (`api://devspaces`, v2 tokens, groups), `devspaces-cli` (public, pre-authorised); admin consent granted
- [ ] Key Vault secrets + workload identity + `SecretProviderClass devspaces-keyvault`
- [ ] Gateway/Ingress with a valid certificate for `PORTAL_HOST`; DNS record
- [ ] Helm install with `values-azure.yaml` + `entra.values.yaml`; portal and oauth2-proxy `Running`
- [ ] Developers/groups onboarded; projects created
- [ ] Smoke test: create → ssh → pod delete keeps files → delete keeps drive → `--from-drive` re-attaches → purge releases PV
- [ ] (Optional) APIM deployed; `401` without token, `200` with; developer portal shows the API
- [ ] (Optional) multicast cluster / mesh enabled and tested
- [ ] Maintenance windows, budgets, alerts, backup policy configured

## Reference — files for Azure

| Path | Purpose |
|---|---|
| `deploy/cluster/providers/azure/env.example` | every name/size/region in one file |
| `…/00-prereqs.sh` → `…/60-multicast-cilium.sh` | the cluster layer, numbered, idempotent, `--dry-run` |
| `…/20-storage.yaml` | home/shared StorageClasses + VolumeSnapshotClass |
| `…/50-apim.bicep`, `…/apim-policy.xml` | API Management + Entra token policy |
| `…/entra.values.yaml` (generated) | ids and issuer for Helm |
| `charts/devspaces/examples/values-azure.yaml` | the only Azure-specific input to the Kubernetes layer |
| `api/openapi.yaml` | the API imported into APIM; what `wsctl` speaks |
| `deploy/platform/mesh/` | optional Istio ambient examples |
| `docs/workspace-networking.md` | pods talking to pods: namespaces, projects, links, mesh |
| `docs/production-deployment.md` | the cloud-agnostic phases this guide specialises |
