# Production deployment guide — any cloud, two layers, six phases

This guide takes the devspaces system from "works on my laptop" (`docs/demo-docker.md`)
to a production service that developers across a company use every day. It works on
**AWS, Google Cloud, Azure, VMware / on-prem, or any other Kubernetes**, because it is
built as two loosely coupled layers with a written contract between them.

If you read nothing else: **the cluster layer builds the building; the Kubernetes layer
opens the store inside it.** Builders and store managers can work on different days, with
different tools, as long as both respect the same floor plan (`deploy/cluster/CONTRACT.md`).

```
                 ┌──────────────────────────────────────────────────────────────────┐
  Phase 3–6      │  KUBERNETES LAYER  (identical on every provider)                  │
  "the store"    │  helm chart charts/devspaces: catalog · RBAC · reaper · guardrails │
                 │  portal + sign-in proxy · multicast · lab tier · onboarding        │
                 ├────────────────────────── CONTRACT (C1…C13) ─────────────────────┤
  Phase 1–2      │  CLUSTER LAYER  (one recipe per provider)                         │
  "the building" │  EKS / GKE / AKS / kubeadm on VMware: node pools, storage class,  │
                 │  identity (OIDC), ingress + TLS, CNI with policy, add-ons         │
                 └──────────────────────────────────────────────────────────────────┘
```

---

## Part A — The idea in plain words

### A grocery store, built by two teams

Imagine a company that opens grocery stores in many towns. In every town the **building**
is different: brick in one place, steel in another, a rented hall in a third. But the
**store inside** is always the same — the same aisles, the same checkout, the same rules
about who may enter the stockroom.

That works because the store team hands the builders a short list: *"We need a floor this
big, a loading dock at the back, electricity, a front door that locks, cold storage, and a
sign outside."* Builders can satisfy that list however they like. The store team never asks
*how* the dock was built; they only check it is there.

Our system is the same:

| Grocery store | devspaces | Which layer |
|---|---|---|
| The building, land, power, plumbing | the Kubernetes cluster: nodes, network, storage, load balancers | **Cluster layer** (Phase 1–2) |
| The builder's checklist from the store team | `deploy/cluster/CONTRACT.md` (promises C1–C13) | the seam between layers |
| Aisles for everyday shopping | the **standard node pool** where normal workspaces run | cluster |
| The loading dock where forklifts drive — dangerous, so only trained staff, one truck at a time | the **lab node pool**: privileged hardware workspaces, one per machine | cluster |
| Cold storage that keeps food fresh overnight | **block storage** for home volumes — files survive when the workspace is off | cluster |
| The front door with a security guard checking badges | **Ingress + TLS + oauth2-proxy** sign-in | cluster provides the door; Kubernetes layer provides the guard |
| The warehouse that supplies every store, so no store shops at random markets | **Artifactory**: images, tools, packages | shared, outside both layers |
| Shelves, price tags, the stock list | the **catalog**: languages, tools, sizes | Kubernetes layer |
| Each employee's locker | each developer's **namespace** (`ws-alice`) | Kubernetes layer |
| The store rules poster ("no forklifts in the aisles") | **Pod Security + admission guardrails** | Kubernetes layer |
| Lights that switch off when nobody is in the room | the **idle reaper** | Kubernetes layer |

### A school, run in phases

Opening a school also happens in an order you cannot shuffle: build the school, connect
water and electricity, hire the staff and set the rules, enroll the students, hold a fire
drill, then run the school year. Each phase has a check at the end, and you don't move on
until the check passes. This guide follows that order:

| Phase | School | What you do |
|---|---|---|
| 0 | Decide where to build and how big | sizing and decisions, one config file |
| 1 | Build the school | create the cluster (provider recipe) |
| 2 | Water, power, doors, alarm | cluster add-ons: ingress, certificates, metrics, multicast, device plugins |
| **gate** | Inspector signs off | `check-contract.sh` |
| 3 | Furnish classrooms, hang the rules | images → registry; `helm install devspaces` |
| 4 | Enroll students | onboard developers |
| 5 | Fire drill | smoke test, go-live checklist |
| 6 | Run the school year | operations: upgrades, backups, scaling, incidents |

### Why two layers, and why "loosely coupled"

* **Different owners.** Cloud/platform engineers own the cluster; the developer-experience
  team owns the store. Neither needs the other's permissions or tools.
