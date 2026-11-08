#!/usr/bin/env bash
# End-to-end check against the deployed API: create a Python pod, wait until Running, exec, read the
# audit log, delete it.  Uses a temporary port-forward.
#   scripts/smoke-test.sh [runtime] [size]
source "$(dirname "$0")/lib.sh"
need kubectl curl python3
RT="${1:-python312}"; SZ="${2:-small}"; NAME="smoke-$(date +%s | tail -c 5)"
LP=18080

kubectl -n "$NAMESPACE" port-forward "svc/${RELEASE}" "${LP}:80" >/dev/null 2>&1 &
PF=$!; trap 'kill $PF 2>/dev/null || true' EXIT
for _ in $(seq 1 30); do curl -sf "http://localhost:${LP}/api/health" >/dev/null && break; sleep 0.5; done
B="http://localhost:${LP}"

info "health";   curl -sf "$B/api/health" | python3 -m json.tool
info "catalog";  curl -sf "$B/api/catalog" | python3 -c "import sys,json;d=json.load(sys.stdin);[print(f'  {k:10} {v[\"image\"]}') for k,v in d['runtimes'].items()]"

info "create $NAME ($RT, $SZ)"
curl -sf -X POST "$B/api/pods" -H 'content-type: application/json' -d "{\"name\":\"$NAME\",\"runtime\":\"$RT\",\"size\":\"$SZ\",\"owner\":\"smoke\"}" | python3 -m json.tool

info "waiting for Running (up to 5 min; first pull of the image can be slow)"
for i in $(seq 1 100); do
  ST="$(curl -sf "$B/api/pods/$NAME" | python3 -c "import sys,json;print(json.load(sys.stdin)['status'])")"
  printf '  %3ds  %s\n' $((i*3)) "$ST"
  [ "$ST" = "Running" ] && break
  sleep 3
done
[ "$ST" = "Running" ] || { warn "not Running; pod events:"; curl -s "$B/api/pods/$NAME/events" | python3 -m json.tool; kubectl -n "$NAMESPACE" describe pod "ws-$NAME-0" | tail -20; exit 1; }

info "exec"
for cmd in "whoami" "python3 --version" "java -version" "git --version" "cat ~/README-workspace.md"; do
  curl -sf -X POST "$B/api/pods/$NAME/exec" -H 'content-type: application/json' -d "{\"command\":\"$cmd\"}" \
    | python3 -c "import sys,json;r=json.load(sys.stdin);print(f'  \$ {r[\"command\"]}\n    exit={r[\"exit_code\"]} {(r[\"stdout\"] or r[\"stderr\"]).strip().splitlines()[0] if (r[\"stdout\"] or r[\"stderr\"]).strip() else \"\"}')"
done

info "pod log"; curl -sf "$B/api/pods/$NAME/logs?tail=20" | python3 -c "import sys,json;print(json.load(sys.stdin)['logs'])"

info "stop / start"
curl -sf -X POST "$B/api/pods/$NAME/stop" >/dev/null && ok "stopped (PVC kept)"
curl -sf -X POST "$B/api/pods/$NAME/start" >/dev/null && ok "start requested"

info "delete (purge volume)"
curl -sf -X DELETE "$B/api/pods/$NAME?purge=true" | python3 -m json.tool

info "audit trail for $NAME (what the API actually ran)"
curl -sf "$B/api/audit?workspace=$NAME" | python3 -c "
import sys,json
for a in reversed(json.load(sys.stdin)['items']): print(f'  #{a[\"id\"]:<4}{a[\"tool\"]:8}{a[\"action\"]:14}exit={a[\"exit_code\"]} {a[\"duration_ms\"]:>7}ms  \$ {a[\"cmd\"][:110]}')"
ok "smoke test passed"
