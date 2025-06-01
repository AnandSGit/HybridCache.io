# Makefile for Database Agnostic Storage Library (Python)

.PHONY: help install install-dev test test-unit test-integration test-performance
.PHONY: lint format type-check security quality coverage benchmark
.PHONY: docs docs-serve clean dev-up dev-down build publish
.PHONY: pre-commit-install pre-commit-run

# Default target
help: ## Show this help message
	@echo "Database Agnostic Storage Library (Python) - Development Commands"
	@echo "================================================================="
	@echo ""
	@echo "Setup Commands:"
	@echo "  install          Install production dependencies"
	@echo "  install-dev      Install development dependencies"
	@echo "  pre-commit-install Install pre-commit hooks"
	@echo ""
	@echo "Development Environment:"
	@echo "  dev-up           Start development databases (Docker)"
	@echo "  dev-down         Stop development databases"
	@echo ""
	@echo "Testing Commands:"
	@echo "  test             Run all tests"
	@echo "  test-unit        Run unit tests only"
	@echo "  test-integration Run integration tests only"
	@echo "  test-performance Run performance tests"
	@echo "  coverage         Run tests with coverage report"
	@echo "  benchmark        Run benchmark tests"
	@echo ""
	@echo "Code Quality:"
	@echo "  lint             Run linting (ruff)"
	@echo "  format           Format code (black + isort)"
	@echo "  type-check       Run type checking (mypy)"
	@echo "  security         Run security scanning (bandit + safety)"
	@echo "  quality          Run all quality checks"
	@echo "  pre-commit-run   Run pre-commit hooks on all files"
	@echo ""
	@echo "Documentation:"
	@echo "  docs             Build documentation"
	@echo "  docs-serve       Serve documentation locally"
	@echo ""
	@echo "Build & Release:"
	@echo "  build            Build package"
	@echo "  publish          Publish to PyPI"
	@echo "  clean            Clean build artifacts"

# Python and Poetry commands
PYTHON := python
POETRY := poetry
PYTEST := $(POETRY) run pytest
MYPY := $(POETRY) run mypy
BLACK := $(POETRY) run black
ISORT := $(POETRY) run isort
RUFF := $(POETRY) run ruff
BANDIT := $(POETRY) run bandit
SAFETY := $(POETRY) run safety
SPHINX := $(POETRY) run sphinx-build

# Directories
SRC_DIR := src
TEST_DIR := tests
DOCS_DIR := docs
BUILD_DIR := dist

# Test settings
TEST_ARGS := -v --tb=short
COVERAGE_ARGS := --cov=$(SRC_DIR) --cov-report=html --cov-report=term --cov-report=xml
INTEGRATION_ARGS := -m integration
PERFORMANCE_ARGS := -m benchmark --benchmark-only

# Setup Commands
install: ## Install production dependencies
	$(POETRY) install --only=main

install-dev: ## Install development dependencies
	$(POETRY) install
	$(POETRY) run pre-commit install

pre-commit-install: ## Install pre-commit hooks
	$(POETRY) run pre-commit install

pre-commit-run: ## Run pre-commit hooks on all files
	$(POETRY) run pre-commit run --all-files

# Development Environment
dev-up: ## Start development databases
	docker-compose up -d postgres redis
	@echo "Waiting for databases to be ready..."
	@sleep 5
	@docker-compose exec postgres pg_isready -U testuser -d testdb || echo "PostgreSQL not ready yet"
	@docker-compose exec redis redis-cli ping || echo "Redis not ready yet"

dev-down: ## Stop development databases
	docker-compose down

# Testing Commands
test: ## Run all tests
	$(PYTEST) $(TEST_ARGS) $(TEST_DIR)

test-unit: ## Run unit tests only
	$(PYTEST) $(TEST_ARGS) $(TEST_DIR)/unit

test-integration: ## Run integration tests only
	$(PYTEST) $(TEST_ARGS) $(INTEGRATION_ARGS) $(TEST_DIR)/integration

test-performance: ## Run performance tests
	$(PYTEST) $(TEST_ARGS) $(PERFORMANCE_ARGS) $(TEST_DIR)/performance

