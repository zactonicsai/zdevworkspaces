# deploy/platform/mesh — OPTIONAL Istio ambient examples

**You do not need a service mesh to let workspaces talk to each other.** The platform's
NetworkPolicies + the headless Service per workspace already give you three tiers
(same namespace → free; project namespace → free; links between developers → consent on both
sides). Read `docs/workspace-networking.md` first.

Add Istio **ambient mode** (no sidecars; a per-node ztunnel) only when you need one of:

| Need | Why plain NetworkPolicy cannot do it | Example file |
|---|---|---|
| Cryptographic identity + mTLS between pods | NetworkPolicy trusts IPs; mTLS proves *which* workspace is calling | `istio-ambient-namespace-label.yaml` |
| L7 rules ("alice's workspace may only GET /api/…") | NetworkPolicy stops at L4 (ports) | `authorizationpolicy-workspace.yaml`, `waypoint.yaml` |
| Route by header to *a developer's* copy of a service ("test my branch against the team stack") | that is traffic steering, not filtering | `httproute-dev-routing.yaml`, `waypoint.yaml` |
| Traffic mirroring, retries, fault injection for a dev stack | same | `httproute-dev-routing.yaml` |

How it is switched on (catalog `networking.mesh: istio-ambient`, Helm `catalogOverrides.networking.mesh`):

1. Install Istio ambient on the cluster layer (`istioctl install --set profile=ambient` or the Helm
   charts `istio-base`, `istiod`, `istio-cni`, `ztunnel`) **and** the Gateway API CRDs. On AKS with
   Azure CNI powered by Cilium use the Istio-managed add-on in ambient mode or `istio-cni` with
   `cni.chained=true`; Cilium keeps enforcing NetworkPolicy underneath.
2. With `mesh: istio-ambient`, onboarding (`workspace admin onboard`, `admin project create`) labels
   tenant/project namespaces `istio.io/dataplane-mode=ambient`. Pods get mTLS and SPIFFE identities
   (`spiffe://<trust-domain>/ns/ws-alice/sa/default`) without restarts or sidecars.
3. Apply the examples below per project as needed. They are *examples*: change names and hosts.

Files:

* `istio-ambient-namespace-label.yaml` — what the label on a namespace looks like (and how to opt one pod out).
* `authorizationpolicy-workspace.yaml` — L4 policy enforced by ztunnel: only the same namespace and
  the linked peer may open connections to `ws-api`; everything else is denied even if a NetworkPolicy
  would allow it.
* `waypoint.yaml` — a waypoint proxy for the project namespace (needed for any L7 feature).
* `httproute-dev-routing.yaml` — the team's `api.checkout.svc` normally goes to the shared service;
  requests carrying `x-dev: alice` go to Alice's workspace pod instead.

Multicast is **not** carried by the mesh; `--multicast` workspaces keep using Multus or Cilium
multicast (ambient leaves UDP multicast alone).
