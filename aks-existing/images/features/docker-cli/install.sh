#!/usr/bin/env bash
# docker CLI + buildx + compose. There is no daemon in a standard workspace: point
# DOCKER_HOST at a remote daemon/BuildKit, or use a lab-tier dind sidecar.
set -Eeuo pipefail; . /opt/workspace/lib.sh
install -m 0755 -d /etc/apt/keyrings
fetch https://download.docker.com/linux/ubuntu/gpg /tmp/docker.asc && gpg --dearmor -o /etc/apt/keyrings/docker.gpg </tmp/docker.asc && rm -f /tmp/docker.asc
echo "deb [arch=$ARCH signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" >/etc/apt/sources.list.d/docker.list
rm -f /var/lib/workspace/apt-updated
apt_install docker-ce-cli docker-buildx-plugin docker-compose-plugin
