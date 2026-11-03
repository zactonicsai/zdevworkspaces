# devspaces — design and architecture

This is the design document for the whole system: what the pieces are, why they are
shaped the way they are, and how the November 2026 additions — home drives that outlive
pods, sharing, project namespaces and links, the Azure/Entra API path and the `wsctl` Go
client — fit in. The README is the quick tour; this is the reasoning.

Analogy used throughout: a **school**. The cluster is the building, each developer has a
**locker** (home drive) and gets a **desk** (pod) set up fresh whenever they come in; a
**classroom** (namespace) belongs to one student, a **project room** is shared by a team;
the **front office** (portal/API) checks IDs and hands out keys; the **caretaker** (reaper)
turns off lights in empty rooms.

---

## 1. Goals and non-goals

| Goal | How it shows up |
|---|---|
| A developer gets a full Linux machine with their languages/tools in about a minute | one command (`wsctl create` / `workspace create`) or one web form |
| Their files survive anything that happens to the machine | **home drives** are separate objects; nothing but an explicit purge deletes one |
| Works the same on any Kubernetes | pure-K8s layer + a written **contract** the cluster layer must satisfy |
| Developers never need cluster credentials | the portal API impersonates them; `wsctl` speaks only HTTP(S) |
| Teams can wire workspaces together | same-namespace DNS, project namespaces, links, optional mesh |
| Security by construction | per-developer namespaces, RBAC, Pod Security, NetworkPolicy, admission policies, user namespaces |

Non-goals: a web IDE (VS Code Remote-SSH is the editor), multi-cluster federation,
replacing the organisation's identity provider or Artifactory.

## 2. The two layers

```
┌──────────────────────────────────────────────────────────────────────────┐
│ KUBERNETES LAYER (identical everywhere)  charts/devspaces, deploy/platform│
│  portal API · catalog · RBAC · admission policies · reaper · tenants      │
├──────────────────────────────────────────────────────────────────────────┤
│ CONTRACT (deploy/cluster/CONTRACT.md, verified by check-contract.sh)     │
│  C1 K8s ≥1.36  C2 user namespaces  C3 lab pool  C4 default StorageClass    │
│  C7 OIDC  C8 NetworkPolicy CNI  C14 Retain home classes  C15 snapshots   │
│  C16 RWX class for shared drives  (C5/C6 ingress+TLS, C9–C13 optional)   │
├──────────────────────────────────────────────────────────────────────────┤
│ CLUSTER LAYER (one recipe per provider)  deploy/cluster/providers/*      │
│  AKS (azure/00-60 scripts) · EKS · GKE · kubeadm/VMware                  │
└──────────────────────────────────────────────────────────────────────────┘
```

The Kubernetes layer never contains a provider name. Provider specifics (disk types,
identity, ingress, API gateway) are expressed as *values* (`charts/devspaces/examples/`)
and cluster-layer scripts.

## 3. Components

| Component | Language | Role | Talks to |
|---|---|---|---|
| `cli/workspace` | Python 3.9 stdlib | kubectl-native client (developers with kubeconfig, admins, GitOps/CI, the reaper) | Kubernetes API via `kubectl` |
| `portal/` | Go stdlib | HTTP API + web UI; impersonates the caller; sharing with its own SA; WebSocket SSH tunnel | Kubernetes API, Artifactory |
| `clients/wsctl` | Go stdlib | portal-API client; device-code login; `proxy` for SSH/VS Code | portal only |
| `images/` | Docker | `workspace-base` + one image per language; features install tools at boot | Artifactory |
| `charts/devspaces` | Helm | the Kubernetes layer | — |
| `deploy/cluster/providers/azure` | bash/Bicep | AKS, storage classes, Entra apps, Key Vault, APIM | Azure |

Both clients render **byte-identical workspace manifests** from the same catalog (the
portal test suite runs the Python CLI and diffs the StatefulSet), so what the web form and
the terminal produce is one thing with two front ends.

## 4. Identity and the API

```
browser ──► oauth2-proxy (Entra ID / any OIDC) ──X-Forwarded-Email──► portal
wsctl   ──Bearer JWT (Entra, aud api://devspaces)─────────────────────► portal
wsctl   ──Bearer JWT──► Azure API Management (validate-azure-ad-token) ► portal
                                                                         │ Impersonate-User
                                                                         ▼
                                                                   Kubernetes API (RBAC = the developer's)
```

