# Design brief — November 2026 additions

(Working notes shared by everyone implementing this round; the finished text lands in
`docs/architecture.md`. Keep it in sync if you change a decision.)

## 1. Home drives: "your files outlive your pod"

* A **home drive** is the PVC `ws-<name>-home` behind `/home/dev`. It is a first-class object:
  labels `devspaces.io/drive=home`, `devspaces.io/workspace=<name>`; annotations
  `devspaces.io/last-workspace`, `devspaces.io/languages`, `devspaces.io/storage` (catalog alias).
* Nothing ordinary deletes it: pod deletion → StatefulSet recreates the pod on the same PVC;
  `stop` → scale to 0; `delete` → StatefulSet/Service/Secrets go, PVC stays (**detached** drive);
  StatefulSet carries `persistentVolumeClaimRetentionPolicy: {whenDeleted: Retain, whenScaled: Retain}`.
* **Only an explicit purge** deletes it: the client first annotates `devspaces.io/allow-delete=true`,
  then deletes. A ValidatingAdmissionPolicy (`workspace-protect-home-drives`) rejects DELETE of a
  `devspaces.io/drive=home` PVC without that annotation. StorageClasses used for homes have
  `reclaimPolicy: Retain`, so even a purged drive's cloud disk survives until an admin removes it.
* **Choice of storage** via the catalog `storage.choices[]` (`{name, storageClass, kind: home|shared,
  accessModes, description, default}`); request field `storage: "fast"`. Azure defaults:
  `fast` = `workspace-home-premium` (managed-csi, Premium_LRS, Retain, expandable),
  `standard` = `workspace-home-standard` (StandardSSD_LRS), `shared` = `workspace-shared-files`
  (azurefile-csi Premium, RWX). Generic clusters: whatever the admin puts in the catalog.
* **Re-attach / clone**: `fromDrive: <pvc>` creates a workspace on a detached drive (any name, so
  you can rename); `fromSnapshot: <snap>` creates a *new* drive from a VolumeSnapshot (PVC
  `dataSource`). Snapshots: `VolumeSnapshot` objects labelled `devspaces.io/drive=<pvc>`; class from
  catalog `storage.snapshotClass`.
* CLI/portal verbs: `drives list|show|delete|resize|snapshot|snapshots|create-shared`,
  `create --from-drive --from-snapshot --storage --mount`, `delete [--purge]`.

## 2. Sharing

* **Share a workspace** (pair programming): owner adds a guest. Effects in the *owner's* namespace:
  guest's public key(s) appended to `ws-<ws>-ssh` `authorized_keys` with comment `guest:<user>`;
  `Role workspace-guest-<ws>` (get pod `ws-<ws>-0`, pods/exec, pods/portforward, get statefulset,
  get service) + `RoleBinding share-<ws>-<guest>` labelled `devspaces.io/share=workspace`,
  `devspaces.io/guest=<sanitised>` and annotation `devspaces.io/guest-user=<full identity>`.
  Guest connects as `owner/name` (`wsctl ssh alice@example.com/api`, `workspace ssh alice/api`).
  The portal reads the guest's profile Secret `ws-<guest>/workspace-profile` and writes the
  RoleBinding with its own SA (ClusterRole `workspace-portal-sharing`: secrets named
  `workspace-profile` get; roles/rolebindings `share-*`/`workspace-guest-*` CRUD; networkpolicies
  `peer-*` CRUD; pv create/delete; pvc `shared-*` create/delete in `ws-*` namespaces; rolebindings list
  cluster-wide for "shared with me").
* **Share a drive**: only RWX (shared) drives. Portal copies the owner's PV `spec.csi`
  (driver, volumeHandle, volumeAttributes, secret refs) into a new PV `shared-<owner>-<name>-<guest>`
  with `claimRef` → PVC `shared-<owner>-<name>` in the guest's namespace, `reclaimPolicy: Retain`,
  optional `readOnly`. Labels `devspaces.io/drive=shared-mount`, `devspaces.io/shared-by=<owner>`.
  Mount syntax in a workspace: `mounts: ["shared:team-data", "shared:alice/team-data"]` →
  `/shared/<name>`; env `WORKSPACE_MOUNTS`.
* Home drives are RWO disks and are never shared; share the workspace (SSH) instead, or snapshot →
  the other dev creates from snapshot (same-namespace only; cross-namespace needs an admin copy).

## 3. Pod-to-pod networking ("two or more dev pods talk to each other")

Three tiers, cheapest first; all are plain NetworkPolicy + DNS (works with Cilium on AKS, Calico, …):

1. **Same namespace = already connected.** Every workspace has a headless Service, so
   `ws-<name>.<namespace>.svc.cluster.local` resolves to the pod IP; the tenant NetworkPolicy allows
   all traffic between pods of one namespace. A developer's own workspaces talk freely on any port.
   Env `WORKSPACE_DNS` and `wsctl network NAME` / `workspace net NAME` print names and peers.
