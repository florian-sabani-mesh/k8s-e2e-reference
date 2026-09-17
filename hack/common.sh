#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH"
export KUBECONFIG="$ROOT/.run/kubeconfig"
export TF_DATA_DIR="$ROOT/.run/terraform-data"
export TF_VAR_kubeconfig="$KUBECONFIG"
export TF_IN_AUTOMATION=1
export KIND_EXPERIMENTAL_PROVIDER=docker
CLUSTER=terraform-k8s-e2e-reference
NAMESPACE=e2e
export IMAGE_TAG="${IMAGE_TAG:-e2e}"
export TF_VAR_image_tag="$IMAGE_TAG"
export API_URL="${API_URL:-http://127.0.0.1:18080}"
export WIREMOCK_URL="${WIREMOCK_URL:-http://127.0.0.1:18081}"
export NATS_URL="${NATS_URL:-nats://127.0.0.1:14222}"
mkdir -p "$ROOT/.run" "$ROOT/artifacts"
cd "$ROOT"
tf() { terraform -chdir="$ROOT/infra/environments/e2e" "$@"; }
need() { command -v "$1" >/dev/null || { echo "Missing prerequisite: $1 (see README.md)" >&2; exit 1; }; }
