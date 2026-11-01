# shellcheck shell=bash
# Helpers for feature installers. Source it: . /opt/workspace/lib.sh
# A feature runs as root, either at image build time (langs/Dockerfile) or at
# pod boot (`--tools`), and must be idempotent. Never write to /home/dev.
export DEBIAN_FRONTEND=noninteractive

ARCH="$(dpkg --print-architecture)"                 # amd64 | arm64
case "$ARCH" in
  amd64) ARCH_ALT=x86_64;  NODE_ARCH=x64   ;;
  arm64) ARCH_ALT=aarch64; NODE_ARCH=arm64 ;;
  *)     ARCH_ALT=$ARCH;   NODE_ARCH=$ARCH ;;
esac
export ARCH ARCH_ALT NODE_ARCH

# apt_install PKG... — `apt-get update` at most every 2h, then install.
apt_install() {
  local stamp=/var/lib/workspace/apt-updated
  if [ ! -e "$stamp" ] || [ -n "$(find "$stamp" -mmin +120 2>/dev/null)" ]; then
    apt-get update -qq
    mkdir -p "$(dirname "$stamp")" && touch "$stamp"
  fi
  apt-get install -y -qq --no-install-recommends "$@"
}

# profile_env NAME 'export FOO=bar' ... — environment every shell (login or
# not, including VS Code terminals) will source from /etc/profile.d/workspace-NAME.sh.
profile_env() {
  local name=$1; shift
  printf '%s\n' "$@" >"/etc/profile.d/workspace-$name.sh"
  chmod 644 "/etc/profile.d/workspace-$name.sh"
}

# seed_dir SRC DST — ask the entrypoint to copy SRC into the persistent home
# (DST under /home/dev) the first time DST is missing.
seed_dir() {
  mkdir -p /opt/workspace/seed.d
  printf '%s %s\n' "$1" "$2" >>"/opt/workspace/seed.d/${FEATURE_NAME:-misc}"
}

# fetch URL DEST — download, rewriting well-known public hosts to the Artifactory
# generic remote when ARTIFACTORY_URL/ARTIFACTORY_REPOS are set (air-gapped clusters).
# Create one generic remote per host (e.g. go-dev-remote → https://go.dev) and map
# it in ARTIFACTORY_MIRRORS='{"go.dev":"go-dev-remote","nodejs.org":"nodejs-remote",…}'.
fetch() {
  local url=$1 dest=$2 host repo
  if [ -n "${ARTIFACTORY_URL:-}" ] && [ -n "${ARTIFACTORY_MIRRORS:-}" ]; then
    host=$(printf '%s' "$url" | sed -E 's#^[a-z]+://##; s#/.*##')
    repo=$(printf '%s' "$ARTIFACTORY_MIRRORS" | jq -r --arg h "$host" '.[$h] // empty')
    if [ -n "$repo" ]; then
      url="$ARTIFACTORY_URL/$repo/${url#*://*/}"
    fi
  fi
  curl -fsSL --retry 3 --retry-delay 2 ${ARTIFACTORY_TOKEN:+-H "Authorization: Bearer $ARTIFACTORY_TOKEN"} "$url" -o "$dest"
}

github_latest_tag() { curl -fsSL "https://api.github.com/repos/$1/releases/latest" | jq -r .tag_name; }
