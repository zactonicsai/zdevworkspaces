#!/usr/bin/env bash
# Remove the portal/API release from the current context.
#   scripts/undeploy.sh            removes the API only; developer pods (ws-*) keep running
#   scripts/undeploy.sh --all      also uninstalls every ws-* release (pods stop; home PVCs are KEPT)
#   scripts/undeploy.sh --purge    --all plus delete the home PVCs (developer files are gone)
source "$(dirname "$0")/lib.sh"
need kubectl helm
MODE="${1:-}"
confirm_context

if [ "$MODE" = "--all" ] || [ "$MODE" = "--purge" ]; then
  info "developer workspaces in $NAMESPACE"
  for rel in $(helm -n "$NAMESPACE" list -q | grep '^ws-' || true); do
    run helm -n "$NAMESPACE" uninstall "$rel"
  done
  if [ "$MODE" = "--purge" ]; then
    warn "deleting home volumes"
    run kubectl -n "$NAMESPACE" delete pvc -l app.kubernetes.io/part-of=dev-workspaces --ignore-not-found
  else
    ok "home PVCs kept: kubectl -n $NAMESPACE get pvc -l app.kubernetes.io/part-of=dev-workspaces"
  fi
fi

info "uninstalling $RELEASE"
helm -n "$NAMESPACE" status "$RELEASE" >/dev/null 2>&1 && run helm -n "$NAMESPACE" uninstall "$RELEASE" || warn "release $RELEASE not found"
ok "done (namespace $NAMESPACE left in place)"
