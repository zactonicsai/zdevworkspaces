#!/usr/bin/env bash
# Prepare an Ubuntu 24.04 VM (VMware, bare metal, any IaaS) as a devspaces node:
# containerd 2.x, kernel/sysctl settings, kubeadm packages, lab extras. Run as root.
#   sudo ./kubeadm-node-prep.sh [--lab] [--k8s 1.36]
set -Eeuo pipefail
LAB=0; K8S=${K8S_VERSION:-1.36}
for a in "$@"; do case "$a" in --lab) LAB=1;; --k8s) ;; 1.*) K8S=$a;; esac; done
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
k=$(uname -r); maj=${k%%.*}; min=${k#*.}; min=${min%%.*}
if [ "$maj" -lt 6 ] || { [ "$maj" -eq 6 ] && [ "$min" -lt 3 ]; }; then echo "kernel $k < 6.3: install Ubuntu 24.04 (6.8) or the HWE kernel"; exit 1; fi
export DEBIAN_FRONTEND=noninteractive
apt-get update && apt-get install -y ca-certificates curl gpg apt-transport-https
# containerd 2.x from Docker's repository (Ubuntu's own is 1.7)
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
apt-get update && apt-get install -y containerd.io
containerd config default > /etc/containerd/config.toml
sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
systemctl restart containerd && systemctl enable containerd
containerd --version | grep -q " v2\." || { echo "containerd is not 2.x"; exit 1; }
# kernel modules + sysctls (overlay/br_netfilter for CNI; forwarding; multicast-friendly defaults)
cat > /etc/modules-load.d/k8s.conf <<M
overlay
br_netfilter
M
modprobe overlay; modprobe br_netfilter
cat > /etc/sysctl.d/99-k8s.conf <<S
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
net.ipv4.igmp_max_memberships = 1024
net.core.rmem_max = 26214400
user.max_user_namespaces = 65536
S
sysctl --system >/dev/null
swapoff -a; sed -i '/ swap / s/^/#/' /etc/fstab
# kubeadm / kubelet / kubectl
curl -fsSL "https://pkgs.k8s.io/core:/stable:/v${K8S}/deb/Release.key" | gpg --dearmor -o /etc/apt/keyrings/kubernetes.gpg
echo "deb [signed-by=/etc/apt/keyrings/kubernetes.gpg] https://pkgs.k8s.io/core:/stable:/v${K8S}/deb/ /" > /etc/apt/sources.list.d/kubernetes.list
apt-get update && apt-get install -y kubelet kubeadm kubectl && apt-mark hold kubelet kubeadm kubectl
systemctl enable kubelet
if [ "$LAB" = 1 ]; then
  apt-get install -y "linux-headers-$(uname -r)" build-essential dkms pciutils usbutils   # C12: module builds
  echo "lab node: after joining, label+taint it: kubectl label node $(hostname) devspaces.io/lab=true; kubectl taint node $(hostname) devspaces.io/lab=true:NoSchedule"
fi
echo "node prepared: kernel $k, $(containerd --version), kubeadm $(kubeadm version -o short)"
