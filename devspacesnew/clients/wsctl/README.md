# wsctl — the Go command-line client for Developer Workspaces

`wsctl` is a single small program that talks to the **portal's HTTP API**
(`api/openapi.yaml`). That is the whole trick: it never needs `kubectl`, a
kubeconfig, or cluster credentials. If your laptop can open the portal in a
browser, `wsctl` works from the same laptop — including `ssh` and VS Code,
which travel through a tunnel the portal provides.

```
you ──► wsctl ──HTTPS (bearer token)──► portal ──(impersonating you)──► Kubernetes API
                                           │
          ssh / VS Code ◄── WebSocket ◄────┘── kubectl-exec-style bridge ──► sshd in your pod
```

Three ways to use the system, same objects underneath:

| | **Portal (web)** | **`wsctl` (Go)** | **`workspace` (Python)** |
|---|---|---|---|
| Needs | a browser | the `wsctl` binary | `kubectl` + a kubeconfig with your RBAC |
| Signs in with | your company login (Entra ID …) | the same login, device-code flow (or `az`, or a pasted token) | your kubeconfig |
| Talks to | portal API | portal API | Kubernetes API directly |
| Good for | guides, first workspace, sharing, drives UI | daily use, scripts, CI, laptops without kubectl | admins, air-gapped clusters, GitOps dry-runs, onboarding |
| SSH / VS Code | shows you the commands | `wsctl ssh`, `wsctl code` (tunnel through the portal) | `workspace ssh` (through `kubectl exec`) |
| Works offline from the cluster API | — | — | `--dry-run --offline` renders YAML |

Both CLIs and the portal read the same catalog and produce the same manifests,
so a workspace created with one can be managed with the others.

## Install

```bash
# from the repo
cd clients/wsctl
make build            # → ./wsctl   (or: go build -o wsctl .)
sudo install -m 755 wsctl /usr/local/bin/

# or straight with go
go install devspaces/wsctl@latest     # when the module is published in your Artifactory Go proxy

# release binaries (linux/darwin/windows, amd64/arm64) in dist/
make release
```

There are no dependencies beyond the Go standard library, so the binary is
static and ~6 MB. A container image exists too (`make docker`), useful in CI.

## Sign in

The portal must know who you are before it does anything *as you*. Pick one:

### 1. Entra ID device-code login (normal case on Azure)

```bash
wsctl login --portal https://workspaces.example.com \
            --tenant contoso.onmicrosoft.com --client-id 2b1f…   # values your admin gives you
# To sign in, use a web browser to open https://microsoft.com/devicelogin and enter the code ABCD-1234
# Signed in to https://workspaces.example.com as alice@contoso.com (device). Config: ~/.config/wsctl/config.json
```

What happens (background): this is the OAuth 2.0 *device authorization* flow —
the same one smart TVs use. `wsctl` asks Entra for a short code, you type it
into a browser on any device and approve, and `wsctl` polls until Entra hands
it an **access token** (valid about an hour) plus a **refresh token** (used
silently to get new access tokens, so you log in rarely). The portal checks
every token's signature against Microsoft's published keys and reads your
name from it; nothing in `wsctl` is trusted on its own.

Admins: the two app registrations and the exact `--tenant/--client-id` values
come from `deploy/cluster/providers/azure/30-entra.sh` (see
`docs/azure-aks-setup.md`). The portal runs with
`-oidc-issuer https://login.microsoftonline.com/<tenant>/v2.0 -oidc-audience api://devspaces`.

### 2. Reuse the Azure CLI

```bash
az login
wsctl login --portal https://workspaces.example.com --az        # az account get-access-token --resource api://devspaces
```

### 3. Paste a token (CI, API Management test consoles)

```bash
echo "$TOKEN" | wsctl login --portal https://workspaces.example.com --token -
# or just for one call:
WSCTL_PORTAL=https://workspaces.example.com WSCTL_TOKEN=$TOKEN wsctl list
```

### 4. Demos without identity

The laptop demos (`portal -mock`, the kind cluster with `-dev-user`) have no
login at all:

```bash
wsctl login --portal http://localhost:8080 --dev
```

`wsctl logout` forgets the token. The config file is `~/.config/wsctl/config.json`
(mode 0600):

```json
{
  "portal": "https://workspaces.example.com",
  "tenant": "contoso.onmicrosoft.com",
  "clientId": "2b1f…",
  "scope": "api://devspaces/Workspaces.ReadWrite offline_access openid profile",
  "auth": "device",
  "token": "eyJ…",
  "refreshToken": "0.AX…",
  "expiresAt": "2026-11-02T18:41:00Z"
}
```

