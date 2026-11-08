# How workspaces talk to each other

A developer often needs more than one workspace at once: an API in one, a database or a
front end in another, a teammate's service in a third. This guide explains, in plain
words, what can reach what, how to find a workspace by name, how to let two developers'
workspaces talk, when a team should get a shared "project" namespace, and when (rarely) a
service mesh is worth adding.

If you read nothing else: **your own workspaces already see each other. A team gets a
project namespace. Two developers link one workspace each. Istio is optional, for
identity and smart routing.**

---

## 1. The picture

Think of the cluster as a school building.

| School | Cluster | Can they talk? |
|---|---|---|
| Your own desk drawers | your workspaces in `ws-alice` | yes, always, any port |
| A classroom the team shares | a **project namespace** `ws-proj-checkout` | yes, everyone in the room |
| Two students in different classrooms passing notes | a **link** between `ws-alice/api` and `ws-bob/web` | only after *both* agree |
| A hall monitor checking student IDs and directing people | **Istio ambient** (optional) | adds "who exactly is this?" and "send Alice's requests over there" |
| The PA system | **multicast** (Multus or Cilium) | separate; unchanged by any of the above |

Every workspace pod has:

* a **DNS name**: `ws-<name>.<namespace>.svc.cluster.local` (a headless Service, so the name
  resolves to the pod's IP directly). Inside the same namespace `ws-<name>` is enough.
* a **pod IP** from the cluster's pod range (on AKS with Azure CNI overlay: `10.244.0.0/16`).
* the environment variables `WORKSPACE_DNS`, `WORKSPACE_NAMESPACE`, `WORKSPACE_NAME`.

Every tenant namespace has two NetworkPolicies installed by onboarding:

* `tenant-ingress`: a pod accepts connections **only from pods in the same namespace** (plus
  the per-workspace `ws-<name>-allow-ssh`, which lets the portal/CLI reach sshd on 2222).
* `tenant-egress`: a pod may talk to its own namespace, to DNS, to the API server, and to the
  internet — but **not** to other pods or Services inside the cluster (the `clusterCIDRs`
  ranges are excluded) and not to the cloud metadata address.

Those two rules are why tiers 2 and 3 below exist: they are the only ways across a namespace wall.

> **Background: why block pod-to-pod by default?** A workspace is a full Linux box where a
> developer runs anything — including, by accident, something that scans the network. Default
> deny keeps one person's experiment from touching another person's database, and keeps a
> compromised workspace from reaching the platform's own services. Opening a path is a
> one-line, visible, revocable action, not a hunt through firewall rules later.

---

## 2. Tier 1 — your own workspaces (nothing to do)

```bash
wsctl create api --lang go
wsctl create db  --lang python --tools postgres-client
wsctl ssh db
dev@ws-db-0:~$ python3 -m http.server 8000 &          # pretend this is a service
wsctl ssh api
dev@ws-api-0:~$ curl http://ws-db:8000/                # short name works inside the namespace
dev@ws-api-0:~$ curl http://ws-db.ws-alice.svc.cluster.local:8000/
dev@ws-api-0:~$ echo $WORKSPACE_DNS                    # ws-api.ws-alice.svc.cluster.local
```

`wsctl network api` (or `workspace net api`, or the **Network** tab on the workspace page in
the portal) prints the DNS name, the pod IP, the other workspaces in the namespace that can
already reach it, and any links.

Ports: anything. There is no list of "allowed ports" between your own pods. If you need a
*stable* address for something you restart often, the DNS name is stable; the pod IP is not.

---

## 3. Tier 2 — a project namespace for a team ("virtual namespace")

When three people build one system, give them one room. A **project** is a namespace
`ws-proj-<name>` that behaves exactly like a personal namespace — same quotas, same
NetworkPolicies, same guardrails — except that several developers are members.

An admin creates it (or declares it in Helm values `projects:`):

```bash
workspace admin project create checkout --members alice@corp.example,bob@corp.example
workspace admin project create checkout --members group:devspaces-checkout-team   # an IdP group
workspace admin project members checkout --add carol@corp.example
```

Members then create workspaces *in* the project:

```bash
wsctl create api --lang go --project checkout            # lands in ws-proj-checkout
wsctl create web --lang node --project checkout
wsctl list                                               # shows personal + project workspaces
```

Inside the project everything is tier 1 again: `curl http://ws-api:8080` from `ws-web`,
no links, no tickets. Everyone in the project **sees** every workspace in it in `list` and in
the portal (filter: *Project: checkout*), can read logs, and can start/stop them. SSH into a
teammate's workspace still needs that teammate to *share* it with you (`wsctl share add api
carol@corp.example`), because SSH keys are per person.

> **Grocery store:** a personal namespace is each employee's locker. A project is the shared
> stockroom: everyone on the shift walks in and out, but you still do not have the key to
> your colleague's locker unless they hand it to you.

When to prefer a project over links: more than two workspaces need each other, the set of
people is stable, or you want one quota and one bill for the team's compute.

---

## 4. Tier 3 — links between two developers' workspaces (consent on both sides)

Alice's `api` needs to call Bob's `web`, just for this week. Neither wants to move into a
project. Each of them opens their side:

```bash
# Alice, in her namespace: allow traffic between MY api and Bob's web
wsctl link add api bob@corp.example/web
#   → NetworkPolicy peer-api-ws-bob-web in ws-alice: ingress from + egress to that one pod
#   → status: pending-peer   (Bob has not linked back yet)

# Bob, in his namespace
wsctl link add web alice@corp.example/api
#   → status: linked  (on both sides now)

# Either of them
wsctl link list api
wsctl network api          # shows peerDNS: ws-web.ws-bob.svc.cluster.local
```

Then, from Alice's pod: `curl http://ws-web.ws-bob.svc.cluster.local:3000/`. Use the **full**
DNS name across namespaces; the short name only works inside one namespace.

Optional port restriction: `wsctl link add api bob@corp.example/web --ports 3000,9229` allows
only those TCP ports (both directions still need both sides).

Removing: `wsctl link rm api bob@corp.example/web` — and the traffic stops immediately, even if
Bob keeps his half (a half-link is just an unanswered invitation).

What the link is made of (so you can audit it with `kubectl get networkpolicy -n ws-alice`):

```yaml
kind: NetworkPolicy
metadata: { name: peer-api-ws-bob-web, namespace: ws-alice,
            labels: { devspaces.io/workspace: api, devspaces.io/link: "true" },
            annotations: { devspaces.io/peer: "bob@corp.example/web" } }
spec:
  podSelector: { matchLabels: { devspaces.io/workspace: api } }
  policyTypes: [Ingress, Egress]
  ingress:
    - from:
        - namespaceSelector: { matchLabels: { devspaces.io/tenant: bob } }
          podSelector: { matchLabels: { devspaces.io/workspace: web } }
  egress:
    - to:
        - namespaceSelector: { matchLabels: { devspaces.io/tenant: bob } }
          podSelector: { matchLabels: { devspaces.io/workspace: web } }
```

Developers may create and delete `peer-*` policies in their own namespace (the
`workspace-developer` ClusterRole allows it); the admission policy forbids touching the
`tenant-*` ones. The portal shows links under **Network** with a *Link back* button when a
peer is waiting on you.

---

## 5. Testing connectivity (copy-paste)

```bash
# in the "server" workspace
python3 -m http.server 8000                 # HTTP on 8000
nc -l -p 9000                                # raw TCP on 9000 (netcat-openbsd: nc -l 9000)

# in the "client" workspace
getent hosts ws-db.ws-alice.svc.cluster.local      # does the name resolve?
curl -m 3 -sS http://ws-db.ws-alice.svc.cluster.local:8000/ | head -3
nc -vz -w 3 ws-db.ws-alice.svc.cluster.local 9000  # port open?
# "Connection timed out" = a NetworkPolicy is in the way (no link / wrong namespace).
# "Connection refused" = policy is fine, nothing is listening on that port.
# "Name or service not known" = typo, wrong namespace, or the workspace is stopped (no pod → no IP).
```

`wsctl network NAME` runs the first two checks for you from outside and tells you which
tier applies.

---

## 6. Multicast is a different thing

Multicast (`--multicast`, groups like `239.255.0.0/24`) is for one-to-many UDP: robotics
buses, market-data feeds, DDS. It travels on a **second interface** (`net1`, Multus/macvlan on
real L2 networks) or through **Cilium's eBPF multicast** (VXLAN overlay, any cloud). It is not
subject to the tiers above (NetworkPolicy does not shape it in Multus mode) and a service mesh
never carries it. Azure VNets do not forward multicast; on AKS only the Cilium (BYO) mode works
— see `docs/azure-aks-setup.md` Phase 9 and `docs/production-deployment.md` Part F.

---

## 7. When to add a service mesh (Istio ambient), and how

Plain NetworkPolicy answers "*may this IP talk to that IP on this port?*". Sometimes a team
needs answers to different questions:

| You want… | NetworkPolicy | Istio ambient |
|---|---|---|
| Alice's pods ↔ Bob's pod, port 3000 | ✅ `wsctl link add` | ✅ |
| Proof of *who* is calling (cryptographic identity, mTLS), not just an IP | ❌ | ✅ SPIFFE identity per namespace, mTLS by ztunnel |
| "Only `GET /api/*` is allowed from Bob" | ❌ (L4 only) | ✅ `AuthorizationPolicy` + waypoint |
| "Send requests with header `x-dev: alice` to *Alice's* copy of the service, everything else to the team's" | ❌ | ✅ `HTTPRoute` on the shared Service |
| Mirror 10 % of team traffic into my workspace to debug | ❌ | ✅ `HTTPRoute` RequestMirror |
| Keep multicast working | ✅ | ✅ (mesh ignores it) |

**Recommendation:** start without a mesh. Most developer-to-developer needs are tier 1–3.
Add Istio **ambient mode** (no sidecars; one `ztunnel` per node, so workspace pods are not
modified and image/boot behaviour stays identical) when a team asks for one of the three
rows marked ❌ above. Do not use sidecar mode: it injects a container into every workspace,
breaks `hostUsers: false` expectations, and fights the hardware-lab profile.

How it is switched on (both layers, loosely coupled):

1. **Cluster layer** installs Istio ambient + Gateway API CRDs (`istioctl install --set
   profile=ambient`; on AKS: Istio add-on in ambient mode or `istio-cni` chained on top of
   Cilium — Cilium keeps enforcing the NetworkPolicies, Istio adds identity on top).
2. **Kubernetes layer** sets catalog `networking.mesh: istio-ambient` (Helm
   `catalogOverrides.networking.mesh`). From then on, onboarding labels tenant and project
   namespaces `istio.io/dataplane-mode=ambient`; existing pods are enrolled live, no restart.
3. Per project, apply what you need from `deploy/platform/mesh/`:
   * `authorizationpolicy-workspace.yaml` — identity-based twin of a link
     (`principals: ["cluster.local/ns/ws-bob/sa/default"]`); and the root policy that keeps
     the portal tunnel and reaper able to reach sshd on 2222.
   * `waypoint.yaml` — one waypoint per project namespace for anything HTTP-aware.
   * `httproute-dev-routing.yaml` — header-based routing to a developer's workspace, and
     mirroring. This is the "test my branch against the real stack" pattern.

Both NetworkPolicy *and* Istio policy apply at once; a connection must pass both. Keep the
NetworkPolicy tiers as the base layer so that removing the mesh later removes features, not
safety.

> **Background: ambient vs sidecar.** Classic Istio put a proxy container ("sidecar") inside
> every pod. Ambient moves the per-pod proxy to one per node (`ztunnel`, L4: identity, mTLS,
> simple allow/deny) and an optional per-namespace `waypoint` (L7: HTTP routing, headers,
> retries). Workspaces stay single-container, which matters for a platform where the pod *is*
> the developer's computer.

---

## 8. Security notes

* Links and projects open **pod-to-pod** paths only; the platform namespace
  (`devspaces-system`), `kube-system` and node/metadata addresses stay unreachable from workspaces.
* A link never grants SSH or `kubectl` rights; sharing a workspace (`wsctl share add`) does,
  and only for that one pod.
* Lab (privileged) workspaces live in `ws-<user>-lab` / `ws-proj-<name>-lab`; linking a lab
  workspace to a standard one is allowed but think twice — the lab pod has host-level power.
* Everything is plain Kubernetes objects with `devspaces.io/*` labels; audit with
  `kubectl get networkpolicy,rolebinding -A -l devspaces.io/link=peer` and the portal's Events.
* In ambient mode, identities are per **namespace** (service account `default`), i.e. per
  developer or per project — that is the right granularity for a dev platform; do not create
  per-workspace service accounts unless you need per-workspace policies.

---

## 9. "I want X → do Y"

| I want to… | Do this |
|---|---|
| my API workspace to call my DB workspace | nothing: `curl http://ws-db:PORT` |
| a stable name for a workspace | use `ws-<name>.<ns>.svc.cluster.local`; it never changes |
| my whole team's workspaces to see each other | admin: `workspace admin project create NAME --members …`; devs: `--project NAME` |
| to call one teammate's workspace this week | both: `wsctl link add <mine> <them>/<theirs>` |
| to restrict that to a port | `--ports 5432` |
| to let a teammate `ssh` into my workspace | `wsctl share add <ws> <them>` (keys + RBAC), not a link |
| to mount the same folder in several workspaces | a **shared drive**: `wsctl drives create-shared data --size 200Gi`, then `--mount shared:data` (see architecture doc §1–2) |
| proof of identity / mTLS between dev pods | Istio ambient, `authorizationpolicy-workspace.yaml` |
| header-based routing to *my* copy of a service | Istio ambient + waypoint, `httproute-dev-routing.yaml` |
| one-to-many UDP (robots, market data) | `--multicast` (Multus on-prem, Cilium BYO in clouds) |
| to see why a connection times out | `wsctl network NAME`; `kubectl get netpol -n ws-<me>`; check both halves of a link exist |

---

## 10. FAQ

**Can I reach a workspace's web app from my laptop?** Not through links — that is for pods.
Use `wsctl port-forward NAME 8080:8080` (or VS Code's port forwarding) which rides the SSH tunnel.

**Why does the short name `ws-web` not work across namespaces?** Kubernetes DNS search
paths only cover your own namespace. Use `ws-web.ws-bob.svc.cluster.local`.

**My link says `pending-peer` forever.** The other developer has not run their half. The
portal shows them a *Link back* button; a link is an invitation until accepted.

**Does stopping a workspace break links?** The policies stay; the pod (and its IP) is gone
until it starts again, so connections fail with "name not known". Start it, and everything
resumes — the DNS name is the same.

**Can a project span the standard and lab tiers?** Yes: `--lab` on `project create` makes
`ws-proj-<name>-lab` too; the two namespaces are *not* automatically linked (different
security tiers), link specific workspaces if you must.

**Do I need Istio for multicast?** No, and it would not help; see §6.

**What about Windows / non-Kubernetes peers (a robot on the lab network)?** That is the
hardware profile's job: `--host-path`, device plugins and Multus put the lab workspace on the
physical segment. Links only connect pods.

## Sidecar mode (AKS Istio add-on)

The add-on installs Istio under a revision (`asm-1-25`). Set `networking.mesh: istio-sidecar` and `networking.istioRevision: asm-1-25` (the `existing/` scripts detect this) and onboarding labels tenant namespaces `istio.io/rev=asm-1-25`; workspace pods get an `istio-proxy` and mTLS between them. Because developer namespaces enforce Pod Security *baseline*, sidecar mode needs `istio-cni` (the add-on ships it). Hardware/kernel profiles are labelled `sidecar.istio.io/inject=false`. Ambient remains the simpler choice when available.
