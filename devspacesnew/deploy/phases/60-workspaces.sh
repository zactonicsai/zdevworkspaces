#!/usr/bin/env bash
# Phase 60 — WORKSPACES. Spin up the first dev pods.
#
# WHAT   Creates the SAMPLE_WORKSPACES ("name:language …") for the first developer with the
#        same CLI a developer would use:  workspace -n ws-<name> create <name> --lang dev
#        Each one is a StatefulSet (1 pod: the language image + sshd) with a home-drive PVC
#        on HOME_SC, a Service, and your SSH public key authorised.
# WHY    Proves the whole chain (images pull, storage binds, policies admit, mesh injects)
#        before anybody else is invited. Developers normally do this themselves in the portal
#        or with wsctl; this phase is optional (SAMPLE_WORKSPACES="" skips it).
# NEEDS  50 (the namespace), ssh-keygen on this machine (to make/authorise your key).
# MAKES  StatefulSets ws-<name> + PVCs ws-<name>-home in ws-<first developer>.
# RE-RUN safe; existing workspaces are skipped.
#   ./60-workspaces.sh [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"; need_detect
WS="python3 $REPO_ROOT/cli/workspace $KCTX_FLAG"; export WORKSPACE_SYSTEM_NAMESPACE=$SYSTEM_NS
[ -n "${SAMPLE_WORKSPACES:-}" ] || { skip "SAMPLE_WORKSPACES is empty — create dev pods in the portal or with wsctl"; echo "Next:  ./70-verify.sh"; exit 0; }
[ "$AUTH" = dev ] && first=$DEV_USER || first=$(printf '%s\n' $DEVELOPERS | head -1)
ns="ws-$(short_name "$first")"
have ssh-keygen || [ "$DRY_RUN" = 1 ] || fail "ssh-keygen not found (the CLI creates your SSH key with it)"

step "Dev pods for $first in $ns"
for pair in $SAMPLE_WORKSPACES; do
  name=${pair%%:*}; lang=${pair##*:}
  if [ -n "$(kq -n "$ns" get statefulset "ws-$name" -o name)" ]; then skip "$name exists"; continue; fi
  run $WS -n "$ns" create "$name" --lang "$lang" --git-auth none --idle-timeout "${WORKSPACE_IDLE:-4h}" --no-wait -y
  ok "$name ($lang) — image pull + disk bind take about a minute"
done
[ "$DRY_RUN" = 1 ] || k -n "$ns" get pods,pvc -l devspaces.io/workspace 2>/dev/null || true
first_ws=${SAMPLE_WORKSPACES%%:*}
cat <<EOT

  Watch:   kubectl $KCTX_FLAG -n $ns get pods -w          ($([ "$MESH" = istio-sidecar ] && echo "2/2 = workspace + istio-proxy" || echo "1/1 Running"))
  Shell:   $WS -n $ns ssh $first_ws
  Portal:  the same pods appear under "My workspaces"
Next:  ./70-verify.sh
EOT
