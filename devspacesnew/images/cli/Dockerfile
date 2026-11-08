# syntax=docker/dockerfile:1.7
# workspace-cli — the CLI plus kubectl, used by the idle reaper CronJob
# (`workspace admin reap -A`) and handy for CI (`workspace create --dry-run --offline`).
# Build context is the repository root: docker build -f images/cli/Dockerfile .
FROM python:3.12-slim
ARG KUBECTL_VERSION=stable
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates openssh-client \
 && rm -rf /var/lib/apt/lists/* \
 && if [ "$KUBECTL_VERSION" = stable ]; then KUBECTL_VERSION="$(curl -fsSL https://dl.k8s.io/release/stable.txt)"; fi \
 && curl -fsSL -o /usr/local/bin/kubectl \
      "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/$(dpkg --print-architecture)/kubectl" \
 && chmod 755 /usr/local/bin/kubectl \
 && useradd -r -u 10001 -s /usr/sbin/nologin reaper
COPY cli/workspace /usr/local/bin/workspace
RUN chmod 755 /usr/local/bin/workspace
USER 10001
ENTRYPOINT ["workspace"]
