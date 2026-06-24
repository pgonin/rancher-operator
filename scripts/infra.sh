#!/usr/bin/env bash
#
# infra.sh — provision a GKE Autopilot cluster to host the rancher-operator.
#
# Usage:
#   GCP_PROJECT=my-project ./scripts/infra.sh [up|down]
#
# Environment variables (all optional except GCP_PROJECT):
#   GCP_PROJECT   GCP project ID (required)
#   GCP_REGION    region for the cluster (default: us-central1)
#   CLUSTER_NAME  Autopilot cluster name (default: rancher-operator-test)
#   RELEASE_CHANNEL  GKE release channel (default: regular)
#
# Re-running 'up' is idempotent: enables APIs and creates the cluster only if
# missing. 'down' deletes the cluster but does not touch APIs or the project.

set -euo pipefail

: "${GCP_PROJECT:?GCP_PROJECT must be set}"
GCP_REGION="${GCP_REGION:-us-central1}"
CLUSTER_NAME="${CLUSTER_NAME:-rancher-operator-test}"
RELEASE_CHANNEL="${RELEASE_CHANNEL:-regular}"

ACTION="${1:-up}"

command -v gcloud >/dev/null || { echo "gcloud not found in PATH" >&2; exit 1; }
command -v kubectl >/dev/null || { echo "kubectl not found in PATH" >&2; exit 1; }

gcloud config set project "$GCP_PROJECT" >/dev/null

enable_apis() {
  echo "Enabling required APIs in project $GCP_PROJECT..."
  gcloud services enable container.googleapis.com --quiet
}

create_cluster() {
  if gcloud container clusters describe "$CLUSTER_NAME" \
       --region "$GCP_REGION" >/dev/null 2>&1; then
    echo "Cluster $CLUSTER_NAME already exists in $GCP_REGION."
  else
    echo "Creating GKE Autopilot cluster $CLUSTER_NAME in $GCP_REGION (this takes ~5 min)..."
    gcloud container clusters create-auto "$CLUSTER_NAME" \
      --region "$GCP_REGION" \
      --release-channel "$RELEASE_CHANNEL"
  fi
}

fetch_credentials() {
  echo "Fetching kubeconfig credentials for $CLUSTER_NAME..."
  gcloud container clusters get-credentials "$CLUSTER_NAME" \
    --region "$GCP_REGION"
  kubectl cluster-info
}

delete_cluster() {
  if gcloud container clusters describe "$CLUSTER_NAME" \
       --region "$GCP_REGION" >/dev/null 2>&1; then
    echo "Deleting cluster $CLUSTER_NAME in $GCP_REGION..."
    gcloud container clusters delete "$CLUSTER_NAME" \
      --region "$GCP_REGION" --quiet
  else
    echo "Cluster $CLUSTER_NAME does not exist in $GCP_REGION — nothing to delete."
  fi
}

case "$ACTION" in
  up)
    enable_apis
    create_cluster
    fetch_credentials
    ;;
  down)
    delete_cluster
    ;;
  *)
    echo "Unknown action: $ACTION (expected 'up' or 'down')" >&2
    exit 2
    ;;
esac
