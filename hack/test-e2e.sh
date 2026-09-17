#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
pids=()
cleanup() { for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; done; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
for spec in gateway:18080:8080 wiremock:18081:8080 nats:14222:4222; do
 IFS=: read -r service local_port remote_port <<< "$spec"
 kubectl -n "$NAMESPACE" port-forward --address 127.0.0.1 "svc/$service" "$local_port:$remote_port" >"artifacts/port-forward-$service.log" 2>&1 &
 pids+=("$!")
done
for url in "$API_URL/readyz" "$WIREMOCK_URL/__admin/health"; do
 ready=0
 for ((attempt=0;attempt<100;attempt++)); do
  for pid in "${pids[@]}"; do kill -0 "$pid" 2>/dev/null || { cat artifacts/port-forward-*.log; exit 1; }; done
  if curl --max-time 2 -fsS "$url" >/dev/null 2>&1; then ready=1; break; fi
  sleep 0.1 # Bounded readiness polling, never a business assertion.
 done
 [[ "$ready" == 1 ]] || { echo "Port-forward readiness failed: $url" >&2; exit 1; }
done
npm --prefix e2e ci --no-audit --no-fund
npm --prefix e2e run typecheck
npm --prefix e2e test -- ${E2E_FILTER:-}
