#!/usr/bin/env bash
set -Eeuo pipefail; . /opt/workspace/lib.sh
: "${KUBECTL_VERSION:=$(curl -fsSL https://dl.k8s.io/release/stable.txt)}"
fetch "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" /usr/local/bin/kubectl && chmod 755 /usr/local/bin/kubectl
fetch https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 /tmp/get-helm && bash /tmp/get-helm --no-sudo && rm -f /tmp/get-helm
kubectl completion bash >/etc/bash_completion.d/kubectl
