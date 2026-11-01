#!/usr/bin/env bash
# Node.js LTS from the official tarball + corepack (pnpm/yarn). Globals install into ~/.npm-global.
set -Eeuo pipefail; . /opt/workspace/lib.sh
: "${NODE_VERSION:=$(curl -fsSL https://nodejs.org/dist/index.json | jq -r '[.[] | select(.lts != false)][0].version')}"
if ! command -v node >/dev/null || ! node --version | grep -q "^${NODE_VERSION}$"; then
  fetch "https://nodejs.org/dist/${NODE_VERSION}/node-${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz" /tmp/node.txz
  tar -C /usr/local --strip-components=1 -xJf /tmp/node.txz --exclude='*/CHANGELOG.md' --exclude='*/README.md' --exclude='*/LICENSE'
  rm -f /tmp/node.txz
fi
corepack enable || true
profile_env node \
  'export NPM_CONFIG_PREFIX="$HOME/.npm-global" npm_config_cache="$HOME/.npm" COREPACK_HOME="$HOME/.cache/corepack"' \
  'export PATH="$HOME/.npm-global/bin:$PATH"'
