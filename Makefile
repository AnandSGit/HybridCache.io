# Database Agnostic Storage Library Makefile

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Project parameters
BINARY_NAME=storage
BINARY_UNIX=$(BINARY_NAME)_unix
MAIN_PATH=./cmd/example
PKG_LIST=$$(go list ./... | grep -v /vendor/)

# Build flags
BUILD_FLAGS=-v
TEST_FLAGS=-v -race -coverprofile=coverage.out
LINT_FLAGS=run --timeout=5m

# Docker parameters
DOCKER_COMPOSE=docker-compose
COMPOSE_FILE=docker-compose.yml

.PHONY: all build clean test test-coverage test-integration lint fmt deps deps-update help

# Default target
all: clean deps fmt lint test build

# Build the binary
build:
	@echo "Building..."
	$(GOBUILD) $(BUILD_FLAGS) -o $(BINARY_NAME) $(MAIN_PATH)

# Build for Linux
build-linux:
	@echo "Building for Linux..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) -o $(BINARY_UNIX) $(MAIN_PATH)

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)
	rm -f coverage.out
	rm -f coverage.html

# Run tests
test:
	@echo "Running unit tests..."
	$(GOTEST) $(TEST_FLAGS) ./...

# Run tests with coverage
test-coverage: test
	@echo "Generating coverage report..."
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run integration tests
test-integration:
	@echo "Starting test databases..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) up -d
	@echo "Waiting for databases to be ready..."
	sleep 10
	@echo "Running integration tests..."
	$(GOTEST) $(TEST_FLAGS) -tags=integration ./...
	@echo "Stopping test databases..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) down

# Run benchmarks
benchmark:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Lint the code
lint:
	@echo "Running linter..."
	$(GOLINT) $(LINT_FLAGS) ./...

# Format the code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Check formatting
fmt-check:
	@echo "Checking code formatting..."
	@if [ -n "$$($(GOFMT) -l .)" ]; then \
		echo "Code is not formatted. Run 'make fmt' to fix."; \
		$(GOFMT) -l .; \
		exit 1; \
	fi

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Update dependencies
deps-update:
	@echo "Updating dependencies..."
	$(GOMOD) get -u ./...
	$(GOMOD) tidy

# Verify dependencies
deps-verify:
	@echo "Verifying dependencies..."
	$(GOMOD) verify

# Install development tools
install-tools:
	@echo "Installing development tools..."
	$(GOGET) github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Start development environment
dev-up:
	@echo "Starting development environment..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) up -d

# Stop development environment
dev-down:
	@echo "Stopping development environment..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) down

# View development environment logs
dev-logs:
	@echo "Viewing development environment logs..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) logs -f

# Run example application
run-example:
	@echo "Running example application..."
	$(GOCMD) run $(MAIN_PATH)/main.go

# Generate documentation
docs:
	@echo "Generating documentation..."
	$(GOCMD) doc -all ./pkg/storage > docs/api-reference.md

# Security scan
security:
	@echo "Running security scan..."
	$(GOGET) github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	gosec ./...

# Check for vulnerabilities
vuln-check:
	@echo "Checking for vulnerabilities..."
	$(GOGET) golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

# Run all quality checks
quality: fmt-check lint test vuln-check
	@echo "All quality checks passed!"

# Release preparation
release-prep: clean deps quality test-integration
	@echo "Release preparation complete!"

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t hybridcache/storage:latest .

# Docker run
docker-run:
	@echo "Running Docker container..."
	docker run --rm -it hybridcache/storage:latest

# Performance profiling
profile-cpu:
	@echo "Running CPU profiling..."
	$(GOTEST) -cpuprofile=cpu.prof -bench=. ./...
	$(GOCMD) tool pprof cpu.prof

profile-mem:
	@echo "Running memory profiling..."
	$(GOTEST) -memprofile=mem.prof -bench=. ./...
	$(GOCMD) tool pprof mem.prof

# Database operations
db-migrate-up:
	@echo "Running database migrations..."
	$(GOCMD) run ./cmd/migrate up

db-migrate-down:
	@echo "Rolling back database migrations..."
	$(GOCMD) run ./cmd/migrate down

db-seed:
	@echo "Seeding database with test data..."
	$(GOCMD) run ./cmd/seed

# Help
help:
	@echo "Available targets:"
	@echo "  build           - Build the binary"
	@echo "  build-linux     - Build for Linux"
	@echo "  clean           - Clean build artifacts"
	@echo "  test            - Run unit tests"
	@echo "  test-coverage   - Run tests with coverage report"
	@echo "  test-integration- Run integration tests"
	@echo "  benchmark       - Run benchmarks"
	@echo "  lint            - Run linter"
	@echo "  fmt             - Format code"
	@echo "  fmt-check       - Check code formatting"
	@echo "  deps            - Download dependencies"
	@echo "  deps-update     - Update dependencies"
	@echo "  deps-verify     - Verify dependencies"
	@echo "  install-tools   - Install development tools"
	@echo "  dev-up          - Start development environment"
	@echo "  dev-down        - Stop development environment"
	@echo "  dev-logs        - View development environment logs"
	@echo "  run-example     - Run example application"
	@echo "  docs            - Generate documentation"
	@echo "  security        - Run security scan"
	@echo "  vuln-check      - Check for vulnerabilities"
	@echo "  quality         - Run all quality checks"
	@echo "  release-prep    - Prepare for release"
	@echo "  docker-build    - Build Docker image"
	@echo "  docker-run      - Run Docker container"
	@echo "  profile-cpu     - Run CPU profiling"
	@echo "  profile-mem     - Run memory profiling"
	@echo "  db-migrate-up   - Run database migrations"
	@echo "  db-migrate-down - Roll back database migrations"
	@echo "  db-seed         - Seed database with test data"
	@echo "  help            - Show this help message"
