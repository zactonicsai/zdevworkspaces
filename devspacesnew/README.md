# devspaces — on-demand developer workspaces on Kubernetes

A developer runs one command and gets a root-capable Linux environment with their
language toolchain, their repo checked out, and an SSH endpoint VS Code can open.
The pod is disposable; the home directory is not. Idle workspaces scale to zero and
come back on the next `ssh`.

```
$ workspace create --lang rust --tools cmake,git --repo git@github.com:acme/engine.git
workspace: persistentvolumeclaim/ws-rust-home created
workspace: statefulset/ws-rust created
workspace 'rust' is ready in namespace ws-alice

  ssh         ssh ws-rust            (or: workspace ssh rust)
  VS Code     code --remote ssh-remote+ws-rust /workspace
  stop/start  workspace stop rust · workspace start rust   (auto-stops after 4h idle)
```

| Requirement | How it is met | Where |
|---|---|---|
| CLI with `--lang` / `--tools` | `workspace`, single-file Python 3.9+, stdlib only, drives `kubectl`; `--lang go,python` for multi-language | `cli/workspace` |
| Web portal (HTML + Tailwind + JS, Go API) | `portal/`: IBM Carbon-style UI, light/dark, 14–36 px text, guides & glossary popups, Artifactory-backed catalog; Go server impersonates the developer | §14, `portal/README.md` |
| Local demo on Docker | Compose (portal + one workspace) and a 3-node kind cluster running everything | `docs/demo-docker.md`, `deploy/demo/` |
| Artifactory as the source of everything | images, tool downloads and every package manager (pip/npm/go/cargo/maven/apt) go through Artifactory repos | §15 |
| VS Code Remote-SSH | sshd in the pod, reached through `kubectl exec` as an SSH `ProxyCommand`; keys and `~/.ssh/config` managed by the CLI; port forwarding native to SSH | §4 |
| Persistence | One PVC per workspace mounted at `/home/dev`; everything a developer touches lives there | §5 |
| Git auth | Per-workspace deploy key (default), or your key / token; clone on first boot, push any time | §6 |
| Language/tool agnostic | One base image + one feature installer per language or tool; languages are baked, tools install at boot or get baked into variants | §7 |
| Multicast | Secondary macvlan network (Multus) with IGMP-tuned sysctls and a `224.0.0.0/4` route; `mcast-check` tool | §8 |
| Device drivers | `hardware` (privileged, host kernel) and `kernel` (Kata micro-VM) profiles on a tainted lab pool; device plugins, VFIO, host paths | §9 |
| Isolation | User namespaces (K8s ≥ 1.36 GA) or gVisor/Kata, Pod Security Admission, per-tenant namespaces, quotas, NetworkPolicy, ValidatingAdmissionPolicy guardrails | §10 |
| Home drives that outlive the pod | the PVC is a first-class **drive**: kept on stop/delete/crash, protected by an admission policy, re-attachable to a new workspace, snapshots, storage choices (fast/standard/shared) | §5, `docs/architecture.md` §6 |
| Sharing | share a workspace (SSH access for a teammate) or a shared RWX drive (`/shared/<name>`), from portal or CLI | §18 |
| Pods talking to each other | same-namespace DNS (free), **project namespaces** for teams, **links** between developers' workspaces; Istio ambient optional | `docs/workspace-networking.md` |
| Azure: AKS + Entra + API | step-by-step AKS guide, scripts/Bicep for storage classes, Entra app registrations, Key Vault, API Management in front of the Workspaces API | `docs/azure-aks-setup.md`, `deploy/cluster/providers/azure/` |
| Existing AKS cluster with Istio | `quickstart.sh`: detect Istio add-on/revision/gateway and storage, create namespaces, Helm-install the portal through the Istio gateway (or a port-forward trial), onboard developers, start sample dev pods, verify | `docs/existing-aks-istio.md`, `deploy/cluster/providers/azure/existing/` |
| **Any cluster, phase by phase** | `deploy/phases/`: eight small scripts (check → namespace → storage → Helm platform → route → developers → dev pods → verify) that only share `env.sh`/`detected.env`; plain kubectl + helm, no cloud CLI; Istio and Ingress optional; `--dry-run` everywhere | `deploy/phases/README.md` |
| **Tutorial: existing AKS + Istio, step by step** | plain-language walkthrough of the scripts above — rent the namespace, Helm install, add the VirtualService to the gateway you already have (explained line by line), spin up dev pods, expose a dev app, switch on Entra/Keycloak sign-in; `60-virtualservice.sh`, `70-expose-workspace.sh`, `AUTH=oidc` | `docs/tutorial-aks-istio.md` |
| Go CLI without kubectl | `wsctl`: talks to the portal API with an Entra bearer token (device-code login), SSH/VS Code through a WebSocket tunnel | `clients/wsctl/` |
| Design document | goals, layers, identity, drives, sharing, networking tiers, security model, data model | `docs/architecture.md` |
| Keycloak + portal on Docker, login required | `deploy/demo/keycloak/`: Compose with Keycloak (realm, users alice/bob/admin, PKCE/device/bearer clients) and the portal image; `up.sh kube` makes the API create real developer pods on the kind cluster by impersonation; `test.sh` smoke test | `deploy/demo/keycloak/README.md`, `docs/demo-docker.md` (Level 1b) |
| Portal image for Helm, Python API, Keycloak | `portal-py/` (Python API + the UI, `Dockerfile`, `build.sh`, `--stub`), UI calls in one module (`api.js`), runtime `config.js` (API URL, OIDC issuer/client), browser PKCE login, footer live/stub switch; `values-keycloak.yaml` | `portal-py/README.md`, `portal/README.md` |

