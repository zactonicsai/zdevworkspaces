#!/usr/bin/env bash
# demo-start.sh — start (or check, or stop) the portal and the other demo services,
# skipping anything that is already running. Safe to run as often as you like.
#
#   ./deploy/demo/demo-start.sh              # start what is missing, print the URLs
#   ./deploy/demo/demo-start.sh status       # just report, change nothing
#   ./deploy/demo/demo-start.sh stop         # stop the services (volumes and the cluster stay)
#   ./deploy/demo/demo-start.sh sim          # open the zero-install static simulation page
#
# Which "level" it manages (see docs/demo-docker.md):
#   kubernetes  a kind cluster named $CLUSTER exists  → platform, portal, port-forward, demo user
#   compose     otherwise, if the images are built     → docker compose: portal (-mock) + one workspace
#   sim         otherwise                               → tells you to open portal/web/simulation.html
# Force one with:  MODE=kubernetes|compose|sim ./deploy/demo/demo-start.sh
# Other knobs: CLUSTER (kind cluster name), PORT (portal port), DEMO_USER, SSH_PUBKEY (compose level)
#
# Works with bash 3.2 (macOS) and needs only the tools of the level you use.
set -Eeuo pipefail
cd "$(dirname "$0")/../.."                     # repo root
CLUSTER=${CLUSTER:-devspaces}
PORT=${PORT:-8080}
DEMO_USER=${DEMO_USER:-demo@example.com}
MODE=${MODE:-auto}
ACTION=${1:-start}
STATE_DIR=${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}/devspaces-demo
PF_PID="$STATE_DIR/port-forward.pid"
PF_LOG="$STATE_DIR/port-forward.log"
mkdir -p "$STATE_DIR"

ok()   { printf '  \033[32m[ok]\033[0m   %s\n' "$*"; }
run()  { printf '  \033[34m[do]\033[0m   %s\n' "$*"; }
skip() { printf '  \033[2m[skip]\033[0m %s\n' "$*"; }
bad()  { printf '  \033[31m[fail]\033[0m %s\n' "$*"; }
head_() { printf '\n\033[1m%s\033[0m\n' "$*"; }
have() { command -v "$1" >/dev/null 2>&1; }
portal_up() { curl -fsS -m 2 "http://localhost:$PORT/healthz" >/dev/null 2>&1; }
open_url() {   # best effort; never fails the script
  case "$(uname -s)" in Darwin) open "$1" 2>/dev/null && return 0;; esac
  have xdg-open && xdg-open "$1" >/dev/null 2>&1 && return 0
  echo "  → open this in your browser:  $1"; return 0
}

# ------------------------------------------------------------------ pick the mode
detect_mode() {
  if [ "$MODE" != auto ]; then echo "$MODE"; return; fi
  if have kind && have kubectl && kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then echo kubernetes; return; fi
  if have docker && docker info >/dev/null 2>&1 && docker image inspect workspace/workspace-portal:dev >/dev/null 2>&1; then echo compose; return; fi
  echo sim
}

# ------------------------------------------------------------------ kubernetes level
k8s_status() {
  head_ "Kubernetes demo (kind cluster '$CLUSTER')"
  if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then bad "cluster '$CLUSTER' does not exist — run ./deploy/demo/demo-up.sh first"; return 1; fi
  ok "cluster '$CLUSTER' exists"
  kubectl config use-context "kind-$CLUSTER" >/dev/null 2>&1 || { bad "kubectl context kind-$CLUSTER missing"; return 1; }
  local notready; notready=$(kubectl get nodes --no-headers 2>/dev/null | awk '$2!="Ready"{c++} END{print c+0}')
  [ "$notready" = 0 ] && ok "all nodes Ready" || bad "$notready node(s) not Ready"
  if kubectl get ns devspaces-system >/dev/null 2>&1; then ok "platform namespace devspaces-system"; else bad "platform not installed"; fi
  if [ "$(kubectl -n devspaces-system get deploy workspace-portal -o jsonpath='{.status.availableReplicas}' 2>/dev/null)" = 1 ]; then ok "portal deployment available"; else bad "portal deployment not available"; fi
  if kubectl get ns "ws-$(user_slug)" >/dev/null 2>&1; then ok "demo user onboarded (ws-$(user_slug))"; else bad "demo user not onboarded"; fi
  if portal_up; then ok "portal answering on http://localhost:$PORT"; else bad "nothing answers on http://localhost:$PORT"; fi
  if [ -f "$PF_PID" ] && kill -0 "$(cat "$PF_PID")" 2>/dev/null; then ok "port-forward running (pid $(cat "$PF_PID"))"; else skip "no port-forward started by this script"; fi
  local n; n=$(kubectl get pods -A -l app.kubernetes.io/name=workspace --no-headers 2>/dev/null | wc -l | tr -d ' ')
  ok "$n workspace pod(s) across all namespaces"
}
user_slug() { printf '%s' "$DEMO_USER" | cut -d@ -f1 | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9-]/-/g'; }

