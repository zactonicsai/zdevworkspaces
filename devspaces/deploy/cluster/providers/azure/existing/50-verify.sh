#!/usr/bin/env bash
# 5/5 — Smoke test: contract checks, portal health, catalog served, a dev pod becomes Ready,
# its home drive is protected, and (mesh) the namespaces are enrolled. Read-only.
#   ./50-verify.sh [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"; need_detect
PASS=0; FAILN=0
chk() { if "$@" >/dev/null 2>&1 || [ "$DRY_RUN" = 1 ]; then PASS=$((PASS+1)); return 0; else FAILN=$((FAILN+1)); return 1; fi; }

step "Platform"
chk k -n "$SYSTEM_NS" get configmap workspace-catalog && ok "catalog ConfigMap" || warn "catalog ConfigMap missing"
chk k -n "$SYSTEM_NS" rollout status deploy/workspace-portal --timeout=60s && ok "portal Deployment available" || warn "portal not available: kubectl -n $SYSTEM_NS logs deploy/workspace-portal"
chk k get validatingadmissionpolicy devspaces-protect-home-drives && ok "home drives protected by admission policy" || warn "admission policy missing (admission.enabled?)"
chk k get clusterrole workspace-portal-sharing && ok "sharing RBAC present" || warn "sharing RBAC missing"
if [ "$DRY_RUN" != 1 ]; then
  pod=$(kq -n "$SYSTEM_NS" get pods -l app.kubernetes.io/name=workspace-portal -o jsonpath='{.items[0].metadata.name}')
  if [ -n "$pod" ]; then
    out=$(k -n "$SYSTEM_NS" exec "$pod" -c portal -- /portal -h 2>&1 | head -1 || true)
    hz=$(kq -n "$SYSTEM_NS" get --raw "/api/v1/namespaces/$SYSTEM_NS/services/workspace-portal:80/proxy/healthz")
    [ "$hz" = ok ] && { ok "portal /healthz through the API server proxy"; PASS=$((PASS+1)); } || { warn "portal /healthz did not answer 'ok' (got '$hz')"; FAILN=$((FAILN+1)); }
    wk=$(kq -n "$SYSTEM_NS" get --raw "/api/v1/namespaces/$SYSTEM_NS/services/workspace-portal:80/proxy/.well-known/devspaces")
    case "$wk" in *login*) ok "login hints: $(printf '%s' "$wk" | cut -c1-120)…";; esac
  fi
fi
step "Contract (deploy/cluster/check-contract.sh)"
if [ -x "$REPO_ROOT/deploy/cluster/check-contract.sh" ] && [ "$DRY_RUN" != 1 ]; then
  KUBECONTEXT=$KCTX SYSTEM_NS=$SYSTEM_NS "$REPO_ROOT/deploy/cluster/check-contract.sh" 2>/dev/null | grep -E "C1[456]|C4|C7|C8" | sed 's/^/  /' || true
else skip "(dry-run)"; fi

step "Developers and dev pods"
tenants=$(kq get ns -l devspaces.io/tenant -o jsonpath='{.items[*].metadata.name}')
[ -n "$tenants" ] && ok "tenant namespaces: $tenants" || warn "no tenant namespaces — run ./40-dev-pods.sh"
if [ "$MESH" != none ] && [ "$DRY_RUN" != 1 ]; then
  for ns in $tenants; do
    l=$(kq get ns "$ns" -o jsonpath='{.metadata.labels.istio\.io/dataplane-mode}{.metadata.labels.istio\.io/rev}{.metadata.labels.istio-injection}')
    [ -n "$l" ] && ok "$ns in the mesh ($l)" || warn "$ns not enrolled in the mesh"
  done
fi
pods=$(kq get pods -A -l devspaces.io/workspace -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}={.status.phase}{" "}{end}')
if [ -n "$pods" ]; then
  ok "workspace pods: $pods"
  first=$(printf '%s' "$pods" | awk '{print $1}' | cut -d= -f1); pns=${first%%/*}; pname=${first##*/}
  if k -n "$pns" wait --for=condition=Ready "pod/$pname" --timeout=300s >/dev/null 2>&1; then
    ok "$pname is Ready (sshd answering)"
    drive=$(kq -n "$pns" get pvc -l devspaces.io/drive=home -o jsonpath='{.items[0].metadata.name}')
    if [ -n "$drive" ]; then
      if k -n "$pns" delete pvc "$drive" --dry-run=server >/dev/null 2>&1; then warn "admission policy did NOT block deleting $drive"; FAILN=$((FAILN+1)); else ok "deleting home drive $drive is refused without the allow-delete mark (as designed)"; PASS=$((PASS+1)); fi
    fi
    ws=$(k -n "$pns" get statefulset -l devspaces.io/workspace -o jsonpath='{.items[0].metadata.labels.devspaces\.io/workspace}')
    ok "try it:  python3 $REPO_ROOT/cli/workspace --context $KCTX -n $pns ssh $ws"
  else
    warn "$pname not Ready yet: kubectl -n $pns describe pod $pname (image pull? storage?)"
  fi
else
  skip "no workspace pods yet (SAMPLE_WORKSPACES empty, or create one in the portal)"
fi
echo; printf '  %d checks passed, %d warnings\n' "$PASS" "$FAILN"