## Ten-minute tutorial

```bash
# 1. who am I, is my namespace ready?
wsctl me

# 2. what can I pick from?
wsctl catalog

# 3. create: Go is the main language (picks the image), Python is installed on top,
#    two tools, a repo cloned on first boot, the fast disk, auto-stop after 2 h idle
wsctl create api --lang go,python --tools cmake,ripgrep \
     --repo git@github.com:acme/api.git --storage fast --idle-timeout 2h
#   created api in ws-alice (image workspace-go:dev)
#   deploy key (add it to your git host with write access): ssh-ed25519 AAAA…
#     Pending — pulling image
#     Starting
#   api is Running on aks-workspaces-123
#     wsctl ssh api        # shell
#     wsctl code api       # VS Code

# 4. shell and editor — no kubectl anywhere
wsctl ssh api
wsctl code api                      # VS Code → Remote-SSH → /workspace

# 5. your files live on a *home drive* that outlives the pod
wsctl stop api                      # pod gone, drive stays
wsctl start api --wait              # same files, same shell history
wsctl delete api                    # workspace gone, drive KEPT
wsctl drives list                   # DRIVE          KIND  SIZE  STATE     LAST LANGUAGES
                                    # ws-api-home    home  50Gi  detached  go,python
wsctl create api2 --lang go --from-drive ws-api-home     # files are back, new name

# 6. snapshots: a save point before something risky
wsctl drives snapshot ws-api2-home --name before-upgrade
wsctl drives restore before-upgrade api-old --lang go   # a second workspace from the snapshot

# 7. a shared folder for the team (ReadWriteMany), mounted in several workspaces
wsctl drives create-shared team-data --size 200Gi
wsctl create web --lang node --mount shared:team-data   # appears at /shared/team-data
wsctl share add drive:team-data bob@contoso.com          # Bob mounts it with --mount shared:alice@contoso.com/team-data

# 8. pair programming: let Bob ssh into your workspace
wsctl share add api2 bob@contoso.com
#   Bob: wsctl ssh alice@contoso.com/api2

# 9. let your workspace talk to Bob's (ports 8080 only); he links back
wsctl link add api2 bob@contoso.com/api --port 8080
#   link api2 ↔ bob@contoso.com/api: pending-peer
#   ask bob@contoso.com to run:  wsctl link add api alice@contoso.com/api2
wsctl network api2                   # DNS name, pod IP, who can reach it
```

### Workspaces in a project (a shared "virtual namespace")

When a team works on one system, an admin creates a *project namespace*
(`workspace admin project create checkout --members alice,bob`). Everything
created there can talk to everything else there with no links, and every
member sees all of it:

```bash
wsctl projects
wsctl create api --lang go --project checkout
wsctl --ns ws-proj-checkout list
wsctl --ns ws-proj-checkout ssh api
```

## How `ssh` works without kubectl (the proxy)

`wsctl ssh NAME` does three small things:

1. `GET /api/workspaces/NAME` to fetch the host key, and writes a block into
   `~/.ssh/config` between `# >>> wsctl ws-NAME` / `# <<< wsctl ws-NAME`:

   ```
   Host ws-api
     HostName ws-api
     User dev
     Port 2222
     ProxyCommand /usr/local/bin/wsctl proxy api
     UserKnownHostsFile ~/.ssh/known_hosts_wsctl
     StrictHostKeyChecking yes
   ```
   plus the host-key line in `~/.ssh/known_hosts_wsctl`, so you never see a
   "trust this host?" prompt and a swapped host key is refused.

2. Runs plain `ssh ws-api`.

3. OpenSSH starts the `ProxyCommand`. `wsctl proxy` opens a WebSocket to
   `/api/workspaces/api/tunnel` with your bearer token and copies bytes both
   ways. The portal, impersonating you, bridges that socket to port 2222 inside
   your pod (the same path `kubectl exec … socat` would take). If the workspace
   is stopped, the portal starts it first and streams progress lines, which
   `wsctl proxy` prints to stderr.

```
 ssh ──stdin/stdout──► wsctl proxy ──wss://portal/api/…/tunnel──► portal ──exec──► pod:2222 (sshd)
```

Because the SSH session itself is end-to-end encrypted between `ssh` and the
pod's `sshd`, the portal only ever sees ciphertext. VS Code Remote-SSH simply
uses the same `Host ws-api` entry (`wsctl code NAME` opens it on `/workspace`).

## Command reference

