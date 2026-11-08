#!/usr/bin/env bash
# Moved: the Azure cluster layer now lives in deploy/cluster/providers/azure/ (numbered, idempotent,
# --dry-run capable). Step-by-step: docs/azure-aks-setup.md.
exec "$(dirname "$0")/azure/10-aks.sh" "$@"