---

## 1. Architecture

```
 developer laptop                              Kubernetes cluster
┌──────────────────────────┐        ┌──────────────────────────────────────────────────────┐
│ workspace CLI            │ kubectl│ devspaces-system                                     │
│  ├─ renders manifests    ├───────►│  ├─ ConfigMap workspace-catalog   (langs/tools/profiles)│
│  ├─ ~/.ssh/config entry  │        │  ├─ ConfigMap workspace-features  (hot-added installers)│
│  └─ keys, known_hosts    │        │  ├─ CronJob  workspace-reaper     (idle → replicas=0)   │
│                          │        │  ├─ NAD      mcast                (macvlan + whereabouts)│
│ VS Code ── Remote-SSH ───┤        │  ├─ RuntimeClasses gvisor, kata                       │
│  Host ws-rust            │        │  └─ ValidatingAdmissionPolicy guardrails              │
│  ProxyCommand:           │        │                                                      │
│   workspace proxy rust ──┼─exec──►│ ws-alice  (PSS baseline)        ws-alice-lab (privileged)│
│   = kubectl exec socat   │  :2222 │  StatefulSet ws-rust (1 replica)  StatefulSet ws-driver │
└──────────────────────────┘        │   ├─ PVC ws-rust-home → /home/dev  ├─ privileged / Kata │
                                    │   ├─ Secret ws-rust-ssh (hostkey,  ├─ hostPath /dev,     │
                                    │   │   authorized_keys)             │   /lib/modules …     │
                                    │   ├─ Secret ws-rust-git (deploy    ├─ device-plugin      │
                                    │   │   key or token)                │   resources          │
                                    │   └─ headless Service              └─ net1 (multicast)   │
                                    │  ResourceQuota · LimitRange · NetworkPolicy · RoleBinding│
                                    └──────────────────────────────────────────────────────┘
```

**Design decisions and why**

* **StatefulSet with one replica, not a Deployment or a bare Pod.** Scaling to zero keeps
  the object (and its spec, idle timeout, labels) while deleting the pod; the PVC is bound
  by name, so the pod always comes back on the same volume; the stable pod name
  `ws-<name>-0` makes `kubectl exec` trivial.
* **No operator/CRD in the critical path.** The CLI renders plain Kubernetes objects and
  `kubectl apply`s them. Admins can read, diff and GitOps everything; `--dry-run --offline`
  gives you the YAML to commit. The only "controller" is a CronJob that reaps idle
  workspaces, and it is the same CLI binary. (An operator can be layered later without
  changing what developers see.)
* **SSH over `kubectl exec`, not a public endpoint.** sshd listens on loopback; the SSH
  stream rides inside an already-authenticated, RBAC-authorized API-server connection, so
  there is no ingress, no LoadBalancer and no second identity system to run. `--expose`
  exists for the cases that need raw TCP (SSH from a device, a CI runner).
* **Per-developer namespaces** (`ws-<user>`, optional `ws-<user>-lab`) are the tenancy
  boundary: RBAC, ResourceQuota, NetworkPolicy and Pod Security levels all attach to the
  namespace, and the lab namespace is the only place privileged pods are admitted.
* **Root inside a user namespace** is the default. The developer is uid 0 with `sudo`,
  `apt`, `mount` and `ptrace` all working, while the host sees an unprivileged uid.
  Kubernetes `hostUsers: false` went GA in v1.36 (April 2026) and needs kernel ≥ 6.3 and
  an idmap-capable filesystem for the home volume (ext4/xfs/btrfs block storage; not NFS).
  On clusters that cannot meet that, `--isolation gvisor` gives an equivalent posture.

## 2. Repository layout

