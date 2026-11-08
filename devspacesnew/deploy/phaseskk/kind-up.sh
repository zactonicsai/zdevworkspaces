#!/usr/bin/env bash
# Pre-step — a throwaway Kubernetes cluster on your laptop with kind (Kubernetes in Docker),
# ready for ./all.sh. Not a phase: phases need a cluster; this makes one.
#
# WHAT   1. kind cluster "devspaces": control-plane + worker, host ports 80/443 mapped so an
#           Ingress works at http://localhost  (kind-config generated next to this script)
#        2. ingress-nginx (the kind flavour)                             — skip with NO_INGRESS=1
#        3. snapshot CRDs + controller, so "drives snapshot" works       — skip with NO_SNAPSHOTS=1
#        4. builds the workspace images and loads them into the nodes   — skip with NO_IMAGES=1
#           (kind cannot pull from a registry you don't have; LANGS="go" keeps it to one language)
#        5. writes env.sh for the phases if it does not exist yet (REGISTRY=workspace TAG=dev,
#           AUTH=dev, EXPOSE=ingress, PORTAL_HOST=localhost, ALLOW_CIDRS=everything-local)
# WHY    Same scripts, same order, same checks as on a real cluster — on a laptop in ~10 min.
# NEEDS  docker (running), kind, kubectl, helm. 8 GB RAM free is comfortable.
#   ./kind-up.sh [--dry-run]         ./kind-up.sh --delete       # removes the cluster (images stay in Docker)
set -Eeuo pipefail
cd "$(dirname "$0")"; REPO_ROOT=$(cd ../.. && pwd)
CLUSTER=${CLUSTER:-devspaces}; LANGS=${LANGS:-go node}
DRY_RUN=0; for a in "$@"; do [ "$a" = --dry-run ] && DRY_RUN=1; done
step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }; ok() { printf '  \033[32m[ok]\033[0m   %s\n' "$*"; }
skip() { printf '  \033[2m[skip]\033[0m %s\n' "$*"; }; fail() { printf '  \033[31m[fail]\033[0m %s\n' "$*"; exit 1; }
run() { if [ "$DRY_RUN" = 1 ]; then printf '  \033[36m$\033[0m %s\n' "$*"; else "$@"; fi; }
have() { command -v "$1" >/dev/null 2>&1; }

if [ "${1:-}" = --delete ]; then step "Deleting kind cluster $CLUSTER"; kind delete cluster --name "$CLUSTER"; rm -f kind-config.yaml; ok "gone (env.sh, detected.env and Docker images kept)"; exit 0; fi

step "Tools"
for t in docker kind kubectl helm; do have "$t" && ok "$t" || { [ "$DRY_RUN" = 1 ] && skip "$t missing (dry-run)" || fail "$t not found — kind: https://kind.sigs.k8s.io/docs/user/quick-start/#installation"; }; done
[ "$DRY_RUN" = 1 ] || docker info >/dev/null 2>&1 || fail "Docker is not running"

step "Cluster '$CLUSTER'"
cat > kind-config.yaml <<'EOF'
# written by deploy/phases/kind-up.sh — one control plane + one worker; ports 80/443 of the
# worker are published on localhost so an Ingress answers at http://localhost
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
    labels: { ingress-ready: "true" }          # ingress-nginx (kind flavour) schedules here
    extraPortMappings:
      - { containerPort: 80,  hostPort: 80,  protocol: TCP }
      - { containerPort: 443, hostPort: 443, protocol: TCP }
EOF
if [ "$DRY_RUN" != 1 ] && kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then skip "exists"
else run kind create cluster --name "$CLUSTER" --config kind-config.yaml --wait 120s; ok "created"; fi
run kubectl config use-context "kind-$CLUSTER" >/dev/null || true
# every node is a workspace node; the lab pool is not part of this trial cluster
[ "$DRY_RUN" = 1 ] || kubectl label nodes -l '!node-role.kubernetes.io/control-plane' devspaces.io/pool=standard --overwrite >/dev/null

