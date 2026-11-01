# syntax=docker/dockerfile:1.7
# workspace-base — everything a workspace needs that is NOT a language runtime:
# sshd, git, sudo, compilers, the entrypoint and the feature installers.
#
# Build context is images/ (see build.sh). Language images are built FROM this
# image by running one feature installer (langs/Dockerfile).
#
# RULE FOR EVERY LAYER: never write to /home/dev. At runtime it is shadowed by
# the developer's persistent volume. Toolchains go to /usr/local or /opt and
# their user-facing state (caches, GOPATH, ~/.cargo, ~/.npm-global) lives in
# $HOME via /etc/profile.d/workspace-*.sh exports.
FROM ubuntu:24.04

ARG DEV_UID=1000
ARG DEV_GID=1000
ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Keep downloaded .debs: at runtime /var/cache/apt/archives is redirected onto the
# persistent volume, so re-installing tools after a restart is fast and offline-capable.
RUN rm -f /etc/apt/apt.conf.d/docker-clean \
 && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/90workspace-keep-debs

RUN apt-get update && apt-get install -y --no-install-recommends \
      tini openssh-server openssh-client sudo git git-lfs curl wget ca-certificates gnupg \
      socat rsync build-essential pkg-config locales tzdata util-linux \
      bash-completion less nano vim-tiny jq unzip zip xz-utils file \
      procps psmisc htop tmux iproute2 iputils-ping net-tools tcpdump dnsutils \
 && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*.deb

# ubuntu:24.04 ships a user 'ubuntu' at uid 1000; replace it with 'dev' (passwordless sudo).
RUN userdel -r ubuntu 2>/dev/null || true \
 && groupadd -g "${DEV_GID}" dev \
 && useradd -m -u "${DEV_UID}" -g dev -s /bin/bash dev \
 && usermod -p '*' dev \
 && echo 'dev ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/dev && chmod 440 /etc/sudoers.d/dev \
 && ln -s /home/dev/workspace /workspace \
 && mkdir -p /run/sshd /var/lib/workspace/features /var/log/workspace /opt/workspace/seed.d

COPY runtime/   /opt/workspace/
COPY features/  /opt/workspace/features/
RUN chmod 755 /opt/workspace/entrypoint.sh /opt/workspace/bin/* \
 && find /opt/workspace/features -type f -name '*.sh' -exec chmod 755 {} + \
 # Pre-seed host keys of the big forges so first `git clone` is not a TOFU prompt (best effort; build may be offline).
 && (ssh-keyscan -t ed25519 github.com gitlab.com bitbucket.org 2>/dev/null >> /etc/ssh/ssh_known_hosts || true)

ENV PATH=/opt/workspace/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    WORKSPACE_IMAGE=base
EXPOSE 2222
# tini reaps zombies (sshd sessions, build daemons); the entrypoint supervises sshd.
ENTRYPOINT ["/usr/bin/tini", "--", "/opt/workspace/entrypoint.sh"]
