#!/usr/bin/env bash
# check-prereqs.sh — verify a laptop is ready for the devspaces demo, and verify the
# demo environment itself once it exists. Read-only (the optional probe pod is the
# one exception: it is created in the kind cluster and deleted again).
#
#   ./deploy/demo/check-prereqs.sh              # Level 2 (kind) expectations, the default
#   ./deploy/demo/check-prereqs.sh --level 1    # Docker-only demo: kind/kubectl become optional
#   ./deploy/demo/check-prereqs.sh --no-probe   # skip the hostUsers:false probe pod
#   ./deploy/demo/check-prereqs.sh --offline    # skip network reachability checks
#
# Output: one line per check — PASS / WARN / FAIL / INFO — with a hint on what to do.
# Exit code 1 if anything FAILed. Works with bash 3.2 (macOS) and newer; needs no root.
set -u
LEVEL=2; PROBE=1; ONLINE=1; CLUSTER=${CLUSTER:-devspaces}
for a in "$@"; do
  case "$a" in
    --level) ;; 1|2) LEVEL=$a ;; --level=1) LEVEL=1 ;; --level=2) LEVEL=2 ;;
    --no-probe) PROBE=0 ;; --offline) ONLINE=0 ;;
    -h|--help) sed -n '2,13p' "$0"; exit 0 ;;
    *) echo "unknown option: $a" >&2; exit 2 ;;
  esac
done
REPO=$(cd "$(dirname "$0")/../.." && pwd)
PASS=0; WARN=0; FAIL=0
if [ -t 1 ]; then G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; B=$'\033[34m'; N=$'\033[0m'; else G=; Y=; R=; B=; N=; fi
pass() { PASS=$((PASS+1)); printf '%s[PASS]%s %-28s %s\n' "$G" "$N" "$1" "${2:-}"; }
warn() { WARN=$((WARN+1)); printf '%s[WARN]%s %-28s %s\n' "$Y" "$N" "$1" "${2:-}"; [ -n "${3:-}" ] && printf '       %s→ %s%s\n' "$Y" "$3" "$N"; }
fail() { FAIL=$((FAIL+1)); printf '%s[FAIL]%s %-28s %s\n' "$R" "$N" "$1" "${2:-}"; [ -n "${3:-}" ] && printf '       %s→ %s%s\n' "$R" "$3" "$N"; }
info() { printf '%s[INFO]%s %-28s %s\n' "$B" "$N" "$1" "${2:-}"; }
head1() { printf '\n%s%s%s\n' "$B" "$1" "$N"; }
have() { command -v "$1" >/dev/null 2>&1; }
# vergte A B → true when dotted version A >= B (numeric parts only; bash 3.2 safe)
vergte() {
  local a b i x y
  IFS=. read -r -a a <<<"${1//[!0-9.]/}"; IFS=. read -r -a b <<<"${2//[!0-9.]/}"
  for i in 0 1 2; do
    x=${a[$i]:-0}; y=${b[$i]:-0}; x=${x:-0}; y=${y:-0}
    if [ "$x" -gt "$y" ] 2>/dev/null; then return 0; elif [ "$x" -lt "$y" ] 2>/dev/null; then return 1; fi
  done
  return 0
}
port_free() { ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
reach() { curl -fsSIL --max-time 8 -o /dev/null "$1" 2>/dev/null; }

head1 "System"
os=$(uname -s); arch=$(uname -m)
case "$os" in
  Darwin) pass "operating system" "macOS ($arch)$( [ "$arch" = arm64 ] && printf ' — images will be built for arm64')" ;;
  Linux)  if grep -qi microsoft /proc/version 2>/dev/null; then pass "operating system" "Windows + WSL 2 ($arch)"; else pass "operating system" "Linux ($arch)"; fi ;;
  MINGW*|MSYS*|CYGWIN*) fail "operating system" "$os" "run this inside a WSL 2 terminal, not Git Bash/MSYS" ;;
  *) warn "operating system" "$os ($arch) — untested" ;;
