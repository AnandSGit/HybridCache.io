# Makefile for the HybridCache.io Go module.
#
# `make` (or `make help`) lists the targets. The Python package has its own
# Makefile in python/; the py-<target> targets run it from here.

.DEFAULT_GOAL := help

GO             ?= go
GOFMT          ?= gofmt
DOCKER_COMPOSE ?= docker compose

# The linters run through `go run pkg@version`, which builds them outside this
# module and never edits go.mod or go.sum. Set GOLANGCI_LINT=golangci-lint to
# use an installed golangci-lint v2 binary instead.
GOLANGCI_LINT_VERSION ?= v2.15.0
GOLANGCI_LINT         ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK           ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@latest

# Connection strings for test-integration and run-examples. The defaults match
# docker-compose.yml; override them in the environment or on the command line.
POSTGRES_DSN ?= postgres://testuser:testpass@localhost:5432/testdb?sslmode=disable
MONGODB_URI  ?= mongodb://localhost:27017/?replicaSet=rs0&directConnection=true

COVERAGE_PROFILE ?= coverage.out
COVERPKG         := ./internal/...,./pkg/...

# The repository's Go files: tracked, or new and not ignored.
GO_FILES = $(wildcard $(shell git ls-files --cached --others --exclude-standard -- '*.go'))

# Every directory under cmd/ or examples/ that holds a main.go.
EXAMPLE_DIRS = $(sort $(patsubst %/main.go,%,$(wildcard cmd/*/main.go examples/*/main.go)))

# Targets of python/Makefile that are available here as py-<target>.
PY_TARGETS := install-dev test test-unit test-integration lint format type-check coverage
PY_GOALS   := $(addprefix py-,$(PY_TARGETS))

.PHONY: help build vet fmt fmt-check test test-integration coverage lint vuln \
	tidy check dev-up dev-down run-examples clean $(PY_GOALS)

help: ## Show this help
	@echo "Usage: make <target> [VARIABLE=value ...]"
	@echo
	@awk 'BEGIN { FS = ":.*## " } /^[a-z][a-z-]*:.*## / { printf "  %-17s %s\n", $$1, $$2 }' $(firstword $(MAKEFILE_LIST))
	@printf '  %-17s %s\n' 'py-<target>' 'Run make <target> in python/, for: $(PY_TARGETS)'
	@echo
	@echo "Variables: POSTGRES_DSN and MONGODB_URI (default to the docker-compose.yml"
	@echo "databases), GOLANGCI_LINT, DOCKER_COMPOSE, COVERAGE_PROFILE."

build: ## Compile every package (writes no binaries)
	$(GO) build ./...

vet: ## Run go vet, including the files behind the integration build tag
	$(GO) vet -tags integration ./...

fmt: ## Format the Go files with gofmt -s (lists the files it changed)
	@files="$(GO_FILES)"; \
	test -n "$$files" || { echo "fmt: no Go files found" >&2; exit 1; }; \
	$(GOFMT) -s -l -w $$files

fmt-check: ## Fail if a Go file is not gofmt -s formatted or does not parse
	@files="$(GO_FILES)"; \
	test -n "$$files" || { echo "fmt-check: no Go files found" >&2; exit 1; }; \
	unformatted="$$($(GOFMT) -s -l $$files)"; status=$$?; \
	if [ -n "$$unformatted" ]; then \
		echo "Not gofmt -s formatted (run 'make fmt'):"; \
		echo "$$unformatted"; \
	fi; \
	[ "$$status" -eq 0 ] && [ -z "$$unformatted" ]

test: ## Run the unit tests with the race detector
	$(GO) test -race -count=1 ./...

test-integration: export POSTGRES_DSN := $(POSTGRES_DSN)
test-integration: export MONGODB_URI := $(MONGODB_URI)
test-integration: ## Run the integration tests against POSTGRES_DSN and MONGODB_URI (make dev-up first)
	$(GO) test -tags integration -race -count=1 ./...

coverage: ## Unit-test coverage of ./internal/... and ./pkg/... (writes coverage.out)
	$(GO) test -count=1 -coverpkg=$(COVERPKG) -coverprofile=$(COVERAGE_PROFILE) ./...
	$(GO) tool cover -func=$(COVERAGE_PROFILE)

lint: ## Run golangci-lint v2 with .golangci.yml
	$(GOLANGCI_LINT) run ./...

vuln: ## Check dependencies for known vulnerabilities with govulncheck
	$(GOVULNCHECK) ./...

tidy: ## Run go mod tidy
	$(GO) mod tidy

check: fmt-check vet lint test ## Run fmt-check, vet, lint and test

dev-up: ## Start PostgreSQL and MongoDB (docker-compose.yml) and wait until they are healthy
	$(DOCKER_COMPOSE) up -d --wait

dev-down: ## Stop the dev databases (their data volumes are kept)
	$(DOCKER_COMPOSE) down

run-examples: export POSTGRES_DSN := $(POSTGRES_DSN)
run-examples: export MONGODB_URI := $(MONGODB_URI)
run-examples: ## Run each example program with go run (make dev-up first)
	@set -e; for dir in $(EXAMPLE_DIRS); do \
		echo "$(GO) run ./$$dir"; \
		$(GO) run ./$$dir; \
	done

clean: ## Remove the coverage profile
	rm -f $(COVERAGE_PROFILE)

# python/Makefile answers unknown targets with exit status 0, so check that the
# target exists before delegating.
$(PY_GOALS): py-%:
	@grep -Eq '^$*:' python/Makefile || { echo "python/Makefile has no '$*' target" >&2; exit 1; }
	$(MAKE) -C python $*
