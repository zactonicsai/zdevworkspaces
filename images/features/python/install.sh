#!/usr/bin/env bash
# Python 3 (Ubuntu's current python3) + pip + venv + uv. User packages and venvs live in $HOME.
set -Eeuo pipefail; . /opt/workspace/lib.sh
apt_install python3 python3-pip python3-venv python3-dev pipx
ln -sf /usr/bin/python3 /usr/local/bin/python
fetch https://astral.sh/uv/install.sh /tmp/uv.sh && UV_INSTALL_DIR=/usr/local/bin UV_NO_MODIFY_PATH=1 sh /tmp/uv.sh && rm -f /tmp/uv.sh
profile_env python \
  'export PIP_USER=1 PIP_BREAK_SYSTEM_PACKAGES=1' \
  'export PIPX_HOME="$HOME/.local/pipx" PIPX_BIN_DIR="$HOME/.local/bin"' \
  'export UV_CACHE_DIR="$HOME/.cache/uv" UV_TOOL_BIN_DIR="$HOME/.local/bin"' \
  'export PYTHONDONTWRITEBYTECODE=1'
