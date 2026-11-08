#!/usr/bin/env bash
# Provider-agnostic cluster add-ons, installed with helm/kubectl against the current
# context. Each one is a toggle in deploy/cluster/env.sh; all steps are idempotent
# (helm upgrade --install). This is still Layer 1: things the cluster must *provide*.
set -Eeuo pipefail
cd "$(dirname "$0")/../.."; . deploy/cluster/env.sh
for t in kubectl helm; do command -v $t >/dev/null || { echo "missing $t"; exit 1; }; done
step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx >/dev/null 2>&1 || true
helm repo add jetstack https://charts.jetstack.io >/dev/null 2>&1 || true
helm repo add metrics-server https://kubernetes-sigs.github.io/metrics-server/ >/dev/null 2>&1 || true
helm repo update >/dev/null

if [ "${INSTALL_INGRESS_NGINX:-false}" = true ]; then
  step "ingress-nginx (C5)"
  helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx -n ingress-nginx --create-namespace \
    --set controller.replicaCount=2 --set controller.ingressClassResource.default=true \
    --set controller.config.proxy-body-size=64m --set controller.config.use-forwarded-headers=true \
    --set controller.nodeSelector."devspaces\.io/pool"=system --set controller.tolerations[0].key=CriticalAddonsOnly --set controller.tolerations[0].operator=Exists
  kubectl -n ingress-nginx rollout status deploy/ingress-nginx-controller --timeout=180s
  echo "external address: $(kubectl -n ingress-nginx get svc ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}{.status.loadBalancer.ingress[0].hostname}')  → create the DNS record for ${PORTAL_HOST}"
fi

if [ "${INSTALL_CERT_MANAGER:-false}" = true ]; then
  step "cert-manager (C6)"
  helm upgrade --install cert-manager jetstack/cert-manager -n cert-manager --create-namespace --set crds.enabled=true
  kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=180s
  if [ "${CERT_ISSUER:-}" = letsencrypt ]; then
    kubectl apply -f - <<YAML
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata: { name: letsencrypt }
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: ${ACME_EMAIL}
    privateKeySecretRef: { name: letsencrypt-account }
    solvers: [{ http01: { ingress: { class: ${INGRESS_CLASS} } } }]
YAML
  else
    echo "CERT_ISSUER=${CERT_ISSUER:-} — create a ClusterIssuer for your corporate CA, e.g. a CA issuer from a Secret: https://cert-manager.io/docs/configuration/ca/"
  fi
fi

if [ "${INSTALL_METRICS_SERVER:-false}" = true ]; then
  step "metrics-server (C13)"
  helm upgrade --install metrics-server metrics-server/metrics-server -n kube-system --set args[0]=--kubelet-insecure-tls
fi

if [ "${INSTALL_MULTUS:-false}" = true ]; then
  step "Multus + whereabouts (C10, multicast mode multus)"
  kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml
  kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/master/doc/crds/daemonset-install.yaml \
                -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/master/doc/crds/whereabouts.cni.cncf.io_ippools.yaml \
                -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/master/doc/crds/whereabouts.cni.cncf.io_overlappingrangeipreservations.yaml
  echo "set multicast.multus.master in your values to the multicast VLAN interface of the pool nodes"
fi

if [ "${ENABLE_CILIUM_MULTICAST:-false}" = true ]; then
  step "Cilium multicast (C10, multicast mode cilium)"
  ./deploy/cluster/multicast-cilium.sh ${MULTICAST_GROUPS:-239.255.0.0/24}
fi

if [ "${INSTALL_SMARTER_DEVICE_MANAGER:-false}" = true ]; then
  step "smarter-device-manager on the lab pool (C12)"
  kubectl apply -f - <<'YAML'
apiVersion: apps/v1
kind: DaemonSet
metadata: { name: smarter-device-manager, namespace: kube-system, labels: { name: smarter-device-manager } }
spec:
  selector: { matchLabels: { name: smarter-device-manager } }
  template:
    metadata: { labels: { name: smarter-device-manager } }
    spec:
      nodeSelector: { devspaces.io/lab: "true" }
      tolerations: [{ key: devspaces.io/lab, operator: Equal, value: "true", effect: NoSchedule }]
      priorityClassName: system-node-critical
      hostname: smarter-device-management
      containers:
        - name: smarter-device-manager
          image: registry.gitlab.com/arm-research/smarter/smarter-device-manager:v1.20.11
          securityContext: { allowPrivilegeEscalation: false, capabilities: { drop: ["ALL"] } }
          resources: { limits: { cpu: 100m, memory: 15Mi }, requests: { cpu: 10m, memory: 15Mi } }
          volumeMounts:
            - { name: device-plugin, mountPath: /var/lib/kubelet/device-plugins }
            - { name: dev-dir, mountPath: /dev }
            - { name: sys-dir, mountPath: /sys }
            - { name: config, mountPath: /root/config }
      volumes:
        - { name: device-plugin, hostPath: { path: /var/lib/kubelet/device-plugins } }
        - { name: dev-dir, hostPath: { path: /dev } }
        - { name: sys-dir, hostPath: { path: /sys } }
        - { name: config, configMap: { name: smarter-device-manager } }
YAML
  echo "the ConfigMap smarter-device-manager comes from the devspaces chart (devicePlugins.smarterDeviceManager); copy it to kube-system: kubectl -n devspaces-system get cm smarter-device-manager -o yaml | sed 's/namespace: devspaces-system/namespace: kube-system/' | kubectl apply -f -"
fi

if [ "${INSTALL_KATA:-false}" = true ]; then
  step "Kata Containers on the lab pool (C11)"
  kubectl apply -f https://raw.githubusercontent.com/kata-containers/kata-containers/main/tools/packaging/kata-deploy/kata-rbac/base/kata-rbac.yaml
  kubectl apply -f https://raw.githubusercontent.com/kata-containers/kata-containers/main/tools/packaging/kata-deploy/kata-deploy/base/kata-deploy.yaml
  kubectl -n kube-system patch ds kata-deploy --type merge -p '{"spec":{"template":{"spec":{"nodeSelector":{"devspaces.io/lab":"true"},"tolerations":[{"key":"devspaces.io/lab","operator":"Equal","value":"true","effect":"NoSchedule"}]}}}}'
  echo "the chart's RuntimeClass 'kata' uses handler kata-qemu; nested virtualization must be enabled on lab VMs"
fi
[ "${INSTALL_GVISOR:-false}" = true ] && { step "gVisor (C11)"; echo "install runsc + containerd-shim-runsc-v1 on nodes labelled devspaces.io/runtime-gvisor=true: https://gvisor.dev/docs/user_guide/containerd/quick_start/ (GKE: --sandbox type=gvisor node pool; the chart's RuntimeClass 'gvisor' uses handler runsc)"; }
echo; echo "add-ons done. Next: deploy/cluster/check-contract.sh"