| Command | What it does |
|---|---|
| `login [--tenant T --client-id ID] [--az] [--token FILE\|-] [--dev]` | sign in; remembers `--portal` |
| `logout` | forget the token |
| `me` | identity, namespaces, projects, token expiry |
| `catalog` | languages, tools, profiles, storage choices |
| `create [NAME] --lang L[,L2] [flags]` | create / re-create (home drive kept). Flags: `--tools --profile --isolation --image --tag --repo --branch --dotfiles --git-auth --git-token-stdin --git-name --git-email --size --storage --storage-class --from-drive --from-snapshot --mount --project --namespace --cpu --memory --multicast --device --host-path --cap --expose --idle-timeout --tz --key --no-wait --timeout` |
| `list [--shared]` | my workspaces (+ ones shared with me) |
| `get NAME` | details, SSH info, deploy key |
| `start NAME [--wait]` / `stop NAME` | scale 1 / 0 |
| `delete NAME [--purge] [-y]` | delete; `--purge` also deletes the home drive |
| `logs NAME [--tail N]` | boot/container log |
| `idle NAME 4h\|off` | idle timeout |
| `keys NAME [--add FILE]` | list / add SSH public keys |
| `ssh NAME [-- args]`, `code NAME [PATH]`, `ssh-config NAME [--remove]`, `proxy NAME` | connect (see above) |
| `network NAME` | DNS name, pod IP, reachable peers, links |
| `link add NAME OWNER/WS [--port N]`, `link rm`, `link list` | pod-to-pod links across developers |
| `share add NAME USER [--key FILE]`, `share add drive:DRIVE USER [--read-only]`, `share rm`, `share list` | sharing |
| `drives list\|show\|delete\|resize\|snapshot\|snapshots\|restore\|create-shared` | home and shared drives |
| `projects` | project namespaces I belong to |
| `completion bash\|zsh` | shell completion |
| `version` | version |

Global flags: `--portal URL`, `--ns NAMESPACE`, `-o table|json`, `--insecure`,
`--config PATH`. Environment: `WSCTL_PORTAL`, `WSCTL_TOKEN`, `WSCTL_CONFIG`.

Names: a plain `NAME` means "mine"; `OWNER/NAME` (e.g. `bob@contoso.com/api`)
means a workspace someone shared with me (sent to the API as `?owner=`).
`--ns` picks an explicit namespace such as a project.

Exit codes: 0 ok · 1 error · 2 usage · 4 not signed in.

## Scripting

```bash
wsctl -o json list | jq -r '.workspaces[] | select(.status=="Running") | .name'
for ws in $(wsctl -o json list | jq -r '.workspaces[].name'); do wsctl stop "$ws"; done
# CI: a throw-away workspace that runs the test suite
WSCTL_TOKEN=$(az account get-access-token --resource api://devspaces --query accessToken -o tsv)
wsctl create ci-$BUILD --lang go --repo "$REPO" --idle-timeout 30m
wsctl ssh ci-$BUILD -- 'cd /workspace/* && go test ./...'
wsctl delete ci-$BUILD --purge -y
```

## Troubleshooting

| Symptom | Meaning / fix |
|---|---|
| `not signed in … run: wsctl login` | no or expired token; device logins refresh themselves, `--az`/`--token` ones do not — log in again |
| `cannot reach the portal … connection refused` | wrong `--portal`, VPN off, or the demo portal is not running (`./deploy/demo/demo-start.sh status`) |
| `TLS problem … self-signed` | demo cluster: add `--insecure` (it is remembered); production: your CA bundle is missing on this machine |
| `your namespace ws-… does not exist yet` | you are not onboarded: an admin runs `workspace admin onboard you@example.com` |
| `ssh: Permission denied (publickey)` | the key `ssh` offers is not in the workspace: `wsctl keys NAME --add ~/.ssh/id_ed25519.pub` (or add it to your profile in the portal) |
| `wsctl proxy` prints progress for a minute | the workspace was stopped and is booting; VS Code waits too |
| `Host key verification failed` | the host key changed (workspace re-created with `--rotate-host-key`): `wsctl ssh-config NAME` rewrites the known_hosts line |
| `wsctl code`: `code` not on PATH | VS Code → Command Palette → *Shell Command: Install 'code' command in PATH*, or open Remote-SSH to `ws-NAME` by hand |
| shared drive missing in `/shared` | the workspace was created before the share; re-create it with `--mount` (the home drive is kept) |

## Development

```bash
make test        # fake portal + websocket framing + device-code + ssh-config tests
make vet fmt
```

The fake portal in `fakeportal_test.go` doubles as a readable example of the
request/response shapes `wsctl` expects.
