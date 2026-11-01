# deploy/demo — run everything locally on Docker

Step-by-step instructions: **[docs/demo-docker.md](../../docs/demo-docker.md)**.

| File | Used by |
|---|---|
| `docker-compose.yaml` | Level 1: portal in mock mode + one workspace container (`ssh -p 2222 dev@localhost`) |
| `make-ssh-keys.sh` | Level 1: creates `ssh/` (host key + your authorized key) for the container |
| `kind-config.yaml` | Level 2: 3-node kind cluster, Kubernetes 1.36.4, one node tainted as the hardware lab pool |
| `kustomization.yaml` + `catalog.demo.json` | Level 2: platform + portal adapted to kind (local images, demo catalog, no Multus, portal in dev-user mode) |
| `demo-up.sh` / `demo-down.sh` | Level 2 in one command, and clean removal |

Everything here is for a throwaway cluster on your own machine; the dev-user portal mode
and the relaxed overlay must never be applied to a shared cluster.