```
api/openapi.yaml                the HTTP API contract (portal serves it at /openapi.yaml; APIM imports it)
cli/workspace                   the kubectl-native CLI (developer + admin commands)
clients/wsctl/                  the Go CLI that needs only the portal URL (login, create, ssh/code via tunnel, drives, share, link)
images/
  base/Dockerfile               ubuntu:24.04 + sshd, git, sudo, compilers, entrypoint, features
  langs/Dockerfile              FROM base; runs one feature installer → workspace-dev
  cli/Dockerfile                CLI + kubectl image used by the reaper CronJob
  runtime/entrypoint.sh         boot sequence (home seed → sshd → git → tools → clone)
  runtime/sshd_config           hardened sshd for Remote-SSH
  runtime/lib.sh                helpers for feature installers (apt_install, profile_env, seed_dir)
  runtime/bin/install-feature   runs features/<name>/install.sh or features.d/<name>.sh
  runtime/skel/                 first-boot dotfiles (persistent bash history, prompt, gitconfig)
  features/<name>/install.sh    python go rust java node cpp cmake gdb docker-cli kubectl protobuf mcast git
  build.sh                      builds/pushes base, languages, variants, multi-arch
deploy/platform/                kubectl apply -k   (installed once per cluster)
  catalog.json                  what developers may request; also the CLI's built-in fallback
  rbac.yaml  reaper.yaml  namespace.yaml
  networking/multicast-nad.yaml compute/runtime-classes.yaml  compute/device-plugins.yaml
  policy/admission.yaml         ValidatingAdmissionPolicy guardrails
  features/example-tool.sh      a hot-added tool installer
deploy/examples/                rendered output for four workspace variants, a tenant, the platform
deploy/portal/                  portal Deployment, impersonation RBAC (base/), oauth2-proxy + Ingress example
deploy/demo/                    Docker Compose + kind overlay for a laptop demo (docs/demo-docker.md)
deploy/cluster/                 production cluster layer: CONTRACT.md, check-contract.sh, providers/, addons.sh, multicast-cilium.sh
deploy/cluster/providers/azure/ AKS step scripts: prereqs, cluster, storage classes, Entra apps, Key Vault, APIM (Bicep + policy), multicast
deploy/cluster/providers/azure/existing/  bring-your-own AKS (+ Istio add-on): detect, namespaces, Helm install, mesh check, dev pods, verify
deploy/platform/mesh/           optional Istio ambient examples (AuthorizationPolicy, waypoint, header-based HTTPRoute)
deploy/prod/                    production phase scripts: images, helm install, onboarding, smoke test
charts/devspaces/               Helm chart for the Kubernetes layer (+ examples/values-{aws,gcp,azure,onprem}.yaml)
docs/architecture.md            design and architecture document
docs/azure-aks-setup.md         Azure AKS, step by step (+ .docx)
docs/existing-aks-istio.md      workspaces on an AKS cluster you already run, with Istio
docs/workspace-networking.md    how workspaces talk to each other: same namespace, projects, links, mesh
docs/production-deployment.md   phased, provider-agnostic production guide
docs/demo-docker.md             step-by-step demo guide
portal/                         Go API + web UI (see portal/README.md); web/api.js = every UI call, web/auth.js = OIDC/Keycloak login,
                                web/config.js = runtime config, web/stub/ = static JSON for the stub switch
portal-py/                      the same API in Python (+ the same UI) as a second image for Helm (portal.backend=python); --stub mode
```

## 3. Lifecycle of a workspace

```
workspace create ──► render ──► kubectl apply ──► wait Ready ──► ~/.ssh/config + known_hosts
                       │                            │
                       │ PVC, Secret(ssh), Secret(git), Service, StatefulSet(replicas=1)
                       │                            │
pod boot:  seed /home/dev from skel (first time) → sshd up (Ready) → heartbeat loop
           → seed toolchains (first time) → git identity/creds → install --tools
           → clone --repo (first time) → dotfiles
idle:      heartbeat stamps devspaces.io/last-active while ssh or an editor server is alive;
           CronJob scales to 0 after devspaces.io/idle-timeout (default 4h, `workspace idle`)
resume:    `ssh ws-rust` → ProxyCommand → `workspace proxy` scales to 1, waits for Ready,
           bridges stdio to sshd — VS Code does not need to know the pod was gone
delete:    `workspace delete` removes everything but the PVC; `--purge` removes that too
```

The CLI stores the full request as the `devspaces.io/spec` annotation on the
StatefulSet, so `workspace status` and a re-run of `create` know exactly what was asked
for. Re-running `create` is idempotent: the PVC is kept, the host key is reused (so
VS Code never sees a changed host key), the deploy key is reused unless `--rotate-git-key`.

## 4. VS Code Remote-SSH

`workspace create` writes a managed block to `~/.ssh/config`:

```
Host ws-rust
    User dev
    HostKeyAlias ws-rust
    ProxyCommand /usr/bin/python3 /usr/local/bin/workspace --namespace ws-alice proxy rust
    IdentityFile ~/.config/workspace/keys/ws-alice--rust
    IdentitiesOnly yes
    UserKnownHostsFile ~/.config/workspace/known_hosts
    StrictHostKeyChecking yes
    ConnectTimeout 240
```