* **Portable.** Moving from AWS to on-prem means redoing Phase 1–2 only. Phase 3–6 is the
  same `helm` command with a different values file.
* **Testable.** The contract is a script. "Does this cluster work for devspaces?" has a
  yes/no answer before anyone installs anything.
* **Replaceable parts.** Any piece of the cluster layer can be swapped (ingress-nginx for a
  cloud load balancer, Calico for Cilium) as long as the promise it keeps stays kept.

The rule that keeps the coupling loose: **nothing in `charts/devspaces` may mention a cloud.**
Provider-specific details live only in `charts/devspaces/examples/values-*.yaml` and in
`deploy/cluster/providers/`.

---

## Part B — Phase 0: plan (one afternoon)

### B.1 Decide the numbers

Think of it as "how many students, how many classrooms":

| Question | Rule of thumb | Where it goes |
|---|---|---|
| How many developers? | each standard workspace reserves 1 CPU / 2 GB and may burst to 4 / 8 GB; plan **1 node of 8 CPU / 32 GB per 5–6 active developers** | `STANDARD_MIN/MAX` |
| How many do hardware work? | one **lab node per concurrently active lab developer** (one privileged workspace per node, enforced) | `LAB_MIN/MAX` |
| Disk per developer? | 30–50 GB home volume; block storage with expansion | catalog `defaults.size`, StorageClass |
| Idle timeout? | 4 h default; the reaper runs every 10 min | catalog `defaults.idleTimeout` |
| Where do images and packages come from? | Artifactory (or the cloud registry for images only) | `REGISTRY`, `ARTIFACTORY_URL` |
| Who signs in, and how? | one OIDC provider for **both** `kubectl` and the portal, usernames = e-mail | `OIDC_*` |
| Multicast needed? | on-prem with a real VLAN → **multus**; any cloud → **cilium** (eBPF) | `multicast.mode` |

### B.2 Make the config file

Everything both layers need sits in one file, so a value is never typed twice:

```bash
cp deploy/cluster/env.example deploy/cluster/env.sh     # git-ignored
$EDITOR deploy/cluster/env.sh                           # PROVIDER, sizes, OIDC, registry, portal host
cp charts/devspaces/examples/values-<provider>.yaml my-values.yaml   # Kubernetes-layer knobs
```

Secrets (OIDC client secret, cookie secret, registry login) go in a second, git-ignored
values file (`SECRETS_VALUES_FILE`), or in Kubernetes Secrets referenced by
`oauth2Proxy.existingSecret` / `portal.existingSecret`.

### B.3 Decisions to write down (the "floor plan" meeting)

1. **Registry:** Artifactory Docker repo (recommended: one place for everything, works
   air-gapped) or the cloud registry (ECR/Artifact Registry/ACR) for images only.
2. **Identity provider** and the exact e-mail claim. The API server and oauth2-proxy must
   agree, or a developer's portal identity will not match their namespace.
3. **Multicast mode** (see Part F).
4. **Node image:** must have kernel ≥ 6.3 and containerd 2.x (contract C2). On cloud
   providers that is the default for Kubernetes 1.36 node images; on-prem use Ubuntu 24.04.
5. **Who are platform admins** (group `devspaces:platform-admins`): they run onboarding and
   may edit tenant network policies.

---

## Part C — Phase 1: the cluster layer (build the building)

Pick **one** recipe. Each produces a cluster that keeps the contract; each is a plain shell
script with the variables at the top, meant to be read and adapted, not treated as a black box.

### C.1 AWS — EKS (`deploy/cluster/providers/aws-eks.sh`)

```bash
source deploy/cluster/env.sh
./deploy/cluster/providers/aws-eks.sh
```
What it does, in order: writes an `eksctl` cluster file → creates the VPC and control plane
(Kubernetes 1.36) with **OIDC identity** (C7) → three managed node groups on Amazon Linux
2023 (kernel 6.x, containerd 2 — C2): `system` (tainted for platform pods), `standard`,
`lab` (labelled + tainted — C3) → VPC CNI with **network policy enabled** (C8) → EBS CSI
driver and a **gp3 default StorageClass with expansion** (C4) → metrics-server (C13) → the
AWS Load Balancer Controller for the Ingress (C5).

