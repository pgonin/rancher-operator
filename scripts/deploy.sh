#!/usr/bin/env bash
#
# deploy.sh — helm install/upgrade the rancher-operator chart on whichever
# cluster the current kubectl context points at.
#
# Usage:
#   ./scripts/deploy.sh [install|uninstall]
#
# Environment variables (all optional):
#   IMAGE_REPO     container image repository (default: ghcr.io/pgonin/rancher-operator)
#   IMAGE_TAG      container image tag        (default: latest)
#   NAMESPACE      namespace to install into  (default: rancher-operator)
#   RELEASE        helm release name          (default: rancher-operator)
#   CHART_PATH     path to the helm chart     (default: dist/chart)
#   METRICS_SECURE expose metrics over HTTPS  (default: false — set to true
#                  once cert-manager is installed on the operator's cluster)
#
# Re-running 'install' upgrades in place via `helm upgrade --install`.

set -euo pipefail

IMAGE_REPO="${IMAGE_REPO:-ghcr.io/pgonin/rancher-operator}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
NAMESPACE="${NAMESPACE:-rancher-operator}"
RELEASE="${RELEASE:-rancher-operator}"
METRICS_SECURE="${METRICS_SECURE:-false}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CHART_PATH="${CHART_PATH:-$SCRIPT_DIR/../dist/chart}"

ACTION="${1:-install}"

command -v helm >/dev/null || { echo "helm not found in PATH" >&2; exit 1; }
command -v kubectl >/dev/null || { echo "kubectl not found in PATH" >&2; exit 1; }

current_context="$(kubectl config current-context 2>/dev/null || true)"
if [[ -z "$current_context" ]]; then
  echo "No current kubectl context — run scripts/infra.sh up first." >&2
  exit 1
fi
echo "Targeting kubectl context: $current_context"

case "$ACTION" in
  install)
    echo "Deploying $RELEASE to namespace $NAMESPACE using $IMAGE_REPO:$IMAGE_TAG..."
    helm upgrade --install "$RELEASE" "$CHART_PATH" \
      --namespace "$NAMESPACE" \
      --create-namespace \
      --set "manager.image.repository=$IMAGE_REPO" \
      --set "manager.image.tag=$IMAGE_TAG" \
      --set "metrics.secure=$METRICS_SECURE" \
      --wait \
      --timeout 5m
    echo
    echo "Deployment status:"
    kubectl -n "$NAMESPACE" get deploy,pods
    ;;
  uninstall)
    echo "Removing $RELEASE from namespace $NAMESPACE..."
    helm uninstall "$RELEASE" --namespace "$NAMESPACE" || true
    echo "Note: CRDs are kept by default (chart values: crd.keep=true)."
    echo "Delete them manually with:"
    echo "  kubectl delete crd targetclusters.core.rancher-operator.io"
    echo "  kubectl delete crd clusterprerequisites.core.rancher-operator.io"
    echo "  kubectl delete crd ranchermanagers.core.rancher-operator.io"
    ;;
  *)
    echo "Unknown action: $ACTION (expected 'install' or 'uninstall')" >&2
    exit 2
    ;;
esac
