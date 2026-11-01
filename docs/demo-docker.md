# Run the devspaces demo on Docker — step by step

This guide takes you from an empty laptop to a working copy of the whole system, using
only Docker. You will end up with:

| Level | What you get | Needs | Time |
|---|---|---|---|
| **1 — Docker only** | the web portal (demo mode) and one real workspace container you can `ssh` into and open in VS Code | Docker | ~15 min (most of it image building) |
| **2 — Full system on kind** | a real Kubernetes cluster in Docker running the platform, the portal, the CLI, hardware-lab node, idle reaper, admission guardrails | Docker + kind + kubectl | ~30 min |
| **3 — Optional: real Artifactory** | tool downloads go through a JFrog Artifactory OSS container | Level 2 | ~15 min |

Every step says what to type, what it does, and what you should see. Commands are run
from the unpacked repository folder (`devspaces/`) unless stated otherwise.

> **Shortcut.** `./deploy/demo/demo-up.sh` runs all of Level 2 for you and prints the
> next steps. Read the steps below at least once anyway: they explain what each piece is.

---

## 0. Before you start

### 0.1 Hardware and operating system

* A laptop with **8 GB of memory available to Docker** (Docker Desktop → *Settings → Resources*),
  **20 GB of free disk**, and an internet connection (base images and tool downloads come
  from the internet in the demo; in production they come from Artifactory).
* macOS (Intel or Apple silicon), Windows 10/11 with **WSL 2**, or Linux. On Windows run
  everything inside the WSL 2 terminal.

### 0.2 Software

| Tool | Minimum | Check | Install |
|---|---|---|---|
| Docker (Desktop or Engine) with Compose v2 | Docker 24 | `docker version` and `docker compose version` | <https://docs.docker.com/get-docker/> |
| kind (Level 2) | **0.32** (fixes `hostUsers: false` on Kubernetes 1.36) — use **0.33.0** | `kind version` | <https://kind.sigs.k8s.io/docs/user/quick-start/#installation> |
| kubectl (Level 2) | 1.33 | `kubectl version --client` | <https://kubernetes.io/docs/tasks/tools/> |
| Python 3 | 3.9 | `python3 --version` | python.org, Homebrew, or your package manager |
| OpenSSH client | any | `ssh -V` | built into macOS, Windows 10+, Linux |
| VS Code + *Remote - SSH* extension (optional) | current | | <https://code.visualstudio.com/docs/remote/ssh> |
| Go 1.22+ (optional, only to run the portal from source) | 1.22 | `go version` | <https://go.dev/dl/> |

Check that Docker runs and which Linux kernel it uses (this matters later for user namespaces):

