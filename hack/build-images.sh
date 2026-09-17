#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
if [[ "${SKIP_BUILD:-0}" == 1 ]]; then
 for service in gateway orders pricing settlement exchange; do docker image inspect "reference/$service:$IMAGE_TAG" >/dev/null; done
 exit 0
fi
for service in gateway orders pricing settlement exchange; do
 DOCKER_BUILDKIT=1 docker build --progress=plain -f "services/$service/Dockerfile" -t "reference/$service:$IMAGE_TAG" .
done
