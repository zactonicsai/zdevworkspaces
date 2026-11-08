#!/usr/bin/env bash
# check-contract.sh — does the CURRENT kubectl context keep the promises in CONTRACT.md?
# Read-only except for one short-lived probe pod (--no-probe to skip). Exit 1 on any FAIL.
set -u
PROBE=1; NS=devspaces-system
for a in "$@"; do case "$a" in --no-probe) PROBE=0;; -h|--help) sed -n '2,4p' "$0"; exit 0;; esac; done
PASS=0; WARN=0; FAIL=0
if [ -t 1 ]; then G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; B=$'\033[34m'; N=$'\033[0m'; else G=; Y=; R=; B=; N=; fi
pass() { PASS=$((PASS+1)); printf '%s[PASS]%s %-5s %-30s %s\n' "$G" "$N" "$1" "$2" "${3:-}"; }
warn() { WARN=$((WARN+1)); printf '%s[WARN]%s %-5s %-30s %s\n' "$Y" "$N" "$1" "$2" "${3:-}"; [ -n "${4:-}" ] && printf '             → %s\n' "$4"; }
fail() { FAIL=$((FAIL+1)); printf '%s[FAIL]%s %-5s %-30s %s\n' "$R" "$N" "$1" "$2" "${3:-}"; [ -n "${4:-}" ] && printf '             → %s\n' "$4"; }
vergte() { local a b i x y; IFS=. read -r -a a <<<"${1//[!0-9.]/}"; IFS=. read -r -a b <<<"${2//[!0-9.]/}"; for i in 0 1 2; do x=${a[$i]:-0}; y=${b[$i]:-0}; [ "${x:-0}" -gt "${y:-0}" ] 2>/dev/null && return 0; [ "${x:-0}" -lt "${y:-0}" ] 2>/dev/null && return 1; done; return 0; }
command -v kubectl >/dev/null || { echo "kubectl not found"; exit 1; }
ctx=$(kubectl config current-context 2>/dev/null || echo none)
printf '%sCluster contract check%s — context %s\n\n' "$B" "$N" "$ctx"
kubectl get --raw /version >/dev/null 2>&1 || { fail C1 "API server" "unreachable" "check kubeconfig / VPN"; exit 1; }

# C1 version
sv=$(kubectl version -o json 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin)["serverVersion"]["gitVersion"])' 2>/dev/null | sed 's/^v//')
if vergte "${sv:-0}" 1.36; then pass C1 "kubernetes version" "v$sv"; elif vergte "${sv:-0}" 1.33; then warn C1 "kubernetes version" "v$sv" "1.33–1.35 need the UserNamespacesSupport feature gate on kubelets; 1.36+ recommended"; else fail C1 "kubernetes version" "v${sv:-?}" "upgrade to 1.36+"; fi

# C2 nodes
nodes=$(kubectl get nodes -o json 2>/dev/null)
python3 - "$nodes" <<'PY' > /tmp/contract-nodes.txt
import json,sys,re
d=json.loads(sys.argv[1]); 
for n in d["items"]:
    i=n["status"]["nodeInfo"]; l=n["metadata"].get("labels",{}); t=[x for x in n["spec"].get("taints",[]) if x["key"]=="devspaces.io/lab"]
    print(n["metadata"]["name"], i["kernelVersion"], i["containerRuntimeVersion"], i["osImage"].replace(" ","_"), l.get("devspaces.io/lab","-"), ("taint" if t else "-"), i["architecture"])
