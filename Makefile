SHELL := /bin/bash
.DEFAULT_GOAL := help
.PHONY: help cluster build load-images infra wait-ready test-e2e diagnostics destroy e2e check
help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
cluster: ## Create the local kind cluster
	@./hack/create-cluster.sh
build: ## Build all four Go images
	@./hack/build-images.sh
load-images: ## Load application images into kind
	@./hack/load-images.sh
infra: ## Apply Terraform, including database migrations
	@./hack/infra.sh
wait-ready: ## Wait for every workload to become ready
	@./hack/wait-ready.sh
test-e2e: ## Forward local ports and run Vitest (E2E_FILTER=happy-path)
	@./hack/test-e2e.sh
diagnostics: ## Save logs, events, probes, and WireMock journals
	@./hack/diagnostics.sh
destroy: ## Destroy Terraform resources and delete the cluster
	@./hack/delete-cluster.sh
e2e: ## Full lifecycle; KEEP_CLUSTER=1 and SKIP_BUILD=1 are supported
	@./hack/e2e.sh
check: ## Go tests/race/vet, formatting, Terraform validation, TypeScript
	@./hack/check.sh
