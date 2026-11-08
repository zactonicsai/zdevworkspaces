#!/usr/bin/env bash
# Enable Cilium's eBPF multicast datapath (beta) and program multicast groups on every node.
#   ./deploy/cluster/multicast-cilium.sh 239.255.0.0/24 239.1.1.1
# How it works (https://docs.cilium.io/en/stable/network/multicast/): with multicast-enabled,
# cilium-agent's eBPF programs replicate a pod's multicast packets to every subscriber —
# local pods that joined via IGMP and remote nodes — over the VXLAN tunnel. No second
# interface, no IGMP querier on the physical network, works in clouds whose VPCs drop
# multicast. Requirements: Cilium in VXLAN tunnel mode, kernel >= 5.10 (amd64) / 6.0 (arm64),
# no IPsec between pods. Groups must be known to every node before pods join them: the
# `cilium multicast add` CLI programs them cluster-wide (new nodes: re-run this script;
# Isovalent Enterprise automates this with the IsovalentMulticastGroup CRD).
set -Eeuo pipefail
groups=("$@"); [ ${#groups[@]} -gt 0 ] || groups=(239.255.0.0/24)
command -v kubectl >/dev/null || { echo "kubectl missing"; exit 1; }
command -v cilium >/dev/null || { echo "install the cilium CLI: https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/#install-the-cilium-cli"; exit 1; }
tunnel=$(kubectl -n kube-system get cm cilium-config -o jsonpath='{.data.tunnel-protocol}' 2>/dev/null || true)
mode=$(kubectl -n kube-system get cm cilium-config -o jsonpath='{.data.routing-mode}' 2>/dev/null || true)
if [ "${mode:-tunnel}" != tunnel ] || { [ -n "$tunnel" ] && [ "$tunnel" != vxlan ]; }; then
  echo "Cilium must run in VXLAN tunnel mode (routing-mode=tunnel, tunnel-protocol=vxlan); found routing-mode=${mode:-?} tunnel-protocol=${tunnel:-?}"
  echo "helm upgrade cilium cilium/cilium -n kube-system --reuse-values --set routingMode=tunnel --set tunnelProtocol=vxlan"; exit 1
fi
if [ "$(kubectl -n kube-system get cm cilium-config -o jsonpath='{.data.enable-ipsec}' 2>/dev/null)" = true ]; then echo "multicast does not work with IPsec encryption enabled"; exit 1; fi
if [ "$(kubectl -n kube-system get cm cilium-config -o jsonpath='{.data.multicast-enabled}' 2>/dev/null)" != true ]; then
  echo "enabling multicast-enabled=true (restarts cilium pods)"; cilium config set multicast-enabled true
  kubectl -n kube-system rollout status ds/cilium --timeout=300s
fi
expand() { # 239.255.0.0/24 → individual group IPs (cilium multicast add takes one IP); /32 or bare IP passes through
  python3 - "$1" <<'PY'
import ipaddress,sys
n=ipaddress.ip_network(sys.argv[1], strict=False)
if n.num_addresses>256: sys.exit("refusing to expand %s (>256 groups); pass smaller ranges or single IPs" % n)
print("\n".join(str(h) for h in (n.hosts() if n.num_addresses>1 else [n.network_address])))
PY
}
for g in "${groups[@]}"; do
  for ip in $(expand "$g"); do cilium multicast add --group-ip "$ip" >/dev/null && printf '.'; done
done
echo; cilium multicast list group | head -5; echo "…"
echo "subscribers per group (nodes):"; cilium multicast list subscriber --all | head -12
echo "done: pods that join a group on their normal interface (eth0) receive it; verify with mcast-check --iface eth0 in two workspaces"