* **Key management.** An ed25519 client key is generated per workspace and never leaves the
  laptop; its public half goes into the `ws-<name>-ssh` Secret. sshd reads
  `authorized_keys` straight off the Secret mount, so `workspace ssh-config rust` on a
  second machine adds that machine's key and it is live within a minute, no restart.
  The host key is generated once per workspace, stored in the same Secret and pinned in a
  private `known_hosts` — strict host-key checking stays on.
* **Transport.** `workspace proxy` = start-if-stopped + `kubectl exec -i … socat - TCP:127.0.0.1:2222`.
  The ProxyCommand pattern is understood by every OpenSSH client, including the one VS Code
  bundles on macOS/Linux/Windows (on Windows use WSL or edit the ProxyCommand quoting).
* **Port forwarding.** sshd allows TCP forwarding, so the VS Code *Ports* panel, `ssh -L`
  and `code --remote` tunnels all work; `workspace port-forward rust 8080:8080` is the
  kubectl alternative. Agent forwarding is available with `--git-agent`.
* **Cold start.** Raise VS Code's connect timeout once:
  `"remote.SSH.connectTimeout": 180`. The VS Code server installs into `~/.vscode-server`
  on the persistent volume, so reconnects skip the download.
* Other editors: anything that speaks SSH (JetBrains Gateway, Zed, Neovim remote) works
  with the same host entry.

## 5. State and persistence

* `/home/dev` is the **home drive** — a PVC (`ws-<name>-home`, `--size`, `--storage fast|standard`)
  labelled `devspaces.io/drive=home`. It is kept on stop, delete, crash and recreate; the
  StatefulSet has `persistentVolumeClaimRetentionPolicy: Retain`, and an admission policy
  refuses to delete a home drive unless it was marked (`delete --purge`, "Delete drive" in the
  portal). Detached drives (`workspace drives list`) can be re-attached to a new workspace
  (`create NEW --from-drive ws-old-home`); `drives snapshot` + `--from-snapshot` make copies.
  Shared RWX drives (`drives create-shared`, `--mount shared:NAME`) appear at `/shared/<name>`.
  `/workspace` is a
  symlink to `~/workspace`, where repos are cloned. Code, shell history (`PROMPT_COMMAND`
  flushes after every command), `~/.cargo`, `~/go`, `~/.npm`, `~/.m2`, `~/.gradle`, venvs,
  `~/.vscode-server`, apt's downloaded `.deb` cache — all on the volume.
* Toolchains live in the image. Those a developer legitimately mutates (rustup) are
  *seeded* into the home volume on first boot (`seed_dir` in `lib.sh`), so
  `rustup toolchain add nightly` survives a restart while the image stays the fast path.
* Everything outside `/home/dev` is rebuilt from the image on every start — by design.
  Tools belong in `--tools` (installed at boot, cached `.deb`s make it fast) or in a baked
  image variant, not in a hand-run `apt install`.
* `fsGroup: 1000` + `OnRootMismatch` sets ownership once per volume; the entrypoint fixes up
  the rest. Volume resize follows the StorageClass (`allowVolumeExpansion`).

## 6. Git integration

| `--git-auth` | What happens | When to use |
|---|---|---|
| `deploy-key` (default) | The CLI generates an ed25519 key held **only** in the `ws-<name>-git` Secret and prints the public half. Add it as a deploy key (write access) or to your account. | Default; nothing from your laptop is copied into the cluster |
| `ssh --git-key ~/.ssh/id_ed25519` | Your key is stored in the Secret | Personal key, many repos |
| `token --git-token-stdin` | HTTPS token (`WORKSPACE_GIT_TOKEN` or stdin) served by a credential helper that reads the Secret mount at use time | GitHub App / PAT / GitLab tokens |
| `none` | Clone anonymously | Public repos, or `--git-agent` |

In the pod the key is placed at `/run/workspace/git_key` (container filesystem, never the
volume), `~/.ssh/config` pins it to `GIT_HOST`, and `git clone --repo [--branch]` runs on
first boot only — an existing checkout is never touched. `--git-name/--git-email` set the
identity once; `.gitconfig` on the volume keeps later changes. Forges' host keys are
pre-seeded in `ssh_known_hosts`, `StrictHostKeyChecking accept-new` covers the rest.
Rotate with `--rotate-git-key`; the new key is active on the next start.

## 7. Language and tool strategy

