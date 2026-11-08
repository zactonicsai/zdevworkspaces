# deploy/prod — the production playbook, scripted

Phased; see **docs/production-deployment.md** for the why and the step-by-step.

| Phase | Script | Layer |
|---|---|---|
| 1 cluster | `deploy/cluster/providers/{aws-eks,gcp-gke,azure-aks}.sh`, `vmware-onprem.md` + `kubeadm-node-prep.sh` | cluster (provider) |
| 2 add-ons | `deploy/cluster/addons.sh`, `deploy/cluster/multicast-cilium.sh` | cluster (agnostic) |
| gate | `deploy/cluster/check-contract.sh` — the contract between the layers | both |
| 3 platform | `10-images.sh` (build+push), `20-platform.sh` (Helm) | Kubernetes |
| 4 people | `30-onboard.sh users.csv` | Kubernetes |
| 5 verify | `40-smoke-test.sh` (also a CI gate) | Kubernetes |

Configuration lives in one file, `deploy/cluster/env.sh` (copy `env.example`), plus a
provider values file from `charts/devspaces/examples/` and a git-ignored secrets values file.