k8s_start() {
  head_ "Kubernetes demo (kind cluster '$CLUSTER')"
  if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
    bad "cluster '$CLUSTER' does not exist. First-time setup is:  ./deploy/demo/demo-up.sh"; return 1
  fi
  ok "cluster exists"
  kubectl config use-context "kind-$CLUSTER" >/dev/null
  if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^$CLUSTER-control-plane$"; then
    run "cluster containers are stopped (Docker was restarted?) — starting them"
    docker start $(docker ps -a --format '{{.Names}}' | grep "^$CLUSTER-") >/dev/null
    kubectl wait --for=condition=Ready node --all --timeout=180s >/dev/null
  fi
  ok "nodes Ready"

  if kubectl get ns devspaces-system >/dev/null 2>&1 && kubectl -n devspaces-system get deploy workspace-portal >/dev/null 2>&1; then
    skip "platform + portal already installed (re-apply with: kubectl apply -k deploy/demo)"
  else
    run "installing platform + portal:  kubectl apply -k deploy/demo"
    kubectl apply -k deploy/demo >/dev/null
  fi
  if [ "$(kubectl -n devspaces-system get deploy workspace-portal -o jsonpath='{.status.availableReplicas}' 2>/dev/null)" = 1 ]; then
    skip "portal deployment already available"
  else
    run "waiting for the portal deployment"
    kubectl -n devspaces-system rollout status deploy/workspace-portal --timeout=180s
  fi

  if kubectl get ns "ws-$(user_slug)" >/dev/null 2>&1; then
    skip "$DEMO_USER already onboarded"
  else
    run "onboarding $DEMO_USER"
    python3 cli/workspace admin onboard "$DEMO_USER" --lab
  fi

  if portal_up; then
    skip "something already answers on http://localhost:$PORT (port-forward or another portal) — leaving it"
  else
    if [ -f "$PF_PID" ] && kill -0 "$(cat "$PF_PID")" 2>/dev/null; then kill "$(cat "$PF_PID")" 2>/dev/null || true; fi
    run "starting port-forward svc/workspace-portal → localhost:$PORT (log: $PF_LOG)"
    nohup kubectl -n devspaces-system port-forward svc/workspace-portal "$PORT:80" >"$PF_LOG" 2>&1 &
    echo $! >"$PF_PID"
    local i=0; until portal_up || [ $i -ge 30 ]; do sleep 1; i=$((i+1)); done
    portal_up && ok "portal is up" || { bad "portal did not answer in 30 s — see $PF_LOG"; return 1; }
  fi
  cat <<EOF

  Portal:   http://localhost:$PORT   (signed in as $DEMO_USER)
  CLI:      export WORKSPACE_NAMESPACE=ws-$(user_slug);  ./cli/workspace list
  Cluster:  ./cli/workspace cluster
  Stop:     ./deploy/demo/demo-start.sh stop      (cluster and volumes stay; demo-down.sh removes everything)
EOF
}