PY
total=$(wc -l < /tmp/contract-nodes.txt | tr -d ' ')
oldk=$(awk '{split($2,k,"."); if (k[1]<6 || (k[1]==6 && k[2]<3)) print $1" ("$2")"}' /tmp/contract-nodes.txt | tr '\n' ' ')
oldrt=$(awk '{if ($3 ~ /containerd:\/\/1\./ || $3 ~ /docker/) print $1" ("$3")"}' /tmp/contract-nodes.txt | tr '\n' ' ')
[ -z "$oldk" ] && pass C2 "node kernels" "all $total nodes >= 6.3" || fail C2 "node kernels" "< 6.3: $oldk" "pick a newer node image (Ubuntu 24.04 / AL2023 / COS) or use --isolation gvisor"
[ -z "$oldrt" ] && pass C2 "container runtime" "$(awk '{print $3}' /tmp/contract-nodes.txt | sort -u | tr '\n' ' ')" || fail C2 "container runtime" "$oldrt" "containerd 2.x required for user namespaces"
arches=$(awk '{print $7}' /tmp/contract-nodes.txt | sort -u | tr '\n' ' '); [ "$(echo $arches | wc -w)" -gt 1 ] && warn C2 "architectures" "$arches" "build multi-arch images (PLATFORMS=linux/amd64,linux/arm64) or pin pools" || pass C2 "architectures" "$arches"

# C3 lab pool
labn=$(awk '$5=="true"' /tmp/contract-nodes.txt | wc -l | tr -d ' '); labt=$(awk '$5=="true" && $6=="taint"' /tmp/contract-nodes.txt | wc -l | tr -d ' ')
if [ "$labn" -eq 0 ]; then warn C3 "lab node pool" "none" "optional: label+taint a pool for hardware/kernel profiles (providers/* do this)"; elif [ "$labn" = "$labt" ]; then pass C3 "lab node pool" "$labn node(s) labelled + tainted"; else fail C3 "lab node pool" "$labn labelled, $labt tainted" "kubectl taint node NAME devspaces.io/lab=true:NoSchedule"; fi

# C4 storage
sc=$(kubectl get storageclass -o json 2>/dev/null)
python3 - "$sc" <<'PY' > /tmp/contract-sc.txt
import json,sys
for s in json.loads(sys.argv[1])["items"]:
    a=s["metadata"].get("annotations",{}); dflt=a.get("storageclass.kubernetes.io/is-default-class")=="true"
    print(s["metadata"]["name"], s.get("provisioner"), "default" if dflt else "-", "expand" if s.get("allowVolumeExpansion") else "noexpand")
PY
dsc=$(awk '$3=="default"' /tmp/contract-sc.txt)
if [ -z "$dsc" ]; then fail C4 "default StorageClass" "none" "kubectl annotate storageclass NAME storageclass.kubernetes.io/is-default-class=true (or set catalog defaults.storageClass)"; else
  set -- $dsc; case "$2" in *nfs*|*efs*|*azurefile*|*filestore*) fail C4 "default StorageClass" "$1 ($2) is a network file system" "user namespaces cannot idmap NFS; use block storage (ebs/pd/managed-csi/vsphere-csi/longhorn)";; *) pass C4 "default StorageClass" "$1 ($2)";; esac
  [ "$4" = expand ] && pass C4 "volume expansion" "$1 allows it" || warn C4 "volume expansion" "$1 does not" "set allowVolumeExpansion: true so home volumes can grow"; fi