```bash
docker run --rm alpine:3.20 uname -r
```
You should see a version such as `6.10.14-linuxkit` (Docker Desktop) or your Linux kernel.
**6.3 or newer** is needed for the default *user namespace* isolation; older kernels still
work with a one-line fallback described in [Troubleshooting](#troubleshooting).

### 0.3 Get the code

Unzip `devspaces.zip` and go into the folder:

```bash
unzip devspaces.zip && cd devspaces
ls
#  README.md  cli/  deploy/  docs/  images/  portal/
```

### 0.4 Make an SSH key (once)

Workspaces accept SSH keys, never passwords. If `ls ~/.ssh/id_ed25519.pub` prints a file
name, you already have one. Otherwise:

```bash
ssh-keygen -t ed25519 -C "you@example.com"      # press Enter to accept the default file
cat ~/.ssh/id_ed25519.pub                       # this one line is your PUBLIC key
```
The line starts with `ssh-ed25519 AAAA…`. You will paste it into the portal later.
The other file (without `.pub`) is private: never paste it anywhere.

---

## Level 1 — Docker only (no Kubernetes)

What runs: two containers from `deploy/demo/docker-compose.yaml`.

```
 laptop                                   Docker
┌───────────────┐   http://localhost:8080  ┌──────────────────────────────────┐
│ browser       ├─────────────────────────►│ portal  (-mock: fake cluster)    │
│               │                          ├──────────────────────────────────┤
│ ssh / VS Code ├─────────────────────────►│ workspace (workspace-go:dev)     │
│               │   localhost:2222         │   sshd · tools · your code       │
└───────────────┘                          │   /home/dev = volume demo-home   │
                                           └──────────────────────────────────┘
```

### Step 1 — Build the images (10–20 minutes the first time)

```bash
LANGS="go" ./images/build.sh
```
What it does: builds `workspace/workspace-base:dev` (Ubuntu 24.04 + sshd, git, compilers,
the boot script), then `workspace/workspace-go:dev` (base + the Go feature), then
`workspace/workspace-cli:dev` (the CLI + kubectl, used by the reaper) and
`workspace/workspace-portal:dev` (the Go API + web UI). Add more languages with
`LANGS="go python rust"`; each adds a few minutes.

What you should see at the end:
```bash
docker images 'workspace/*'
# workspace/workspace-portal   dev   …   30MB
# workspace/workspace-cli      dev   …   190MB
# workspace/workspace-go       dev   …   1.4GB
# workspace/workspace-base     dev   …   900MB
```
If `npm` is not installed the script says so and uses the committed stylesheet — not an
error. If a build fails inside an `apt-get` or a download, run the script again; a flaky
network is the usual cause and Docker resumes from its layer cache.

### Step 2 — Create the SSH files the workspace container needs

In Kubernetes the CLI/portal generate a host key and an `authorized_keys` Secret. Under
plain Docker we make the same files by hand:

```bash
./deploy/demo/make-ssh-keys.sh                 # uses ~/.ssh/id_ed25519.pub
# authorized_keys now contains: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI… (you@example.com)
# host key fingerprint: SHA256:k3…
```
Note the fingerprint: ssh will show it on the first connection so you can compare.
The files land in `deploy/demo/ssh/`.

### Step 3 — Start the containers

```bash
docker compose -f deploy/demo/docker-compose.yaml up -d
docker compose -f deploy/demo/docker-compose.yaml logs -f workspace
```
You should see the boot log of the workspace:
```
[workspace 09:00:01] workspace 'demo' starting: lang=go profile=standard tools=cmake
[workspace 09:00:01] first boot: seeding /home/dev from /opt/workspace/skel
[workspace 09:00:02] sshd listening on 0.0.0.0:2222 (pid 42)
[workspace 09:00:03] installing tool: cmake
[workspace 09:00:20] tool ready: cmake
[workspace 09:00:20] installing artifact tool: ripgrep
[workspace 09:00:22] tool ready: ripgrep
[workspace 09:00:22] cloning https://github.com/golang/example.git into /home/dev/workspace/example
[workspace 09:00:25] clone complete
[workspace 09:00:25] ready
```
Press `Ctrl`+`C` to stop following the log (the containers keep running). The two
install lines show both tool mechanisms: `cmake` is a scripted *feature* from the image,
`ripgrep` is an *artifact* download — in production from Artifactory, here straight from
GitHub.

### Step 4 — Open the portal

Open <http://localhost:8080>. A yellow banner says **Demo mode**: this portal instance runs
a make-believe cluster inside the Go process (`-mock`), so you can click through everything
without Kubernetes:

1. **Profile** → paste your public key line → *Save profile*.
2. **Create** → name `engine`, tick **Go** then **Python** (Go becomes *main*) → *Next* →
   tick `ripgrep` and `cmake` → *Next* → repository `git@github.com:golang/example.git` →
   *Next* → *Medium* → *Next* → leave *Advanced* → *Next* → *Next* → **Create workspace**.
3. Watch it go from *Starting* to *Running* (about five seconds in mock mode) and read the
   *Connect* tab, the deploy key and the boot log.
4. Try the moon/sun button (dark mode), **A+** until 36 px (the header switches to the
   phone layout), and the **(i)** buttons that open glossary popups with documentation links.
5. **Guides** → *Create your first workspace* is the same flow explained for newcomers.

Nothing you create here touches the real workspace container from Step 3.

### Step 5 — Connect to the real workspace

```bash
ssh -p 2222 dev@localhost
# The authenticity of host '[localhost]:2222' can't be established.
# ED25519 key fingerprint is SHA256:k3…        ← compare with Step 2, then type yes
```
You are `dev` inside Ubuntu 24.04, root-capable via `sudo`. Try:
```bash
go version                    # from the image
cmake --version               # installed at boot (feature)
rg --version                  # installed at boot (artifact download)
ls ~/workspace/example        # cloned at boot
echo $WORKSPACE_LANGUAGES     # go
ws-logs                       # the boot log from inside
sudo apt install -y cowsay && /usr/games/cowsay "root works"
exit
```

### Step 6 — See what survives a restart

```bash
ssh -p 2222 dev@localhost 'echo hello > ~/keep-me.txt'
docker compose -f deploy/demo/docker-compose.yaml down        # container removed, volume kept
docker compose -f deploy/demo/docker-compose.yaml up -d
sleep 10
ssh -p 2222 dev@localhost 'cat ~/keep-me.txt; which cowsay || echo "cowsay is gone"'
# hello
# cowsay is gone
```
This is the central rule of workspaces: everything under `/home/dev` is on the volume and
survives; everything else is rebuilt from the image. Tools you want every time go in the
tool list (they were reinstalled during the second boot — check `ws-logs`).

### Step 7 — Open it in VS Code

Add this to `~/.ssh/config` on your laptop:
```
Host demo-ws
    HostName localhost
    Port 2222
    User dev
    IdentityFile ~/.ssh/id_ed25519
```
In VS Code: `F1` → *Remote-SSH: Connect to Host…* → `demo-ws` → *Open Folder* → `/workspace`.
The first connection installs the VS Code server into the workspace (it lands on the
volume, so later connections are quick). Open a terminal (`Ctrl`+`` ` ``) and run
`go run ./hello` inside `~/workspace/example`.

### Step 8 — Stop

```bash
docker compose -f deploy/demo/docker-compose.yaml down        # keep the home volume
docker compose -f deploy/demo/docker-compose.yaml down -v     # …or remove it too
```

---

## Level 2 — The full system on a local Kubernetes cluster

[kind](https://kind.sigs.k8s.io/) runs Kubernetes nodes as Docker containers. We create
three: a control plane, a normal worker, and a worker labelled and tainted as the
**hardware lab pool** — the same layout the production manifests expect.

```
 laptop                                Docker
┌──────────────────┐                  ┌──────────────────────────────────────────────┐
│ kubectl / CLI    ├─────────────────►│ kind cluster "devspaces"                     │
│ browser ─────────┼─ port-forward ──►│  devspaces-control-plane                     │
│ ssh / VS Code ───┼─ kubectl exec ──►│  devspaces-worker        ws-demo/ws-go-0     │
└──────────────────┘                  │  devspaces-worker2 (lab) ws-demo-lab/ws-hw-0 │
                                      │  devspaces-system: portal, reaper, catalog   │
                                      └──────────────────────────────────────────────┘
```

### Step 1 — Install kind and kubectl

Follow the links in the table above, then:
```bash
kind version          # kind v0.33.0 …   (0.32 or newer)
kubectl version --client
```

### Step 2 — Create the cluster (2–3 minutes)

```bash
kind create cluster --config deploy/demo/kind-config.yaml --wait 120s
kubectl get nodes --show-labels | cut -c1-120
```
What the config does: pins the Kubernetes **1.36.4** node image (user namespaces are GA
there), names the cluster `devspaces`, labels the third node `devspaces.io/lab=true` and
taints it `devspaces.io/lab=true:NoSchedule` so only lab-profile workspaces land on it.

You should see three nodes in `Ready` state. `kubectl` now points at context
`kind-devspaces`.

### Step 3 — Build the images

If you did Level 1 you already have them. Otherwise, or to add Python:
```bash
LANGS="go python" ./images/build.sh
```

### Step 4 — Load the images into the cluster (3–5 minutes)

kind nodes cannot see your laptop's Docker images, and there is no registry in the demo,
so the images are copied into the nodes:
```bash
kind load docker-image workspace/workspace-go:dev workspace/workspace-python:dev \
  workspace/workspace-cli:dev workspace/workspace-portal:dev --name devspaces
```
(Leave out `workspace-python` if you did not build it.) The demo catalog sets
`imagePullPolicy: IfNotPresent`, so the side-loaded images are used as they are. Check:
```bash
docker exec devspaces-worker crictl images | grep workspace
```

### Step 5 — Install the platform and the portal

```bash
kubectl apply -k deploy/demo
kubectl -n devspaces-system get deploy,cronjob,configmap
kubectl -n devspaces-system rollout status deploy/workspace-portal
```
`deploy/demo` is a kustomize overlay on top of the production manifests. It creates:

| Object | Purpose |
|---|---|
| Namespace `devspaces-system` | platform pieces live here |
| ConfigMap `workspace-catalog` | **demo catalog**: local images, no pull secret, tool downloads from GitHub |
| ClusterRoles `workspace-developer`, `workspace-admin`, … | what a developer may do inside their own namespace |
| CronJob `workspace-reaper` | stops idle workspaces every 10 minutes |
| RuntimeClasses `gvisor`, `kata` | present but unusable in kind (no such runtimes) |
| ValidatingAdmissionPolicy `devspaces-workspace-guardrails` | the rules every workspace pod must pass |
| Deployment `workspace-portal` | the web UI, in **dev-user mode** (`demo@example.com`, no sign-in) |

Differences from production, on purpose: no Multus network attachment (kind has no Multus),
no oauth2-proxy, portal NetworkPolicy removed so `port-forward` works. Never use the overlay
outside a throwaway cluster.

### Step 6 — Onboard the demo developer

A platform admin creates each developer's namespaces once. Here you are the admin:
```bash
./cli/workspace admin onboard demo@example.com --lab
kubectl get ns ws-demo ws-demo-lab --show-labels
kubectl -n ws-demo get resourcequota,networkpolicy,rolebinding
```
You should see two namespaces: `ws-demo` enforces the Pod Security *baseline* level,
`ws-demo-lab` the *privileged* level; each has a quota, network policies and a
RoleBinding for the user `demo@example.com`.

Tell the CLI to use that namespace (your kubectl identity in kind is `kubernetes-admin`,
which has no namespace of its own):
```bash
export WORKSPACE_NAMESPACE=ws-demo
```

### Step 7 — Create a workspace in the portal

```bash
kubectl -n devspaces-system port-forward svc/workspace-portal 8080:80
```
Leave that running and open <http://localhost:8080>. This time there is **no demo banner**:
the portal talks to the real cluster, impersonating `demo@example.com`.

1. **Profile** → paste your public key → *Save profile*. (It is stored as the Secret
   `ws-demo/workspace-profile`: `kubectl -n ws-demo get secret workspace-profile`.)
2. **Create** → name `go` → tick **Go** (and **Python** if you loaded it) → *Next* → tick
   `ripgrep` → *Next* → leave the repository empty or use a public one such as
   `https://github.com/golang/example.git` with *No login* → *Next* → *Small* → *Next* →
   *Next* → *Next* → **Create workspace**.
3. The workspace page shows *Pending* → *Starting* → *Running* (the first start takes about
   a minute: the volume is created, the image unpacked, ripgrep downloaded). Watch the same
   thing from the cluster side:
   ```bash
   kubectl -n ws-demo get statefulset,pod,pvc,secret,svc
   kubectl -n ws-demo logs ws-go-0 -f
   ```

What was created: a `StatefulSet` with one pod, a 5 GB `PersistentVolumeClaim` mounted at
`/home/dev`, a Secret with the SSH host key and your authorized key, a headless Service.
Exactly what `workspace create --lang go --tools ripgrep` would have made.

### Step 8 — Connect from your laptop

```bash
./cli/workspace ssh-config go      # writes the "ws-go" entry in ~/.ssh/config, pins the host key
./cli/workspace ssh go             # or simply: ssh ws-go
```
No port is open anywhere: the SSH session travels inside `kubectl exec` through the API
server (that is the `ProxyCommand` in the entry just written). Inside, try `rg --version`,
`cat ~/README-workspace.md`, `ws-logs`.

VS Code: set `remote.SSH.connectTimeout` to `180`, then `F1` → *Remote-SSH: Connect to
Host…* → `ws-go` → open `/workspace`; or `./cli/workspace code go`.

Port forwarding: inside the workspace run `python3 -m http.server 8000` (Python image) or
`go run` a small server; VS Code's *Ports* panel forwards it, or from a second terminal:
`./cli/workspace port-forward go 8000:8000` → <http://localhost:8000>.

### Step 9 — Stop, start, idle timeout, reaper

```bash
./cli/workspace stop go               # pod gone, volume kept
kubectl -n ws-demo get pod,pvc        # no pod; PVC still Bound
ssh ws-go                             # connecting starts it again (about a minute)
./cli/workspace idle go 30m           # change the auto-stop timeout
./cli/workspace admin reap -A --dry-run -v     # what the CronJob would do right now
kubectl -n devspaces-system create job --from=cronjob/workspace-reaper reap-now
kubectl -n devspaces-system logs job/reap-now
```
The reaper only stops a workspace whose last SSH/editor activity (pod annotation
`devspaces.io/last-active`, stamped every minute by the workspace itself) is older than
its timeout.

### Step 10 — The hardware lab tier

```bash
WORKSPACE_NAMESPACE=ws-demo-lab ./cli/workspace create hw --lang go --profile hardware \
    --host-path /dev --git-auth none
kubectl -n ws-demo-lab get pod -o wide          # NODE = devspaces-worker2, the lab node
WORKSPACE_NAMESPACE=ws-demo-lab ./cli/workspace ssh hw
```
Inside: `id` (root-capable), `ls /dev | head`, `cat /proc/1/status | grep CapEff` (all
capabilities: this pod is privileged), `ls /lib/modules` (the host's kernel modules,
read-only). Real hardware is not present in kind, but you can see the placement (taint +
label + one-lab-workspace-per-node anti-affinity) and the privilege boundary working.

### Step 11 — Watch the guardrails say no

Things the platform forbids are rejected by the cluster itself, not just by the portal:
```bash
# privileged in a standard namespace → Pod Security "baseline" refuses it
kubectl -n ws-demo run bad1 --image=busybox --privileged --restart=Never -- sleep 1
# host networking anywhere, even in the lab → the ValidatingAdmissionPolicy refuses it
kubectl -n ws-demo-lab run bad2 --image=busybox --restart=Never \
   --overrides='{"spec":{"hostNetwork":true,"nodeSelector":{"devspaces.io/lab":"true"},"tolerations":[{"key":"devspaces.io/lab","operator":"Equal","value":"true","effect":"NoSchedule"}]}}' -- sleep 1
# a standard-tier pod without a sandbox (no hostUsers:false, no gvisor/kata) → refused
kubectl -n ws-demo run bad3 --image=busybox --restart=Never -- sleep 1
```
Each prints an error naming the rule. Clean up any that slipped through with
`kubectl -n ws-demo delete pod bad1 bad3 --ignore-not-found`.

### Step 12 — Tear down

```bash
./deploy/demo/demo-down.sh          # deletes the kind cluster and the Compose containers
```
Images stay on your machine; `docker rmi $(docker images 'workspace/*' -q)` removes them.

---

## Level 3 (optional) — Put a real Artifactory in the loop

Artifactory OSS is free and runs as one container. The OSS edition only has *Generic*
and *Maven* repositories, which is enough to show the tool-download path (Docker, PyPI
and npm repositories need the Pro edition).

1. Start it on the same Docker network as the kind nodes:
   ```bash
   docker run -d --name artifactory --network kind -p 8082:8082 \
     releases-docker.jfrog.io/jfrog/artifactory-oss:latest
   ```
   Wait 2–3 minutes, open <http://localhost:8082>, log in (`admin` / `password`) and set a
   new password when asked.
2. *Administration → Repositories → Create a repository → Remote → Generic*: key
   `github-remote`, URL `https://github.com`. Save.
3. Find the address the cluster sees and point the demo catalog at it:
   ```bash
   docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' artifactory   # e.g. 172.18.0.6
   ```
   In `deploy/demo/catalog.demo.json` set `"url": "http://172.18.0.6:8082/artifactory"` and
   `"repos": { "github": "github-remote" }`, then `kubectl apply -k deploy/demo`.
4. Create a workspace with `--tools ripgrep`. Its boot log now shows the download URL
   under `…/artifactory/github-remote/BurntSushi/ripgrep/…`, and Artifactory's *Artifacts*
   view shows the cached file. The portal's **Tools** page shows availability dots because
   it can now ask Artifactory (restart the portal pod after changing the catalog:
   `kubectl -n devspaces-system rollout restart deploy/workspace-portal`).
5. Remove with `docker rm -f artifactory`.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Workspace pod stays `Pending`/`CreateContainerError`, events mention `hostUsers`, `user namespaces` or `MOUNT_ATTR_IDMAP` | Docker's kernel is older than 6.3, or kind < 0.32 | Upgrade Docker Desktop / kind. **Demo-only fallback**: `kubectl delete validatingadmissionpolicybinding devspaces-workspace-guardrails`, then create with `--isolation none` (CLI) — this removes the sandbox requirement, acceptable only in a throwaway cluster |
| `ErrImagePull` / `ImagePullBackOff` | image not loaded into the kind nodes | `kind load docker-image <image> --name devspaces`; verify with `docker exec devspaces-worker crictl images` |
| `workspace: error: namespace ws-kubernetes-admin does not exist` | CLI derived the namespace from your kubectl identity | `export WORKSPACE_NAMESPACE=ws-demo` (or `--namespace ws-demo`) |
| `Permission denied (publickey)` | your key is not on the workspace | portal → workspace page → *SSH keys* → add; or `./cli/workspace ssh-config go` on this laptop |
| VS Code: *Could not establish connection* after ~15 s | workspace was stopped and is starting | set `remote.SSH.connectTimeout` to 180 and retry |
| Portal shows *Please sign in* | you started the portal without `-dev-user` and without a proxy | use the demo overlay, or `go run . -mock` |
| Portal page empty / connection refused | the `port-forward` stopped | run it again (it dies when the portal pod restarts) |
| `exceeded quota` in events | the demo quota is small on purpose | `./cli/workspace stop` another workspace, or `kubectl -n ws-demo edit resourcequota workspace-quota` |
| Pods `Pending` with *Insufficient memory* | Docker has too little memory | give Docker 8 GB; use *Small* workspaces |
| `kind load` takes very long | images are 1–2 GB each | normal; load only the languages you need |
| Apple silicon: a tool fails to download | the project publishes no arm64 asset | check the URL in the boot log; most catalog tools do have arm64 builds |
| `./images/build.sh` fails in `npm` | Node is missing | harmless; the committed `tailwind.css` is used |

Where to look: `./cli/workspace status NAME`, `./cli/workspace logs NAME`,
`kubectl -n ws-demo describe pod ws-NAME-0`, `kubectl -n devspaces-system logs deploy/workspace-portal`,
`kubectl get events -A --sort-by=.lastTimestamp | tail`.

## What the demo does not show

* **Multicast** needs Multus and a multicast-capable node network; `--multicast` is accepted
  but the pod stays `Pending` because the network attachment does not exist in kind.
* **NetworkPolicy isolation** between developers is created but not *enforced*: kind's default
  network plugin (kindnet) ignores NetworkPolicy. Create the cluster with
  `disableDefaultCNI: true` and install Calico or Cilium to see it enforced.
* **gVisor / Kata** sandboxes: the RuntimeClasses exist, the runtimes do not.
* **Real sign-in**: the portal runs as a fixed user. Production uses oauth2-proxy or
  another authenticating proxy (`deploy/portal/`).
* **Artifactory for images and package managers** (Level 3 covers tool downloads only).
