# deploy/cluster/providers/azure — the cluster layer on Azure (AKS)

Step-by-step explanation: **[docs/azure-aks-setup.md](../../../../docs/azure-aks-setup.md)**.
These scripts are the cluster layer (Phase 1–2 of `docs/production-deployment.md`) for Azure.
They are numbered in the order to run them, every one is idempotent ("already exists → skip"),
and every one accepts `--dry-run` (prints the `az` commands instead of running them).

```bash
cp deploy/cluster/providers/azure/env.example deploy/cluster/providers/azure/env.sh   # fill in
./deploy/cluster/providers/azure/00-prereqs.sh
./deploy/cluster/providers/azure/10-aks.sh
./deploy/cluster/providers/azure/20-storage.sh
./deploy/cluster/providers/azure/30-entra.sh
./deploy/cluster/providers/azure/40-keyvault.sh
./deploy/cluster/addons.sh                      # cloud-agnostic add-ons (cert-manager, metrics-server…)
./deploy/cluster/check-contract.sh              # the gate between the layers
# … Kubernetes layer: deploy/prod/20-platform.sh with charts/devspaces/examples/values-azure.yaml …
./deploy/cluster/providers/azure/50-apim.sh     # optional: Azure API Management in front of /api
./deploy/cluster/providers/azure/60-multicast-cilium.sh   # optional: eBPF multicast
```

| Script | Creates | Contract items |
|---|---|---|
| `env.example` → `env.sh` | nothing — all the names and sizes in one file | — |
| `00-prereqs.sh` | nothing — checks `az`/`kubectl`/`helm`, login, resource providers, vCPU quota, name availability | — |
| `10-aks.sh` | resource group, VNet + subnets, AKS (Azure CNI overlay **powered by Cilium**, OIDC issuer, workload identity, Entra ID + Azure RBAC), node pools `system`, `workspaces` (autoscaled), `lab` (tainted, optional GPU, optional `spot`), ACR attached, kubeconfig | C1 C2 C3 C7 C8 C9 |
| `20-storage.yaml` / `20-storage.sh` | StorageClasses `workspace-home-premium` (default), `workspace-home-standard`, `workspace-shared-files` (RWX), VolumeSnapshotClass `csi-azuredisk-vsc`; verifies the snapshot CRDs/controller | C4 C14 C15 C16 |
| `30-entra.sh` | Entra app registrations `devspaces-portal` (web, group claims), `devspaces-api` (exposes `api://devspaces/Workspaces.ReadWrite`), `devspaces-cli` (public client, device code); writes `entra.values.yaml` snippet | C7 |
| `40-keyvault.sh` | Key Vault, secrets for oauth2-proxy, user-assigned identity + federated credential (workload identity), `SecretProviderClass` for the CSI secrets store | (supports C6/C7: TLS + OIDC secrets) |
| `50-apim.bicep` / `50-apim.sh` / `apim-policy.xml` | API Management instance, "Workspaces API" imported from `api/openapi.yaml`, inbound policy `validate-azure-ad-token` + rate limit | (above the contract: an Azure front door for the API) |
| `60-multicast-cilium.sh` | enables Cilium eBPF multicast and programs the groups; refuses on managed Azure-CNI-Cilium where the knob is not exposed | C10 |

Ingress (C5) and TLS (C6) come from `deploy/cluster/addons.sh` (NGINX + cert-manager) or
from the AKS **Application Gateway for Containers** add-on enabled in `10-aks.sh`
(`INGRESS=appgw-for-containers`). Delete everything with `10-aks.sh --delete` (asks first).

Nothing in `charts/devspaces` knows it is on Azure: the only Azure-specific words live in
`charts/devspaces/examples/values-azure.yaml` (StorageClass names, Entra issuer, ingress class).