```
workspace-base ──┬── workspace-python ─┐
 (ubuntu 24.04,  ├── workspace-go      │   + --tools at boot:  cmake gdb docker-cli kubectl protobuf mcast …
  sshd, git,     ├── workspace-rust    ├── + baked variants:   workspace-rust-cmake-gdb   (build.sh EXTRA_FEATURES)
  sudo, gcc,     ├── workspace-java    │   + hot-added tools:  ConfigMap workspace-features/<name>.sh
  entrypoint)    ├── workspace-node    │   + add a language:   features/dev/install.sh + catalog.json entry
                 └── workspace-cpp    ─┘
```

A **feature** is one idempotent `install.sh` that runs as root, either at image build time
(`langs/Dockerfile` for languages) or at pod boot (`--tools`). The same script serves both,
so there is exactly one place to maintain per tool. Rules: install under `/usr/local` or
`/opt`, export user-state locations into `$HOME` via `profile_env`, never write to
`/home/dev`.

**More than one language per workspace** is first-class: `--lang go,python` (or ticking
several languages in the portal) uses the first language's image and installs the others
as features during the first boot (`WORKSPACE_LANGUAGES=go,python`, `WORKSPACE_TOOLS=…,python`).
Teams that always need the same mix bake a combined image
(`LANGS=go EXTRA_FEATURES=python ./build.sh` → `workspace-go-python`); the portal detects
such images in Artifactory and offers them.

**Artifact tools** need no script at all: a catalog entry with an `artifact` block
(repo, path with `{version}`/`{arch}`/`{goarch}` placeholders, install type `tar|zip|binary|deb|script`,
binaries to link) is enough. At boot `install-artifact` downloads it from Artifactory, verifies
an optional sha256, and links the binaries into `/usr/local/bin`. Eighteen common open-source
tools ship this way (ripgrep, fd, bat, fzf, yq, gh, lazygit, delta, shellcheck, hadolint,
golangci-lint, buf, just, hyperfine, k9s, direnv, duckdb…).

Adding a tool without touching images: drop `foo.sh` into the `workspace-features`
ConfigMap and add `"foo"` to `catalog.json` → `workspace create --tools foo` works on
the next pod start. Adding a language: `features/foo/install.sh`, a `catalog.json`
`languages` entry, `LANGS="foo" ./build.sh`. The catalog ConfigMap is the source of truth;
the CLI reads it live and only falls back to its built-in copy when it cannot.

Images are multi-arch capable (`PLATFORMS=linux/amd64,linux/arm64`), pin nothing but the
Ubuntu release, and resolve "latest stable" for Go/Node/Gradle at build time (override with
`GO_VERSION=…` etc. for reproducible builds).

## 8. Multicast

Primary pod networks are unicast overlays: VXLAN/Geneve replicate nothing, IPAM datapaths
drop `224.0.0.0/4`, and NetworkPolicy engines have no notion of a group. Instead of
depending on any single CNI's multicast feature, `--multicast` attaches a **second interface**
(`net1`) from the `devspaces-system/mcast` NetworkAttachmentDefinition:

* `macvlan` in bridge mode on the node NIC/VLAN carrying the multicast segment, so pods on
  one node see each other in-kernel and pods across nodes see each other through the switch
  (IGMP snooping with a querier, or snooping off).
* `whereabouts` IPAM from a dedicated range, plus a `224.0.0.0/4` route via `net1` so
  libraries that do not bind an interface still send out the right one.
* a chained `tuning` plugin that sets `rp_filter=0`, `igmp_max_memberships=1024`,
  `igmp_max_msf`, and socket buffer limits *inside the pod netns* at CNI ADD time — no
  pod-level sysctls, so it passes Pod Security `baseline` and needs no kubelet flags.
* `--tools mcast` ships `iperf`, `smcroute`, `tcpdump`, `socat` and `mcast-check`, which
  joins a group on `$WORKSPACE_MCAST_IFACE`, sends, and confirms receipt
  (`mcast-check --listen` in a second workspace proves cross-pod delivery).

Multicast on `net1` is deliberately a shared, flat segment (that is what cross-workspace
testing needs); keep it on its own VLAN. Cloud VPCs do not forward multicast between VMs:
run the multicast pool on bare-metal/on-prem nodes, or enable Cilium's eBPF multicast
datapath (beta, VXLAN mode, `cilium config set multicast-enabled true`) and program
groups/subscribers — notes are in `networking/multicast-nad.yaml`.

## 9. Device drivers and hardware

| Profile | Runtime | Kernel | Use for | Guardrails |
|---|---|---|---|---|
| `standard` | runc + user namespace | host, unprivileged | everyday development | PSS baseline, no host access |
| `hardware` | runc, **privileged** | host | USB/serial/I2C/GPU userspace drivers, out-of-tree modules against the *host* kernel, perf/eBPF | tainted lab pool, `nodeSelector devspaces.io/lab`, **one lab workspace per node** (cross-namespace pod anti-affinity), hostPath allow-list, no host namespaces |
| `kernel` | **Kata** micro-VM | own guest kernel | kernel module development you expect to crash, PCI devices via VFIO | lab pool, blast radius = the VM |

