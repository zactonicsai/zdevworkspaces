#!/usr/bin/env bash
# Smoke test the running stack (stub or kube profile):
#   anonymous call refused → token from Keycloak (password grant, client devspaces-smoke, demo realm only)
#   → identity and groups mapped → profile saved → workspace created and deleted as alice.
#   ./test.sh [user] [password]        defaults: alice@example.com / alice
set -Eeuo pipefail
KC=${KC:-http://localhost:8180}; PORTAL=${PORTAL:-http://localhost:8080}
USER_=${1:-alice@example.com}; PASS=${2:-alice}
ok()   { printf '  \033[32m[ok]\033[0m   %s\n' "$*"; }
fail() { printf '  \033[31m[fail]\033[0m %s\n' "$*"; exit 1; }
api()  { curl -sS -H "Authorization: Bearer $TOKEN" "$@"; }

code=$(curl -s -o /dev/null -w '%{http_code}' "$PORTAL/api/me")
[ "$code" = 401 ] && ok "anonymous /api/me → 401 (login required)" || fail "anonymous /api/me → $code (expected 401)"

TOKEN=$(curl -fsS -X POST "$KC/realms/devspaces/protocol/openid-connect/token" \
  -d grant_type=password -d client_id=devspaces-smoke -d "username=$USER_" -d "password=$PASS" -d "scope=openid profile email" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])') || fail "could not get a token from Keycloak ($KC)"
ok "access token for $USER_"
python3 - "$TOKEN" <<'PY'
import sys, json, base64
p = sys.argv[1].split(".")[1]; p += "=" * (-len(p) % 4); c = json.loads(base64.urlsafe_b64decode(p))
aud = c.get("aud"); aud = aud if isinstance(aud, list) else [aud]
print(f"  token: iss={c.get('iss')} aud={aud} user={c.get('preferred_username')} groups={c.get('groups')}")
assert str(c.get("iss", "")).endswith("/realms/devspaces"), "unexpected issuer"
assert "devspaces-api" in aud, "audience devspaces-api missing (client scope devspaces-api not applied)"
PY

api "$PORTAL/api/me" | python3 -c '
import json, sys
m = json.load(sys.stdin); u = m["user"]
assert m.get("auth") == "bearer", m
print("  me:", u["name"], "short=" + u["short"], "groups=%s" % u.get("groups"), "namespace=%s" % m.get("namespace"))
'
ok "API maps the token to the developer (auth=bearer)"

api "$PORTAL/api/catalog" | python3 -c '
import json, sys
c = json.load(sys.stdin)
print("  catalog: %d images from %s" % (len(c.get("images", [])), c.get("registry")))
'

api -X PUT -H 'Content-Type: application/json' "$PORTAL/api/profile" \
  -d '{"sshPublicKeys":["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGm7gPZQ0cYQhh3v4u5n9Vd2D5rR0f7w3q1Yx2cZbWvK smoke@test"],"gitName":"Smoke"}' >/dev/null \
  && ok "profile saved"

out=$(api -X POST -H 'Content-Type: application/json' "$PORTAL/api/workspaces" \
  -d '{"name":"smoke","languages":["go"],"gitAuth":"none","idleTimeout":"30m"}')
echo "$out" | python3 -c 'import json,sys; j=json.load(sys.stdin); assert j.get("name")=="smoke", j' 2>/dev/null \
  && ok "workspace 'smoke' created as $USER_" || fail "create failed: $out"
api "$PORTAL/api/workspaces/smoke" | python3 -c '
import json, sys
w = json.load(sys.stdin)["workspace"]
print("  smoke: status=%s drive=%s dns=%s" % (w.get("status"), w.get("drive"), w.get("dns")))
'
api -X DELETE "$PORTAL/api/workspaces/smoke?purge=true" >/dev/null && ok "workspace deleted (purged)"
echo; echo "all good — open $PORTAL and sign in as $USER_ / $PASS"