esac
bv=${BASH_VERSION%%(*}; if vergte "$bv" 3.2; then pass "bash" "$bv"; else fail "bash" "$bv" "bash 3.2 or newer is required"; fi
avail_kb=$(df -Pk "$REPO" 2>/dev/null | awk 'NR==2{print $4}'); avail_gb=$(( ${avail_kb:-0} / 1024 / 1024 ))
if [ "$avail_gb" -ge 20 ]; then pass "free disk (repo volume)" "${avail_gb} GB"; elif [ "$avail_gb" -ge 8 ]; then warn "free disk (repo volume)" "${avail_gb} GB" "20 GB recommended: images are 1-2 GB each"; else fail "free disk (repo volume)" "${avail_gb} GB" "free at least 8 GB (20 GB recommended)"; fi

head1 "Repository"
missing=""
for f in cli/workspace images/build.sh images/base/Dockerfile images/langs/Dockerfile images/runtime/entrypoint.sh \
         portal/main.go portal/Dockerfile portal/web/index.html portal/web/tailwind.css deploy/platform/kustomization.yaml \
         deploy/platform/catalog.json deploy/demo/docker-compose.yaml deploy/demo/kind-config.yaml deploy/demo/kustomization.yaml \
         deploy/demo/catalog.demo.json deploy/demo/make-ssh-keys.sh deploy/demo/demo-up.sh docs/demo-docker.md; do
  [ -e "$REPO/$f" ] || missing="$missing $f"
done
if [ -z "$missing" ]; then pass "project files" "$REPO"; else fail "project files" "missing:$missing" "unzip devspaces.zip again; run this script from inside it"; fi
for f in cli/workspace images/build.sh deploy/demo/make-ssh-keys.sh deploy/demo/demo-up.sh deploy/demo/demo-down.sh; do
  [ -x "$REPO/$f" ] || { warn "executable bit" "$f" "chmod +x $f"; }
done
if have python3 && python3 -c 'import sys; sys.exit(0 if sys.version_info >= (3,9) else 1)' 2>/dev/null; then
  pass "python3" "$(python3 --version 2>&1 | awk '{print $2}')"
  if python3 - "$REPO/cli/workspace" <<'EOF' 2>/dev/null
import sys, py_compile; py_compile.compile(sys.argv[1], doraise=True)
EOF
  then pass "workspace CLI" "parses"; else fail "workspace CLI" "does not compile" "the file may be truncated; unzip again"; fi
else
  fail "python3" "$(have python3 && python3 --version 2>&1 || echo 'not found')" "install Python 3.9+ (python.org, brew install python, apt install python3)"
fi

head1 "SSH"
if have ssh && have ssh-keygen; then pass "OpenSSH client" "$(ssh -V 2>&1 | awk '{print $1}')"; else fail "OpenSSH client" "ssh/ssh-keygen not found" "install the OpenSSH client (built into macOS/Windows 10+/most Linux)"; fi
pub=$(ls "$HOME"/.ssh/id_ed25519.pub "$HOME"/.ssh/id_ecdsa.pub "$HOME"/.ssh/id_rsa.pub 2>/dev/null | head -1)
if [ -n "$pub" ]; then
  pass "SSH public key" "$pub ($(ssh-keygen -lf "$pub" 2>/dev/null | awk '{print $2}'))"
else
  warn "SSH public key" "none in ~/.ssh" 'ssh-keygen -t ed25519 -C "you@example.com"   (press Enter for the default file)'
fi
if grep -q "^Host ws-\|^Host demo-ws" "$HOME/.ssh/config" 2>/dev/null; then info "~/.ssh/config" "has workspace entries: $(grep -E '^Host (ws-|demo-ws)' "$HOME/.ssh/config" | awk '{print $2}' | tr '\n' ' ')"; fi

head1 "Docker"
if ! have docker; then
  fail "docker CLI" "not found" "https://docs.docker.com/get-docker/"
else
  cv=$(docker version --format '{{.Client.Version}}' 2>/dev/null | head -1); cv=${cv:-0}
  if vergte "$cv" 24; then pass "docker CLI" "$cv"; else warn "docker CLI" "$cv" "Docker 24+ recommended (BuildKit by default)"; fi
  if ! docker info >/dev/null 2>&1; then
    fail "docker daemon" "not reachable" "start Docker Desktop, or: sudo systemctl start docker; on Linux add yourself to the docker group"
  else
    sv=$(docker info --format '{{.ServerVersion}}' 2>/dev/null); sos=$(docker info --format '{{.OSType}}/{{.Architecture}}' 2>/dev/null)
    ncpu=$(docker info --format '{{.NCPU}}' 2>/dev/null); mem=$(docker info --format '{{.MemTotal}}' 2>/dev/null); memgb=$(( ${mem:-0} / 1024 / 1024 / 1024 ))
    pass "docker daemon" "$sv, $sos, ${ncpu:-?} CPUs, ${memgb} GB"
    if [ "$memgb" -ge 8 ]; then pass "memory for Docker" "${memgb} GB"; elif [ "$memgb" -ge 6 ]; then warn "memory for Docker" "${memgb} GB" "8 GB recommended for the 3-node kind cluster + workspaces (Docker Desktop → Settings → Resources)"; else fail "memory for Docker" "${memgb} GB" "give Docker at least 6 GB (8 recommended)"; fi
    if [ "${ncpu:-0}" -ge 4 ]; then pass "CPUs for Docker" "$ncpu"; else warn "CPUs for Docker" "${ncpu:-?}" "4+ CPUs make image builds much faster"; fi
    if docker compose version >/dev/null 2>&1; then pass "docker compose v2" "$(docker compose version --short 2>/dev/null)"; else fail "docker compose v2" "not found" "Docker Desktop includes it; on Linux: apt install docker-compose-plugin"; fi
    if docker buildx version >/dev/null 2>&1; then pass "docker buildx" "$(docker buildx version 2>/dev/null | awk '{print $2}')"; else info "docker buildx" "not found — only needed for multi-arch builds (PLATFORMS=…)"; fi
    if docker info --format '{{.Driver}}' 2>/dev/null | grep -q overlay; then pass "storage driver" "$(docker info --format '{{.Driver}}')"; else info "storage driver" "$(docker info --format '{{.Driver}}' 2>/dev/null)"; fi
    if [ "$ONLINE" = 1 ]; then
      kver=$(docker run --rm alpine:3.20 uname -r 2>/dev/null || true)
      if [ -z "$kver" ]; then
        warn "Docker kernel version" "could not run alpine:3.20" "check internet access / Docker Hub; needed for the first image pulls anyway"
      elif vergte "${kver%%-*}" 6.3; then
        pass "Docker kernel version" "$kver (user namespaces OK)"
      else
        warn "Docker kernel version" "$kver" "user namespaces need kernel >= 6.3: update Docker Desktop / your kernel, or use the --isolation none fallback in docs/demo-docker.md (demo only)"
      fi
    fi
    # images built so far
    built=""; for i in base go python rust java node cpp cli portal; do docker image inspect "workspace/workspace-$i:dev" >/dev/null 2>&1 && built="$built $i"; done
    if [ -n "$built" ]; then pass "images built" "workspace/*:dev →$built"; else info "images built" "none yet — LANGS=\"go\" ./images/build.sh"; fi
    if docker compose -f "$REPO/deploy/demo/docker-compose.yaml" ps --status running --services 2>/dev/null | grep -q .; then
      info "Level 1 stack" "running: $(docker compose -f "$REPO/deploy/demo/docker-compose.yaml" ps --status running --services 2>/dev/null | tr '\n' ' ')"
    fi
  fi
fi

head1 "Ports on this machine"
for p in 8080 2222; do
  if port_free "$p"; then pass "port $p free" ""; else
    if [ "$p" = 8080 ]; then warn "port $p in use" "" "something already listens on $p (the portal or an old port-forward?) — stop it or use another port"; else warn "port $p in use" "" "the Level 1 workspace publishes sshd on 2222; stop whatever uses it or edit docker-compose.yaml"; fi
  fi
done

if [ "$ONLINE" = 1 ]; then
  head1 "Network (downloads during builds)"
  have curl || warn "curl" "not found" "install curl to run the reachability checks"
  if have curl; then
    for u in "https://registry-1.docker.io/v2/|Docker Hub (base images)" "https://go.dev/dl/|go.dev (Go toolchain)" "https://github.com/|github.com (tool downloads)" \
             "https://dl.k8s.io/release/stable.txt|dl.k8s.io (kubectl in the CLI image)" "https://registry.npmjs.org/|npm registry (Tailwind build)" "https://pypi.org/simple/|PyPI (uv)"; do
      url=${u%%|*}; label=${u#*|}
      if reach "$url"; then pass "reach $label" ""; else warn "reach $label" "$url" "unreachable from here: builds that need it will fail; a proxy may need HTTPS_PROXY in Docker's settings"; fi
    done
  fi
fi

head1 "Kubernetes tools (Level 2)"
req() { if [ "$LEVEL" = 2 ]; then fail "$@"; else warn "$@"; fi; }
if have kind; then
  kv=$(kind version 2>/dev/null | awk '{print $2}' | sed 's/^v//')
  if vergte "$kv" 0.32; then pass "kind" "v$kv"; else req "kind" "v$kv" "kind 0.32+ is required (fixes hostUsers:false on Kubernetes 1.36): https://kind.sigs.k8s.io/docs/user/quick-start/#installation"; fi
else req "kind" "not found" "https://kind.sigs.k8s.io/docs/user/quick-start/#installation"; fi
if have kubectl; then
  kcv=$(kubectl version --client 2>/dev/null | awk -F'[ v]+' '/Client Version/{print $3}')
  if vergte "${kcv:-0}" 1.33; then pass "kubectl" "v$kcv"; else req "kubectl" "v${kcv:-?}" "kubectl 1.33+ recommended: https://kubernetes.io/docs/tasks/tools/"; fi
else req "kubectl" "not found" "https://kubernetes.io/docs/tasks/tools/"; fi
have go   && pass "go (optional)" "$(go version | awk '{print $3}')" || info "go (optional)" "not found — only needed to run the portal from source"
have npm  && pass "npm (optional)" "$(npm --version 2>/dev/null)" || info "npm (optional)" "not found — the committed tailwind.css is used"
have code && pass "VS Code CLI (optional)" "code" || info "VS Code CLI (optional)" "not found — 'workspace code' needs it; connect from the Remote-SSH menu instead"

# ---------------------------------------------------------------- live environment
if have kind && have kubectl && kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  head1 "kind cluster '$CLUSTER' (verifying the environment)"
  K="kubectl --context kind-$CLUSTER"
  if ! $K get nodes >/dev/null 2>&1; then
    fail "API server" "not reachable" "kind export kubeconfig --name $CLUSTER; is Docker running?"
  else
    total=$($K get nodes --no-headers 2>/dev/null | wc -l | tr -d ' '); ready=$($K get nodes --no-headers 2>/dev/null | awk '$2=="Ready"' | wc -l | tr -d ' ')
    if [ "$ready" = "$total" ] && [ "$total" -ge 3 ]; then pass "nodes" "$ready/$total Ready ($($K get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}'), containerd $($K get nodes -o jsonpath='{.items[0].status.nodeInfo.containerRuntimeVersion}' | sed 's#.*//##'))"; else warn "nodes" "$ready/$total Ready" "kubectl get nodes; the demo expects 3 nodes from deploy/demo/kind-config.yaml"; fi
    kv=$($K get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}' 2>/dev/null | sed 's/^v//')
    if vergte "${kv:-0}" 1.36; then pass "kubernetes version" "v$kv (user namespaces GA)"; else warn "kubernetes version" "v$kv" "1.36+ expected; recreate with deploy/demo/kind-config.yaml"; fi
    lab=$($K get nodes -l devspaces.io/lab=true -o name 2>/dev/null | wc -l | tr -d ' ')
    taint=$($K get nodes -l devspaces.io/lab=true -o jsonpath='{.items[*].spec.taints[?(@.key=="devspaces.io/lab")].effect}' 2>/dev/null)
    if [ "$lab" -ge 1 ] && [ -n "$taint" ]; then pass "lab node pool" "$lab node(s) labelled + tainted ($taint)"; else warn "lab node pool" "label=$lab taint='${taint:-none}'" "hardware/kernel profiles need a node with devspaces.io/lab=true and the NoSchedule taint"; fi
    if $K get ns devspaces-system >/dev/null 2>&1; then
      pass "platform installed" "namespace devspaces-system"
      $K get configmap workspace-catalog -n devspaces-system >/dev/null 2>&1 && pass "catalog ConfigMap" "$($K get configmap workspace-catalog -n devspaces-system -o jsonpath='{.data.catalog\.json}' | python3 -c 'import sys,json; c=json.load(sys.stdin); print(c["registry"]+":"+c["tag"], len(c["languages"]),"languages,", len([t for t in c["tools"] if not t.startswith("$")]),"tools")' 2>/dev/null)" || fail "catalog ConfigMap" "missing" "kubectl apply -k deploy/demo"
      $K get validatingadmissionpolicy devspaces-workspace-guardrails >/dev/null 2>&1 && pass "admission guardrails" "ValidatingAdmissionPolicy present" || warn "admission guardrails" "missing" "kubectl apply -k deploy/demo (or deliberately removed for the no-userns fallback)"
      rr=$($K -n devspaces-system get deploy workspace-portal -o jsonpath='{.status.readyReplicas}' 2>/dev/null)
      if [ "${rr:-0}" -ge 1 ]; then pass "portal" "ready ($rr replica) — kubectl -n devspaces-system port-forward svc/workspace-portal 8080:80"; else fail "portal" "not ready" "kubectl -n devspaces-system describe deploy workspace-portal; is workspace/workspace-portal:dev loaded into kind?"; fi
      $K get cronjob workspace-reaper -n devspaces-system >/dev/null 2>&1 && pass "idle reaper" "CronJob present ($($K get cronjob workspace-reaper -n devspaces-system -o jsonpath='{.spec.schedule}'))" || warn "idle reaper" "missing" "kubectl apply -k deploy/demo"
    else
      warn "platform installed" "no namespace devspaces-system" "kubectl apply -k deploy/demo"
    fi
    tenants=$($K get ns -l devspaces.io/tenant -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
    if [ -n "$tenants" ]; then pass "developers onboarded" "$tenants"; else warn "developers onboarded" "none" "./cli/workspace admin onboard demo@example.com --lab"; fi
    loaded=$(docker exec "$CLUSTER-worker" crictl images 2>/dev/null | awk '/workspace\//{print $1}' | sed 's#.*/##' | tr '\n' ' ')
    if [ -n "$loaded" ]; then pass "images loaded in nodes" "$loaded"; else warn "images loaded in nodes" "none" "kind load docker-image workspace/workspace-go:dev workspace/workspace-cli:dev workspace/workspace-portal:dev --name $CLUSTER"; fi
    sc=$($K get storageclass -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}' 2>/dev/null)
    if [ -n "$sc" ]; then pass "default StorageClass" "$sc"; else fail "default StorageClass" "none" "home volumes cannot be created; kind normally provides 'standard'"; fi
    ws=$($K get statefulsets -A -l devspaces.io/workspace --no-headers 2>/dev/null | awk '{print $2"("$3")"}' | tr '\n' ' ')
    [ -n "$ws" ] && info "workspaces" "$ws"
    if [ "$PROBE" = 1 ]; then
      $K -n default delete pod userns-probe --ignore-not-found --wait=false >/dev/null 2>&1
      if $K -n default run userns-probe --image=busybox:1.36 --restart=Never --overrides='{"apiVersion":"v1","spec":{"hostUsers":false}}' -- cat /proc/self/uid_map >/dev/null 2>&1; then
        phase=""; for _ in $(seq 1 45); do phase=$($K -n default get pod userns-probe -o jsonpath='{.status.phase}' 2>/dev/null); case "$phase" in Succeeded|Failed) break ;; esac; sleep 2; done
        if [ "$phase" = Succeeded ]; then
          map=$($K -n default logs userns-probe 2>/dev/null | head -1 | tr -s ' ')
          case "$map" in *" 0 4294967295"*) warn "user namespace probe" "pod ran but is NOT remapped ($map)" "the node runtime ignores hostUsers:false; check containerd >= 2.0 (kind >= 0.27) and kubelet feature gates" ;;
                         *) pass "user namespace probe" "hostUsers:false works (uid_map: $map)" ;; esac
        else
          reason=$($K -n default describe pod userns-probe 2>/dev/null | awk '/Events:/{f=1} f' | tail -3 | tr -s ' ' | tr '\n' ';')
          warn "user namespace probe" "pod ended in '${phase:-Pending}'" "kernel < 6.3 or kind < 0.32? ${reason:-no events}. Fallback: see Troubleshooting in docs/demo-docker.md"
        fi
        $K -n default delete pod userns-probe --ignore-not-found --wait=false >/dev/null 2>&1
      else
        warn "user namespace probe" "could not create the probe pod" "kubectl -n default run … failed; check RBAC/cluster health"
      fi
    fi
  fi
else
  [ "$LEVEL" = 2 ] && info "kind cluster '$CLUSTER'" "not created yet — ./deploy/demo/demo-up.sh (or: kind create cluster --config deploy/demo/kind-config.yaml)"
fi

head1 "Summary"
printf '  %s%d passed%s, %s%d warnings%s, %s%d failed%s\n' "$G" "$PASS" "$N" "$Y" "$WARN" "$N" "$R" "$FAIL" "$N"
if [ "$FAIL" -gt 0 ]; then echo "  Fix the FAIL items first; WARN items are advisory."; exit 1; fi
[ "$WARN" -gt 0 ] && echo "  Ready, with warnings. Next: docs/demo-docker.md"
[ "$WARN" -eq 0 ] && echo "  Ready. Next: docs/demo-docker.md"
exit 0
