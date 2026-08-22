OAPI_CODEGEN_VERSION := v2.8.0
OAPI_CODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)
SPECTRAL := npx --yes @stoplight/spectral-cli@6
OASDIFF := go run github.com/oasdiff/oasdiff@latest

SPECS := contracts/auth/v1/openapi.yaml

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: generate-go
generate-go: ## Regenerate the Go clients from contracts (add a line per contract)
	cd packages/go/generated/authv1 && $(OAPI_CODEGEN) -config gen.yaml ../../../../contracts/auth/v1/openapi.yaml

.PHONY: generate-ts
generate-ts: ## Scaffolded: TS codegen runs per contract once a TS consumer exists
	@echo "Run per contract, e.g.:"
	@echo "  bunx openapi-typescript contracts/auth/v1/openapi.yaml -o packages/ts/src/auth/v1/index.ts"

.PHONY: tidy
tidy: ## go mod tidy the Go package
	cd packages/go && go mod tidy

.PHONY: build
build: ## Build the Go package
	cd packages/go && go build ./...

.PHONY: test
test: ## Test the Go package
	cd packages/go && go test ./...

.PHONY: lint
lint: ## Lint all OpenAPI specs with spectral
	$(SPECTRAL) lint $(SPECS)

.PHONY: breaking
breaking: ## Check for breaking changes vs BASE (default origin/main)
	$(OASDIFF) breaking $(BASE):contracts/auth/v1/openapi.yaml contracts/auth/v1/openapi.yaml --fail-on WARN

.PHONY: verify
verify: generate-go build test ## Regenerate, build and test (CI runs this)
	git diff --exit-code -- contracts packages

.PHONY: release-patch release-minor release-major
release-patch release-minor release-major: ## Regenerate, commit and tag a release (usage: make release-patch)
	$(MAKE) generate-go tidy
	@V=$$(git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0); \
	MAJOR=$$(echo $$V | cut -d. -f1 | tr -d v); \
	MINOR=$$(echo $$V | cut -d. -f2); \
	PATCH=$$(echo $$V | cut -d. -f3); \
	case "$(@:release-%=%)" in \
	  patch) PATCH=$$((PATCH+1)) ;; \
	  minor) MINOR=$$((MINOR+1)); PATCH=0 ;; \
	  major) MAJOR=$$((MAJOR+1)); MINOR=0; PATCH=0 ;; \
	esac; \
	NV=v$$MAJOR.$$MINOR.$$PATCH; \
	git add contracts packages && \
	git commit -m "chore: release $$NV" && \
	git tag $$NV && \
	git tag packages/go/$$NV && \
	echo "Released $$NV — push with: git push && git push --tags"
