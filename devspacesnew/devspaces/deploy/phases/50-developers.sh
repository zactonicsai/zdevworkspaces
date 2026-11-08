#!/usr/bin/env bash
# Phase 50 — DEVELOPERS. Give each person a room.
#
# WHAT   For every e-mail in DEVELOPERS (or DEV_USER when AUTH=dev) runs
#          workspace admin onboard <user>
#        which creates namespace ws-<name> with a ResourceQuota, a RoleBinding (the developer
#        may act only there), NetworkPolicies (pods talk only to each other, DNS and out),
#        the mesh label if Istio is present, and a copy of the pull Secret. Then creates each
#        PROJECTS namespace (ws-proj-<name>) with all developers as members.
# WHY    Onboarding is separate from installing: platform admins run 30 once, team leads run
#        50 whenever someone joins. With AUTH=oidc the user name must be what the token says
#        (preferred_username, usually the e-mail) because RBAC is checked by impersonation.
# NEEDS  30 (the catalog ConfigMap in SYSTEM_NS tells the CLI the mesh mode and pull secret).
# MAKES  namespaces ws-<name>, ws-proj-<project>.
# RE-RUN safe; existing namespaces are skipped (REONBOARD=true re-applies quota/RBAC/policies).
#   ./50-developers.sh [--dry-run] [extra e-mails…]
. "$(dirname "$0")/_lib.sh" "$@"; need_detect
WS="python3 $REPO_ROOT/cli/workspace $KCTX_FLAG"; export WORKSPACE_SYSTEM_NAMESPACE=$SYSTEM_NS
extra=""; for _a in "$@"; do [ "$_a" = --dry-run ] || extra="$extra $_a"; done
[ "$AUTH" = dev ] && people="$DEV_USER$extra" || people="$DEVELOPERS$extra"

step "Onboarding: $people"
for u in $people; do
  ns="ws-$(short_name "$u")"
  if [ -n "$(kq get ns "$ns" -o name)" ] && [ "${REONBOARD:-false}" != true ]; then skip "$u → $ns exists"; continue; fi
  run $WS admin onboard "$u"; ok "$u → $ns (quota, RBAC, NetworkPolicies${MESH:+, mesh $MESH})"
done

if [ -n "${PROJECTS:-}" ]; then
  step "Project namespaces (shared 'virtual namespaces')"
  members=$(printf '%s\n' $people | paste -sd, -)
  for p in $PROJECTS; do run $WS admin project create "$p" --members "$members"; ok "ws-proj-$p: $members"; done
  echo "  members create there with:  wsctl create api --lang go --project <name>"
fi
[ "$DRY_RUN" = 1 ] || k get ns -l devspaces.io/tenant -o custom-columns='NAMESPACE:.metadata.name,TENANT:.metadata.labels.devspaces\.io/tenant,KIND:.metadata.labels.devspaces\.io/kind' 2>/dev/null || true
echo; echo "Next:  ./60-workspaces.sh"