Mechanisms (all composable on the command line):

```
--device nvidia.com/gpu=1                 device plugins (NVIDIA, smarter-device-manager for
--device smarter-devices/ttyUSB0=1        /dev/tty*, i2c, spi, video, gpio — config shipped),
--device intel.com/sriov_netdevice=1      SR-IOV VFs for NIC drivers, VFIO for whole PCI functions
--device hugepages-2Mi=1Gi                hugepages for DPDK-style drivers (mounted at /dev/hugepages)
--host-path /dev --host-path /sys/bus/pci hardware profile already mounts /lib/modules, /usr/src,
                                          /lib/firmware read-only so `make -C /lib/modules/$(uname -r)/build` works
--cap SYS_RAWIO                           extra capabilities on the kernel profile (privileged needs none)
```

`deploy/platform/compute/` contains the RuntimeClasses, the smarter-device-manager config,
lab-node bootstrap snippets (labels/taints, kernel headers, VFIO binding) and GPU notes.

## 10. Isolation model

What a developer **can** do: be root in their container, `sudo`, `apt`, `ptrace` their own
processes, bind any port, mount FUSE/tmpfs, use the full CPU/memory they asked for, and in
the lab tier load modules and touch hardware on a node nobody else is using.

What they **cannot** do, and which layer stops them:

| Threat | Control |
|---|---|
| Container breakout → host root | `hostUsers: false` maps container root to an unprivileged host uid (or gVisor/Kata); seccomp `RuntimeDefault`; capabilities dropped to the baseline set |
| Running privileged / hostPath / host namespaces in the standard tier | Pod Security Admission `baseline` on `ws-<user>`; VAP re-checks and additionally requires userns or a sandbox RuntimeClass |
| Lab pods touching kubelet/CRI state or other hosts' namespaces | VAP: hostPath allow-list, `/var/lib/kubelet`, `/run/containerd`, `/etc/kubernetes` denied, `hostNetwork/PID/IPC` denied everywhere |
| Reaching another developer's workspace | NetworkPolicy: ingress only from the same namespace; egress blocks cluster CIDRs except DNS and the API server; sshd on loopback |
| Reading another developer's objects or Secrets | RBAC: `workspace-developer` is bound only in that developer's namespaces; the catalog is the only shared readable object |
| Starving the cluster | ResourceQuota per namespace, LimitRange defaults, ephemeral-storage limits, lab anti-affinity |
| Cloud credential theft via metadata | Egress to `169.254.169.254` blocked |
| Stale credentials on disk | Git key/token stay on the ephemeral container FS, never on the volume; secrets are rotated by re-running `create` |
| Forgotten workspaces | Heartbeat + reaper; `workspace idle NAME off` opts out explicitly |

The lab tier is the honest exception: a privileged pod shares the host kernel, so the
security boundary there is the **node**, enforced by the taint, the selector and the
one-per-node anti-affinity. For kernel work that may panic, use `kernel` (Kata) instead.

## 11. Try it on your laptop first

`docs/demo-docker.md` is a step-by-step walkthrough that needs nothing but Docker
(`./deploy/demo/check-prereqs.sh` tells you whether your machine is ready):
Level 0 is `portal/web/simulation.html`, a single static page that simulates the whole
flow — create, boot log, a shell in the browser, stop/start persistence, the reaper —
with localStorage as its "cluster"; Level 1 runs the portal (demo mode) and one real workspace container via Compose;
Level 1b (`deploy/demo/keycloak/up.sh`) adds Keycloak and makes login mandatory, optionally driving real pods on the kind cluster;
Level 2 runs the complete system — platform, portal, CLI, lab node, reaper, admission
guardrails — on a 3-node kind cluster (`./deploy/demo/demo-up.sh`); Level 3 adds a real
Artifactory OSS container for the tool-download path. After that, `./deploy/demo/demo-start.sh`
(`start|status|stop|sim`) brings the portal and services back up, skipping whatever already runs.

## 12. Production on any cloud (two layers)

`docs/production-deployment.md` is the phased production guide: a **cluster layer** with one
recipe per provider (`deploy/cluster/providers/` — EKS, GKE, AKS, VMware/kubeadm), a written
**contract** between the layers (`deploy/cluster/CONTRACT.md`, verified by
`check-contract.sh`), provider-agnostic add-ons (`addons.sh`, Cilium eBPF multicast via
`multicast-cilium.sh`), and the **Kubernetes layer** as a Helm chart (`charts/devspaces`,
example values per provider) driven by `deploy/prod/10…40` scripts. The quickstart below is
the short form of Phase 3.

## 13. Platform install (admin)

