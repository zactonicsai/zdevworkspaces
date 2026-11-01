#!/usr/bin/env bash
# Creates deploy/demo/ssh/ with what a workspace container needs when it runs under
# plain Docker (Level 1 of docs/demo-docker.md): a host key pair and an
# authorized_keys file holding YOUR public key. In Kubernetes the CLI/portal
# generate these into a Secret; here we do it by hand.
set -Eeuo pipefail
here=$(cd "$(dirname "$0")" && pwd)
out="$here/ssh"
mkdir -p "$out"
pub="${1:-$HOME/.ssh/id_ed25519.pub}"
if [ ! -f "$pub" ]; then
  echo "No public key at $pub."
  echo "Create one with:  ssh-keygen -t ed25519 -C \"you@example.com\"   (press Enter for the default file)"
  echo "or pass the path to a .pub file as the first argument."
  exit 1
fi
if [ ! -f "$out/ssh_host_ed25519_key" ]; then
  ssh-keygen -q -t ed25519 -N "" -C "host:demo-workspace" -f "$out/ssh_host_ed25519_key"
  echo "generated host key $out/ssh_host_ed25519_key"
fi
cp "$pub" "$out/authorized_keys"
chmod 644 "$out/authorized_keys" "$out/ssh_host_ed25519_key.pub"
chmod 600 "$out/ssh_host_ed25519_key"
echo "authorized_keys now contains: $(cut -c1-40 "$pub")… ($(awk '{print $3}' "$pub"))"
echo "host key fingerprint: $(ssh-keygen -lf "$out/ssh_host_ed25519_key.pub" | awk '{print $2}')"
echo
echo "Next:  docker compose -f $here/docker-compose.yaml up -d"
