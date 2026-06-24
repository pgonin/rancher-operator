#!/usr/bin/env bash
#
# build.sh — local image build + push, mirroring what the publish GitHub
# Actions workflow does. Use this for ad-hoc dev builds; release images
# should be produced by the workflow.
#
# Usage:
#   ./scripts/build.sh [tag]
#
# Environment variables (all optional):
#   IMAGE_REPO  full image repository (default: ghcr.io/pgonin/rancher-operator)
#   PLATFORMS   buildx target platforms (default: linux/amd64)
#
# Prerequisite: `docker login ghcr.io` with a PAT that has packages:write.

set -euo pipefail

IMAGE_REPO="${IMAGE_REPO:-ghcr.io/pgonin/rancher-operator}"
PLATFORMS="${PLATFORMS:-linux/amd64}"
TAG="${1:-dev-$(git -C "$(dirname "$0")/.." rev-parse --short HEAD)}"

command -v docker >/dev/null || { echo "docker not found in PATH" >&2; exit 1; }

echo "Building $IMAGE_REPO:$TAG for $PLATFORMS..."
docker buildx build \
  --platform "$PLATFORMS" \
  --tag "$IMAGE_REPO:$TAG" \
  --push \
  "$(dirname "$0")/.."

echo
echo "Pushed: $IMAGE_REPO:$TAG"
echo "Deploy with:"
echo "  IMAGE_REPO=$IMAGE_REPO IMAGE_TAG=$TAG ./scripts/deploy.sh"
