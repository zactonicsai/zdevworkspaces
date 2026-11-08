#!/usr/bin/env bash
# Open the portal through a port-forward (when there is no ingress).
#   scripts/port-forward.sh [local-port]
source "$(dirname "$0")/lib.sh"
need kubectl
PORT="${1:-$PORT}"
info "forwarding http://localhost:${PORT}/ -> svc/${RELEASE} in ${NAMESPACE} (context $(current_context)); Ctrl-C to stop"
exec kubectl -n "$NAMESPACE" port-forward "svc/${RELEASE}" "${PORT}:80"
