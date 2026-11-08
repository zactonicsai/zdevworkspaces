#!/usr/bin/env bash
# 4/5 — Onboard developers (namespaces, quota, RBAC, NetworkPolicies, mesh label), create the
# shared PROJECT namespace, and start sample dev pods (workspaces) for the first developer so
# the portal has something to show. Uses the Python CLI from this repository (needs kubectl
# + python3; no ssh-keygen needed for onboarding). Idempotent: re-running re-applies.
#   ./40-dev-pods.sh [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"; need_detect
WS="python3 $REPO_ROOT/cli/workspace --context $KCTX"
export WORKSPACE_SYSTEM_NAMESPACE=$SYSTEM_NS
[ "$AUTH" = dev ] && DEVELOPERS=${DEVELOPERS_OVERRIDE:-$DEV_USER}

step "Onboarding developers: $DEVELOPERS"
for u in $DEVELOPERS; do
  short=$(printf '%s' "$u" | cut -d@ -f1 | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9-]/-/g')
  if [ -n "$(kq get ns "ws-$short" -o name)" ] && [ "${REONBOARD:-false}" != true ]; then skip "ws-$short exists (REONBOARD=true to re-apply)"; continue; fi
  run $WS admin onboard "$u" $([ "$LAB_NAMESPACES" = true ] && [ "${LAB_NODES:-0}" -gt 0 ] && echo --lab)
  ok "$u → ws-$short (mesh label from catalog: $MESH)"
done

if [ -n "$PROJECT" ]; then
  step "Project namespace ws-proj-$PROJECT (shared by all developers)"
  members=$(printf '%s\n' $DEVELOPERS | paste -sd, -)
  run $WS admin project create "$PROJECT" --members "$members"
  ok "members can create there with:  wsctl create api --lang go --project $PROJECT"
fi

first=$(printf '%s\n' $DEVELOPERS | head -1)
fshort=$(printf '%s' "$first" | cut -d@ -f1 | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9-]/-/g')
if [ -n "$SAMPLE_WORKSPACES" ]; then
  step "Sample dev pods for $first (namespace ws-$fshort)"
  if ! have ssh-keygen; then warn "ssh-keygen not found: the CLI needs it to create your SSH key — skipping sample workspaces"; else
    for pair in $SAMPLE_WORKSPACES; do
      name=${pair%%:*}; lang=${pair##*:}
      if [ -n "$(kq -n "ws-$fshort" get statefulset "ws-$name" -o name)" ]; then skip "workspace $name exists"; continue; fi
      run $WS -n "ws-$fshort" create "$name" --lang "$lang" --tools "$SAMPLE_TOOLS" --git-auth none --idle-timeout "$WORKSPACE_IDLE" --no-wait -y
      ok "created $name ($lang) — boots in about a minute"
    done
  fi
fi

step "What you have now"
[ "$DRY_RUN" = 1 ] || k get ns -l devspaces.io/tenant -o custom-columns='NAMESPACE:.metadata.name,TENANT:.metadata.labels.devspaces\.io/tenant,KIND:.metadata.labels.devspaces\.io/kind,MESH:.metadata.labels.istio\.io/dataplane-mode,REV:.metadata.labels.istio\.io/rev' 2>/dev/null || true
[ "$DRY_RUN" = 1 ] || k get pods -A -l devspaces.io/workspace -o wide 2>/dev/null || true
cat <<EOT

  Portal:  $([ "$AUTH" = entra ] && echo "https://$PORTAL_HOST" || echo "kubectl --context $KCTX -n $SYSTEM_NS port-forward svc/workspace-portal 8080:80  → http://localhost:8080")
  CLI:     $WS -n ws-$fshort list      ·  $WS -n ws-$fshort ssh ${SAMPLE_WORKSPACES%%:*}
  wsctl:   wsctl login --portal $([ "$AUTH" = entra ] && echo "https://$PORTAL_HOST" || echo "http://localhost:8080")   (device-code login / --dev for AUTH=dev)
Next:  ./50-verify.sh
EOT