```bash
# 1. Node prerequisites: K8s ≥ 1.36 (userns GA) or ≥ 1.33 with the gate, kernel ≥ 6.3,
#    containerd ≥ 2.0 / runc ≥ 1.2; Multus + whereabouts on the multicast pool;
#    gVisor and/or kata-deploy where you want those RuntimeClasses; label+taint the lab pool.
# 2. Images
cd images && REGISTRY=ghcr.io/acme/devspaces TAG=2026.10 PUSH=1 ./build.sh
# 3. Edit deploy/platform/catalog.json: registry, tag, cluster CIDRs, default sizes.
#    Edit networking/multicast-nad.yaml: master NIC and IP range.
kubectl apply -k deploy/platform
# 4. Onboard developers (creates ws-<user> [+ ws-<user>-lab], quota, RBAC, NetworkPolicy)
workspace admin onboard alice@corp.example --lab
workspace admin onboard eng-platform --group --lab   # or bind a whole group
# 5. Ship the CLI: it is one file.  cp cli/workspace /usr/local/bin/
```

# 6. Portal (optional): edit deploy/portal/ingress-oauth2-proxy.yaml for your IdP, then
kubectl apply -k deploy/portal

Day-2: `workspace list -A`, `workspace admin reap -A --dry-run`, bump `catalog.json`
`tag` to roll images (next start of each workspace picks it up), add tools through the
`workspace-features` ConfigMap, snapshot PVCs with your CSI driver's VolumeSnapshot.

## 14. Developer quick reference

```bash
workspace cluster                                   # which cluster am I on: nodes, platform, workspaces, pods
workspace catalog                                   # what you can ask for
workspace create --lang go,python --tools kubectl,protobuf,ripgrep --repo git@github.com:acme/api.git
workspace create ml --lang python --size 100Gi --memory 32Gi --cpu 8
workspace create fw --lang cpp --profile hardware --device smarter-devices/ttyUSB0=1 --multicast --tools mcast
workspace create kmod --lang cpp --profile kernel --device vfio.io/pci-8086-1521=1
workspace ssh go            # starts it if stopped
workspace code go           # VS Code on /workspace
workspace port-forward go 8080:8080
workspace idle go 8h        # or 'off'
workspace ssh-config go     # on a second laptop
workspace stop go · workspace start go · workspace delete go [--purge]
workspace drives list · drives snapshot go · create go2 --lang go --from-snapshot SNAP · create api --from-drive ws-go-home
workspace drives create-shared team-data --size 100Gi · create api --lang go --mount shared:team-data
workspace share add go bob@corp.example --key bob.pub      # bob: workspace ssh alice/go  (or wsctl ssh alice/go)
workspace link add api --to bob@corp.example/api --ports 8080 · workspace net api
workspace --project checkout create api --lang go        # team project namespace (admin: workspace admin project create)
```

Or without kubectl, through the portal API (`clients/wsctl/README.md`):

```bash
wsctl login --portal https://workspaces.corp.example     # Entra device-code login (--az, --token, --dev also work)
wsctl create api --lang go,python --tools ripgrep --storage fast --wait
wsctl ssh api · wsctl code api                          # SSH/VS Code through the portal's WebSocket tunnel
wsctl drives list · wsctl share add api bob@corp.example · wsctl link add web bob@corp.example/api --ports 8080
```

## 15. Web portal

`portal/` is a single Go binary (standard library only) that serves a vanilla-JS,
Tailwind-styled single-page app and a JSON API. It renders **the same Kubernetes objects
as the CLI** (a test compares the two), so portal and CLI can manage each other's workspaces.

* **Identity & authorization.** The portal sits behind an authenticating proxy
  (oauth2-proxy example in `deploy/portal/`) and *impersonates* the signed-in developer
  towards the API server. Its ServiceAccount can only `impersonate`; RBAC is the developer's
  own, audit logs name the developer. Identity headers are trusted only from the proxy CIDRs,
  a NetworkPolicy admits only the proxy, state changes need an `X-Requested-With` header, and a
  strict CSP (no inline or third-party scripts) is set on every response.
* **UI.** IBM Carbon look: IBM Plex, Blue 60 actions, square corners; light theme white /
  Gray 100 text, dark theme black / white text; follows the OS, remembered per browser.
  A text-size control from 14 to 36 px scales the whole page (the header switches to the
  mobile layout when the text no longer fits). Mobile-first grids, native `<dialog>` popups
  for every glossary term with links to the official documentation, keyboard and
  screen-reader friendly.
* **Create wizard.** Basics (name, several languages, image version from Artifactory) →
  Tools (grouped, with live Artifactory availability and docs) → Code (repo, deploy key /
  token, dotfiles) → Size presets → Advanced (profiles, sandbox, multicast, devices, host
  paths, public SSH) → Connect (SSH keys) → Review. Drafts survive a reload.
