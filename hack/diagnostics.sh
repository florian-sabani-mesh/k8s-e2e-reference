#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
DEST="$ROOT/artifacts/diagnostics-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$DEST"
echo "Collecting Kubernetes diagnostics in $DEST"
kubectl get pods -A -o wide >"$DEST/pods.txt" 2>&1 || true
kubectl get events -A --sort-by=.metadata.creationTimestamp >"$DEST/events.txt" 2>&1 || true
kubectl -n "$NAMESPACE" describe pods >"$DEST/pod-descriptions.txt" 2>&1 || true
kubectl -n "$NAMESPACE" get pods -o json >"$DEST/pods.json" 2>&1 || true
kubectl -n "$NAMESPACE" get deployments,statefulsets,jobs,pvc -o wide >"$DEST/workloads.txt" 2>&1 || true
while IFS= read -r pod; do
 [[ -n "$pod" ]] || continue
 name="${pod#pod/}"
 kubectl -n "$NAMESPACE" logs "$pod" --all-containers --timestamps >"$DEST/$name.log" 2>&1 || true
 kubectl -n "$NAMESPACE" logs "$pod" --all-containers --previous --timestamps >"$DEST/$name.previous.log" 2>&1 || true
done < <(kubectl -n "$NAMESPACE" get pods -o name 2>/dev/null || true)
# A dedicated port-forward also works after the test runner's forwards have closed.
kubectl -n "$NAMESPACE" port-forward --address 127.0.0.1 svc/wiremock 18082:8080 >"$DEST/wiremock-forward.log" 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true' EXIT
for ((attempt=0;attempt<30;attempt++)); do
 if curl -fsS --max-time 1 http://127.0.0.1:18082/__admin/health >/dev/null 2>&1; then break; fi
 kill -0 "$pid" 2>/dev/null || break
 sleep 0.1
done
for endpoint in requests requests/unmatched mappings scenarios; do
 curl -fsS --max-time 3 "http://127.0.0.1:18082/__admin/$endpoint" >"$DEST/wiremock-${endpoint//\//-}.json" 2>/dev/null || true
done
kubectl get --raw '/api/v1/namespaces/e2e/services/nats:8222/proxy/jsz?consumers=true&streams=true&config=true' >"$DEST/jetstream.json" 2>&1 || true
cat "$DEST/pods.txt"
echo "Logs, events, probe failures, restarts, JetStream state and WireMock journals: $DEST"
