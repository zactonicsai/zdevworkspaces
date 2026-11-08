#!/usr/bin/env bash
# Layer 1 on Google Cloud: a GKE Standard cluster that keeps deploy/cluster/CONTRACT.md.
# Needs: gcloud (logged in, project set), kubectl, helm. Reads deploy/cluster/env.sh.
#   ./deploy/cluster/providers/gcp-gke.sh | --delete
# Notes: GKE's Dataplane V2 enforces NetworkPolicy (C8). Container-Optimized OS ships a
# 6.x kernel and containerd 2.x on GKE 1.36 (C2). For multicast mode "cilium" the Cilium
# multicast beta needs OSS Cilium (BYO CNI: --enable-dataplane-v2 off); mode "multus" is
# not available on GKE (no L2). Identity: GKE uses Google accounts (e-mail usernames) by
# default, which already match the portal's OIDC e-mail (C7).
set -Eeuo pipefail
cd "$(dirname "$0")/../../.."; . deploy/cluster/env.sh
for t in gcloud kubectl helm; do command -v $t >/dev/null || { echo "missing $t"; exit 1; }; done
PROJECT=$(gcloud config get-value project 2>/dev/null)
if [ "${1:-}" = --delete ]; then gcloud container clusters delete "$CLUSTER_NAME" --region "$REGION" --quiet; exit 0; fi
if ! gcloud container clusters describe "$CLUSTER_NAME" --region "$REGION" >/dev/null 2>&1; then
  gcloud container clusters create "$CLUSTER_NAME" --region "$REGION" --release-channel regular --cluster-version "$K8S_VERSION" \
    --enable-dataplane-v2 --enable-ip-alias --enable-shielded-nodes --workload-pool="${PROJECT}.svc.id.goog" \
    --addons HttpLoadBalancing,GcePersistentDiskCsiDriver --enable-autoscaling \
    --machine-type "$SYSTEM_INSTANCE" --num-nodes 1 --min-nodes 1 --max-nodes 3 \
    --node-labels devspaces.io/pool=system --node-taints CriticalAddonsOnly=true:NoSchedule --image-type COS_CONTAINERD
fi
gcloud container node-pools describe standard --cluster "$CLUSTER_NAME" --region "$REGION" >/dev/null 2>&1 || \
gcloud container node-pools create standard --cluster "$CLUSTER_NAME" --region "$REGION" --machine-type "$STANDARD_INSTANCE" \
  --enable-autoscaling --min-nodes "$STANDARD_MIN" --max-nodes "$STANDARD_MAX" --num-nodes "$STANDARD_MIN" --disk-size 100 \
  --image-type COS_CONTAINERD --node-labels devspaces.io/pool=standard
gcloud container node-pools describe lab --cluster "$CLUSTER_NAME" --region "$REGION" >/dev/null 2>&1 || \
gcloud container node-pools create lab --cluster "$CLUSTER_NAME" --region "$REGION" --machine-type "$LAB_INSTANCE" \
  --enable-autoscaling --min-nodes "$LAB_MIN" --max-nodes "$LAB_MAX" --num-nodes "$LAB_MIN" --disk-size 200 \
  --image-type UBUNTU_CONTAINERD --node-labels devspaces.io/pool=lab,devspaces.io/lab=true \
  --node-taints devspaces.io/lab=true:NoSchedule        # Ubuntu nodes: kernel headers for module builds (C12)
gcloud container clusters get-credentials "$CLUSTER_NAME" --region "$REGION"
# C4: pd-balanced default with expansion (GKE's standard-rwo already allows expansion; make it default)
kubectl annotate storageclass standard storageclass.kubernetes.io/is-default-class- 2>/dev/null || true
kubectl annotate storageclass standard-rwo storageclass.kubernetes.io/is-default-class=true --overwrite
echo; echo "GKE ready. Next: deploy/cluster/addons.sh, then deploy/cluster/check-contract.sh"