* **Manage.** Status with auto-refresh, start/stop/delete (keep or purge the volume),
  connection instructions for VS Code / terminal / public address with copyable
  `~/.ssh/config` and `known_hosts` lines, deploy key, add SSH keys live, idle timeout,
  boot log, "recreate with changes".
* **Learn.** Thirteen step-by-step guides (what a workspace is, SSH keys, first workspace,
  VS Code, terminal, files & persistence, Git, languages & tools, Artifactory, stop/start,
  multicast, hardware & drivers, troubleshooting) written at a plain-language level, plus
  a 33-term glossary. Content lives in `portal/web/content.js`.
* **Demo mode.** `portal -mock` runs an in-memory cluster; opening `web/index.html` from
  disk runs a browser-only demo. Both make the UI explorable without Kubernetes.

Run: `cd portal && go run . -mock` → http://localhost:8080. Deploy: `kubectl apply -k deploy/portal`.

## 16. Artifactory: all resources from one place

`catalog.json → artifactory` names the Artifactory base URL and one repository per
ecosystem (`docker`, `generic`, `github`, `apt`, `pypi`, `npm`, `go`, `maven`, `cargo`).
With that in place:

| What | How it reaches Artifactory |
|---|---|
| Workspace images | `registry` points at the Docker repo; `pullSecret` is copied into each tenant namespace |
| Tools | `artifact` catalog entries resolve to `<url>/<repo>/<path>`; `install-artifact` downloads at boot |
| pip / uv, npm / pnpm / yarn, Go modules, Cargo, Maven / Gradle, apt | `configure-artifactory` runs at boot and writes `/etc/pip.conf`, npmrc, `GOPROXY`, `~/.cargo/config.toml`, `~/.m2/settings.xml`, apt sources — developers use the tools unchanged |
| Toolchain downloads in feature scripts (Go tarballs, Node, rustup) | `lib.sh fetch()` rewrites public hosts to generic remotes listed in `artifactory.mirrors` |
| Credentials | per-developer token in `workspace-profile` (portal Profile page or `kubectl create secret`), injected as `ARTIFACTORY_TOKEN`; a netrc at `/run/workspace/netrc` serves curl, pip, Go and git |
| Portal catalog | lists images/tags and checks tool availability through the Artifactory REST API (read-only token) |

## 17. Known limits and trade-offs

* `hostUsers: false` + NFS home volumes do not work (no idmap support in the NFS client);
  use block storage or `--isolation gvisor`.
* gVisor does not implement every socket option multicast libraries use; use userns or Kata
  for multicast-heavy work.
* `--expose` publishes sshd on a LoadBalancer; it stays key-only, but your host-key pinning
  must be set up by hand for that path (`workspace status` prints the address).
* The reaper decides on SSH/editor-process presence, not CPU: a long batch job with no
  session open will be stopped at the timeout — set `workspace idle NAME off` (or run it
  under `tmux` inside an open session).
* Docker inside a standard workspace means the docker CLI against a remote daemon
  (`DOCKER_HOST`) or BuildKit; a dind sidecar belongs in the lab tier.

## 18. Sharing, projects and links (working together)

* **Share a workspace** — pair programming: the teammate's key is added to the workspace's
  `authorized_keys` and a RoleBinding in *your* namespace lets them exec into that one pod.
  Portal: workspace page → Sharing. CLI: `wsctl share add api bob@corp.example` (the portal
  looks up Bob's profile key) or `workspace share add api bob@corp.example --key bob.pub`.
  Bob connects with `wsctl ssh alice/api`; the workspace shows under *Shared with me*.
* **Share a drive** — data: only ReadWriteMany drives (Azure Files, EFS, NFS…). The portal
  creates a PV/PVC in the guest's namespace that points at the same share
  (`--mount shared:alice/team-data` → `/shared/team-data`), read-write or read-only.
* **Project namespace** — a team's "virtual namespace" (`ws-proj-<name>`): every member's
  workspaces live together, see each other and talk freely. `workspace admin project create
  checkout --members alice@corp.example,group:team-checkout`, or Helm `projects:`.
* **Links** — ad hoc pod-to-pod traffic between two developers' workspaces, consent on both
  sides, optional port list. Plain NetworkPolicies in your own namespace.
* **Mesh (optional)** — `networking.mesh: istio-ambient` labels tenant namespaces for Istio
  ambient; examples in `deploy/platform/mesh/`. Use when you need mTLS identity, L7 rules
  or header-based routing. Full guide: `docs/workspace-networking.md`.

The portal does sharing with its own ServiceAccount (ClusterRole `workspace-portal-sharing`),
confined by the `devspaces-portal-sharing-scope` admission policy to `share-*`,
`workspace-guest-*`, `peer-*` and `shared-*` objects; everything else is impersonation.