2. **Project namespace ("virtual namespace")** for a team: `ws-proj-<name>` (labels
   `devspaces.io/tenant=proj-<name>`, `devspaces.io/kind=project`, `devspaces.io/members=a.b.c`),
   RoleBinding of ClusterRole `workspace-developer` for each member, quota, the same tenant
   NetworkPolicies. Created by `workspace admin project create NAME --members a,b` (and Helm values
   `projects:`). Members create workspaces there with `namespace: ws-proj-<name>` / `--project NAME`;
   everything in it is mutually reachable, visible to all members in `list`.
3. **Links across personal namespaces** (ad hoc, consent on both sides): `POST .../links {peer}` in
   *my* namespace creates NetworkPolicy `peer-<ws>-<peerns>-<peerws>` with ingress from
   `namespaceSelector{devspaces.io/tenant=<peer>} + podSelector{devspaces.io/workspace=<peerws>}`
   and the matching egress. Status `pending-peer` until the peer links back. Uses impersonation
   (devs may manage NetworkPolicies in their own namespace — add to ClusterRole `workspace-developer`).

**Service mesh (Istio ambient) is optional**, catalog `networking.mesh: none|istio-ambient`. When set,
onboarding labels namespaces `istio.io/dataplane-mode=ambient` (mTLS + identity without sidecars),
and `deploy/platform/mesh/` ships examples: `AuthorizationPolicy` (only the owner's and linked
workspaces may call), a `VirtualService`/`HTTPRoute` that routes `api.checkout.svc` to a developer's
workspace by header `x-dev: alice` (test your branch against the team's stack). Recommendation in the
docs: start with tiers 1–3; add ambient mesh when you need mTLS identity, L7 authorization or
header-based routing. Multicast stays Multus/Cilium as before (not through the mesh).

## 4. Azure

* **AKS**: Azure CNI powered by Cilium (NetworkPolicy + optional eBPF multicast), node pools
  `system`, `workspaces` (autoscaled, user-namespace capable Ubuntu 22.04+/containerd 2), `lab`
  (tainted `devspaces.io/lab=true:NoSchedule`, optionally GPU), OIDC issuer + Workload Identity,
  ACR (optionally behind Artifactory), Key Vault + CSI secrets driver for the oauth2-proxy secret,
  CSI snapshot controller, storage classes above, Entra ID app registrations.
* **Identity**: oauth2-proxy `--provider=entra-id` for the browser; the portal validates Entra
  **bearer JWTs** for the CLI (`-oidc-issuer https://login.microsoftonline.com/<tenant>/v2.0
  -oidc-audience api://devspaces -oidc-user-claim preferred_username`). Two app registrations:
  `devspaces-portal` (web, redirect URI for oauth2-proxy) and `devspaces-api` (exposes scope
  `api://devspaces/Workspaces.ReadWrite`, allows public client `devspaces-cli` with device-code flow).
  Group claims → `groups` (so a group can be onboarded / made project member).
* **Azure API**: Azure API Management fronts the portal's `/api/*` as the "Workspaces API": imports
  `api/openapi.yaml`, policy `validate-azure-ad-token` (tenant, audience `api://devspaces`), rate limit,
  forwards to the internal ingress with the original bearer token. Bicep + CLI steps in
  `deploy/cluster/providers/azure/`. Portal `-oidc-*` flags are what make the token usable end-to-end.
* **Ingress**: Application Gateway for Containers (or NGINX ingress + cert-manager) with TLS;
  internal-only option via private load balancer.

## 5. Go CLI `wsctl` (`clients/wsctl`)

* Talks only to the portal API (no kubectl needed). Config `~/.config/wsctl/config.json`
  (`portal`, `tenant`, `clientId`, `scope`, `token`, `refreshToken`, `expiresAt`). Login modes:
  `wsctl login` (OAuth2 device-code flow against Entra; stdlib), `wsctl login --token -`
  (paste), `wsctl login --az` (`az account get-access-token --resource api://devspaces`),
  `wsctl login --dev` (demo portals with `-dev-user`: no token).
* Commands: `create list get start stop delete logs idle keys ssh code proxy ssh-config
  drives (list show delete resize snapshot snapshots restore create-shared) share (add rm list)
  link (add rm list) network projects catalog me version`. `ssh`/`code` write an OpenSSH config
  entry whose `ProxyCommand` is `wsctl proxy NAME`, which bridges stdin/stdout to the portal's
  WebSocket tunnel (`/api/workspaces/{name}/tunnel`). Output: table by default, `-o json`.
* Python `cli/workspace` stays the kubectl-native client (admins, air-gapped, GitOps); both read
  the same catalog and produce identical manifests (parity test).
