#!/usr/bin/env bash
# Workspace container entrypoint. Runs as root — uid 0 of the pod's user
# namespace when hostUsers=false — under tini.
#
# Boot order is deliberate: sshd is listening within a second or two so VS Code
# can attach while the slower steps (tool installs, git clone) keep running.
# Everything here is idempotent: the pod is recreated on every start.
set -Eeuo pipefail

WS_DIR=/opt/workspace
DEV_USER=dev
DEV_HOME=/home/dev
LOG=/var/log/workspace
STATE=/var/lib/workspace
SSHD_CFG=/etc/ssh/sshd_config.workspace
SSHD_PID=""

mkdir -p "$LOG" "$STATE/features" /run/sshd
log()    { printf '[workspace %s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
as_dev() { runuser -u "$DEV_USER" -- env HOME="$DEV_HOME" USER="$DEV_USER" LOGNAME="$DEV_USER" "$@"; }

# managed_block FILE ID CONTENT — replace or append a marker-delimited block.
managed_block() {
  local file=$1 id=$2 content=$3 tmp
  tmp=$(mktemp)
  if [ -f "$file" ]; then
    sed "/^# >>> $id >>>$/,/^# <<< $id <<<$/d" "$file" >"$tmp"
  fi
  printf '# >>> %s >>>\n%s\n# <<< %s <<<\n' "$id" "$content" "$id" >>"$tmp"
  mv "$tmp" "$file"
}

# ---------------------------------------------------------------- 1. home
seed_home() {
  if [ ! -e "$DEV_HOME/.workspace/initialized" ]; then
    log "first boot: seeding $DEV_HOME from $WS_DIR/skel"
    mkdir -p "$DEV_HOME"
    cp -rT "$WS_DIR/skel" "$DEV_HOME"
    mkdir -p "$DEV_HOME/workspace" "$DEV_HOME/.ssh" "$DEV_HOME/.workspace" "$DEV_HOME/.cache"
    chown -R "$DEV_USER:$DEV_USER" "$DEV_HOME"
    chmod 700 "$DEV_HOME/.ssh"
    date -u +%FT%TZ >"$DEV_HOME/.workspace/initialized"
  fi
  chown "$DEV_USER:$DEV_USER" "$DEV_HOME"

  # apt keeps its downloaded .debs on the volume → tool re-installs are fast and work offline.
  mkdir -p "$DEV_HOME/.cache/apt-archives/partial"
  rm -rf /var/cache/apt/archives
  ln -s "$DEV_HOME/.cache/apt-archives" /var/cache/apt/archives
  chown _apt:root "$DEV_HOME/.cache/apt-archives/partial" 2>/dev/null || true
  return 0
}

# Directories registered by image features (e.g. the rustup toolchain) are copied
# into the persistent home the first time they are missing, so `rustup toolchain
# add …` or `cargo install` survive restarts. Runs after sshd is up: it can take
# a minute on a fresh volume and only blocks the tool it seeds.
seed_toolchains() {
  local f src dst
  for f in "$WS_DIR"/seed.d/*; do
    [ -f "$f" ] || continue
    while read -r src dst; do
      if [ -n "$src" ] && [ -d "$src" ] && [ ! -e "$dst" ]; then
        log "seeding $dst from $src (first boot only)"
        cp -a "$src" "$dst.partial" && chown -R "$DEV_USER:$DEV_USER" "$dst.partial" && mv "$dst.partial" "$dst"
      fi
    done <"$f"
  done
  return 0
}

# Make WORKSPACE_* visible in SSH sessions (sshd does not pass its own environment on).
write_env() {
  local v
  {
    for v in WORKSPACE_NAME WORKSPACE_NAMESPACE WORKSPACE_LANG WORKSPACE_LANGUAGES WORKSPACE_PROFILE WORKSPACE_TOOLS \
             WORKSPACE_MCAST_IFACE WORKSPACE_REPO WORKSPACE_REPO_DIR GIT_HOST TZ; do
      if [ -n "${!v:-}" ]; then printf 'export %s=%q\n' "$v" "${!v}"; fi
    done
  } >/etc/profile.d/workspace-env.sh
  return 0
}

# ---------------------------------------------------------------- 2. sshd
start_sshd() {
  local src=/etc/workspace/ssh listen="${WORKSPACE_SSH_LISTEN:-127.0.0.1}"
  # The host key must be a root-owned 0600 regular file; the Secret mount is 0440, so copy it.
  # authorized_keys is read straight from the mount (see sshd_config).
  install -m 600 "$src/ssh_host_ed25519_key"     /etc/ssh/ssh_host_ed25519_key
  install -m 644 "$src/ssh_host_ed25519_key.pub" /etc/ssh/ssh_host_ed25519_key.pub
  install -d -m 755 /etc/ssh/authorized_keys.d
  install -m 644 "$src/authorized_keys" "/etc/ssh/authorized_keys.d/$DEV_USER"
  sed "s/__LISTEN__/$listen/" "$WS_DIR/sshd_config" >"$SSHD_CFG"
  /usr/sbin/sshd -t -f "$SSHD_CFG"
  /usr/sbin/sshd -D -e -f "$SSHD_CFG" >>"$LOG/sshd.log" 2>&1 &
  SSHD_PID=$!
  log "sshd listening on $listen:2222 (pid $SSHD_PID)"
  return 0
}

# ---------------------------------------------------------------- 3. git
setup_git() {
  if [ -n "${GIT_USER_NAME:-}" ] && ! as_dev git config --global user.name >/dev/null 2>&1; then
    as_dev git config --global user.name "$GIT_USER_NAME"
  fi
  if [ -n "${GIT_USER_EMAIL:-}" ] && ! as_dev git config --global user.email >/dev/null 2>&1; then
    as_dev git config --global user.email "$GIT_USER_EMAIL"
  fi
  case "${GIT_AUTH_MODE:-none}" in
    ssh)
      # The key lives in the (ephemeral) container filesystem, not on the volume:
      # rotating the Secret takes effect on the next start, and a volume snapshot
      # never contains credentials. ssh insists on a 0600 file owned by the user,
      # which a Secret mount cannot provide.
      install -d -m 700 -o "$DEV_USER" -g "$DEV_USER" /run/workspace
      install -m 600 -o "$DEV_USER" -g "$DEV_USER" /etc/workspace/git/ssh_key /run/workspace/git_key
      managed_block "$DEV_HOME/.ssh/config" "workspace-git" "$(printf '%s\n' \
        "Host ${GIT_HOST:-*}" \
        "    IdentityFile /run/workspace/git_key" \
        "    IdentitiesOnly yes" \
        "    StrictHostKeyChecking accept-new")"
      chown "$DEV_USER:$DEV_USER" "$DEV_HOME/.ssh/config"
      chmod 600 "$DEV_HOME/.ssh/config"
      ;;
    token)
      # The helper reads the mounted Secret at use time; nothing lands in ~/.git-credentials.
      if [ -n "${GIT_HOST:-}" ]; then
        as_dev git config --global "credential.https://${GIT_HOST}.helper" "$WS_DIR/bin/git-credential-workspace"
      else
        as_dev git config --global credential.helper "$WS_DIR/bin/git-credential-workspace"
      fi
      ;;
  esac
  return 0
}

# ---------------------------------------------------------------- 4. tools
# Package managers go through Artifactory when the platform says so (ARTIFACTORY_URL).
setup_artifactory() {
  [ -n "${ARTIFACTORY_URL:-}" ] || return 0
  log "configuring package managers for Artifactory at $ARTIFACTORY_URL"
  "$WS_DIR/bin/configure-artifactory" >>"$LOG/artifactory.log" 2>&1 || log "configure-artifactory reported errors (see $LOG/artifactory.log)"
  return 0
}

# Two kinds of tool: scripted features (features/<name>/install.sh, or hot-added
# features.d/<name>.sh) and catalog artifacts downloaded from Artifactory
# ($WORKSPACE_ARTIFACTS, a JSON array). Languages beyond the image's own arrive
# here too (`--lang go,python` → image go + feature python).
install_tools() {
  local t list spec
  if [ -n "${WORKSPACE_TOOLS:-}" ]; then
    IFS=',' read -r -a list <<<"$WORKSPACE_TOOLS"
    for t in "${list[@]}"; do
      t="${t//[[:space:]]/}"
      [ -n "$t" ] || continue
      if [ -e "$STATE/features/$t.done" ]; then
        continue # baked into the image
      fi
      if [ ! -f "$WS_DIR/features/$t/install.sh" ] && [ ! -f "$WS_DIR/features.d/$t.sh" ]; then
        continue # an artifact tool, handled below
      fi
      log "installing tool: $t"
      if "$WS_DIR/bin/install-feature" "$t" >>"$LOG/features.log" 2>&1; then
        log "tool ready: $t"
      else
        log "tool FAILED: $t (see $LOG/features.log)"
      fi
    done
  fi
  if [ -n "${WORKSPACE_ARTIFACTS:-}" ] && [ "$WORKSPACE_ARTIFACTS" != "[]" ]; then
    while IFS= read -r spec; do
      [ -n "$spec" ] || continue
      t=$(printf '%s' "$spec" | jq -r .name)
      log "installing artifact tool: $t"
      if "$WS_DIR/bin/install-artifact" "$spec" >>"$LOG/features.log" 2>&1; then
        log "tool ready: $t"
      else
        log "tool FAILED: $t (see $LOG/features.log)"
      fi
    done < <(printf '%s' "$WORKSPACE_ARTIFACTS" | jq -c '.[]')
  fi
  return 0
}

# ---------------------------------------------------------------- 5. code
clone_repo() {
  [ -n "${WORKSPACE_REPO:-}" ] || return 0
  local dir="$DEV_HOME/workspace/${WORKSPACE_REPO_DIR:-repo}" args=()
  if [ -e "$dir/.git" ]; then
    log "repo already present at $dir (left untouched)"
    return 0
  fi
  if [ -n "${WORKSPACE_BRANCH:-}" ]; then args+=(--branch "$WORKSPACE_BRANCH"); fi
  log "cloning $WORKSPACE_REPO into $dir"
  if as_dev git clone "${args[@]}" "$WORKSPACE_REPO" "$dir" >>"$LOG/git.log" 2>&1; then
    log "clone complete"
  else
    log "clone FAILED (see $LOG/git.log) — is the deploy key / token authorised?"
  fi
  return 0
}

setup_dotfiles() {
  [ -n "${WORKSPACE_DOTFILES:-}" ] || return 0
  local dir="$DEV_HOME/.dotfiles" s
  if [ -e "$dir" ]; then return 0; fi
  log "installing dotfiles from $WORKSPACE_DOTFILES"
  if ! as_dev git clone --depth 1 "$WORKSPACE_DOTFILES" "$dir" >>"$LOG/git.log" 2>&1; then
    log "dotfiles clone failed (see $LOG/git.log)"
    return 0
  fi
  for s in install.sh setup.sh bootstrap.sh; do
    if [ -x "$dir/$s" ]; then
      (cd "$dir" && as_dev "./$s") >>"$LOG/dotfiles.log" 2>&1 || log "dotfiles $s exited non-zero"
      break
    fi
  done
  return 0
}

# ---------------------------------------------------------------- 6. idle
# Every minute: if an SSH session or an editor server is running, stamp the pod
# with devspaces.io/last-active. The reaper CronJob scales idle workspaces to 0.
heartbeat() {
  if ! pgrep -u "$DEV_USER" -f '^sshd(-session)?: ' >/dev/null \
     && ! pgrep -u "$DEV_USER" -f 'vscode-server|code-server|cursor-server|jetbrains' >/dev/null; then
    return 0
  fi
  local sa=/var/run/secrets/kubernetes.io/serviceaccount
  [ -r "$sa/token" ] || return 0
  curl -fsS --max-time 10 --cacert "$sa/ca.crt" \
    -H "Authorization: Bearer $(cat "$sa/token")" \
    -H 'Content-Type: application/merge-patch+json' -X PATCH \
    "https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT:-443}/api/v1/namespaces/${WORKSPACE_NAMESPACE}/pods/${POD_NAME}" \
    -d "{\"metadata\":{\"annotations\":{\"devspaces.io/last-active\":\"$(date -u +%FT%TZ)\"}}}" \
    >/dev/null 2>&1 || true
  return 0
}
heartbeat_loop() { while :; do heartbeat; sleep 60; done; }

# ---------------------------------------------------------------- main
main() {
  log "workspace '${WORKSPACE_NAME:-?}' starting: lang=${WORKSPACE_LANG:-?} profile=${WORKSPACE_PROFILE:-?} tools=${WORKSPACE_TOOLS:-none}"
  seed_home
  write_env
  start_sshd                      # from here on `ssh`/VS Code can connect
  heartbeat_loop &
  local hb=$!
  trap 'log "stopping"; kill -TERM "$SSHD_PID" "$hb" 2>/dev/null' TERM INT
  seed_toolchains
  setup_artifactory
  setup_git
  install_tools
  clone_repo
  setup_dotfiles
  log "ready"
  wait "$SSHD_PID" || true        # the container lives as long as sshd does
  kill "$hb" 2>/dev/null || true
}
main "$@"
