#!/usr/bin/env bash
# Phase 70 — VERIFY. Prove it works, read-only.
#
# WHAT   Checks the release, the portal's /healthz (through a short port-forward, so it works
#        on any cluster), the catalog, the home-drive protection (asks the API server to
#        delete a drive in --dry-run=server mode and expects "denied"), the first dev pod
#        becoming Ready, snapshots if configured, the mesh labels, and the route if public.
# WHY    Every phase said [ok]; this is the one that looks at the result the way a developer
#        will. Run it after any change, or in CI.
# NEEDS  30 (+50/60 for the pod checks). Changes nothing.
#   ./70-verify.sh [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"; need_detect
PASS=0; WARN=0
good() { PASS=$((PASS+1)); ok "$*"; }; bad() { WARN=$((WARN+1)); warn "$*"; }
[ "$DRY_RUN" = 1 ] && { skip "dry-run: nothing to verify"; exit 0; }

step "Platform"
h -n "$SYSTEM_NS" status devspaces >/dev/null 2>&1 && good "Helm release devspaces deployed" || bad "no Helm release — run ./30-platform.sh"
k -n "$SYSTEM_NS" rollout status deploy/workspace-portal --timeout=60s >/dev/null 2>&1 && good "portal Deployment available" || bad "portal not available: kubectl -n $SYSTEM_NS describe deploy workspace-portal"
[ -n "$(kq -n "$SYSTEM_NS" get configmap workspace-catalog -o name)" ] && good "catalog ConfigMap" || bad "catalog ConfigMap missing"
[ -n "$(kq get validatingadmissionpolicy devspaces-protect-home-drives -o name)" ] && good "home drives protected by ValidatingAdmissionPolicy" || bad "admission policy missing (Kubernetes < 1.30? admission.enabled?)"
port=$((20000 + RANDOM % 20000))
k -n "$SYSTEM_NS" port-forward svc/workspace-portal "$port:80" >/dev/null 2>&1 & pf=$!; sleep 2
hz=$(curl -s -m 5 "http://127.0.0.1:$port/healthz" || true)
[ "$hz" = ok ] && good "portal /healthz → ok" || bad "portal /healthz → '$hz'"
me=$(curl -s -m 5 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/api/me" || true)
if [ "$AUTH" = oidc ]; then [ "$me" = 401 ] && good "anonymous /api/me → 401 (login required)" || bad "anonymous /api/me → $me, expected 401"
else [ "$me" = 200 ] && good "/api/me → 200 as $DEV_USER (AUTH=dev)" || bad "/api/me → $me"; fi
wk=$(curl -s -m 5 "http://127.0.0.1:$port/.well-known/devspaces" || true); case "$wk" in *login*) good "login hints: $(printf '%s' "$wk" | cut -c1-100)…";; esac
kill $pf 2>/dev/null || true

step "Storage"
sc=${HOME_SC:-${HOME_STORAGE_CLASS:-}}
pol=$(kq get storageclass "$sc" -o jsonpath='{.reclaimPolicy}')
[ "$pol" = Retain ] && good "StorageClass $sc keeps disks (Retain)" || bad "StorageClass $sc reclaimPolicy=$pol"
if [ -n "${SNAP_CLASS:-}" ]; then
  [ -n "$(kq get volumesnapshotclass "$SNAP_CLASS" -o name)" ] && good "VolumeSnapshotClass $SNAP_CLASS" || bad "VolumeSnapshotClass $SNAP_CLASS missing"
  [ -n "$(kq get pods -A -o json | python3 -c 'import json,sys;print([c["name"] for p in json.load(sys.stdin)["items"] for c in p["spec"]["containers"] if "snapshotter" in c["image"]][:1])' 2>/dev/null | grep -v '^\[\]$')" ] && good "a csi-snapshotter sidecar is running" || bad "no csi-snapshotter sidecar found: snapshots will stay Pending"
fi

step "Developers and dev pods"
tenants=$(kq get ns -l devspaces.io/tenant -o jsonpath='{.items[*].metadata.name}')
[ -n "$tenants" ] && good "tenant namespaces: $tenants" || bad "no tenant namespaces — run ./50-developers.sh"
if [ "$MESH" != none ]; then for ns in $tenants; do
  l=$(kq get ns "$ns" -o jsonpath='{.metadata.labels.istio\.io/dataplane-mode}{.metadata.labels.istio\.io/rev}{.metadata.labels.istio-injection}')
  [ -n "$l" ] && good "$ns in the mesh ($l)" || bad "$ns not enrolled in the mesh"; done; fi
pods=$(kq get pods -A -l devspaces.io/workspace -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}={.status.phase} {end}')
if [ -n "$pods" ]; then
  good "workspace pods: $pods"
  first=$(printf '%s' "$pods" | awk '{print $1}' | cut -d= -f1); pns=${first%%/*}; pname=${first##*/}
  if k -n "$pns" wait --for=condition=Ready "pod/$pname" --timeout=300s >/dev/null 2>&1; then
    good "$pname is Ready"
    drive=$(kq -n "$pns" get pvc -l devspaces.io/drive=home -o jsonpath='{.items[0].metadata.name}')
    if [ -n "$drive" ]; then
      k -n "$pns" delete pvc "$drive" --dry-run=server >/dev/null 2>&1 && bad "deleting $drive was NOT blocked" || good "deleting home drive $drive is refused (needs devspaces.io/allow-delete=true) — as designed"
    fi
  else bad "$pname not Ready: kubectl -n $pns describe pod $pname  (image pull? PVC pending?)"; fi
else skip "no workspace pods yet"; fi

if [ "$EXPOSE" != port-forward ] && [ -n "${PORTAL_HOST:-}" ]; then
  step "Route"
  code=$(curl -s -o /dev/null -m 10 -w '%{http_code}' "$(scheme)://$PORTAL_HOST/healthz" || echo 000)
  case "$code" in 200) good "$(scheme)://$PORTAL_HOST/healthz → 200";; 000) bad "no answer from $PORTAL_HOST (DNS not set yet? try: curl -H 'Host: $PORTAL_HOST' http://<address>/healthz)";; 403) bad "403: this machine is not in ALLOW_CIDRS";; *) bad "HTTP $code from the route";; esac
fi
echo; printf '  %d checks passed, %d warnings\n' "$PASS" "$WARN"; [ "$WARN" = 0 ]