* **Session**: the proxy authenticates; headers are trusted only from `-trusted-proxies`;
  state-changing calls need `X-Requested-With` (CSRF).
* **Bearer**: `portal/oidc.go` validates RS256 JWTs against the issuer's JWKS (discovery,
  cached, rotation-aware): issuer, audience, `exp`/`nbf`, then maps `preferred_username`
  (configurable) to the developer and `groups` to groups. No CSRF header needed (nothing
  ambient about a bearer token). This is the path for `wsctl` and for automation.
* **Azure API Management** (optional) fronts `/api/*` as the "Workspaces API": imports
  `api/openapi.yaml`, enforces `validate-azure-ad-token`, rate limits, forwards the token.
  WebSockets (the SSH tunnel) go to the portal host directly (`wsctl login --tunnel-host`).
* `/.well-known/devspaces` publishes the login hints (tenant, client id, scope) so
  `wsctl login --portal URL` needs no other flags.
* **Impersonation** means the portal cannot do more than the developer could with kubectl.
  The one exception is **sharing** (§7), done with the portal's own ServiceAccount under a
  ClusterRole limited to profile keys, `share-*`/`workspace-guest-*` RBAC, `peer-*`
  policies and `shared-*` volumes — and an admission policy that enforces those prefixes.

The API contract is `api/openapi.yaml` (served at `/openapi.yaml`).

## 5. A workspace

```
Namespace ws-alice (tenant)                       ┌─ PVC ws-api-home  (home drive, RWO)
 ├ StatefulSet ws-api  replicas 0|1 ──► Pod ws-api-0 ┤  /home/dev
 │   retention: Retain/Retain                      ├─ Secret ws-api-ssh (host key, authorized_keys)
 ├ Service ws-api (headless → DNS name)            ├─ Secret ws-api-git (deploy key | token)
 ├ NetworkPolicy ws-api-allow-ssh (only if exposed)└─ PVC shared-team-data (RWX, optional, /shared/team-data)
 └ annotations: devspaces.io/spec (the request), idle-timeout, last-active (heartbeat)
```

* **StatefulSet with one replica** because it gives a stable pod name, a stable PVC binding
  and scale-to-zero for free. `stop` = `replicas: 0`; the reaper CronJob does the same when
  `last-active` is older than the idle timeout; `ssh`/`wsctl proxy` scale back to 1.
* **sshd on loopback** inside the pod; clients reach it through `kubectl exec socat` (CLI)
  or the portal's WebSocket exec bridge (wsctl). Nothing listens on the network unless the
  developer asks for `--expose`.
* **Images**: one base, one feature installer per language/tool; a tool is installed at
  boot (cached `.deb`/Artifactory download) or baked into a variant. The catalog decides
  what exists; hot-added installers come from a ConfigMap.

## 6. Home drives: files outlive pods

The single most important promise: *deleting a workspace never deletes your work.*

| Mechanism | What it guarantees |
|---|---|
| PVC is a separate object with labels `devspaces.io/drive=home`, annotations `last-workspace`, `languages`, `storage` | the Drives page / `drives list` can show detached drives with history |
| `persistentVolumeClaimRetentionPolicy: Retain/Retain` on the StatefulSet | scaling or deleting the StatefulSet leaves the PVC |
| `delete` removes everything *but* the PVC; `--purge` first annotates `devspaces.io/allow-delete=true` | deletion is a two-step, explicit act |
| ValidatingAdmissionPolicy `devspaces-protect-home-drives` rejects DELETE of a home PVC without that annotation | a mistyped kubectl, a GitOps prune, a cleanup script cannot erase a drive |
| StorageClasses for homes use `reclaimPolicy: Retain` + `allowVolumeExpansion` | even a purged drive's cloud disk survives until an admin removes it; drives grow online |
| `fromDrive` on create attaches any detached drive to a new workspace (any name, any languages); the API refuses to steal an attached one | "put a new workspace on my old disk" |
| `VolumeSnapshot` through the CSI driver, labelled with the drive; `fromSnapshot` creates a *new* drive from it | copies before risky changes, hand-offs, restores that never overwrite |
| **Storage choices** in the catalog (`storage.choices[]`: name, class, kind home\|shared) | developers choose "fast / standard / shared" without knowing class names; admins map them per cloud (Azure: Premium/Standard SSD managed disks, Azure Files) |

