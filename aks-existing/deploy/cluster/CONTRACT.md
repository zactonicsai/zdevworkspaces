# The cluster contract

The Kubernetes layer (`charts/devspaces`) runs on **any** cluster that keeps these
promises. The cluster layer (`deploy/cluster/providers/*`) is how each provider keeps
them. `deploy/cluster/check-contract.sh` verifies a cluster against this list, so the
two layers can be owned, changed and upgraded independently.

| # | Promise | Why | Checked by |
|---|---|---|---|
| C1 | Kubernetes **≥ 1.36** (≥ 1.33 with `UserNamespacesSupport` gate) | user namespaces GA; ValidatingAdmissionPolicy GA | version |
| C2 | Linux nodes, kernel **≥ 6.3**, containerd **≥ 2.0** (or CRI-O ≥ 1.25) | idmapped mounts for `hostUsers: false` | node info + probe pod |
| C3 | Node pools labelled: standard (no label needed), **lab** `devspaces.io/lab=true` + taint `devspaces.io/lab=true:NoSchedule` (optional but required for hardware/kernel profiles) | privileged workspaces isolated on their own machines | labels/taints |
| C4 | A **default StorageClass** backed by block storage (not NFS) with `allowVolumeExpansion: true` | home volumes; idmap needs ext4/xfs; growth without data loss | StorageClass |
| C5 | `LoadBalancer` Services or an **Ingress controller** reachable by developers; DNS name for the portal | portal (and optional `--expose` workspaces) | IngressClass present |
| C6 | **TLS** certificates (cert-manager ClusterIssuer, or the cloud LB terminates TLS) | portal over HTTPS | cert-manager / annotation |
| C7 | **OIDC** identity for `kubectl` (`--oidc-username-claim=email`) and for the portal's proxy, with the *same* e-mail | RBAC per developer, impersonation | `kubectl auth whoami` |
| C8 | A **NetworkPolicy-enforcing CNI** (Cilium, Calico, cloud-native CNIs with policy on) | tenant isolation | CNI detection |
| C9 | Outbound access from nodes to **Artifactory** (and nothing else required) | images, tools, packages | reachability from a probe pod |
| C10 | *Optional:* multicast — **Multus + macvlan** on an L2 pool, **or Cilium ≥ 1.16 in VXLAN mode** with `multicast-enabled` | `--multicast` workspaces | NAD CRD / cilium config |
| C11 | *Optional:* **gVisor** (`runsc`) and/or **Kata** handlers on labelled nodes | `--isolation gvisor|kata` | RuntimeClass handlers |
| C12 | *Optional:* device plugins on the lab pool (smarter-device-manager, NVIDIA, SR-IOV) and kernel headers installed | hardware profiles | node resources |
| C13 | metrics-server (for `kubectl top`, HPA) and a log/metrics pipeline | operations | metrics API |
| C14 | **Home-drive StorageClasses** with `reclaimPolicy: Retain` + `allowVolumeExpansion: true` for every `storage.choices[kind=home]` entry (e.g. `workspace-home-premium`) | a purged drive's disk survives for an admin to recover; drives grow | StorageClass fields |
| C15 | **CSI snapshot support**: external-snapshotter CRDs + controller and a `VolumeSnapshotClass` named in `storage.snapshotClass` | `drives snapshot` / `--from-snapshot` | CRDs + class present |
| C16 | *Optional:* a **ReadWriteMany StorageClass** (Azure Files, EFS, Filestore, NFS) for `storage.choices[kind=shared]` | shared drives mounted by several workspaces / developers | class + RWX probe |

Anything not in this table is the Kubernetes layer's own business and must not be assumed
from the provider: no provider CRDs, no cloud-specific annotations outside `values-*.yaml`.
