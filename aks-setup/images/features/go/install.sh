#!/usr/bin/env bash
# Latest stable Go under /usr/local/go; GOPATH and module/build caches in $HOME.
set -Eeuo pipefail; . /opt/workspace/lib.sh
: "${GO_VERSION:=$(curl -fsSL 'https://go.dev/VERSION?m=text' | awk 'NR==1')}"   # first line, e.g. go1.25.3 (awk reads all input: no SIGPIPE under pipefail)
case "$GO_VERSION" in go[0-9]*) ;; *) echo "could not determine the Go version (got '$GO_VERSION'); set GO_VERSION=go1.xx.y" >&2; exit 1 ;; esac
if [ ! -x /usr/local/go/bin/go ] || ! /usr/local/go/bin/go version | grep -q "$GO_VERSION"; then
  fetch "https://go.dev/dl/${GO_VERSION}.linux-${ARCH}.tar.gz" /tmp/go.tgz
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm -f /tmp/go.tgz
fi
ln -sf /usr/local/go/bin/go /usr/local/bin/go
ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
profile_env go \
  'export GOPATH="$HOME/go" GOMODCACHE="$HOME/go/pkg/mod" GOCACHE="$HOME/.cache/go-build" GOTOOLCHAIN=auto' \
  'export PATH="$HOME/go/bin:/usr/local/go/bin:$PATH"'
