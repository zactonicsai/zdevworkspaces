# The dev-pods API image (portal + FastAPI + kubectl + helm + the dev-pod chart).
# Build from the repo root:
#   docker build -f images/api/Dockerfile -t REGISTRY/dev-pods-api:0.1.0 .
FROM python:3.12-slim

ARG KUBECTL_VERSION=v1.31.4
ARG HELM_VERSION=v3.16.3
ARG TARGETARCH=amd64

RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
 && curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" -o /usr/local/bin/kubectl \
 && chmod +x /usr/local/bin/kubectl \
 && curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" | tar xz -C /tmp \
 && mv /tmp/linux-${TARGETARCH}/helm /usr/local/bin/helm && rm -rf /tmp/linux-${TARGETARCH} \
 && apt-get purge -y curl && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY api/requirements.txt api/requirements.txt
RUN pip install --no-cache-dir -r api/requirements.txt
COPY api/ api/
COPY web/ web/
COPY helm/dev-pod/ helm/dev-pod/

RUN useradd -u 10001 -m api && mkdir -p /app/api/logs /tmp/helm && chown -R api /app/api/logs /tmp/helm
USER 10001
ENV HOME=/tmp \
    HELM_CACHE_HOME=/tmp/helm/cache HELM_CONFIG_HOME=/tmp/helm/config HELM_DATA_HOME=/tmp/helm/data \
    WS_CHART=/app/helm/dev-pod WS_LOG_DIR=/app/api/logs WS_CREATE_NAMESPACE=0 PYTHONUNBUFFERED=1

WORKDIR /app/api
EXPOSE 8080
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8080"]
