#!/usr/bin/env bash
# 3/5 (optional) — Show what Istio does for the platform on this cluster and verify it:
# namespace enrolment, portal mTLS, the Gateway/VirtualService route, and that a developer
# namespace will be meshed the same way. Read-only except for `--apply-examples`, which
# installs deploy/platform/mesh/ examples (waypoint, AuthorizationPolicy) into PROJECT's namespace.
#   ./30-istio-check.sh [--apply-examples] [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"; need_detect
[ "$MESH" = none ] && { warn "MESH=none: nothing to check (Istio not found or disabled)"; exit 0; }

step "Mesh enrolment"
for ns in "$SYSTEM_NS" $(kq get ns -l devspaces.io/tenant -o jsonpath='{.items[*].metadata.name}'); do
  l=$(kq get ns "$ns" -o jsonpath='{.metadata.labels.istio\.io/dataplane-mode}{" "}{.metadata.labels.istio\.io/rev}{" "}{.metadata.labels.istio-injection}')
  case "$l" in
    ambient*) ok "$ns: ambient";;
    *" $ISTIO_REV "*|*enabled*) ok "$ns: sidecar injection ($l)";;
    *) [ "$DRY_RUN" = 1 ] && ok "$ns: (dry-run)" || warn "$ns: not enrolled — run ./10-namespaces.sh / onboard again";;
  esac
done

step "Portal in the mesh"
if [ "$MESH" = istio-sidecar ]; then
  c=$(kq -n "$SYSTEM_NS" get pods -l app.kubernetes.io/name=workspace-portal -o jsonpath='{.items[0].spec.containers[*].name}')
  case " $c " in *" istio-proxy "*) ok "portal pod has istio-proxy";; *) [ "$DRY_RUN" = 1 ] || warn "portal pod has no sidecar yet: kubectl -n $SYSTEM_NS rollout restart deploy/workspace-portal";; esac
else
  ok "ambient: ztunnel carries portal traffic (no sidecar container expected)"
fi
pa=$(kq -n "$SYSTEM_NS" get peerauthentication default -o jsonpath='{.spec.mtls.mode}')
[ "$pa" = STRICT ] && ok "PeerAuthentication STRICT in $SYSTEM_NS" || skip "no STRICT PeerAuthentication (istioAuth.strictMTLS=false?)"
if [ -n "$(kq -n "$SYSTEM_NS" get requestauthentication workspace-portal-jwt -o name)" ]; then ok "RequestAuthentication: Entra JWTs are checked by Istio before the portal"; else skip "no RequestAuthentication (AUTH=dev)"; fi

step "Ingress route"
if [ -n "$(kq -n "$SYSTEM_NS" get virtualservice devspaces -o name)" ] || [ "$DRY_RUN" = 1 ]; then
  ok "VirtualService devspaces → $(kq -n "$SYSTEM_NS" get virtualservice devspaces -o jsonpath='{.spec.http[-1].route[0].destination.host}')"
  gwns=${GW_NS:-$SYSTEM_NS}
  [ -n "$(kq -n "$SYSTEM_NS" get gateway devspaces -o name)" ] && ok "Gateway devspaces in $SYSTEM_NS selecting ${GW_SELECTOR}" || warn "Gateway object missing"
  if [ -n "$GW_IP" ] && [ "$DRY_RUN" != 1 ]; then
    code=$(curl -s -o /dev/null -m 8 -w '%{http_code}' -H "Host: $PORTAL_HOST" "http://$GW_IP/healthz" || true)
    case "$code" in 200|301|302|308) ok "http://$GW_IP/healthz with Host: $PORTAL_HOST → $code";; *) warn "gateway answered '$code' — DNS/TLS not ready yet, or the Gateway/VirtualService did not bind (istioctl analyze -n $SYSTEM_NS)";; esac
  fi
else
  skip "no VirtualService (AUTH=dev uses port-forward)"
fi

step "Workspace pods and the mesh"
cat <<EOT
  * Developer namespaces are enrolled the same way as $SYSTEM_NS when they are onboarded (catalog networking.mesh=$MESH).
  * SSH to a workspace is 'kubectl exec … socat' or the portal's exec tunnel — pod-local, so the mesh never sees it.
  * Workspace-to-workspace calls (ws-api.ws-alice.svc) get mTLS from Istio on top of the NetworkPolicy rules.
  * hardware/kernel profiles are labelled out of the mesh (sidecar.istio.io/inject=false).
  * L7 features (header routing to a developer's workspace, AuthorizationPolicy): deploy/platform/mesh/ — see --apply-examples.
EOT
if printf '%s\n' "$@" | grep -qx -- --apply-examples; then
  [ -n "$PROJECT" ] || fail "--apply-examples needs PROJECT in env.sh"
  pns=ws-proj-$PROJECT
  step "Applying mesh examples to $pns"
  for f in authorizationpolicy-workspace.yaml waypoint.yaml httproute-dev-routing.yaml; do
    run sh -c "sed -e 's/ws-proj-checkout/$pns/g' -e 's/checkout-waypoint/$PROJECT-waypoint/g' '$REPO_ROOT/deploy/platform/mesh/$f' | kubectl --context '$KCTX' apply -f -"
  done
  [ "$MESH" = istio-ambient ] && run k label ns "$pns" istio.io/use-waypoint="$PROJECT-waypoint" --overwrite
  ok "examples applied; read deploy/platform/mesh/README.md for what each one does"
fi
echo; echo "Next:  ./40-dev-pods.sh"
