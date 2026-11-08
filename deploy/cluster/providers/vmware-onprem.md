# Layer 1 on VMware / bare metal / any on-prem Kubernetes

There is no single CLI here, so this page is the recipe. Any distribution works — kubeadm,
Rancher RKE2, VMware Tanzu (TKG), k0s, Talos — as long as the result keeps
`deploy/cluster/CONTRACT.md`. Use `kubeadm-node-prep.sh` on every VM for kubeadm/RKE2.

| Contract | How to keep it on-prem |
|---|---|
| C1 Kubernetes ≥ 1.36 | kubeadm 1.36, RKE2 `v1.36.x+rke2r1`, TKG release with 1.36 |
| C2 kernel ≥ 6.3, containerd ≥ 2.0 | Ubuntu 24.04 VM template (kernel 6.8) + containerd 2.x from Docker's apt repo; `kubeadm-node-prep.sh` does this |
| C3 node pools | three VM groups (system / standard / lab); label and taint lab nodes: `kubectl label node NAME devspaces.io/lab=true && kubectl taint node NAME devspaces.io/lab=true:NoSchedule` |
| C4 block storage | vSphere CSI (`csi.vsphere.vmware.com`) StorageClass with `allowVolumeExpansion: true` as default; or Longhorn / Ceph RBD. **Not NFS** for home volumes |
| C5 ingress / LB | MetalLB (L2 or BGP) for `LoadBalancer` Services + ingress-nginx (`addons.sh`) |
| C6 TLS | cert-manager with your corporate CA as a `ClusterIssuer` (`addons.sh`, `CERT_ISSUER=corp-ca`) |
| C7 identity | kube-apiserver flags `--oidc-issuer-url=… --oidc-client-id=… --oidc-username-claim=email --oidc-groups-claim=groups` (kubeadm: `ClusterConfiguration.apiServer.extraArgs`; RKE2: `kube-apiserver-arg` in `config.yaml`); Keycloak/Entra/Okta as the provider |
| C8 NetworkPolicy CNI | Cilium (recommended: also gives multicast mode cilium) or Calico |
| C9 Artifactory reachable | firewall rule from node subnets to Artifactory (443) |
| C10 multicast | **multus** mode: a dedicated VLAN trunked to every multicast-pool VM (vSphere: port group with VLAN ID; enable *promiscuous mode / forged transmits / MAC changes* on that port group, or use macvlan in *passthru*); install Multus + whereabouts (`addons.sh INSTALL_MULTUS=true`); set `multicast.multus.master` to the VLAN sub-interface. **cilium** mode: Cilium in VXLAN tunnel mode + `multicast-cilium.sh` |
| C11 gVisor / Kata | gVisor: install `runsc` + containerd shim on labelled nodes; Kata: `kata-deploy` DaemonSet restricted to the lab pool (VMs need nested virtualization enabled in vSphere) |
| C12 device plugins | PCI passthrough / DirectPath I/O of the device to lab VMs, then smarter-device-manager / SR-IOV plugin; `linux-headers-$(uname -r)` on lab nodes |
| C13 metrics | metrics-server (`addons.sh`), Prometheus stack of your choice |

Minimal kubeadm flow (per VM: `sudo deploy/cluster/providers/kubeadm-node-prep.sh`):

```bash
# control plane
sudo kubeadm init --config deploy/cluster/providers/kubeadm-config.yaml   # see the example below
# join workers with the printed command; then label pools
kubectl label node lab-1 lab-2 devspaces.io/lab=true devspaces.io/pool=lab
kubectl taint node lab-1 lab-2 devspaces.io/lab=true:NoSchedule
# CNI
helm repo add cilium https://helm.cilium.io && helm upgrade --install cilium cilium/cilium -n kube-system \
  --set kubeProxyReplacement=true --set routingMode=tunnel --set tunnelProtocol=vxlan   # vxlan = multicast-capable
```

`kubeadm-config.yaml` example (API server OIDC for C7):

```yaml
apiVersion: kubeadm.k8s.io/v1beta4
kind: ClusterConfiguration
kubernetesVersion: v1.36.4
networking: { podSubnet: 10.244.0.0/16, serviceSubnet: 10.96.0.0/12 }
apiServer:
  extraArgs:
    - { name: oidc-issuer-url, value: https://keycloak.corp.example/realms/corp }
    - { name: oidc-client-id, value: kubernetes }
    - { name: oidc-username-claim, value: email }
    - { name: oidc-groups-claim, value: groups }
```
