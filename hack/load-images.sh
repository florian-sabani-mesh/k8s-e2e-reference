#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
for service in gateway orders pricing settlement exchange; do
 kind load docker-image --name "$CLUSTER" "reference/$service:$IMAGE_TAG"
done
