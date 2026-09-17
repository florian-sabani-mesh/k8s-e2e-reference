#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
[[ -f .run/cluster-owned ]] || { echo "No repository-owned cluster to delete."; exit 0; }
status=0
if [[ -f .run/terraform.tfstate ]]; then
 tf destroy -input=false -auto-approve || status=$?
fi
kind delete cluster --name "$CLUSTER"
# A deleted cluster cannot retain managed resources. Archive state for diagnostics
# before starting the next fresh run (including recovery from partial apply).
for file in .run/terraform.tfstate .run/terraform.tfstate.backup; do
 if [[ -f "$file" ]]; then mv "$file" "artifacts/$(basename "$file").last-destroy"; fi
done
rm -f .run/cluster-owned .run/kubeconfig
echo "Cluster deleted (Terraform destroy status: $status)."
exit "$status"