k8s_stop() {
  head_ "Stopping the Kubernetes demo services"
  if [ -f "$PF_PID" ] && kill -0 "$(cat "$PF_PID")" 2>/dev/null; then run "stopping port-forward"; kill "$(cat "$PF_PID")" 2>/dev/null || true; rm -f "$PF_PID"; else skip "no port-forward to stop"; fi
  if kubectl config use-context "kind-$CLUSTER" >/dev/null 2>&1; then
    local names; names=$(kubectl get statefulsets -A -l app.kubernetes.io/name=workspace -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
    if [ -n "$names" ]; then
      run "scaling workspaces to zero (their volumes stay)"
      echo "$names" | while IFS=/ read -r ns name; do kubectl -n "$ns" scale statefulset "$name" --replicas=0 >/dev/null; done
    else skip "no workspaces running"; fi
    run "scaling the portal to zero (start again with this script)"
    kubectl -n devspaces-system scale deploy workspace-portal --replicas=0 >/dev/null
  fi
  echo "  To stop the whole cluster without deleting it:  docker stop \$(docker ps -q --filter name=$CLUSTER-)"
}

# ------------------------------------------------------------------ compose level
COMPOSE="docker compose -f deploy/demo/docker-compose.yaml"
compose_status() {
  head_ "Docker Compose demo (no Kubernetes)"
  docker info >/dev/null 2>&1 && ok "docker daemon reachable" || { bad "docker daemon not reachable"; return 1; }
  for img in workspace/workspace-portal:dev workspace/workspace-go:dev; do
    docker image inspect "$img" >/dev/null 2>&1 && ok "image $img" || bad "image $img missing (LANGS=go ./images/build.sh)"
  done
  [ -f deploy/demo/ssh/authorized_keys ] && ok "deploy/demo/ssh/ prepared" || bad "deploy/demo/ssh/ missing (./deploy/demo/make-ssh-keys.sh)"
  local running; running=$($COMPOSE ps --status running --services 2>/dev/null || true)
  for s in portal workspace; do echo "$running" | grep -qx "$s" && ok "service $s running" || bad "service $s not running"; done
  portal_up && ok "portal answering on http://localhost:$PORT" || bad "nothing answers on http://localhost:$PORT"
  (exec 3<>/dev/tcp/127.0.0.1/2222) 2>/dev/null && ok "workspace sshd on localhost:2222" || bad "nothing listening on localhost:2222"
}

compose_start() {
  head_ "Docker Compose demo (no Kubernetes)"
  docker info >/dev/null 2>&1 || { bad "Docker is not running — start Docker Desktop / dockerd"; return 1; }
  if docker image inspect workspace/workspace-portal:dev >/dev/null 2>&1 && docker image inspect workspace/workspace-go:dev >/dev/null 2>&1; then
    skip "images already built"
  else
    run "building images (first time: 10-20 minutes):  LANGS=go ./images/build.sh"
    ( cd images && LANGS=go ./build.sh )
  fi
  if [ -f deploy/demo/ssh/authorized_keys ] && [ -f deploy/demo/ssh/ssh_host_ed25519_key ]; then
    skip "deploy/demo/ssh/ already prepared"
  else
    run "preparing SSH keys:  ./deploy/demo/make-ssh-keys.sh ${SSH_PUBKEY:-}"
    ./deploy/demo/make-ssh-keys.sh ${SSH_PUBKEY:+"$SSH_PUBKEY"} || { bad "no SSH public key: create one (ssh-keygen -t ed25519) or SSH_PUBKEY=/path/to/key.pub $0"; return 1; }
  fi
  local running; running=$($COMPOSE ps --status running --services 2>/dev/null || true)
  if echo "$running" | grep -qx portal && echo "$running" | grep -qx workspace; then
    skip "portal and workspace containers already running"
  else
    run "docker compose up -d (only the missing services start)"
    $COMPOSE up -d
  fi
  local i=0; until portal_up || [ $i -ge 30 ]; do sleep 1; i=$((i+1)); done
  portal_up && ok "portal is up" || { bad "portal did not answer on :$PORT in 30 s:  $COMPOSE logs portal"; return 1; }
  i=0; until (exec 3<>/dev/tcp/127.0.0.1/2222) 2>/dev/null || [ $i -ge 120 ]; do sleep 1; i=$((i+1)); done
  (exec 3<>/dev/tcp/127.0.0.1/2222) 2>/dev/null && ok "workspace sshd is up" || bad "sshd not up yet (first boot installs tools):  $COMPOSE logs -f workspace"
  cat <<EOF

  Portal:     http://localhost:$PORT   (fake in-memory cluster, -mock)
  Workspace:  ssh -p 2222 dev@localhost
  Stop:       ./deploy/demo/demo-start.sh stop   ($COMPOSE down; the home volume stays)
EOF
}
compose_stop() { head_ "Stopping the Compose demo"; run "$COMPOSE down"; $COMPOSE down; echo "  volume demo-home kept; remove it with:  $COMPOSE down -v"; }

# ------------------------------------------------------------------ simulation (no Docker at all)
sim_page() {
  head_ "Static simulation (nothing to install)"
  local f="$PWD/portal/web/simulation.html"
  [ -f "$f" ] || { bad "$f missing"; return 1; }
  ok "page: $f"
  echo "  It needs no server: open the file in a browser. Everything is stored in the browser (localStorage)."
  if portal_up; then ok "the real portal is also up: http://localhost:$PORT/simulation.html"; fi
  [ "$1" = open ] && open_url "file://$f"
  return 0
}

# ------------------------------------------------------------------ main
mode=$(detect_mode)
case "$ACTION" in
  start)
    case "$mode" in
      kubernetes) k8s_start ;;
      compose)    compose_start ;;
      sim)        echo "No kind cluster '$CLUSTER' and no built images found."
                  echo "  • Full demo on Kubernetes:   ./deploy/demo/demo-up.sh"
                  echo "  • Docker only:               LANGS=go ./images/build.sh && MODE=compose $0"
                  sim_page open ;;
    esac ;;
  status)
    echo "detected mode: $mode"
    case "$mode" in kubernetes) k8s_status || true ;; compose) compose_status || true ;; sim) sim_page noopen ;; esac
    if [ "$mode" != kubernetes ] && have kind && kind get clusters 2>/dev/null | grep -q .; then echo; echo "  other kind clusters: $(kind get clusters | tr '\n' ' ')"; fi ;;
  stop)
    case "$mode" in kubernetes) k8s_stop ;; compose) compose_stop ;; sim) echo "nothing is running (static page only)";; esac ;;
  sim) sim_page open ;;
  *) echo "usage: $0 [start|status|stop|sim]   (env: MODE=kubernetes|compose|sim CLUSTER=$CLUSTER PORT=$PORT DEMO_USER=$DEMO_USER)"; exit 2 ;;
esac