Shared drives (`kind: shared`, RWX) are the second kind of drive: a folder several workspaces
mount at `/shared/<name>`. They need a file service (Azure Files, EFS, Filestore, NFS); the
contract item C16 says so, and the UI hides the feature when no such class is configured.

## 7. Sharing

Two different things, deliberately kept apart:

* **Share a workspace** (people): the guest's public key goes into the owner's
  `authorized_keys` (comment `guest:<user>`), and a Role/RoleBinding in the *owner's*
  namespace lets the guest exec/port-forward into that one pod. The guest connects as
  `owner/name`; the workspace appears under *Shared with me*. Revoking removes both.
* **Share a drive** (data): only RWX drives. The portal copies the owner's PV `csi` block
  (driver, `volumeHandle`, attributes, secrets) into a new PV with a `claimRef` to a PVC
  `shared-<owner>-<name>` in the guest's namespace — the same file share, mounted from two
  namespaces, optionally read-only. Home drives (single-writer disks) are never shared;
  share the workspace or a snapshot instead.

Both need rights no developer has (reading someone else's profile, writing into their
namespace), which is why they are the only operations done with the portal's identity,
after the portal verified ownership through the caller's impersonated client.

## 8. Networking between workspaces ("two or more dev pods talk")

Three tiers, cheapest first; each is plain DNS + NetworkPolicy (Cilium on AKS, Calico, …):

1. **Same namespace: already connected.** Every workspace has a headless Service, so
   `ws-<name>.<ns>.svc.cluster.local` resolves to the pod; the tenant policy allows all
   traffic between pods of one namespace. `$WORKSPACE_DNS` is set inside the pod.
2. **Project namespace** (`ws-proj-<name>`, labels `devspaces.io/kind=project`,
   `devspaces.io/tenant=proj-<name>`): a team's "virtual namespace". Members (users or
   groups) get the `workspace-developer` ClusterRole bound there; quota and policies are
   the tenant ones; everything inside is mutually reachable and visible to all members.
   Created by `workspace admin project create` or Helm `projects:`; chosen in the Basics
   step / `--project`.
3. **Links** across personal namespaces: a `peer-<ws>-<peerns>-<peerws>` NetworkPolicy in
   the caller's namespace allows ingress from and egress to exactly one peer pod
   (optionally on chosen ports). Traffic flows only when *both* sides link (consent), which
   the portal reports as `linked` / `pending-peer`. Developers may manage NetworkPolicies in
   their own namespace, so no admin is involved.

