#!/usr/bin/env bash
# Phase 4 — onboard developers (like enrolling students: each gets a locker = namespace).
#   ./deploy/prod/30-onboard.sh users.csv        # lines: email[,lab]   e.g. alice@corp.example,lab
#   ./deploy/prod/30-onboard.sh --group eng-platform --lab   # bind a whole OIDC group to one shared namespace
# Re-running is safe (kubectl apply). Quotas: CPU/MEM/STORAGE env override the defaults.
set -Eeuo pipefail
cd "$(dirname "$0")/../.."
CLI=./cli/workspace; command -v workspace >/dev/null && CLI=workspace
if [ "${1:-}" = --group ]; then $CLI admin onboard "$2" --group ${3:-} --cpu "${CPU:-64}" --memory "${MEM:-256Gi}" --storage "${STORAGE:-2Ti}"; exit 0; fi
file=${1:?usage: 30-onboard.sh users.csv | --group NAME [--lab]}
n=0
while IFS=, read -r email flag _; do
  email=$(echo "$email" | tr -d ' \r'); [ -z "$email" ] || [ "${email#\#}" != "$email" ] && continue
  lab=""; [ "$(echo "${flag:-}" | tr -d ' \r')" = lab ] && lab=--lab
  echo "== $email $lab"
  $CLI admin onboard "$email" $lab --cpu "${CPU:-16}" --memory "${MEM:-64Gi}" --storage "${STORAGE:-500Gi}"
  n=$((n+1))
done < "$file"
echo "onboarded $n developer(s). They sign in at https://${PORTAL_HOST:-<portal>} or run: workspace create --lang <lang>"