AWS specifics: the Ingress class is `alb` with TLS terminated on the ALB (ACM
certificate), so `ingress.tls.enabled: false` in the values. Multicast: VPCs drop it; use
Cilium mode (Part F). Driver work needing real PCI devices: use `*.metal` instances for the
lab group.

### C.2 Google Cloud — GKE (`deploy/cluster/providers/gcp-gke.sh`)

```bash
./deploy/cluster/providers/gcp-gke.sh
```
Creates a GKE Standard regional cluster with **Dataplane V2** (Cilium-based, enforces
NetworkPolicy — C8), Workload Identity, the PD CSI driver (`standard-rwo` made default,
expansion on — C4), and three node pools: `system` (COS), `standard` (COS),
`lab` (Ubuntu, so kernel headers are available for module builds — C12). Identity: GKE
authenticates `kubectl` with Google accounts, i.e. e-mails, which match the portal (C7).
Ingress class `gce` with a Google-managed certificate.

Multicast on GKE: there is no L2, so **multus mode is not possible**. For cilium mode the
Cilium multicast beta needs OSS Cilium rather than the managed Dataplane V2 — create the
cluster with the default CNI disabled and install Cilium yourself (Part F).

### C.3 Azure — AKS (`deploy/cluster/providers/azure-aks.sh`)

```bash
./deploy/cluster/providers/azure-aks.sh
```
Creates AKS with **Azure CNI powered by Cilium** (overlay, NetworkPolicy enforced — C8),
Entra ID authentication (UPN e-mails — C7), the app-routing add-on (managed nginx Ingress —
C5), and `system` / `standard` / `lab` pools (`managed-csi-premium` as the default
StorageClass — C4). Same multicast note as GKE: managed Cilium does not expose the
multicast beta; BYO Cilium for cilium mode.

### C.4 VMware / bare metal / any on-prem (`deploy/cluster/providers/vmware-onprem.md`)

No single CLI exists here, so the recipe is a table that maps each contract line to a
concrete choice (vSphere CSI for storage, MetalLB + ingress-nginx for the door, Keycloak for
identity, Cilium or Calico for policy, a VLAN for multicast). `kubeadm-node-prep.sh` turns
an Ubuntu 24.04 VM into a node with containerd 2.x, the right sysctls and — for lab nodes —
kernel headers:

```bash
sudo ./deploy/cluster/providers/kubeadm-node-prep.sh            # standard/system node
sudo ./deploy/cluster/providers/kubeadm-node-prep.sh --lab      # lab node
```
Then `kubeadm init` with the OIDC flags from the page, join the workers, label and taint
the lab nodes, and install Cilium in **VXLAN** mode (multicast-ready).

### C.5 Any other Kubernetes (OpenShift, Rancher, Talos, a cluster you already have)

Skip the recipes and go straight to the inspector: `deploy/cluster/check-contract.sh` tells
you exactly which promise is missing and how to add it. The Kubernetes layer does not care
how the cluster came to exist.

**End-of-phase check:** `kubectl get nodes -o wide --show-labels` shows all pools `Ready`,
the lab nodes carry `devspaces.io/lab=true` and the taint, and
`kubectl get storageclass` shows a default block class.

---

## Part D — Phase 2: add-ons (water, power, doors, alarm)

Add-ons are things the cluster must *provide* but that are not tied to a provider. One
script, one toggle per add-on in `env.sh`, all idempotent:

```bash
./deploy/cluster/addons.sh
```

| Toggle | Installs | Contract |
|---|---|---|
| `INSTALL_INGRESS_NGINX` | ingress-nginx on the system pool (skip on AWS/GCP when using the cloud ingress) | C5 |
| `INSTALL_CERT_MANAGER` | cert-manager + a `letsencrypt` ClusterIssuer, or instructions for a corporate CA | C6 |
| `INSTALL_METRICS_SERVER` | metrics-server | C13 |
| `INSTALL_MULTUS` | Multus (thick) + whereabouts IPAM — multicast mode **multus** | C10 |
| `ENABLE_CILIUM_MULTICAST` | runs `multicast-cilium.sh` — multicast mode **cilium** | C10 |
| `INSTALL_SMARTER_DEVICE_MANAGER` | the device plugin DaemonSet on the lab pool | C12 |
| `INSTALL_KATA` | kata-deploy on the lab pool (nested virtualization required) | C11 |
| `INSTALL_GVISOR` | prints the per-provider way to get `runsc` on labelled nodes | C11 |