if [ "${NO_INGRESS:-0}" != 1 ]; then
  step "ingress-nginx (so EXPOSE=ingress works at http://localhost)"
  if [ "$DRY_RUN" != 1 ] && kubectl get ns ingress-nginx >/dev/null 2>&1; then skip "installed"; else
    run kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml >/dev/null
    [ "$DRY_RUN" = 1 ] || kubectl -n ingress-nginx wait --for=condition=Ready pod -l app.kubernetes.io/component=controller --timeout=180s >/dev/null
    ok "ingress-nginx ready; IngressClass nginx"
  fi
fi

if [ "${NO_SNAPSHOTS:-0}" != 1 ]; then
  step "VolumeSnapshot CRDs + controller (drives snapshot)"
  if [ "$DRY_RUN" != 1 ] && kubectl get crd volumesnapshotclasses.snapshot.storage.k8s.io >/dev/null 2>&1; then skip "installed"; else
    SNAP_VER=${SNAP_VER:-v8.2.0}
    for f in client/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml client/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml client/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml; do
      run kubectl apply -f "https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/$SNAP_VER/$f" >/dev/null
    done
    ok "snapshot controller $SNAP_VER (kind's local-path provisioner cannot snapshot; a CSI driver can — the phases detect which)"
  fi
fi

if [ "${NO_IMAGES:-0}" != 1 ]; then
  step "Workspace images → kind nodes (LANGS=\"$LANGS\"; first build 10-20 min)"
  imgs="workspace/workspace-cli:dev workspace/workspace-portal:dev"; for l in $LANGS; do imgs="$imgs workspace/workspace-$l:dev"; done
  missing=0; for i in $imgs; do [ "$DRY_RUN" = 1 ] || docker image inspect "$i" >/dev/null 2>&1 || missing=1; done
  if [ "$missing" = 1 ] || [ "$DRY_RUN" = 1 ]; then ( cd "$REPO_ROOT/images" && run env REGISTRY=workspace TAG=dev LANGS="$LANGS" ./build.sh ); else skip "images already built (delete them to rebuild)"; fi
  run kind load docker-image $imgs --name "$CLUSTER"
  ok "loaded: $imgs"
fi

step "env.sh for the phases"
if [ -f env.sh ]; then skip "env.sh exists — check REGISTRY=workspace TAG=dev EXPOSE=ingress PORTAL_HOST=localhost"; else
  python3 - "$CLUSTER" "$([ "${NO_INGRESS:-0}" = 1 ] && echo port-forward || echo ingress)" "$(set -- $LANGS; echo "$1")" <<'PY'
import re, sys
cluster, expose, lang = sys.argv[1:]
s = open("env.example").read()
def put(key, val, note=""):
    global s
    s = re.sub(r'(?m)^%s=.*$' % key, '%s=%s%s' % (key, val, ("   # " + note) if note else ""), s, count=1)
put("KUBE_CONTEXT", '"kind-%s"' % cluster)
put("REGISTRY", "workspace", "images loaded by kind-up.sh")
put("TAG", "dev")
put("EXPOSE", expose, "ingress-nginx on http://localhost" if expose == "ingress" else "")
put("PORTAL_HOST", "localhost")
put("ALLOW_CIDRS", '"0.0.0.0/0"', "kind only listens on this machine")
put("SAMPLE_WORKSPACES", '"demo:%s"' % lang)
open("env.sh", "w").write(s)
PY
  ok "env.sh written (AUTH=dev, DEV_USER=you@example.com — change DEV_USER if you like)"
fi
cat <<EOT

  Cluster:  kind-$CLUSTER   (kubectl get nodes)
  Note:     kind's default network (kindnet) does not enforce NetworkPolicy, so tenant isolation is
            not real here. For that, create the cluster with Calico/Cilium (kind: disableDefaultCNI).
  Next:     ./all.sh           then open http://localhost   (AUTH=dev: you are signed in as DEV_USER)
  Remove:   ./90-uninstall.sh ; ./kind-up.sh --delete
EOT
