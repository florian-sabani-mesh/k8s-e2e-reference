#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
need kind
if kind get clusters | grep -qx "$CLUSTER"; then
 [[ -f .run/cluster-owned ]] || { echo "Refusing to reuse an unowned cluster named $CLUSTER" >&2; exit 1; }
 kind export kubeconfig --name "$CLUSTER" --kubeconfig "$KUBECONFIG"
else
 touch .run/cluster-owned
 kind create cluster --name "$CLUSTER" --config kind/cluster.yaml --kubeconfig "$KUBECONFIG" --wait 120s
fi
kubectl cluster-info
