#!/usr/bin/env bash
# Shared entrypoint for all developer images. Baked into the image at /opt/dev-pod/entrypoint.sh.
# /home/dev is a PVC: it survives stop/start. Everything else is rebuilt from the image.
set -eu
echo "[dev-pod] workspace=${WORKSPACE_NAME:-?} runtime=${WORKSPACE_RUNTIME:-?} owner=${WORKSPACE_OWNER:-?} image=${DEV_POD_IMAGE:-?}"

mkdir -p /home/dev/workspace /home/dev/.ssh
chmod 700 /home/dev/.ssh
if [ ! -f /home/dev/.first-boot-done ]; then
  echo "[dev-pod] first boot: seeding /home/dev from /opt/dev-pod/skel"
  cp -rn /opt/dev-pod/skel/. /home/dev/ 2>/dev/null || true
  touch /home/dev/.first-boot-done
fi

# optional one-time clone
if [ -n "${WORKSPACE_REPO:-}" ]; then
  dir="/home/dev/workspace/$(basename "${WORKSPACE_REPO%.git}")"
  if [ ! -d "$dir" ]; then
    echo "[dev-pod] cloning ${WORKSPACE_REPO} -> ${dir}"
    git clone "${WORKSPACE_REPO}" "$dir" || echo "[dev-pod] clone failed (continuing)"
  fi
fi

# optional sshd (enable with WORKSPACE_SSHD=1; keys in /home/dev/.ssh/authorized_keys)
if [ "${WORKSPACE_SSHD:-0}" = "1" ] && command -v sshd >/dev/null 2>&1; then
  mkdir -p /tmp/sshd && [ -f /tmp/sshd/host_key ] || ssh-keygen -q -t ed25519 -N "" -f /tmp/sshd/host_key
  /usr/sbin/sshd -f /opt/dev-pod/sshd_config -h /tmp/sshd/host_key -p "${WORKSPACE_SSH_PORT:-2222}" -E /tmp/sshd/log
  echo "[dev-pod] sshd listening on :${WORKSPACE_SSH_PORT:-2222}"
fi

echo "[dev-pod] tools:"
for c in git curl jq make python3 pip java mvn gradle node; do
  command -v "$c" >/dev/null 2>&1 && printf '  %-8s %s\n' "$c" "$("$c" --version 2>&1 | head -1)" || true
done
echo "[dev-pod] ready — kubectl exec -it $(hostname) -n ${POD_NAMESPACE:-default} -- bash"
exec tail -f /dev/null
