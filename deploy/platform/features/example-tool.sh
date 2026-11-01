#!/usr/bin/env bash
# Example hot-added feature. Admins put installers in the `workspace-features`
# ConfigMap (deploy/platform/kustomization.yaml); the file name minus .sh is the
# tool name developers pass to --tools, after adding it to catalog.json "tools".
# Runs as root at pod boot; must be idempotent; never write to /home/dev.
set -Eeuo pipefail
. /opt/workspace/lib.sh
apt_install shellcheck bats
profile_env example-tool 'export EXAMPLE_TOOL=1'
