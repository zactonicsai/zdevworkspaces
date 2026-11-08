# Developer Pods

Start, stop and inspect per-developer pods (Python or Java) on an existing Kubernetes cluster from a
one-page portal. A small Python API does the work by running `kubectl` and `helm`, and logs every
command it runs. Developer pods use **pre-built images** with tools baked in.

```
dev-pods/
├── web/index.html              the portal (static; talks to /api)
├── api/app.py                  FastAPI: wraps kubectl + helm, full audit log
├── api/catalog.example.json    runtime catalog format (the chart generates this from values)
├── images/
│   ├── base/Dockerfile         Ubuntu 24.04 + git, ssh, vim, jq, ripgrep, build tools, `dev` user (uid 1000)
│   ├── python/Dockerfile       base + Python 3.x, uv, poetry, pytest, ruff, black, mypy, ipython
│   ├── java/Dockerfile         base + Temurin JDK, Maven, Gradle
│   ├── api/Dockerfile          the API image (python + kubectl + helm + web/ + helm/dev-pod)
│   ├── common/entrypoint.sh    shared entrypoint baked into every dev image
│   └── build.sh                builds & pushes all of the above
├── helm/
│   ├── dev-pods/               deploys the PORTAL + API: Deployment, Service, Ingress, SA, Role, RoleBinding, catalog ConfigMap
│   └── dev-pod/                one developer workspace: StatefulSet + headless Service + PVC (used BY the API)
└── scripts/
    ├── deploy.sh               deploy onto the current kubectl context (build/push or load images, helm upgrade, smoke)
    ├── port-forward.sh         open the portal without an ingress
    ├── smoke-test.sh           create → run → exec → stop/start → delete a pod through the API, print the audit trail
    ├── create-pod.sh           start a developer pod from the CLI (through the API, so it is logged)
    └── undeploy.sh             remove the portal/API (optionally all ws-* pods and their volumes)
```

## Quick start on the current cluster context

```bash
kubectl config current-context                 # scripts deploy to whatever this says

# A) local cluster (kind / minikube / k3d / docker-desktop): build images and load them, no registry
scripts/deploy.sh --build --load
scripts/port-forward.sh                        # http://localhost:8080/

# B) real cluster with a registry + ingress
REGISTRY=registry.example.com/dev HOST=devpods.example.com TLS_SECRET=devpods-tls \
  scripts/deploy.sh --build --push             # add REGISTRY_USER/REGISTRY_PASS to auto-create a pull secret

# C) images already pushed; just (re)install or upgrade the chart
REGISTRY=registry.example.com/dev HOST=devpods.example.com scripts/deploy.sh

# D) try the portal with no real pods (API in dry-run: commands are logged, not executed)
scripts/deploy.sh --dry-run-api --build --load

scripts/smoke-test.sh                          # proves pods start: creates, execs, deletes one via the API
scripts/create-pod.sh alice-svc java21 large alice https://github.com/acme/svc.git
scripts/undeploy.sh [--all|--purge]
```

`deploy.sh` shows the current context and asks for confirmation (`--yes` to skip), creates the namespace,
optionally creates the pull secret, runs `helm lint`, then `helm upgrade --install dev-pods helm/dev-pods
-n dev-workspaces --wait`, waits for the rollout and hits `/api/health`. Everything it does is printed as
the command it runs. Env knobs: `NAMESPACE RELEASE REGISTRY API_TAG HOST INGRESS_CLASS TLS_SECRET
STORAGE_CLASS PULL_SECRET VALUES_FILE`.

## Deploy by hand (what the scripts do)

### 1. Build and push the images

```bash
export REGISTRY=registry.example.com/dev        # any registry your cluster can pull from
./images/build.sh --push
```

Produces `dev-pod-base:24.04`, `dev-pod-python:3.12`, `dev-pod-python:3.11`, `dev-pod-java:21`,
`dev-pod-java:17` and `dev-pods-api:0.1.0`. Add tools by editing the Dockerfiles; add a runtime by
adding a tag to `build.sh` and an entry to the catalog (step 2).

If the registry is private, create a pull secret in the namespace:
`kubectl -n dev-workspaces create secret docker-registry regcred --docker-server=… --docker-username=… --docker-password=…`

### 2. Write your values

`my-values.yaml`:

```yaml
image:
  repository: registry.example.com/dev/dev-pods-api
  tag: 0.1.0
imagePullSecrets: [{ name: regcred }]          # if private

ingress:
  className: nginx                              # or your ingress class
  host: devpods.example.com
  tls: true
  tlsSecret: devpods-tls                        # cert-manager or an existing cert
  annotations:                                  # PUT AUTH HERE — /exec runs commands in developer pods
    nginx.ingress.kubernetes.io/auth-url: "https://oauth2-proxy.example.com/oauth2/auth"
    nginx.ingress.kubernetes.io/auth-signin: "https://oauth2-proxy.example.com/oauth2/start?rd=$scheme://$host$request_uri"

catalog:
  registry: registry.example.com/dev
  imagePullSecret: regcred                      # developer pods pull with it too
  storageClass: ""                              # "" = cluster default; e.g. gp3, standard, longhorn
  sshd: false
  # runtimes / sizes: defaults are the 4 images above and small/medium/large — override to trim or extend
```

### 3. Install