**Service mesh is optional.** `networking.mesh: istio-ambient` makes onboarding label
tenant namespaces `istio.io/dataplane-mode=ambient`; `deploy/platform/mesh/` holds examples
(AuthorizationPolicy, waypoint, HTTPRoute that routes `x-dev: alice` to Alice's workspace).
Recommendation: tiers 1–3 first; add ambient mesh when you need mTLS identity, L7 rules or
header-based routing between developers' versions of a service. Multicast is unchanged
(Multus macvlan or Cilium eBPF) and does not go through the mesh.

## 9. The SSH tunnel without kubectl

```
ssh/VS Code ─stdio─► wsctl proxy ─WebSocket (binary frames)─► portal ─WebSocket v4.channel.k8s.io─► API server ─► pod: socat - TCP:127.0.0.1:2222
```

`portal/ws.go` is a ~250-line RFC 6455 implementation (server for `/tunnel`, client for
the Kubernetes exec API); `tunnel.go` starts a stopped workspace, waits for Ready (sending
progress as text frames), then bridges stdin/stdout of `socat` to the client. The exec is
made *as the caller*, so `pods/exec` RBAC still applies — which is also what the guest Role
grants for shared workspaces. In `-mock` mode the tunnel goes to `-mock-tunnel host:port`
(the Compose workspace) or a built-in echo server.

## 10. Azure specifics (the reference cloud for this round)

| Need | Azure answer | Where |
|---|---|---|
| Cluster | AKS, Azure CNI powered by Cilium, node pools system / workspaces (autoscaled, user-namespace capable) / lab (tainted, optional GPU), OIDC issuer + workload identity | `deploy/cluster/providers/azure/10-aks.sh` |
| Home drives | `workspace-home-premium` (Premium_LRS, Retain, expandable), `workspace-home-standard`; `csi-azuredisk-vsc` snapshot class | `20-storage.yaml` |
| Shared drives | `workspace-shared-files` (Azure Files Premium, RWX, uid/gid 1000 mount options) | `20-storage.yaml` |
| Browser sign-in | oauth2-proxy `entra-id` provider, app registration `devspaces-portal`, group claims | `30-entra.sh`, values |
| CLI / API tokens | app `devspaces-api` exposing `api://devspaces/Workspaces.ReadWrite`; public client `devspaces-cli` with device-code flow; portal `-oidc-*` flags | `30-entra.sh`, `portal.oidc`, `portal.cliLogin` |
| Secrets | Key Vault + Secrets Store CSI driver syncing the oauth2-proxy Secret | `40-keyvault.sh`, `oauth2Proxy.secretProviderClass` |
| Azure API | API Management importing `api/openapi.yaml`, `validate-azure-ad-token` policy, rate limit | `50-apim.bicep`, `apim-policy.xml` |
| Ingress | Application Gateway for Containers (Gateway API HTTPRoute) or NGINX + cert-manager | `ingress.gatewayAPI` |
| Multicast | Cilium eBPF multicast (BYO Cilium) | `60-multicast-cilium.sh` |

Step by step: `docs/azure-aks-setup.md`.

## 11. Security model (summary)

* **Tenancy**: one namespace per developer (+ `-lab`), project namespaces for teams; quotas,
  LimitRange, Pod Security `baseline` (standard) / `privileged` (lab, with admission rules
  that replace what PSA no longer checks).
* **Isolation**: user namespaces (`hostUsers: false`) by default — root inside is an
  unprivileged uid on the node; gVisor/Kata as alternatives; lab workspaces one per node.
* **Network**: default-deny between tenants; DNS and egress allowed; cloud metadata blocked;
  links and `--expose` are explicit opt-ins; sshd key-only.
* **Admission**: workspace guardrails (host namespaces, hostPath allow-list, lab node
  selector), protected tenant policies, protected home drives, portal sharing scope.
* **Identity**: proxy headers trusted only from the proxy; bearer JWTs validated locally;
  impersonation everywhere except the audited sharing path; portal runs non-root, read-only
  root filesystem, strict CSP.
* **Secrets**: per-workspace SSH host key and deploy key in Secrets in the tenant
  namespace; tokens never leave it; Key Vault on Azure for platform secrets.

## 12. Data model cheat sheet

| Object | Name | Key labels / annotations |
|---|---|---|
| Namespace | `ws-<user>`, `ws-<user>-lab`, `ws-proj-<name>` | `devspaces.io/tenant`, `devspaces.io/tier`, `devspaces.io/kind=project`, `devspaces.io/members` |
| StatefulSet/Service/Pod | `ws-<name>`, `ws-<name>-0` | `devspaces.io/workspace`, `lang`, `profile`; ann `devspaces.io/spec`, `idle-timeout`, `last-active` |
| Home drive PVC | `ws-<name>-home` (or any adopted name) | `devspaces.io/drive=home`; ann `last-workspace`, `languages`, `storage`, `allow-delete` |
| Shared drive PVC | `shared-<name>` / guest `shared-<owner>-<name>` | `devspaces.io/drive=shared` / `shared-mount`, `devspaces.io/shared-by` |
| Snapshot | any | `devspaces.io/drive=<pvc>` |
| Share | Role `workspace-guest-<ws>`, RoleBinding `share-<ws>-<guest>` | `devspaces.io/share=workspace`, `devspaces.io/guest`; ann `guest-user` |
| Link | NetworkPolicy `peer-<ws>-<peerns>-<peerws>` | `devspaces.io/link=peer`; ann `devspaces.io/peer=owner/ws` |

## 13. Where to read next

* `README.md` — overview and quick references · `api/openapi.yaml` — the API
* `docs/azure-aks-setup.md` — AKS, step by step · `docs/production-deployment.md` — any cloud
* `docs/workspace-networking.md` — the three tiers and the mesh option
* `clients/wsctl/README.md` — the Go CLI · `portal/README.md` — the API/UI server
* `docs/demo-docker.md` — try everything on a laptop (`portal/web/simulation.html` needs nothing at all)