coverage: ## Run tests with coverage report
	$(PYTEST) $(TEST_ARGS) $(COVERAGE_ARGS) $(TEST_DIR)
	@echo "Coverage report generated in htmlcov/"

benchmark: ## Run benchmark tests
	$(PYTEST) $(PERFORMANCE_ARGS) $(TEST_DIR)/performance --benchmark-sort=mean

# Code Quality
lint: ## Run linting
	$(RUFF) check $(SRC_DIR) $(TEST_DIR)

format: ## Format code
	$(BLACK) $(SRC_DIR) $(TEST_DIR) examples/
	$(ISORT) $(SRC_DIR) $(TEST_DIR) examples/

type-check: ## Run type checking
	$(MYPY) $(SRC_DIR)

security: ## Run security scanning
	$(BANDIT) -r $(SRC_DIR) -f json -o bandit-report.json || true
	$(BANDIT) -r $(SRC_DIR)
	$(SAFETY) check --json --output safety-report.json || true
	$(SAFETY) check

quality: lint type-check security ## Run all quality checks
	@echo "All quality checks completed!"

# Documentation
docs: ## Build documentation
	$(SPHINX) -b html $(DOCS_DIR) $(DOCS_DIR)/_build/html

docs-serve: docs ## Serve documentation locally
	@echo "Serving documentation at http://localhost:8000"
	cd $(DOCS_DIR)/_build/html && $(PYTHON) -m http.server 8000

# Build & Release
build: clean ## Build package
	$(POETRY) build

publish: build ## Publish to PyPI
	$(POETRY) publish

clean: ## Clean build artifacts
	rm -rf $(BUILD_DIR)
	rm -rf .pytest_cache
	rm -rf .mypy_cache
	rm -rf .ruff_cache
	rm -rf htmlcov
	rm -rf .coverage
	rm -rf *.egg-info
	find . -type d -name __pycache__ -delete
	find . -type f -name "*.pyc" -delete
	find . -type f -name "*.pyo" -delete
	find . -type f -name "*~" -delete
	rm -f bandit-report.json safety-report.json

# Development helpers
check-deps: ## Check for dependency vulnerabilities
	$(POETRY) show --outdated
	$(SAFETY) check

update-deps: ## Update dependencies
	$(POETRY) update

lock: ## Update lock file
	$(POETRY) lock

shell: ## Open poetry shell
	$(POETRY) shell

# Docker commands
docker-build: ## Build Docker image
	docker build -t storage-python:latest .

docker-test: ## Run tests in Docker
	docker run --rm -v $(PWD):/app storage-python:latest make test

# Performance profiling
profile: ## Run performance profiling
	$(POETRY) run py-spy record -o profile.svg -- $(PYTHON) -m pytest $(TEST_DIR)/performance -v

memory-profile: ## Run memory profiling
	$(POETRY) run mprof run $(PYTHON) -m pytest $(TEST_DIR)/performance -v
	$(POETRY) run mprof plot

# CI/CD helpers
ci-install: ## Install dependencies for CI
	$(POETRY) install --no-dev
	$(POETRY) install --only=dev

ci-test: ## Run tests for CI
	$(PYTEST) $(TEST_ARGS) $(COVERAGE_ARGS) $(TEST_DIR) --junitxml=junit.xml

ci-quality: ## Run quality checks for CI
	$(RUFF) check $(SRC_DIR) $(TEST_DIR) --output-format=github
	$(MYPY) $(SRC_DIR) --junit-xml mypy-report.xml
	$(BANDIT) -r $(SRC_DIR) -f json -o bandit-report.json
	$(SAFETY) check --json --output safety-report.json

# Example commands
run-example-basic: ## Run basic usage example
	$(POETRY) run python examples/basic_usage.py

run-example-performance: ## Run performance example
	$(POETRY) run python examples/async_performance.py

run-example-transaction: ## Run transaction example
	$(POETRY) run python examples/transaction_demo.py

# Validation commands (for CI)
validate: quality test ## Run full validation suite
	@echo "✅ All validation checks passed!"

validate-fast: lint type-check test-unit ## Run fast validation (no integration tests)
	@echo "✅ Fast validation checks passed!"

# Help target (must be last)
%:
	@echo "Unknown target: $@"
	@echo "Run 'make help' to see available targets."