After the ingress controller is up, the script prints its public address: **create the DNS
record** `workspaces.corp.example → that address` now, so the certificate can be issued in
Phase 3.

### The gate: inspect the building

```bash
./deploy/cluster/check-contract.sh
```
It prints one line per promise. It also starts one tiny probe pod with `hostUsers: false`
and reads its `uid_map` — the only way to be sure user namespaces truly work on *these*
nodes — and checks that pods can reach Artifactory. **Do not continue with a FAIL.** This
is the inspector's signature on the building; the store team relies on it.

---

## Part E — Phase 3: the Kubernetes layer (open the store)

From here on, every command is the same on every provider.

### E.1 Put the goods in the warehouse: images

```bash
docker login artifactory.corp.example            # or: aws ecr get-login-password … | docker login …
./deploy/prod/10-images.sh                       # builds base + 6 languages + cli + portal, pushes <REGISTRY>/<image>:<TAG>
```
A release is a new `TAG` (`2026.10`, `2026.11`, …). Never move a tag: workspaces pull
`IfNotPresent`, and a moved tag would mean two developers run different images under the
same name. Bump `global.tag` in the values to roll a release out (Phase 6).

Teams that always use the same mix can get a pre-baked image:
`LANGS=go EXTRA_FEATURES=python ./images/build.sh` → `workspace-go-python`; the portal
detects it in Artifactory automatically.

### E.2 Install the chart

```bash
./deploy/prod/20-platform.sh --dry-run      # render + schema-check, touch nothing
./deploy/prod/20-platform.sh                # runs the contract check, then helm upgrade --install
```
Under the hood:
```bash
helm upgrade --install devspaces charts/devspaces -n devspaces-system --create-namespace \
  -f charts/devspaces/examples/values-aws.yaml -f my-secrets.yaml \
  --set global.registry=… --set global.tag=… --set ingress.host=workspaces.corp.example
```
What the chart creates (the whole store): the **catalog** ConfigMap (chart's `catalog.json`
deep-merged with your `catalogOverrides`, registry/tag/multicast injected so chart and
catalog can never disagree), **RBAC** ClusterRoles (`workspace-developer`, read-only
catalog access for everyone signed in), the **reaper** CronJob, the **admission
guardrails** (ValidatingAdmissionPolicies), **RuntimeClasses** for gVisor/Kata, the
multicast **NetworkAttachmentDefinition** (multus mode), the device-plugin ConfigMap,
the **portal** (impersonating ServiceAccount, Deployment, Service, NetworkPolicy),
**oauth2-proxy**, and the **Ingress**.

The chart refuses to install anywhere but namespace `devspaces-system` (the CLI and the
portal look for the catalog there) unless you say `allowAnyNamespace=true` on purpose.

### E.3 Values worth understanding

| Value | Meaning | Grocery-store version |
|---|---|---|
| `global.registry`, `global.tag` | where images come from | the warehouse address and the delivery date |
| `catalogOverrides` | any catalog field: sizes, tools, Artifactory repos, CIDRs | editing the shelf plan without rebuilding shelves |
| `multicast.mode` | `multus`, `cilium` or `none` | which kind of PA system the store has |
| `admission.platformAdminGroups` | who may touch tenant network policies | who has keys to the security office |
| `portal.trustedProxies` | which pod CIDR may assert identity headers | only the guard at the door may hand out visitor badges |
| `oauth2Proxy.*` | your IdP | the badge printer |
| `ingress.*` | hostname, class, TLS | the sign outside and the lock on the door |

**End-of-phase check:** `helm status devspaces -n devspaces-system` is `deployed`,
`https://workspaces.corp.example` shows the sign-in page, and signing in shows the portal
Home page with your e-mail.

---

## Part F — Multicast in production: two ways to build the PA system

Multicast is the store's PA system: one announcement, every speaker plays it. Normal
Kubernetes networks have no speakers — they are built for one-to-one phone calls — so you
add them in one of two ways. Developers see no difference: they tick *Multicast network*
(or pass `--multicast`), run `mcast-check`, and it works.

### F.1 Multus mode — real wires (on-prem, bare metal, VMware)

