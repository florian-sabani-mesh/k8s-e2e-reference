#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
for module in pkg/events pkg/runtime services/gateway services/orders services/pricing services/settlement services/exchange; do
 (cd "$module" && GOWORK=off go test -race ./... && GOWORK=off go vet ./...)
done
unformatted="$(gofmt -l pkg services)"
[[ -z "$unformatted" ]] || { echo "$unformatted"; exit 1; }
terraform fmt -check -recursive infra
tf init -backend=false -input=false
tf validate
npm --prefix e2e ci --no-audit --no-fund
npm --prefix e2e run typecheck
for script in hack/*.sh; do bash -n "$script"; done