```bash
helm upgrade --install dev-pods helm/dev-pods -n dev-workspaces --create-namespace -f my-values.yaml
kubectl -n dev-workspaces rollout status deploy/dev-pods
```

Open `https://devpods.example.com/` (portal) and `/api/docs` (Swagger). Without an ingress:
`kubectl -n dev-workspaces port-forward svc/dev-pods 8080:80` → http://localhost:8080/.

What the chart creates in `dev-workspaces`: a **Deployment** (`dev-pods`: the portal at `/` and the API at `/api`, 1 replica, non-root,
probes on `/api/health`), a **Service** (ClusterIP :80), an **Ingress**, a **ServiceAccount** with a
namespace-scoped **Role/RoleBinding** (statefulsets, pods, pods/log, pods/exec, events, PVCs, services,
configmaps, secrets — secrets because Helm stores release state there), and a **ConfigMap** with the
runtime catalog mounted at `/etc/dev-pods/catalog.json`. Developer pods are created in the *same*
namespace as StatefulSets `ws-<name>` by the API running `helm upgrade --install` with the bundled
`helm/dev-pod` chart. No cluster-wide permissions are needed.

The portal page ships in the image. To serve a customised copy without rebuilding:
`helm upgrade … --set-file portal.customHtml=web/index.html` (mounted over `/app/web/index.html`).

### 4. Upgrade / change the catalog

Edit `my-values.yaml` and run the same `helm upgrade --install` — the Deployment carries a checksum of
the catalog ConfigMap, so a catalog change rolls the API pod.

```bash
helm -n dev-workspaces uninstall dev-pods            # removes the API only; ws-* releases stay
```

### Alternative: run the API outside the cluster

```bash
cd api && pip install -r requirements.txt
WS_NAMESPACE=dev-workspaces WS_CATALOG=catalog.json uvicorn app:app --port 8080   # uses your kubeconfig
WS_DRY_RUN=1 uvicorn app:app --port 8080                                         # no cluster; logs commands only
```

## API → commands

| Endpoint                              | Runs                                                                       |
|---------------------------------------|----------------------------------------------------------------------------|
| `POST /api/pods`                      | `helm upgrade --install ws-NAME /app/helm/dev-pod -n NS --set image=REGISTRY/dev-pod-java:21 --set resources.cpu=… …` |
| `GET /api/pods`                       | `kubectl get statefulsets -l app.kubernetes.io/part-of=dev-workspaces -o json` + same for `pods` |
| `POST /api/pods/NAME/stop`            | `kubectl scale statefulset ws-NAME --replicas=0` (PVC kept)               |
| `POST /api/pods/NAME/start`           | `kubectl scale statefulset ws-NAME --replicas=1`                           |
| `DELETE /api/pods/NAME?purge=true`    | `helm uninstall ws-NAME` (+ `kubectl delete pvc -l workspace/name=NAME`)   |
| `GET /api/pods/NAME/logs`             | `kubectl logs ws-NAME-0 -c dev --tail=200`                                 |
| `GET /api/pods/NAME/events`           | `kubectl get events --field-selector involvedObject.name=ws-NAME-0`        |
| `POST /api/pods/NAME/exec {command}`  | `kubectl exec ws-NAME-0 -c dev -- sh -lc COMMAND`                          |
| `GET /api/catalog`                    | the mounted catalog (runtimes, sizes, registry)                            |
| `GET /api/audit?tool=&workspace=`     | the audit buffer                                                           |

## Logging

Every kubectl/helm call goes through one function, `run()` (argv list, never a shell), which records
`id, ts, request_id, workspace, action, tool, argv, cmd (shell-quoted), cwd, exit_code, duration_ms, stdout, stderr` to:

1. **stdout** as JSON lines → `kubectl -n dev-workspaces logs deploy/dev-pods -f | jq .` (ship to Loki/ELK/Splunk as usual)
2. **`/app/api/logs/ws-api.jsonl`** (rotating; set `logs.persistence.enabled=true` to keep it on a PVC)
3. **`GET /api/audit`** — last 1000 calls, shown on the portal's *API log* page and per pod.

HTTP requests are logged too, with an `X-Request-ID` returned in the response so a click can be joined
to the commands it caused.

## Developer images

All images share `images/base` (Ubuntu 24.04, `dev` user uid 1000 with passwordless sudo, git, ssh,
vim, jq, ripgrep, fd, tmux, build-essential, tini) and `images/common/entrypoint.sh`, which seeds
`/home/dev` on first boot, optionally clones `WORKSPACE_REPO`, optionally starts sshd
(`catalog.sshd: true`, keys in `~/.ssh/authorized_keys`), prints tool versions, then idles.
`/home/dev` is a PVC and survives stop/start; everything else is rebuilt from the image.

## Security notes

* Names, runtimes and sizes are validated against a pattern / the catalog before reaching `--set`.
* The API's Role is namespace-scoped; it cannot see other namespaces.
* `/exec` runs arbitrary commands inside developer pods — authenticate the Ingress (oauth2-proxy, SSO,
  API gateway) before exposing it, and consider a NetworkPolicy around `dev-workspaces`.
* Developer pods run as uid 1000 with `sudo` inside the container (convenience for one-off `apt`);
  remove the sudoers line in `images/base/Dockerfile` if that's not acceptable.