kubectl get crd volumesnapshotclasses.snapshot.storage.k8s.io >/dev/null 2>&1 && pass C15 "volume snapshots" "CSI snapshot CRDs present" || warn C15 "volume snapshots" "no snapshot CRDs" "install the external-snapshotter (drives snapshot needs it)"
# C14/C16: the classes the catalog's storage.choices name
cat_json=$(kubectl -n "${SYSTEM_NS:-devspaces-system}" get configmap workspace-catalog -o jsonpath='{.data.catalog\.json}' 2>/dev/null || true)
if [ -n "$cat_json" ] && command -v python3 >/dev/null 2>&1; then
  while IFS=$'\t' read -r cname ckind cclass; do
    [ -z "$cclass" ] && continue
    rp=$(kubectl get storageclass "$cclass" -o jsonpath='{.reclaimPolicy} {.allowVolumeExpansion}' 2>/dev/null || true)
    if [ -z "$rp" ]; then fail C14 "storage choice $cname" "StorageClass $cclass missing" "create it (deploy/cluster/providers/*/20-storage.yaml)"; continue; fi
    case "$ckind" in
      home) case "$rp" in "Retain true") pass C14 "storage choice $cname" "$cclass Retain, expandable";; *) warn C14 "storage choice $cname" "$cclass is '$rp'" "home classes should be reclaimPolicy Retain + allowVolumeExpansion";; esac;;
      shared) pass C16 "shared storage $cname" "$cclass present (must be ReadWriteMany-capable)";;
    esac
  done < <(printf '%s' "$cat_json" | python3 -c 'import json,sys
c=json.load(sys.stdin)
for ch in (c.get("storage") or {}).get("choices", []): print("%s\t%s\t%s" % (ch.get("name",""), ch.get("kind","home"), ch.get("storageClass","")))')
  sc=$(printf '%s' "$cat_json" | python3 -c 'import json,sys;print((json.load(sys.stdin).get("storage") or {}).get("snapshotClass",""))')
  if [ -n "$sc" ]; then kubectl get volumesnapshotclass "$sc" >/dev/null 2>&1 && pass C15 "snapshot class" "$sc" || fail C15 "snapshot class" "$sc missing" "create the VolumeSnapshotClass"; fi
fi

# C5/C6 ingress + tls
ic=$(kubectl get ingressclass -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
[ -n "$ic" ] && pass C5 "ingress classes" "$ic" || warn C5 "ingress classes" "none" "install ingress-nginx (addons.sh) or use the cloud ingress controller"
kubectl get crd clusterissuers.cert-manager.io >/dev/null 2>&1 && pass C6 "cert-manager" "installed ($(kubectl get clusterissuer -o jsonpath='{.items[*].metadata.name}' 2>/dev/null))" || warn C6 "cert-manager" "not installed" "addons.sh installs it; skip if the load balancer terminates TLS"

# C7 identity
who=$(kubectl auth whoami -o jsonpath='{.status.userInfo.username}' 2>/dev/null)
case "$who" in *@*) pass C7 "kubectl identity" "$who (e-mail style: matches onboarding)";; "") warn C7 "kubectl identity" "unknown (kubectl < 1.28?)";; *) warn C7 "kubectl identity" "$who" "developers must authenticate with OIDC e-mail usernames so `workspace admin onboard alice@…` matches";; esac

# C8 CNI
cni=$(kubectl -n kube-system get ds -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
case " $cni " in *cilium*) pass C8 "CNI" "cilium (NetworkPolicy enforced)";; *calico*) pass C8 "CNI" "calico (NetworkPolicy enforced)";; *kindnet*) warn C8 "CNI" "kindnet" "does NOT enforce NetworkPolicy — fine for a demo, not for production";; *aws-node*) if echo " $cni " | grep -q "network-policy\|aws-network-policy"; then pass C8 "CNI" "aws-vpc-cni with network policy"; else warn C8 "CNI" "aws-vpc-cni" "enable the VPC CNI network policy feature or install Calico/Cilium"; fi;; *) warn C8 "CNI" "$cni" "confirm NetworkPolicy is enforced";; esac

# C10 multicast
kubectl get crd network-attachment-definitions.k8s.cni.cncf.io >/dev/null 2>&1 && pass C10 "multus" "NAD CRD present (multicast mode: multus possible)" || warn C10 "multus" "not installed" "needed only for multicast mode multus"
if kubectl -n kube-system get cm cilium-config >/dev/null 2>&1; then
  mc=$(kubectl -n kube-system get cm cilium-config -o jsonpath='{.data.multicast-enabled}' 2>/dev/null); tm=$(kubectl -n kube-system get cm cilium-config -o jsonpath='{.data.tunnel-protocol}' 2>/dev/null)
  [ "$mc" = true ] && pass C10 "cilium multicast" "enabled (tunnel ${tm:-?})" || warn C10 "cilium multicast" "disabled (tunnel ${tm:-?})" "multicast-cilium.sh enables it (needs vxlan)"
