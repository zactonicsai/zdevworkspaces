#!/usr/bin/env bash
# Start a developer pod from the command line, going through the API (so it is audited) — handy for
# automation or when you don't want to open the portal.
#   scripts/create-pod.sh NAME [runtime=python312] [size=medium] [owner=$USER] [repo-url]
#   API=https://devpods.example.com scripts/create-pod.sh alice-svc java21 large alice
source "$(dirname "$0")/lib.sh"
need curl python3
NAME="${1:?usage: create-pod.sh NAME [runtime] [size] [owner] [repo]}"; RT="${2:-python312}"; SZ="${3:-medium}"; OWNER="${4:-${USER:-dev}}"; REPO="${5:-}"

if [ -n "${API:-}" ]; then B="$API"; PF=""
else
  need kubectl; LP=18081
  kubectl -n "$NAMESPACE" port-forward "svc/${RELEASE}" "${LP}:80" >/dev/null 2>&1 & PF=$!; trap 'kill $PF 2>/dev/null || true' EXIT
  for _ in $(seq 1 30); do curl -sf "http://localhost:${LP}/api/health" >/dev/null && break; sleep 0.5; done
  B="http://localhost:${LP}"
fi

info "POST $B/api/pods  name=$NAME runtime=$RT size=$SZ owner=$OWNER${REPO:+ repo=$REPO}"
BODY="$(python3 -c "import json,sys;print(json.dumps({'name':sys.argv[1],'runtime':sys.argv[2],'size':sys.argv[3],'owner':sys.argv[4],'repo':sys.argv[5] or None}))" "$NAME" "$RT" "$SZ" "$OWNER" "$REPO")"
curl -sf -X POST "$B/api/pods" -H 'content-type: application/json' -d "$BODY" | python3 -m json.tool
echo
echo "  watch:  kubectl -n $NAMESPACE get pod ws-$NAME-0 -w"
echo "  shell:  kubectl -n $NAMESPACE exec -it ws-$NAME-0 -- bash"
