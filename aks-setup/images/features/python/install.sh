#!/usr/bin/env bash
# Python 3 (Ubuntu's current python3) + pip + venv + uv. User packages and venvs live in $HOME.
set -Eeuo pipefail; . /opt/workspace/lib.sh
apt_install python3 python3-pip python3-venv python3-dev pipx
ln -sf /usr/bin/python3 /usr/local/bin/python
# uv from PyPI (a wheel with the static binary): deterministic location, no vendor installer.
python3 -m pip install --break-system-packages --no-cache-dir --root-user-action=ignore --upgrade uv
command -v uv >/dev/null || ln -sf "$(python3 -c 'import sysconfig;print(sysconfig.get_path("scripts"))')/uv" /usr/local/bin/uv
profile_env python \
  'export PIP_USER=1 PIP_BREAK_SYSTEM_PACKAGES=1' \
  'export PIPX_HOME="$HOME/.local/pipx" PIPX_BIN_DIR="$HOME/.local/bin"' \
  'export UV_CACHE_DIR="$HOME/.cache/uv" UV_TOOL_BIN_DIR="$HOME/.local/bin"' \
  'export PYTHONDONTWRITEBYTECODE=1'
