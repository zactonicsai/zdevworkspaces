#!/usr/bin/env bash
# Phase 10 — NAMESPACE. Make the room the platform lives in.
#
# WHAT   Creates SYSTEM_NS with the labels the rest expects: restricted Pod Security, the
#        "part-of" label, and — if 00-check found Istio — the mesh label. If the registry is
#        private, also creates the image pull Secret there (Helm copies it to developers).
# WHY    Namespaces are the unit of isolation. Doing this outside Helm keeps the namespace
#        (and the pull Secret) when you uninstall the release, and lets a cluster admin
#        pre-create it for a team that is not admin.
# NEEDS  env.sh, detected.env (MESH, ISTIO_REV).
# MAKES  namespace SYSTEM_NS, Secret registry-pull (optional).
# RE-RUN safe; labels are re-applied.
#   ./10-namespace.sh [--dry-run]
. "$(dirname "$0")/_lib.sh" "$@"; need_detect

step "Namespace $SYSTEM_NS"
[ -n "$(kq get ns "$SYSTEM_NS" -o name)" ] && skip "exists" || run k create namespace "$SYSTEM_NS"
run k label ns "$SYSTEM_NS" app.kubernetes.io/part-of=devspaces pod-security.kubernetes.io/enforce=restricted pod-security.kubernetes.io/enforce-version=latest --overwrite
case "$MESH" in
  istio-ambient) run k label ns "$SYSTEM_NS" istio.io/dataplane-mode=ambient --overwrite; ok "in the mesh (ambient: no sidecars)";;
  istio-sidecar) if [ -n "$ISTIO_REV" ]; then run k label ns "$SYSTEM_NS" istio.io/rev="$ISTIO_REV" istio-injection- --overwrite; else run k label ns "$SYSTEM_NS" istio-injection=enabled --overwrite; fi; ok "in the mesh (sidecar injection on)";;
  *) ok "no mesh label (MESH=none)";;
esac

step "Image pull secret"
if [ -n "$PULL_SECRET_DOCKERCONFIG" ]; then
  printf '%s' "$PULL_SECRET_DOCKERCONFIG" | base64 -d >/dev/null 2>&1 || fail "PULL_SECRET_DOCKERCONFIG is not valid base64 (base64 -w0 ~/.docker/config.json)"
  run k -n "$SYSTEM_NS" create secret generic registry-pull --type=kubernetes.io/dockerconfigjson --from-literal=.dockerconfigjson="$(printf '%s' "$PULL_SECRET_DOCKERCONFIG" | base64 -d)" --dry-run=client -o yaml | run k apply -f - >/dev/null
  ok "Secret registry-pull in $SYSTEM_NS (onboarding copies it into every developer namespace)"
else
  skip "public registry ($REGISTRY) — no pull secret"
fi
echo; echo "Next:  ./20-storage.sh"
