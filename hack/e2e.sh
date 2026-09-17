#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
./hack/check-prerequisites.sh
# Lock the lifecycle to prevent two runs sharing a cluster, state, or WireMock.
if ! mkdir .run/e2e.lock 2>/dev/null; then echo "An E2E run is already active (.run/e2e.lock)." >&2; exit 1; fi
finish(){
 result=$?
 trap - EXIT INT TERM
 if [[ "$result" != 0 ]]; then ./hack/diagnostics.sh || true; fi
 if [[ "${KEEP_CLUSTER:-0}" == 1 ]]; then
  echo "Cluster retained. KUBECONFIG=$KUBECONFIG; clean up with make destroy."
 else
  ./hack/delete-cluster.sh || { if [[ "$result" == 0 ]]; then result=1; fi; }
 fi
 rmdir .run/e2e.lock
 exit "$result"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
./hack/create-cluster.sh
./hack/build-images.sh
./hack/load-images.sh
./hack/infra.sh
./hack/wait-ready.sh
./hack/test-e2e.sh
