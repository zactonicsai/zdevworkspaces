#!/usr/bin/env bash
# Layer 1 on AWS: an EKS cluster that keeps deploy/cluster/CONTRACT.md.
# Needs: aws CLI (logged in), eksctl >= 0.200, kubectl, helm. Reads deploy/cluster/env.sh.
#   ./deploy/cluster/providers/aws-eks.sh            # create or update
#   ./deploy/cluster/providers/aws-eks.sh --delete
# What it builds: a VPC, 3 managed node groups (system, standard, lab — lab tainted and
# labelled), the EBS CSI driver with a gp3 default StorageClass (expansion on), OIDC
# authentication to the API server with e-mail usernames, and the AWS Load Balancer Controller.
set -Eeuo pipefail
cd "$(dirname "$0")/../../.."; . deploy/cluster/env.sh
for t in aws eksctl kubectl helm; do command -v $t >/dev/null || { echo "missing $t"; exit 1; }; done
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
if [ "${1:-}" = --delete ]; then eksctl delete cluster --name "$CLUSTER_NAME" --region "$REGION" --wait; exit 0; fi

cat > /tmp/eks-cluster.yaml <<YAML
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata: { name: ${CLUSTER_NAME}, region: ${REGION}, version: "${K8S_VERSION}" }
iam:
  withOIDC: true
addons:
  - { name: vpc-cni, configurationValues: '{"enableNetworkPolicy": "true"}' }   # C8: NetworkPolicy enforcement
  - { name: coredns }
  - { name: kube-proxy }
  - { name: aws-ebs-csi-driver, wellKnownPolicies: { ebsCSIController: true } }  # C4
  - { name: metrics-server }                                                      # C13
  - { name: eks-pod-identity-agent }
accessConfig:
  authenticationMode: API_AND_CONFIG_MAP
identityProviders:                                      # C7: developers log in with OIDC e-mails
  - name: corp-oidc
    type: oidc
    issuerURL: ${OIDC_ISSUER_URL}
    clientID: ${OIDC_CLIENT_ID}
    usernameClaim: ${OIDC_USERNAME_CLAIM}
    groupsClaim: ${OIDC_GROUPS_CLAIM}
managedNodeGroups:
  - name: system
    instanceType: ${SYSTEM_INSTANCE}
    amiFamily: AmazonLinux2023                          # C2: kernel 6.x, containerd 2.x on EKS 1.36 AMIs
    desiredCapacity: 2
    labels: { devspaces.io/pool: system }
    taints: [{ key: CriticalAddonsOnly, value: "true", effect: NoSchedule }]
  - name: standard
    instanceType: ${STANDARD_INSTANCE}
    amiFamily: AmazonLinux2023
    minSize: ${STANDARD_MIN}
    maxSize: ${STANDARD_MAX}
    desiredCapacity: ${STANDARD_MIN}
    volumeSize: 100
    labels: { devspaces.io/pool: standard }
    iam: { withAddonPolicies: { autoScaler: true } }
  - name: lab                                            # C3: hardware pool, one privileged workspace per node
    instanceType: ${LAB_INSTANCE}                        # use a *.metal type when drivers need real PCI devices
    amiFamily: AmazonLinux2023
    minSize: ${LAB_MIN}
    maxSize: ${LAB_MAX}
    desiredCapacity: ${LAB_MIN}
    volumeSize: 200
    labels: { devspaces.io/pool: lab, devspaces.io/lab: "true" }
    taints: [{ key: devspaces.io/lab, value: "true", effect: NoSchedule }]
    iam: { withAddonPolicies: { autoScaler: true } }
YAML
if eksctl get cluster --name "$CLUSTER_NAME" --region "$REGION" >/dev/null 2>&1; then
  echo "cluster exists: reconciling node groups"; eksctl create nodegroup -f /tmp/eks-cluster.yaml || true
else
  eksctl create cluster -f /tmp/eks-cluster.yaml
fi
aws eks update-kubeconfig --name "$CLUSTER_NAME" --region "$REGION"

# C4: gp3 default StorageClass with expansion
kubectl apply -f - <<YAML
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata: { name: gp3, annotations: { storageclass.kubernetes.io/is-default-class: "true" } }
provisioner: ebs.csi.aws.com
parameters: { type: gp3, encrypted: "true" }
allowVolumeExpansion: true
volumeBindingMode: WaitForFirstConsumer
YAML
kubectl annotate storageclass gp2 storageclass.kubernetes.io/is-default-class- 2>/dev/null || true

# C5: AWS Load Balancer Controller (ALB/NLB for the Ingress)
helm repo add eks https://aws.github.io/eks-charts >/dev/null; helm repo update >/dev/null
eksctl create iamserviceaccount --cluster "$CLUSTER_NAME" --region "$REGION" --namespace kube-system --name aws-load-balancer-controller \
  --attach-policy-arn "arn:aws:iam::${ACCOUNT}:policy/AWSLoadBalancerControllerIAMPolicy" --approve --override-existing-serviceaccounts 2>/dev/null || \
  echo "note: create the AWSLoadBalancerControllerIAMPolicy first (see https://kubernetes-sigs.github.io/aws-load-balancer-controller/)"
helm upgrade --install aws-load-balancer-controller eks/aws-load-balancer-controller -n kube-system \
  --set clusterName="$CLUSTER_NAME" --set serviceAccount.create=false --set serviceAccount.name=aws-load-balancer-controller
# cluster autoscaler is recommended: https://github.com/kubernetes/autoscaler (or Karpenter)
echo; echo "EKS ready. Next: deploy/cluster/addons.sh, then deploy/cluster/check-contract.sh"
