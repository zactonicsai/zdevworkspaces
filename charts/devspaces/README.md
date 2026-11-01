# charts/devspaces

Helm chart for the **Kubernetes layer**: catalog, RBAC, idle reaper, admission guardrails,
multicast network attachment (or Cilium mode), runtime classes, device-plugin config, the
web portal, oauth2-proxy and the Ingress. It assumes a cluster that satisfies
`deploy/cluster/CONTRACT.md` — nothing in here knows which cloud it runs on.

```bash
helm upgrade --install devspaces charts/devspaces -n devspaces-system --create-namespace \
  -f charts/devspaces/examples/values-aws.yaml -f my-secrets.yaml
helm template devspaces charts/devspaces -n devspaces-system -f … | kubeconform -strict   # dry run
```

Most templates are generated from `deploy/platform/*` (the kustomize path used by the demo)
with configuration points substituted; when you change one, change the other. The catalog
is shipped as `catalog.json` (a copy of `deploy/platform/catalog.json`) and deep-merged with
`.Values.catalogOverrides`, with `global.registry/tag/pullSecretName` and `multicast.*`
applied on top so the chart's images and the catalog can never disagree.

`examples/` holds starting values for AWS EKS, GCP GKE, Azure AKS and on-prem/VMware.
