# syntax=docker/dockerfile:1.7
# devspaces portal — Python API + the web UI, one image for the Helm chart.
# Build context is the repository root (the UI and the CLI are shared with the Go portal):
#   docker build -f portal-py/Dockerfile -t workspace-portal-py:dev .
#   ./portal-py/build.sh            (same, plus push/tag options)
#
# Stage 1 (optional): compile Tailwind; the committed web/tailwind.css is used with SKIP_CSS=1.
FROM node:22-alpine AS css
ARG SKIP_CSS=0
WORKDIR /src
COPY portal/package.json portal/tailwind.config.js ./
COPY portal/web ./web
RUN if [ "$SKIP_CSS" = "1" ]; then echo "using committed tailwind.css"; \
    else npm install --no-audit --no-fund && npm run build:css; fi

# Stage 2: runtime. python:slim + kubectl + openssh-client (ssh-keygen for host/deploy keys).
FROM python:3.12-slim AS runtime
ARG KUBECTL_VERSION=v1.36.4
ARG TARGETARCH=amd64
RUN apt-get update && apt-get install -y --no-install-recommends openssh-client ca-certificates curl \
 && curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" -o /usr/local/bin/kubectl \
 && chmod 755 /usr/local/bin/kubectl \
 && apt-get purge -y curl && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY portal-py/requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY portal-py/api.py portal-py/handlers.py portal-py/kube.py portal-py/oidc.py portal-py/stub.py ./
COPY cli/workspace ./wscli.py
COPY deploy/platform/catalog.json ./catalog.json
COPY portal/web ./web
COPY api/openapi.yaml ./web/openapi.yaml
COPY --from=css /src/web/tailwind.css ./web/tailwind.css
RUN printf '/* generated from deploy/platform/catalog.json */\nwindow.DEMO_CATALOG = ' > web/catalog.js && cat catalog.json >> web/catalog.js && printf ';\n' >> web/catalog.js \
 && python3 -m py_compile api.py handlers.py kube.py oidc.py stub.py \
 && useradd -r -u 65532 -g root -d /tmp -s /sbin/nologin portal \
 && mkdir -p /tmp/portal-home/.config && chown -R 65532:0 /tmp/portal-home
# the CLI module keeps its (unused here) key cache under WORKSPACE_CONFIG_DIR; keep it off the read-only root fs
ENV HOME=/tmp/portal-home WORKSPACE_CONFIG_DIR=/tmp/portal-home/.config/workspace PYTHONUNBUFFERED=1 PORTAL_ADDR=:8080
USER 65532
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD python3 -c "import urllib.request;urllib.request.urlopen('http://127.0.0.1:8080/healthz')" || exit 1
ENTRYPOINT ["python3", "/app/api.py"]
