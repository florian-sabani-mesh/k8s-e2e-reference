#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
for workload in statefulset/postgres statefulset/nats deployment/wiremock deployment/orders deployment/pricing deployment/settlement deployment/gateway; do
 kubectl -n "$NAMESPACE" rollout status "$workload" --timeout=180s
done
kubectl -n "$NAMESPACE" wait --for=condition=complete job --all --timeout=120s
