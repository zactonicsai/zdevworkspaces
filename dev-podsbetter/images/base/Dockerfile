# Common layer for every developer image: Ubuntu + everyday tools + the `dev` user.
#   docker build -f images/base/Dockerfile -t REGISTRY/dev-pod-base:24.04 images   (context = images/, it copies common/)
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive LANG=C.UTF-8
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget git git-lfs openssh-client openssh-server sudo \
      bash-completion vim nano less jq unzip zip tar gzip make build-essential \
      ripgrep fd-find tmux htop iproute2 iputils-ping dnsutils netcat-openbsd procps tini \
 && ln -s /usr/bin/fdfind /usr/local/bin/fd \
 && rm -rf /var/lib/apt/lists/*

# dev user, uid 1000 (matches fsGroup in the chart); passwordless sudo for one-off apt installs
RUN userdel -r ubuntu 2>/dev/null || true \
 && useradd -m -u 1000 -s /bin/bash dev \
 && echo "dev ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/dev && chmod 440 /etc/sudoers.d/dev \
 && mkdir -p /run/sshd

COPY common/entrypoint.sh common/sshd_config /opt/dev-pod/
COPY common/skel /opt/dev-pod/skel
RUN chmod 755 /opt/dev-pod/entrypoint.sh

USER dev
WORKDIR /home/dev
ENTRYPOINT ["/usr/bin/tini", "--", "/opt/dev-pod/entrypoint.sh"]
