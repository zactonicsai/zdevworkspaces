#!/usr/bin/env bash
# Latest stable Go under /usr/local/go; GOPATH and module/build caches in $HOME.
set -Eeuo pipefail; . /opt/workspace/lib.sh
: "${GO_VERSION:=$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -1)}"
if [ ! -x /usr/local/go/bin/go ] || ! /usr/local/go/bin/go version | grep -q "$GO_VERSION"; then
  fetch "https://go.dev/dl/${GO_VERSION}.linux-${ARCH}.tar.gz" /tmp/go.tgz
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm -f /tmp/go.tgz
fi
ln -sf /usr/local/go/bin/go /usr/local/bin/go
ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
profile_env go \
  'export GOPATH="$HOME/go" GOMODCACHE="$HOME/go/pkg/mod" GOCACHE="$HOME/.cache/go-build" GOTOOLCHAIN=auto' \
  'export PATH="$HOME/go/bin:/usr/local/go/bin:$PATH"'