fi
# C11 runtimes
for h in runsc kata-qemu kata-clh; do rc=$(kubectl get runtimeclass -o json 2>/dev/null | python3 -c "import sys,json; print(' '.join(r['metadata']['name'] for r in json.load(sys.stdin)['items'] if r.get('handler')=='$h'))" 2>/dev/null); [ -n "$rc" ] && pass C11 "runtime $h" "RuntimeClass $rc (handler must exist on labelled nodes)"; done
# C12 device plugins
ext=$(kubectl get nodes -o json | python3 -c 'import sys,json; s=set(); [s.update(k for k in n["status"].get("allocatable",{}) if "/" in k and not k.startswith("hugepages")) for n in json.load(sys.stdin)["items"]]; print(" ".join(sorted(s)))')
[ -n "$ext" ] && pass C12 "device plugin resources" "$ext" || warn C12 "device plugin resources" "none advertised" "optional: smarter-device-manager / NVIDIA / SR-IOV on the lab pool"
# C13 metrics
kubectl get --raw /apis/metrics.k8s.io/v1beta1 >/dev/null 2>&1 && pass C13 "metrics-server" "metrics API available" || warn C13 "metrics-server" "missing" "addons.sh installs it"

# C2/C9 probe pod: user namespace + registry reachability from inside the cluster
if [ "$PROBE" = 1 ]; then
  kubectl -n default delete pod contract-probe --ignore-not-found --wait=false >/dev/null 2>&1
  tgt=${ARTIFACTORY_URL:-${REGISTRY:-}}; tgt=${tgt#*://}; tgt=${tgt%%/*}
  cmd="cat /proc/self/uid_map"; [ -n "$tgt" ] && cmd="$cmd; (nc -z -w 5 ${tgt%%:*} ${tgt##*:} 2>/dev/null && echo NET_OK) || (wget -q -T 5 --spider https://$tgt 2>/dev/null && echo NET_OK) || echo NET_FAIL"
  if kubectl -n default run contract-probe --image=busybox:1.36 --restart=Never --overrides='{"apiVersion":"v1","spec":{"hostUsers":false}}' -- sh -c "$cmd" >/dev/null 2>&1; then
    phase=""; for _ in $(seq 1 45); do phase=$(kubectl -n default get pod contract-probe -o jsonpath='{.status.phase}' 2>/dev/null); case "$phase" in Succeeded|Failed) break;; esac; sleep 2; done
    out=$(kubectl -n default logs contract-probe 2>/dev/null | tr -s ' ')
    if [ "$phase" = Succeeded ]; then
      case "$out" in *" 0 4294967295"*) fail C2 "user namespace probe" "pod not remapped" "runtime ignores hostUsers:false (containerd < 2.0 or feature gate off)";; *) pass C2 "user namespace probe" "hostUsers:false works";; esac
      if [ -n "$tgt" ]; then case "$out" in *NET_OK*) pass C9 "reach $tgt from pods" "ok";; *) fail C9 "reach $tgt from pods" "blocked" "allow egress from nodes/pods to Artifactory";; esac; fi
    else fail C2 "user namespace probe" "pod ended in ${phase:-Pending}" "$(kubectl -n default describe pod contract-probe 2>/dev/null | awk '/Events:/{f=1} f' | tail -2 | tr -s ' ' | tr '\n' ';')"; fi
    kubectl -n default delete pod contract-probe --ignore-not-found --wait=false >/dev/null 2>&1
  else warn C2 "user namespace probe" "could not create probe pod"; fi
fi
printf '\n  %s%d passed%s, %s%d warnings%s, %s%d failed%s\n' "$G" "$PASS" "$N" "$Y" "$WARN" "$N" "$R" "$FAIL" "$N"
[ "$FAIL" -gt 0 ] && { echo "  The cluster layer must fix the FAIL items before the Kubernetes layer is installed."; exit 1; }
echo "  Contract satisfied. Next: deploy/prod/20-platform.sh"