A second network card (`net1`) in each multicast workspace, bridged by `macvlan` onto a
dedicated VLAN that the physical switches already carry multicast on. Cluster layer:
trunk the VLAN to every multicast-pool node (vSphere: a port group with that VLAN and
promiscuous/forged-transmit allowed), `INSTALL_MULTUS=true`. Kubernetes layer:
`multicast.mode: multus`, `multicast.multus.master: ens192.300` (the VLAN sub-interface),
an IP range for `net1`. Best when you have L2 and want wire-speed, switch-handled multicast.

### F.2 Cilium mode — eBPF, no wires (any cloud, and on-prem too)

Clouds' virtual networks drop multicast entirely. Cilium's multicast datapath
(<https://cilium.io/use-cases/multicast/>, docs
<https://docs.cilium.io/en/stable/network/multicast/>) solves this *in software*: eBPF
programs attached to the pod interfaces watch for IGMP joins, keep a map of which pods and
which nodes subscribe to each group, and **replicate** a sender's packets to every
subscriber — local pods directly, remote nodes over Cilium's VXLAN tunnel. The physical
or virtual network only ever sees ordinary unicast VXLAN packets, so VPC limitations do not
matter, and there is no IGMP querier to run on switches.

What you need (cluster layer):

| Requirement | Why |
|---|---|
| Cilium as the CNI, **routing mode `tunnel`, protocol `vxlan`** | replication rides the tunnel |
| Node kernel **≥ 5.10 on amd64, ≥ 6.0 on arm64** | eBPF helpers used by the datapath (already satisfied by contract C2) |
| **No IPsec** between pods (WireGuard is fine) | the beta does not work with IPsec |
| OSS Cilium you manage (not GKE Dataplane V2 / AKS managed Cilium / EKS add-on) | the beta flag is not exposed by the managed variants |
| The `cilium` CLI on the admin machine | to flip the flag and program groups |

Enable it and program the group range the catalog advertises (`multicast.groups`):

```bash
# cluster layer, once (and again whenever nodes are added)
cilium config set multicast-enabled true                 # patches cilium-config, restarts agents
./deploy/cluster/multicast-cilium.sh 239.255.0.0/24      # cilium multicast add --group-ip <ip> for each group
cilium multicast list group                              # groups known to the datapath
cilium multicast list subscriber --all                   # which nodes subscribe to which group
```
`multicast-cilium.sh` checks the tunnel mode and IPsec first, enables the flag if needed,
expands the CIDR into individual group addresses (the CLI takes one IP at a time; it refuses
ranges larger than /24 so a typo cannot program millions of groups), and prints the state.
Under the hood each agent gets `cilium-dbg bpf multicast group add <group>` plus one
`subscriber add <group> <other-node-CiliumInternalIP>` per peer node — the CLI does that
cluster-wide so you do not have to visit each node.

Kubernetes layer: `multicast.mode: cilium` (the chart writes it into the catalog; the CLI
and portal then stop requesting a second interface and set `WORKSPACE_MCAST_IFACE=eth0`).
Developers' programs bind `eth0`, join `239.255.0.x`, done.

Honest limits of the beta: groups must be programmed **before** pods join them (hence the
pre-declared range), new nodes need the script re-run (automate it in your node bootstrap or
use the enterprise CRDs — Isovalent's `IsovalentMulticastGroup` does exactly this), the
feature is labelled beta by Cilium, and throughput is bounded by VXLAN replication on the
sender's node rather than by switch hardware. For most development and testing of multicast
applications that is more than enough; for wire-speed market-data replay, keep an on-prem
multus pool.

### F.3 Verifying either mode

```bash
# two multicast workspaces, A and B
mcast-check --listen                         # in A
mcast-check                                  # in B → A prints B's packet
cilium multicast list subscriber --all       # cilium mode: both nodes subscribed
sudo tcpdump -ni ${WORKSPACE_MCAST_IFACE} udp port 5007   # see the packets arrive
```

---

## Part G — Phase 4: enroll the students (onboard developers)

Each developer gets their own locker — a namespace with a quota, network policies, a
RoleBinding for *their* e-mail, and (for hardware people) a second, privileged lab namespace.

```bash
cat > users.csv <<EOF
alice@corp.example,lab
bob@corp.example
# comments and blank lines are fine
EOF
./deploy/prod/30-onboard.sh users.csv
./deploy/prod/30-onboard.sh --group eng-platform --lab     # or a shared namespace for an OIDC group
```
Why e-mails: the RoleBinding subject must equal the username the API server derives from
the OIDC token (`--oidc-username-claim=email`), and oauth2-proxy forwards the same e-mail to
the portal, which impersonates it. One identity, three places, no mapping table.

Quotas default to 16 CPU / 64 GB / 500 GB per developer (`CPU/MEM/STORAGE` env to change).
Re-running onboarding is safe; it is a `kubectl apply`.

**End-of-phase check:** a developer signs in, `Home` shows *Set up: yes* and their
namespace, and `workspace create --lang go` works from their laptop with their own
kubeconfig (no admin rights).

---

## Part H — Phase 5: fire drill and go-live checklist

```bash
./deploy/prod/40-smoke-test.sh
```
The smoke test onboards a throwaway tenant, creates a workspace, waits for Ready, runs
commands inside through the same `kubectl exec` path SSH uses (root inside, home writable,
sshd up, uid remapped), stops and starts it (volume re-attached), checks the portal's
health endpoint, the reaper, the admission policy, and that a privileged pod is **rejected**
in a standard namespace. Then it deletes everything. Use it as the CI gate on every deploy.

Go-live checklist (print it, tick it):

- [ ] `check-contract.sh` all PASS on the production context
- [ ] smoke test passes twice in a row
- [ ] DNS + certificate valid (`curl -I https://workspaces.corp.example`)
- [ ] sign in with a **non-admin** account: Home, Create, SSH connect from a laptop
- [ ] `--profile hardware` lands on a lab node and nowhere else; two lab workspaces never share a node
- [ ] multicast verified between two workspaces on **different nodes**
- [ ] reaper stops an idle workspace (set `workspace idle NAME 1m`, wait 10 min)
- [ ] a developer cannot `kubectl get pods -n ws-<someone-else>` (403)
- [ ] VolumeSnapshot of a home PVC works and restores (`kubectl get volumesnapshotclass`)
- [ ] dashboards/alerts: node pool capacity, PVC usage, portal 5xx, reaper job failures
- [ ] runbooks below linked from the help channel

---

## Part I — Phase 6: running the school year (operations)

| Task | How | Notes |
|---|---|---|
| **Release new images** | `TAG=2026.11 ./deploy/prod/10-images.sh`, then `TAG=2026.11 ./deploy/prod/20-platform.sh` | running workspaces keep the old image until their next start; nothing restarts by surprise |
| **Add a tool** | edit `catalog.json` (`artifact` block pointing at Artifactory) or add a script under `features:` in values; `20-platform.sh` | no image rebuild for artifact tools |
| **Add a language** | `images/features/<lang>/install.sh` + catalog entry + `10-images.sh` | |
| **Grow a developer's disk** | `kubectl -n ws-alice patch pvc ws-go-home -p '{"spec":{"resources":{"requests":{"storage":"100Gi"}}}}'` | needs `allowVolumeExpansion` (contract C4) |
| **More capacity** | raise `STANDARD_MAX` / `LAB_MAX` and re-run the provider script (node groups reconcile); cluster autoscaler does the rest | pending workspaces with *Insufficient cpu* = pool at max |
| **Backups** | nightly `VolumeSnapshot` of every `*-home` PVC via your CSI snapshot controller or Velero; Git remains the real backup of code | smoke-test restores quarterly |
| **Rotate secrets** | new OIDC client secret → update the Secret, `kubectl rollout restart deploy/oauth2-proxy`; a developer's deploy key → `workspace create … --rotate-git-key` | |
| **Upgrade Kubernetes** | provider recipe (control plane, then pools); re-run `check-contract.sh`; `40-smoke-test.sh` | keep ≥ 1.36 |
| **Upgrade the chart** | `helm upgrade` with the new chart version; `--dry-run` first | the chart never touches tenant namespaces |
| **Audit who did what** | API audit log shows the developer (impersonation), not the portal | |
| **Cost** | reaper timeout per team (`catalogOverrides.defaults.idleTimeout`), lab pool `LAB_MIN=0`, right-size presets | |

Runbooks (short versions):

* **Workspace Pending** → `workspace status NAME`; *exceeded quota* → stop another workspace or raise the tenant quota; *Insufficient cpu* → pool at max; *ImagePullBackOff* → tag not pushed / pull secret missing in the tenant namespace (`workspace admin onboard` copies it); *hostUsers* errors → node image regressed below contract C2, drain it.
* **Nobody can sign in** → `kubectl -n devspaces-system logs deploy/oauth2-proxy`; usually an expired client secret or a redirect URI mismatch (`https://HOST/oauth2/callback`).
* **Portal says "not allowed"** → the developer's e-mail differs between IdP and API server (claim mismatch); compare `kubectl auth whoami` from their laptop with the portal Home page.
* **Multicast stopped after adding nodes (cilium mode)** → re-run `multicast-cilium.sh`.
* **Reaper stopped a long job** → `workspace idle NAME off` for that workspace; teach `tmux`.

---

## Part J — Reference

### J.1 Files added for production

```
deploy/cluster/CONTRACT.md                  the promises between the layers (C1–C13)
deploy/cluster/env.example                  one config file for both layers
deploy/cluster/check-contract.sh            the inspector (run before Phase 3 and after any node change)
deploy/cluster/providers/aws-eks.sh         Phase 1 recipes
deploy/cluster/providers/gcp-gke.sh
deploy/cluster/providers/azure-aks.sh
deploy/cluster/providers/vmware-onprem.md + kubeadm-node-prep.sh
deploy/cluster/addons.sh                    Phase 2 (ingress, cert-manager, metrics, Multus, device plugins, Kata)
deploy/cluster/multicast-cilium.sh          Phase 2, multicast mode cilium
charts/devspaces/                           Phase 3: the Kubernetes layer as a Helm chart (+ examples/values-*.yaml)
deploy/prod/10-images.sh … 40-smoke-test.sh Phase 3–5, scripted
```

### J.2 Provider comparison

| | AWS EKS | GCP GKE | Azure AKS | VMware / on-prem |
|---|---|---|---|---|
| Identity (C7) | EKS OIDC identity provider | Google accounts | Entra ID | API server OIDC flags → Keycloak/Entra/Okta |
| Storage (C4) | EBS CSI `gp3` | PD CSI `standard-rwo` | `managed-csi-premium` | vSphere CSI / Longhorn / Ceph RBD |
| Ingress (C5) | ALB controller (`alb`) or nginx+NLB | `gce` or nginx | app routing (`webapprouting…`) or nginx | MetalLB + nginx |
| Policy CNI (C8) | VPC CNI network policy, or Cilium | Dataplane V2 | Azure CNI + Cilium | Cilium / Calico |
| Multicast (C10) | cilium (BYO Cilium) | cilium (BYO Cilium) | cilium (BYO Cilium) | multus (VLAN) or cilium |
| Lab hardware (C12) | `*.metal` instances, SR-IOV ENA | GPU/local-SSD nodes | GPU / NVv-series | PCI passthrough to VMs, or bare metal |
| Kernel ≥ 6.3 (C2) | AL2023 / Bottlerocket | COS / Ubuntu | Azure Linux / Ubuntu 24.04 | Ubuntu 24.04 |

### J.3 Security hardening beyond the defaults

* Private API endpoint; `kubectl` through VPN; portal Ingress internal-only where possible.
* `portal.trustedProxies` = the oauth2-proxy pod CIDR only.
* Node OS auto-updates on, pools rebuilt monthly (immutable node images).
* Image signing (cosign) in `10-images.sh` and a verify policy (Kyverno/sigstore policy-controller) on `devspaces.io/*` images.
* Separate clusters (or at least separate lab pools) for teams with different hardware trust levels.
* Audit logging to a SIEM; alert on `impersonate` by anything other than the portal ServiceAccount.

### J.4 Glossary for this guide

* **Layer** — a set of things owned together and changed together. Two here: cluster and Kubernetes.
* **Contract** — the short list of promises one layer makes to the other, checked by a script.
* **Node pool** — a group of identical machines (standard aisles, loading dock, staff room).
* **Add-on** — software the cluster must provide but that is the same on every provider (ingress, certificates, metrics).
* **Helm chart** — a package of Kubernetes objects with knobs (`values.yaml`); `helm upgrade --install` makes the cluster match it.
* **OIDC** — the standard that lets one sign-in (your company login) prove who you are to many systems; here, both `kubectl` and the portal.
* **Impersonation** — the portal asking Kubernetes to treat a request *as the developer*, so their permissions apply and audit logs name them.
* **Multicast** — one sender, many receivers (the PA system); **multus mode** uses a real second network, **cilium mode** replicates in software with eBPF.
* **Reaper** — the timer that switches off idle workspaces.
