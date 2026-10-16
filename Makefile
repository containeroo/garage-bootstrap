.DEFAULT_GOAL := help

## Frontend

BIN := garage-bootstrap
VERSION ?= dev

NPX ?= npx

## Tool Versions

# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.14.0

# renovate: datasource=github-releases depName=gi8lino/dev-tools
DEV_TOOLS_VERSION ?= v0.9.0


## Formatting

PRETTIER_MD_SOURCES := README.md "**/*.md"

## Shared development tools

include bin/dev-tools.mk
include $(call dev-tools-module,tag)
include $(call dev-tools-module,help)

##@ Formatting

.PHONY: fmt
fmt: fmt-web fmt-templates fmt-go fmt-md ## Format all supported files.

.PHONY: fmt-web
fmt-web: $(NODE_MODULES) ## Format CSS and TypeScript source.
	$(NPX) prettier --write \
		"web/src/**/*.css" \
		"web/src/**/*.ts" \
		"test/**/*.ts"

.PHONY: fmt-templates
fmt-templates: ## Format Go HTML templates.
	djlint web/src/templates --reformat

.PHONY: fmt-go
fmt-go: ## Format Go code.
	go fmt ./...

.PHONY: fmt-md
fmt-md: $(NODE_MODULES) ## Format Markdown files.
	$(NPX) prettier --write --prose-wrap never $(PRETTIER_MD_SOURCES)


##@ Dependencies

.PHONY: dev-tools
dev-tools: $(DEV_PORT) $(OPEN_BROWSER) $(DEV_TAG) $(MAKE_HELP) $(GO_INSTALL_TOOL) $(FAVICON_GENERATE) $(SVG_TO_PNG) ## Download the pinned development tools.

.PHONY: golangci-lint
golangci-lint: $(GO_INSTALL_TOOL) ## Download golangci-lint locally if necessary.
	@$(GO_INSTALL_TOOL) \
		--target "$(GOLANGCI_LINT)" \
		--package github.com/golangci/golangci-lint/v2/cmd/golangci-lint \
		--tool-version "$(GOLANGCI_LINT_VERSION)"
