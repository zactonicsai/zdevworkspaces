#!/usr/bin/env bash
# git is part of the base image; this exists so `--tools git` is a no-op that adds the extras.
set -Eeuo pipefail; . /opt/workspace/lib.sh
apt_install git git-lfs tig
git lfs install --system --skip-repo
