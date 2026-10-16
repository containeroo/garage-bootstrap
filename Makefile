.DEFAULT_GOAL := help

BIN := garage-bootstrap
VERSION ?= dev

# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.15.0

# renovate: datasource=github-releases depName=gi8lino/dev-tools
DEV_TOOLS_VERSION ?= v0.9.0

GOLANGCI_LINT := bin/golangci-lint
ADMIN_URL ?= http://127.0.0.1:3903
CONFIG ?= deploy/bootstrap.yaml

## Shared development tools

include bin/dev-tools.mk
include $(call dev-tools-module,tag)
include $(call dev-tools-module,help)

##@ Development

.PHONY: download
download: ## Download Go dependencies.
	go mod download

.PHONY: run
run: ## Run garage-bootstrap against a local Garage instance.
	go run ./cmd/garage-bootstrap \
		--admin-url="$(ADMIN_URL)" \
		--config="$(CONFIG)" \
		--listen-address="127.0.0.1:8080" \
		$(RUN_ARGS)

##@ Build

.PHONY: build
build: ## Build the binary into bin/.
	@mkdir -p bin
	go build -trimpath -ldflags "-X main.Version=$(VERSION)" -o bin/$(BIN) ./cmd/garage-bootstrap

.PHONY: docker
docker: ## Build the local Docker image.
	docker build --build-arg VERSION="$(VERSION)" -t $(BIN):$(VERSION) .

##@ Quality

.PHONY: fmt
fmt: ## Format Go source files.
	go fmt ./...

.PHONY: test
test: ## Run unit tests.
	go test ./...

.PHONY: test-race
test-race: ## Run tests with the race detector.
	go test -race ./...

.PHONY: vet
vet: ## Run Go static analysis.
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint.
	$(GOLANGCI_LINT) run ./...

##@ Dependencies

.PHONY: dev-tools
dev-tools: $(DEV_TAG) $(MAKE_HELP) $(GO_INSTALL_TOOL) golangci-lint ## Download pinned development tools.

.PHONY: golangci-lint
golangci-lint: $(GO_INSTALL_TOOL) ## Install golangci-lint locally when necessary.
	@$(GO_INSTALL_TOOL) \
		--target "$(GOLANGCI_LINT)" \
		--package github.com/golangci/golangci-lint/v2/cmd/golangci-lint \
		--tool-version "$(GOLANGCI_LINT_VERSION)"
