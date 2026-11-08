# Tutorial: put Developer Workspaces on your AKS cluster that already has Istio

*A step-by-step guide. Short steps, plain words, and after every step you check that it worked
before you move on. If you can copy a command into a terminal, you can do this.*

---

## What you are going to build

Think of your Kubernetes cluster as a big office building that your company already owns.
Istio is the building's **front door and hallways**: it already exists, it already has a
security guard (the *ingress gateway*) and a public street address (an IP).

You are going to:

1. **Rent a floor** in the building for the platform (a namespace called `devspaces-system`).
2. **Install the furniture** with Helm: the web portal, the catalog, the rules.
3. **Put a sign on the front door** so that when someone types `workspaces.acme-dev.com`
   the guard sends them to your portal. In Istio that sign is called a **VirtualService**.
   You do *not* build a new door — you only add a sign to the door that is already there.
4. **Hand out rooms** to developers (one namespace each) and **spin up dev pods** —
   little Linux computers with Go or Python or Node already installed.
5. Optionally give a developer's web app its own sign too (`web-alice.apps.acme-dev.com`).

Here is the picture:

```
   developer's laptop                     your AKS cluster
   ─────────────────                      ─────────────────────────────────────────────
   browser ──► workspaces.acme-dev.com ──► Istio ingress gateway ──► VirtualService "devspaces"
                                           (already there)            │
                                                                      ▼
                                                               portal (namespace devspaces-system)
                                                                      │ "alice wants a Go workspace"
                                                                      ▼
                                                               namespace ws-alice:  pod ws-api  (Go)
                                                                                    pod ws-web  (Node)
                                                                                    home drive (disk)
   VS Code / ssh ──► wsctl tunnel ───────► (same door) ─────────────► inside the pod
```

Every step below has three parts: **Do this**, **You should see**, **If not**.

---

## Chapter 0 — Words you will meet

| Word | What it means here |
|---|---|
| **AKS** | Azure Kubernetes Service — Microsoft runs the Kubernetes control room for you. |
| **namespace** | A folder inside the cluster. Things in one folder cannot see into another unless allowed. |
| **pod** | One running container (or a few). A *dev pod* is a pod with a developer's tools. |
| **Helm** | An installer. A *chart* is an install package; *values* are the settings you pick. |
| **Istio** | A service mesh: it controls traffic between pods and from the outside in. |
| **ingress gateway** | Istio's front door: a pod with a public IP. Already running on your cluster. |
| **Gateway** (capital G) | An Istio object that says "on that door, listen for *this* host name". |
| **VirtualService** | An Istio object that says "requests for *this* host name go to *that* Service". |
| **Service** | A stable name and port in front of one or more pods. |
| **PVC / home drive** | A disk that survives when the pod is deleted. A developer's `/home`. |
| **OIDC / Entra ID / Keycloak** | Ways to *sign in*. Entra ID is Microsoft's; Keycloak is open source; OIDC is the standard both speak. |

---

## Chapter 1 — What you need before you start

**Do this.** Open a terminal and check each tool. (On Windows use WSL or Git Bash.)

```bash
az version          # Azure CLI       → "azure-cli": "2.6x" or newer
kubectl version --client   # Kubernetes CLI  → Client Version: v1.3x
helm version        # Helm 3          → version.BuildInfo{Version:"v3.1x"
python3 --version   # Python 3.9+     → used by the onboarding CLI
git --version
```

Missing one? Install guides: `https://learn.microsoft.com/cli/azure/install-azure-cli`,
`https://kubernetes.io/docs/tasks/tools/`, `https://helm.sh/docs/intro/install/`.

**Do this.** Log in to Azure and get the code.

```bash
az login                                   # a browser window opens; pick your work account
az account show --query name -o tsv        # prints the subscription name you are in
git clone <your copy of this repository> devspaces
cd devspaces
```

**You should see** the subscription name printed, and a folder `devspaces` with `charts/`,
`deploy/`, `docs/` inside.

**Do this.** Make sure the cluster exists and has Istio. Replace the two names with yours.

```bash
az aks show -g acme-rg -n acme-aks --query "serviceMeshProfile.mode" -o tsv
```

**You should see** `Istio`. That is the AKS Istio add-on, which is what this tutorial expects.

**If not** — it printed nothing or `Disabled`:

* *You have Istio but installed it yourself* (namespace `istio-system`): that is fine, the
  scripts detect it too. Skip ahead.
* *You have no Istio at all*: turn the add-on on (about 5 minutes) and give it a public door:
  ```bash
  az aks mesh enable -g acme-rg -n acme-aks
  az aks mesh enable-ingress-gateway -g acme-rg -n acme-aks --ingress-gateway-type external
  ```
* *You have no cluster at all*: `docs/azure-aks-setup.md` builds one with
  `deploy/cluster/providers/azure/10-aks.sh`, then come back here.

> **Example.** Pat works at Acme. Her cluster is `acme-aks` in resource group `acme-rg`,
> Istio add-on is on, and her team owns the DNS zone `acme-dev.com`. We will follow Pat.

---

## Chapter 2 — Fill in the form (`env.sh`)

Everything the scripts need is in one file. You fill it in once.

**Do this.**

```bash
cd deploy/cluster/providers/azure/existing
cp env.example env.sh
nano env.sh          # or code env.sh, vim env.sh …
```

Change only these lines for the first run (Pat's values shown):

```bash
RESOURCE_GROUP=acme-rg
CLUSTER_NAME=acme-aks
REGISTRY=acmeacr.azurecr.io/devspaces   # where your workspace images are (see the box below)
TAG=dev
AUTH=dev                                # start simple: no sign-in yet, only you can reach it
DEV_USER=pat@acme.com                   # you
PORTAL_HOST=workspaces.acme-dev.com     # the name people will type (DNS comes later)
ALLOW_CIDRS="203.0.113.7/32"            # YOUR public IP — run:  curl -s https://ifconfig.me
DEVELOPERS="pat@acme.com"
SAMPLE_WORKSPACES="go:go web:node"      # two dev pods to look at
```

> **Images box.** The dev pods and the portal are container images. Build and push them once:
> ```bash
> REGISTRY=acmeacr.azurecr.io/devspaces TAG=dev ./images/build.sh --push     # from the repo root
> az aks update -g acme-rg -n acme-aks --attach-acr acmeacr                       # lets the cluster pull them
> ```
> If your company already has them in Artifactory, point `REGISTRY` there and set
> `PULL_SECRET_DOCKERCONFIG` to the base64 of your `~/.docker/config.json`.

**Why `AUTH=dev` and `ALLOW_CIDRS`?** `dev` means the portal has *no login page* — it believes
everyone is `DEV_USER`. That is perfect to try things out, and dangerous to show the internet.
So the scripts refuse to put a `dev` portal on the front door unless you give an IP allow-list
(`ALLOW_CIDRS`). Later (Chapter 8) you switch to a real sign-in and remove the list.

---

## Chapter 3 — Look around (nothing changes yet)

**Do this.**

```bash
./00-detect.sh
```

**You should see** something like:

```
==> Connecting to acme-aks (resource group acme-rg)
  [ok]   context acme-aks → https://acme-aks-xxxx.hcp.eastus2.azmk8s.io:443 (Kubernetes v1.31.x)
==> Istio
  [ok]   AKS Istio add-on (namespace aks-istio-system), revision asm-1-25
  [ok]   ingress gateway: aks-istio-ingress (istio: aks-istio-ingressgateway-external) → 20.51.123.45
  [ok]   mesh mode for tenant namespaces: istio-sidecar (istio.io/rev=asm-1-25)
==> Storage
  [ok]   fast home drives → managed-csi-premium
  [ok]   standard home drives → managed-csi
  [ok]   shared drives (RWX) → azurefile-csi-premium
  [ok]   snapshots: VolumeSnapshotClass csi-azuredisk-vsc
==> Writing detected.env
```

Write down the gateway IP (`20.51.123.45` for Pat). That is the street address of the door.

**If not**

* `ingress gateway: none` → run the `az aks mesh enable-ingress-gateway …` command from Chapter 1
  and wait 2 minutes.
* The IP is `<pending>` → Azure is still creating the load balancer; wait and run again.
* `[fail] no permission` → you need at least *Azure Kubernetes Service RBAC Cluster Admin*
  on the cluster (ask whoever owns it).

---

## Chapter 4 — Rent the floor (namespaces and storage)

**Do this.**

```bash
./10-namespaces.sh
```

**You should see** `devspaces-system ready (mesh: istio-sidecar)` and two StorageClasses
`workspace-home-premium` / `workspace-home-standard`.

**What just happened.** A namespace was created and given the label `istio.io/rev=asm-1-25`.
That label is how you tell Istio "pods on this floor are part of the mesh". The storage
classes say "home drives are kept (`Retain`) even if someone deletes the claim".

Check for yourself:

```bash
kubectl get ns devspaces-system --show-labels
kubectl get storageclass | grep workspace-home
```

---

## Chapter 5 — Install the furniture (Helm)

**Do this.**

```bash
./20-helm.sh
```

This writes `values-existing.yaml` (open it — it is the full list of settings Helm got) and runs
one command, which you could also type by hand:

```bash
helm upgrade --install devspaces ../../../../../charts/devspaces \
  -n devspaces-system -f values-existing.yaml --wait --timeout 10m
```

**You should see**

```
==> helm upgrade --install devspaces (…/charts/devspaces)
Release "devspaces" has been installed. Happy Helming!
  [ok]   portal running: Running
  kubectl --context acme-aks -n devspaces-system port-forward svc/workspace-portal 8080:80 → http://localhost:8080
```

**Try it right now, before any door sign.** Port-forward is a private tunnel from your laptop
straight to the portal; it needs no DNS and no Istio:

```bash
kubectl -n devspaces-system port-forward svc/workspace-portal 8080:80
```

Open `http://localhost:8080`. You are signed in as `pat@acme.com` (that is `AUTH=dev`). Press
`Ctrl-C` to stop the tunnel when you are done.

**If not**

* `ImagePullBackOff` in `kubectl -n devspaces-system get pods` → the cluster cannot pull your
  images: check `REGISTRY`/`TAG` and `--attach-acr` from Chapter 2.
* Pod stuck in `Init` with `istio-validation` → the Istio CNI is not ready; `kubectl -n
  aks-istio-system get pods` should all be Running.
* `--wait` timed out → `kubectl -n devspaces-system describe pod -l app.kubernetes.io/name=workspace-portal`
  and read the Events at the bottom; they say exactly what is wrong.

---

## Chapter 6 — Put the sign on the door (the VirtualService)

Istio is already set up. You are only adding the route. Here is the **whole** thing the script
is about to create — read it once, it is shorter than it looks:

```yaml
# 1) The Gateway: "on the gateway pods that already exist, listen for this host name"
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: devspaces
  namespace: devspaces-system
spec:
  selector:
    istio: aks-istio-ingressgateway-external     # ← picks the door that already exists
  servers:
    - port: { number: 80, name: http, protocol: HTTP }
      hosts: ["workspaces.acme-dev.com", "*.apps.workspaces.acme-dev.com"]
---
# 2) The VirtualService: "requests for this host name go to this Service"
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: devspaces
  namespace: devspaces-system
spec:
  hosts: ["workspaces.acme-dev.com"]
  gateways: ["devspaces-system/devspaces"]       # ← the Gateway above
  http:
    - name: tunnel                               # the SSH-over-WebSocket path: keep it open for hours
      match: [{ uri: { regex: "^/api/workspaces/[^/]+/tunnel$" } }]
      timeout: 0s
      retries: { attempts: 0 }
      route:
        - destination: { host: workspace-portal.devspaces-system.svc.cluster.local, port: { number: 80 } }
    - name: portal                               # everything else
      timeout: 120s
      route:
        - destination: { host: workspace-portal.devspaces-system.svc.cluster.local, port: { number: 80 } }
---
# 3) The door lock (only because AUTH=dev): anyone NOT in ALLOW_CIDRS gets "403" for this host
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: devspaces-allow-cidrs
  namespace: aks-istio-ingress
spec:
  selector: { matchLabels: { istio: aks-istio-ingressgateway-external } }
  action: DENY
  rules:
    - to: [{ operation: { hosts: ["workspaces.acme-dev.com", "workspaces.acme-dev.com:*"] } }]
      from: [{ source: { notRemoteIpBlocks: ["203.0.113.7/32"] } }]
```

Line by line, in plain words:

* `selector: istio: aks-istio-ingressgateway-external` — *which door*. The AKS add-on labels its
  external gateway pods like this; `00-detect.sh` found it for you.
* `hosts:` — *which names the door answers to*. The second one, `*.apps…`, is for developers'
  apps in Chapter 9.
* `gateways: ["devspaces-system/devspaces"]` — *this sign belongs to that door*.
* `match … /tunnel$` with `timeout: 0s` — the SSH tunnel is one long WebSocket; without this,
  Istio would cut it after 15 seconds and VS Code would keep disconnecting.
* `destination.host` — the portal's Service, written with its full in-cluster name.
* `notRemoteIpBlocks` — *everyone except these addresses is denied*. Delete this object when
  you turn on real sign-in.

**Do this.**

```bash
./60-virtualservice.sh
```

**You should see**

```
==> Portal route on the existing Istio gateway (aks-istio-ingress, istio: aks-istio-ingressgateway-external) for http://workspaces.acme-dev.com
  [ok]   wrote …/route-portal.yaml
  [ok]   applied: Gateway, VirtualService, AuthorizationPolicy for 203.0.113.7/32
  [warn] AUTH=dev: everyone who can reach workspaces.acme-dev.com is 'pat@acme.com'. …
  [ok]   gateway Service aks-istio-ingressgateway-external keeps client IPs (externalTrafficPolicy=Local)
==> Checking the route
  [ok]   http://20.51.123.45/healthz with Host: workspaces.acme-dev.com → 200 — the gateway reaches the portal
```

**Test before DNS exists.** You can pretend to be DNS with the `Host:` header:

```bash
curl -s -H 'Host: workspaces.acme-dev.com' http://20.51.123.45/healthz     # → ok
```

**Now create the DNS record** (in your DNS provider, or Azure DNS):

```bash
az network dns record-set a add-record -g dns-rg -z acme-dev.com -n workspaces -a 20.51.123.45
az network dns record-set a add-record -g dns-rg -z acme-dev.com -n '*.apps.workspaces' -a 20.51.123.45
```

Wait a minute, then open **http://workspaces.acme-dev.com** in your browser. That is the portal,
through the front door, with no port-forward.

**If not**

| You see | It means | Do |
|---|---|---|
| `404` from curl | the gateway got it but no sign matched | `kubectl -n devspaces-system get gateway,virtualservice`; check the host name has no typo |
| `403` | you are not in `ALLOW_CIDRS` | `curl -s https://ifconfig.me` and fix `env.sh`, run `./60-virtualservice.sh` again |
| `403` from *every* IP, even yours | the gateway Service hides client IPs | the script prints the `kubectl patch … externalTrafficPolicy Local` command; run it |
| no answer at all | port 80 blocked | Azure NSG on the node subnet; or the gateway Service is `internal` — use the external one |
| `[fail] AUTH=dev + gateway = …` | you forgot `ALLOW_CIDRS` | it is a feature, not a bug — set it |

**HTTPS.** Put a certificate Secret in the gateway namespace (`aks-istio-ingress`) — from
cert-manager or Key Vault — and set `TLS_CREDENTIAL_NAME=workspaces-tls` in `env.sh`. Run
`./60-virtualservice.sh` again: it adds the 443 listener and an HTTP→HTTPS redirect. The
certificate should cover both `workspaces.acme-dev.com` and `*.apps.workspaces.acme-dev.com`.

---

## Chapter 7 — Hand out rooms and spin up dev pods

**Do this.**

```bash
./40-dev-pods.sh
```

**You should see**

```
==> Onboarding developers: pat@acme.com
  [ok]   pat@acme.com → ws-pat (mesh label from catalog: istio-sidecar)
==> Project namespace ws-proj-team-demo (shared by all developers)
==> Sample dev pods for pat@acme.com (namespace ws-pat)
  [ok]   created go (go) — boots in about a minute
  [ok]   created web (node) — boots in about a minute
```

**What just happened.** For each developer the CLI made a namespace `ws-<name>` with a CPU/memory
quota, a RoleBinding (the developer may touch only this namespace), NetworkPolicies (pods here
talk only to each other, DNS and the internet), and the Istio label. Then it created two
workspaces: each is a **StatefulSet** with one pod and one **home drive** (PVC).

Watch them come up:

```bash
kubectl -n ws-pat get pods -w
# NAME       READY   STATUS     RESTARTS   AGE
# ws-go-0    0/2     Init:0/1   0          10s     ← 2 containers: the workspace + istio-proxy
# ws-go-0    2/2     Running    0          58s
# ws-web-0   2/2     Running    0          61s
kubectl -n ws-pat get pvc
# ws-go-home    Bound   …   32Gi   workspace-home-premium
```

`2/2` is the proof Istio is in the picture: the second container is the sidecar.

**Now do it the way a developer would — from the portal.** Open the portal, click **Create**,
choose *Python*, name it `api`, press Create. Watch the boot log in the page. When it says
Running, click **Connect** → copy the `ssh ws-api` line or the VS Code button.

**Or from the command line** with `wsctl` (the Go CLI, no kubectl needed):

```bash
wsctl login --portal http://workspaces.acme-dev.com --dev      # --dev because AUTH=dev
wsctl create api --lang python --tools ripgrep,git
wsctl ls
wsctl ssh api                                                   # you are inside the pod
```

**Check the home drive really survives.** Inside the pod: `echo hello > ~/keep.txt`, exit, then
`wsctl delete api` (the drive is kept unless you say `--purge`), then
`wsctl create api --lang python --from-drive api` and `wsctl ssh api -- cat ~/keep.txt` → `hello`.

**If not**

* Pod `Pending` for minutes → `kubectl -n ws-pat describe pod ws-go-0`: usually *no nodes with
  enough CPU* (autoscaler is adding one — wait) or *PVC pending* (storage class name wrong).
* `1/2 Running` forever → the sidecar is up but the workspace container is not: `kubectl -n ws-pat
  logs ws-go-0 -c workspace`.
* `ssh-keygen not found` → install OpenSSH client; the CLI needs it to make your key.

---

## Chapter 8 — Make sure everything is right

**Do this.**

```bash
./50-verify.sh
```

**You should see** all `[ok]`: portal health, catalog, *home drives protected by admission
policy* (it tries to delete a drive the wrong way and expects the cluster to say no), mesh
enrolment, and at least one dev pod Ready.

And the mesh view:

```bash
./30-istio-check.sh
istioctl proxy-status 2>/dev/null | grep ws-            # if you have istioctl: every pod SYNCED
```

---

## Chapter 9 — Give a developer's app its own sign

Pat is building a website inside the `web` workspace. It listens on port 3000. She wants to
show it to a teammate without that teammate SSH-ing in.

**Do this** (inside the pod, the app must listen on all interfaces, not just localhost):

```bash
wsctl ssh web
npm create vite@latest demo -- --template react && cd demo && npm install
npm run dev -- --host 0.0.0.0 --port 3000
```

In another terminal, on your laptop:

```bash
./70-expose-workspace.sh pat@acme.com web 3000
```

**You should see**

```
==> Exposing pat@acme.com / workspace 'web' port 3000 as http://web-pat.apps.workspaces.acme-dev.com
  [ok]   wrote …/route-pat-web-3000.yaml
  service/ws-web-web-3000 created
  networkpolicy.networking.k8s.io/allow-gateway-web-3000 created
  virtualservice.networking.istio.io/ws-web-3000 created
  Open:   http://web-pat.apps.workspaces.acme-dev.com
```

Open that address. It is Pat's Vite dev server, live-reload and all, through the same front door.

**What the three objects do** (open `route-pat-web-3000.yaml`):

1. `Service ws-web-web-3000` — a name for port 3000 of the pod labelled `devspaces.io/workspace: web`.
2. `NetworkPolicy allow-gateway-web-3000` — developer namespaces only accept traffic from
   themselves; this opens *one port* to *one namespace* (the gateway's).
3. `VirtualService ws-web-3000` — `web-pat.apps.workspaces.acme-dev.com` → that Service. It uses
   the Gateway from Chapter 6, which is why the Gateway listed `*.apps.…`.

Remove it later with `./70-expose-workspace.sh pat@acme.com web 3000 --remove`. If `ALLOW_CIDRS`
is set, the app gets the same IP lock as the portal.

---

## Chapter 10 — Turn on real sign-in (do this before inviting the team)

Right now everyone who reaches the portal *is Pat*. Pick one of two sign-ins. Both end with the
same two commands.

### Option A — Entra ID (Microsoft accounts your company already has)

```bash
# 1) three app registrations (portal, api, cli) — needs Application Administrator in the tenant
../30-entra.sh
# 2) copy the four values it prints into env.sh:
AUTH=entra
ENTRA_TENANT_ID=…  ENTRA_PORTAL_CLIENT_ID=…  ENTRA_PORTAL_CLIENT_SECRET=…  ENTRA_CLI_CLIENT_ID=…
ALLOW_CIDRS=""                     # the lock is no longer needed: there is a login now
```

### Option B — Keycloak (or Okta, Auth0 … anything OpenID Connect)

In Keycloak make a realm with two **public** clients: `devspaces-portal` (PKCE, redirect URI
`https://workspaces.acme-dev.com/*`) and `devspaces-cli` (device authorization grant on), and a
client scope that adds the audience `devspaces-api` to tokens. `deploy/demo/keycloak/realm-devspaces.json`
is a complete example realm you can import and rename.

```bash
AUTH=oidc
OIDC_ISSUER=https://keycloak.acme.com/realms/devspaces
OIDC_PORTAL_CLIENT_ID=devspaces-portal
OIDC_CLI_CLIENT_ID=devspaces-cli
OIDC_AUDIENCE=devspaces-api
ALLOW_CIDRS=""
```

### Then, for either option

```bash
./20-helm.sh            # re-installs the portal with sign-in; Helm now owns the Gateway + VirtualService
./60-virtualservice.sh  # sees that, removes nothing, re-tests the route
kubectl -n aks-istio-ingress delete authorizationpolicy devspaces-allow-cidrs   # drop the IP lock
```

Open the portal: you get a login page. `wsctl login --portal https://workspaces.acme-dev.com`
prints a code and a URL (device login) — no `--dev` any more.

**What changed under the hood.** With a login, the Helm chart itself creates the Gateway and
VirtualService (the same ones you read in Chapter 6), plus two Istio security objects:
`RequestAuthentication` (the sidecar checks every token's signature against the provider's keys)
and `AuthorizationPolicy` (`/api/*` needs a valid token; the UI files are public so the browser
can start the login). The portal checks the token *again* — belt and braces.

**Onboard the team**: set `DEVELOPERS="alice@acme.com bob@acme.com"` and run `./40-dev-pods.sh`.
Each person gets their room; each sees only their own workspaces (and ones shared with them).

---

## Chapter 11 — Every day

| I want to… | Portal | `wsctl` |
|---|---|---|
| new dev pod | Create → pick language → Create | `wsctl create api --lang go` |
| open it in VS Code | Connect → VS Code button | `wsctl code api` |
| a shell | Connect → copy `ssh` line | `wsctl ssh api` |
| stop it for the night (drive stays) | Stop | `wsctl stop api` |
| delete it but keep my files | Delete → *keep drive* | `wsctl delete api` |
| new pod on an old drive | Create → *Start from drive* | `wsctl create api2 --lang go --from-drive api` |
| let Bob into my pod | Sharing → add bob | `wsctl share add api bob@acme.com` |
| my pod talks to Bob's pod | Network → Link (both sides) | `wsctl link add api bob@acme.com/web --ports 8080` |
| show my app to someone | — | `./70-expose-workspace.sh me web 3000` |
| see what is running | My workspaces | `wsctl ls` |

Idle pods are stopped automatically after `WORKSPACE_IDLE` (2 h by default); the drive stays.

---

## Chapter 12 — Clean up

```bash
./70-expose-workspace.sh pat@acme.com web 3000 --remove        # app routes you added
kubectl delete -f route-portal.yaml                            # the door sign (AUTH=dev route)
helm -n devspaces-system uninstall devspaces                   # the furniture
kubectl delete ns ws-pat                                       # a developer's room (home drives are Retain: PVs stay)
kubectl delete ns devspaces-system                             # the floor
```

Home drives are `Retain`: after deleting a namespace the PersistentVolumes and the Azure disks
still exist (`kubectl get pv | grep Released`); delete them when you are sure.

---

## Appendix A — The same thing with plain Helm, no scripts

If you prefer to type everything yourself, this is the complete, minimal path for an existing
AKS + Istio add-on cluster. Replace the four UPPERCASE values.

```bash
az aks get-credentials -g acme-rg -n acme-aks
kubectl create ns devspaces-system
kubectl label ns devspaces-system istio.io/rev=asm-1-25          # the revision from: kubectl get ns aks-istio-system -o yaml | grep rev
kubectl apply -f deploy/cluster/providers/azure/20-storage.yaml   # Retain storage classes + snapshot class

cat > values.yaml <<'EOF'
global: { registry: REGISTRY, tag: "TAG" }
storage:
  choices:
    - { name: fast, label: Fast SSD, kind: home, storageClass: workspace-home-premium, accessModes: [ReadWriteOnce], default: true }
    - { name: shared, label: Shared folder, kind: shared, storageClass: azurefile-csi-premium, accessModes: [ReadWriteMany] }
  snapshotClass: csi-azuredisk-vsc
networking: { mesh: istio-sidecar, istioRevision: asm-1-25, links: true }
multicast: { mode: none }
portal:
  oidc:   { issuer: https://login.microsoftonline.com/TENANT_ID/v2.0, audience: api://devspaces }
  cliLogin: { tenant: TENANT_ID, clientId: CLI_CLIENT_ID }
oauth2Proxy:
  enabled: true
  provider: entra-id
  issuerUrl: https://login.microsoftonline.com/TENANT_ID/v2.0
  clientId: PORTAL_CLIENT_ID
  clientSecret: PORTAL_CLIENT_SECRET
  cookieSecret: "RANDOM_32_BYTES_BASE64"
ingress:
  enabled: true
  host: workspaces.acme-dev.com
  istio:
    enabled: true
    gatewaySelector: { istio: aks-istio-ingressgateway-external }
    extraHosts: ["*.apps.workspaces.acme-dev.com"]
    credentialName: ""              # name of a TLS Secret in aks-istio-ingress for HTTPS
istioAuth:
  enabled: true
  strictMTLS: true
  jwt:
    enabled: true
    issuer: https://login.microsoftonline.com/TENANT_ID/v2.0
    jwksUri: https://login.microsoftonline.com/TENANT_ID/discovery/v2.0/keys
    audiences: ["api://devspaces"]
EOF

helm upgrade --install devspaces charts/devspaces -n devspaces-system -f values.yaml --wait
kubectl -n devspaces-system get gateway,virtualservice            # created by the chart
python3 cli/workspace admin onboard alice@acme.com                  # one room
python3 cli/workspace -n ws-alice create api --lang go -y           # one dev pod
```

For Keycloak instead of Entra, drop the `oauth2Proxy` block and use
`charts/devspaces/examples/values-keycloak.yaml` as the model for `portal.oidc`, `portal.ui.oidc`
and `istioAuth.jwt`.

## Appendix B — Only the VirtualService, by hand

Your Gateway already exists (say `istio-system/company-gateway` listening on `*.acme-dev.com`)
and you only want the route:

```yaml
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata: { name: devspaces, namespace: devspaces-system }
spec:
  hosts: ["workspaces.acme-dev.com"]
  gateways: ["istio-system/company-gateway"]
  http:
    - match: [{ uri: { regex: "^/api/workspaces/[^/]+/tunnel$" } }]
      timeout: 0s
      retries: { attempts: 0 }
      route: [{ destination: { host: workspace-portal.devspaces-system.svc.cluster.local, port: { number: 80 } } }]
    - route: [{ destination: { host: workspace-portal.devspaces-system.svc.cluster.local, port: { number: 80 } } }]
      timeout: 120s
```

`kubectl apply -f` it, or set `EXISTING_GATEWAY=istio-system/company-gateway` in `env.sh` and let
`./60-virtualservice.sh` write and apply exactly this (with `oauth2-proxy` as the destination
when `AUTH=entra`). If the Helm release has `oauth2Proxy.enabled`, point the route at
`oauth2-proxy.devspaces-system.svc.cluster.local` instead.

## Appendix C — Where things are

| | |
|---|---|
| the scripts in this tutorial | `deploy/cluster/providers/azure/existing/` (`README.md` there has the per-script table) |
| the Helm chart and its settings | `charts/devspaces/values.yaml` (every key commented), `charts/devspaces/examples/` |
| the Istio objects the chart creates | `charts/devspaces/templates/istio.yaml` |
| the CLI that onboards and creates pods | `cli/workspace` (Python) and `clients/wsctl` (Go) |
| how the mesh, links and project namespaces work | `docs/workspace-networking.md` |
| creating the AKS cluster itself | `docs/azure-aks-setup.md` |
| the bigger picture | `docs/architecture.md` |
