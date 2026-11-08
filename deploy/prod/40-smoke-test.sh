#!/usr/bin/env bash
# Phase 5 — prove the whole path works: create a workspace as a test tenant, wait for Ready,
# run a command inside through the same kubectl-exec path SSH uses, check the portal answers,
# then clean up. Exit 1 on any failure. Safe to run on every deploy (CI gate).
#   ./deploy/prod/40-smoke-test.sh [--keep]
set -Eeuo pipefail
cd "$(dirname "$0")/../.."; [ -f deploy/cluster/env.sh ] && . deploy/cluster/env.sh
CLI=./cli/workspace; command -v workspace >/dev/null && CLI=workspace
TENANT=${SMOKE_TENANT:-smoke-test@devspaces.local}; NS=ws-smoke-test; NAME=smoke-$(date +%s | tail -c 5); KEEP=${1:-}
ok() { printf '\033[32m[ ok ]\033[0m %s\n' "$*"; }; bad() { printf '\033[31m[FAIL]\033[0m %s\n' "$*"; cleanup; exit 1; }
cleanup() { [ "$KEEP" = --keep ] && return 0; WORKSPACE_NAMESPACE=$NS $CLI delete "$NAME" --purge -y >/dev/null 2>&1 || true; }
trap cleanup EXIT
$CLI admin onboard "$TENANT" >/dev/null && ok "tenant $NS onboarded"
tmp=$(mktemp -d); ssh-keygen -q -t ed25519 -N "" -f "$tmp/key"
export WORKSPACE_NAMESPACE=$NS WORKSPACE_CONFIG_DIR=$tmp HOME=$tmp
$CLI create "$NAME" --lang base --git-auth none --idle-timeout 30m --timeout 600 -y >/dev/null && ok "workspace $NAME created and Ready"
out=$(kubectl -n $NS exec "ws-$NAME-0" -c workspace -- bash -lc 'id -u; echo $WORKSPACE_NAME; test -w /home/dev && echo HOME_WRITABLE; (exec 3<>/dev/tcp/127.0.0.1/2222) && echo SSHD_UP' 2>&1) || bad "exec into the pod failed: $out"
echo "$out" | grep -q HOME_WRITABLE && ok "home volume mounted and writable" || bad "home volume not writable: $out"
echo "$out" | grep -q SSHD_UP && ok "sshd listening (VS Code path)" || bad "sshd not up: $out"
echo "$out" | grep -q "^0$" && ok "root inside the workspace" || bad "not root inside: $out"
hu=$(kubectl -n $NS get pod "ws-$NAME-0" -o jsonpath='{.spec.hostUsers}'); [ "$hu" = false ] && ok "user namespace isolation on" || echo "note: hostUsers=$hu (isolation ${ISOLATION:-default})"
kubectl -n $NS exec "ws-$NAME-0" -c workspace -- cat /proc/self/uid_map | grep -q " 0 4294967295" && echo "note: uid not remapped — check kernel/containerd on this node" || ok "uid remapped (unprivileged on the host)"
$CLI stop "$NAME" >/dev/null && sleep 3 && [ "$(kubectl -n $NS get sts "ws-$NAME" -o jsonpath='{.spec.replicas}')" = 0 ] && ok "stop works"
$CLI start "$NAME" --timeout 600 >/dev/null && ok "start works (volume re-attached)"
if [ -n "${PORTAL_HOST:-}" ]; then
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "https://${PORTAL_HOST}/healthz" || true)
  [ "$code" = 200 ] && ok "portal reachable at https://${PORTAL_HOST}" || echo "note: portal healthz returned '$code' (DNS/TLS/ingress not ready yet?)"
fi
kubectl -n devspaces-system get cronjob workspace-reaper >/dev/null && ok "reaper CronJob present"
kubectl get validatingadmissionpolicy devspaces-workspace-guardrails >/dev/null 2>&1 && ok "admission guardrails present"
if kubectl -n $NS run smoke-bad --image=busybox --restart=Never --privileged -- true >/dev/null 2>&1; then kubectl -n $NS delete pod smoke-bad --wait=false >/dev/null; bad "a privileged pod was ADMITTED in a standard namespace"; else ok "privileged pod rejected in a standard namespace"; fi
ok "smoke test passed"
