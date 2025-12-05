# CLAUDE.md - AI Assistant Guide for HybridCache.io

**Last Updated**: 2025-12-05
**Repository**: github.com/AnandSGit/HybridCache.io
**Purpose**: Database Agnostic Storage Library with Multi-Language Support

---

## 📋 Table of Contents

1. [Project Overview](#project-overview)
2. [Architecture & Structure](#architecture--structure)
3. [Development Workflows](#development-workflows)
4. [Key Conventions](#key-conventions)
5. [Testing Strategy](#testing-strategy)
6. [Git Workflow](#git-workflow)
7. [Code Quality Standards](#code-quality-standards)
8. [Common Tasks](#common-tasks)
9. [Important Files & Locations](#important-files--locations)
10. [Multi-Language Implementation](#multi-language-implementation)
11. [AI Assistant Guidelines](#ai-assistant-guidelines)

---

## Project Overview

### What is HybridCache.io?

HybridCache.io is a **production-ready**, **database-agnostic storage library** that provides a unified interface across multiple database types while maintaining the unique strengths of each database system. The library is available in both **Go** and **Python** with full feature parity.

### Current Status

- ✅ **Production Ready MVP** - Fully implemented and tested
- ✅ **Go Implementation** - Complete with PostgreSQL and MongoDB adapters
- ✅ **Python Implementation** - Complete with async/await patterns
- ✅ **95%+ Test Coverage** - Comprehensive unit and integration tests
- ✅ **CI/CD Pipeline** - Automated testing and quality gates

### Supported Databases

**Tier 1 (Production Ready)**:
- PostgreSQL 12+ (Go & Python)
- MongoDB 4.4+ (Go & Python)

**Tier 2 (Framework Ready)**:
- SQLite 3.35+
- MySQL 8.0+
- Redis 6.0+

**Tier 3 (Planned)**:
- CockroachDB
- DynamoDB
- Cassandra

---

## Architecture & Structure

### Clean Architecture Principles

The project follows **Clean Architecture** with clear separation of concerns:

```
┌─────────────────────────────────────────────────────────┐
│                    Application Layer                    │
│                  (cmd/, examples/)                       │
├─────────────────────────────────────────────────────────┤
│                  Public API Layer                        │
│                   (pkg/storage/)                         │
├─────────────────────────────────────────────────────────┤
│                  Application Layer                       │
│            (internal/application/query/)                 │
├─────────────────────────────────────────────────────────┤
│                    Domain Layer                          │
│     (internal/domain/ - interfaces, types, errors)       │
├─────────────────────────────────────────────────────────┤
│                 Infrastructure Layer                     │
│         (internal/infrastructure/adapters/)              │
└─────────────────────────────────────────────────────────┘
```

### Directory Structure

```
HybridCache.io/
├── pkg/storage/                    # Public API (USE THIS FOR IMPORTS)
│   ├── storage.go                  # Core types re-export
│   ├── query.go                    # Query builder API
│   └── adapters.go                 # Adapter factories
│
├── internal/                       # Internal implementation
│   ├── domain/                     # Business logic (PURE)
│   │   ├── interfaces.go           # Core interfaces
│   │   ├── types.go               # Domain types
│   │   └── errors.go              # Error types
│   │
│   ├── application/               # Use cases
│   │   └── query/                 # Query building service
│   │       └── builder.go
│   │
│   └── infrastructure/            # External dependencies
│       └── adapters/              # Database adapters
│           ├── postgres/          # PostgreSQL implementation
│           │   ├── adapter.go
│           │   ├── result.go
│           │   ├── transaction.go
│           │   └── connection.go
│           └── mongodb/           # MongoDB implementation
│               ├── adapter.go
│               ├── result.go
│               ├── transaction.go
│               └── storage.go
│
├── cmd/                           # CLI applications
│   └── example/
│       └── main.go
│
├── examples/                      # Example code
│   ├── basic-usage/
│   ├── performance/
│   └── mongodb/
│
├── tests/                         # Test suites
│   ├── unit/
│   └── integration/
│
├── python/                        # Python implementation
│   ├── src/storage/               # Python core library
│   │   ├── interfaces.py          # Async ABC interfaces
│   │   ├── types.py
│   │   ├── errors.py
│   │   ├── query/
│   │   └── adapters/
│   │       ├── postgresql/
│   │       └── mongodb/
│   ├── tests/
│   └── examples/
│
├── scripts/                       # Utility scripts
├── docs/                          # Documentation
├── .github/workflows/             # CI/CD pipelines
├── Makefile                       # Build automation
├── go.mod                         # Go module definition
└── docker-compose.yml             # Development environment
```

### Key Architectural Decisions

1. **Module Structure**: Single Go module with `internal/` for private code
2. **Import Path**: `github.com/AnandSGit/HybridCache.io`
3. **Public API**: All external imports through `pkg/storage`
4. **Dependency Flow**: Dependencies point inward (Infrastructure → Application → Domain)
5. **Interface-Driven**: Domain defines interfaces, infrastructure implements them

---

## Development Workflows

### Setting Up Development Environment

#### Go Development

```bash
# Clone repository
git clone https://github.com/AnandSGit/HybridCache.io.git
cd HybridCache.io

# Download dependencies
go mod download
go mod tidy

# Start test databases
make dev-up

# Run tests
make test

# Run example
go run cmd/example/main.go
```

#### Python Development

```bash
# Navigate to Python directory
cd python

# Install dependencies
make install-dev

# Start databases
make dev-up

# Run tests
make test

# Run example
python examples/basic_usage.py
```

### Making Changes

1. **Always read existing code first** before making changes
2. **Follow the layer architecture**:
   - Domain changes → `internal/domain/`
   - Application logic → `internal/application/`
   - Database adapters → `internal/infrastructure/adapters/`
   - Public API → `pkg/storage/`
3. **Update tests** alongside code changes
4. **Run quality checks** before committing

---

## Key Conventions

### Go Conventions

#### Import Organization

```go
import (
    // Standard library
    "context"
    "fmt"

    // External dependencies
    "github.com/jackc/pgx/v5"

    // Internal imports
    "github.com/AnandSGit/HybridCache.io/internal/domain"
    "github.com/AnandSGit/HybridCache.io/pkg/storage"
)
```

#### Interface Naming

- Interfaces use noun names: `Storage`, `Adapter`, `QueryBuilder`
- Method names are verb-based: `Query()`, `Execute()`, `Connect()`
- Avoid `-er` suffix redundancy (e.g., `Storage` not `Storager`)

#### Error Handling

```go
// Use domain errors with context
if err != nil {
    return nil, domain.NewQueryError(
        "failed to execute query",
        err,
        domain.ErrorCodeQueryExecution,
    ).WithContext("table", tableName)
}
```

#### File Naming

- Implementation files: `adapter.go`, `storage.go`, `builder.go`
- Test files: `adapter_test.go`, `storage_test.go`
- Interface definitions: `interfaces.go`
- Type definitions: `types.go`
- Error definitions: `errors.go`

### Python Conventions

#### Import Organization

```python
# Standard library
import asyncio
from typing import Optional, List

# External dependencies
import asyncpg

# Internal imports
from storage.interfaces import Storage, Adapter
from storage.types import Config, Query
from storage.errors import QueryError
```

#### Async/Await Pattern

```python
# All I/O operations are async
async def query(self, query: Query) -> Result:
    async with self.pool.acquire() as conn:
        rows = await conn.fetch(query.sql, *query.parameters)
        return Result(rows)
```

#### Type Hints

- **100% type coverage required**
- Use `typing` module for all function signatures
- Comply with `mypy --strict` mode

### Naming Conventions

#### Database Adapters

- Class/package name: `PostgreSQLAdapter`, `MongoDBAdapter`
- Factory function: `NewPostgreSQLAdapter()`, `NewMongoDBAdapter()`
- File location: `internal/infrastructure/adapters/postgres/`

#### Query Builder

- Fluent API with method chaining
- Method names match SQL clauses: `Select()`, `From()`, `Where()`
- Condition helpers: `Equal()`, `Like()`, `In()`, `Between()`

#### Testing

- Unit tests: `TestAdapterConnect`, `TestQueryBuilderSelect`
- Integration tests: `TestPostgreSQLIntegration`
- Benchmarks: `BenchmarkQueryBuilder`

---

## Testing Strategy

### Test Coverage Requirements

- **Minimum**: 95% coverage
- **Domain Layer**: 100% coverage (pure business logic)
- **Adapters**: 95%+ coverage
- **Integration**: All critical paths covered

### Test Organization

```
tests/
├── unit/                          # Unit tests (fast, isolated)
│   ├── domain/
│   ├── query/
│   └── postgres/
└── integration/                   # Integration tests (require DB)
    ├── postgres_test.go
    └── mongodb_test.go
```

### Running Tests

#### Go Tests

```bash
# All tests
make test

# Unit tests only
go test ./pkg/... ./internal/...

# Integration tests (requires Docker)
make test-integration

# Coverage report
make test-coverage

# Benchmarks
make benchmark
```

#### Python Tests

```bash
# All tests
make test

# Unit tests
make test-unit

# Integration tests
make test-integration

# Coverage report
make coverage
```

### Test Writing Guidelines

1. **Arrange-Act-Assert** pattern
2. **Table-driven tests** for multiple scenarios
3. **Mock external dependencies** in unit tests
4. **Use testcontainers** for integration tests
5. **Test error paths** alongside happy paths

Example:

```go
func TestQueryBuilder_Select(t *testing.T) {
    tests := []struct {
        name     string
        fields   []string
        expected string
    }{
        {"single field", []string{"id"}, "SELECT id"},
        {"multiple fields", []string{"id", "name"}, "SELECT id, name"},
        {"all fields", []string{"*"}, "SELECT *"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Arrange
            builder := query.NewBuilder()

            // Act
            result := builder.Select(tt.fields...).Build()

            // Assert
            assert.Equal(t, tt.expected, result.SQL)
        })
    }
}
```

---

## Git Workflow

### Branch Strategy

- **Main branch**: Production-ready code
- **Feature branches**: `claude/claude-md-mishutf5m16963lr-01CK1FVjGkii3S1st16H8vf4`
- **Naming**: Always start with `claude/` for AI-generated branches

### Commit Conventions

#### Commit Message Format

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `refactor`: Code refactoring
- `test`: Test additions/changes
- `docs`: Documentation changes
- `chore`: Build/tooling changes

**Examples**:
```
feat(postgres): Add transaction savepoint support

Implement savepoint creation and rollback functionality
for PostgreSQL adapter to enable nested transactions.

Closes #123

---

fix(query): Handle NULL values in WHERE conditions

Update condition builder to properly handle nil values
and generate correct SQL for NULL comparisons.

---

refactor(domain): Extract common error handling

Move error wrapping logic to shared utility functions
to reduce code duplication across adapters.
```

### Making Commits

```bash
# Stage changes
git add <files>

# Create commit
git commit -m "$(cat <<'EOF'
feat(mongodb): Implement aggregation pipeline support

Add support for MongoDB aggregation pipeline operations
through the query builder interface.
EOF
)"

# Push to feature branch
git push -u origin claude/claude-md-mishutf5m16963lr-01CK1FVjGkii3S1st16H8vf4
```

### Pull Request Guidelines

When creating pull requests:

1. **Review all changes** from branch divergence point
2. **Create comprehensive PR description**:
   ```markdown
   ## Summary
   - Bullet points of changes
   - Why these changes were made

   ## Test Plan
   - [ ] Unit tests pass
   - [ ] Integration tests pass
   - [ ] Manual testing performed
   ```
3. **Use heredoc for PR body** to ensure formatting
4. **Include relevant issue numbers**

---

## Code Quality Standards

### Go Quality Standards

#### Linting

- **Tool**: golangci-lint v1.54+
- **Config**: `.golangci.yml`
- **Run**: `make lint`

Required checks:
- `gofmt` - Code formatting
- `govet` - Go vet analysis
- `errcheck` - Error handling
- `staticcheck` - Static analysis
- `gosec` - Security scanning

#### Security

```bash
# Vulnerability scanning
make vuln-check

# Security scan
make security
```

### Python Quality Standards

#### Tools

- **Formatter**: Black + isort
- **Linter**: Ruff
- **Type Checker**: mypy (strict mode)
- **Security**: Bandit + Safety

#### Running Checks

```bash
# Format code
make format

# Lint
make lint

# Type check
make type-check

# Security scan
make security

# All quality checks
make quality
```

### Code Review Checklist

- [ ] Follows clean architecture patterns
- [ ] No import cycles
- [ ] All tests pass
- [ ] Coverage meets minimum (95%)
- [ ] No security vulnerabilities
- [ ] Linting passes
- [ ] Type checking passes (Python)
- [ ] Documentation updated
- [ ] Examples updated if needed

---

## Common Tasks

### Adding a New Database Adapter

#### 1. Create Adapter Structure

```bash
# For Go
mkdir -p internal/infrastructure/adapters/mysql
touch internal/infrastructure/adapters/mysql/{adapter,connection,result,transaction}.go

# For Python
mkdir -p python/src/storage/adapters/mysql
touch python/src/storage/adapters/mysql/{__init__,adapter,connection,result,transaction}.py
```

#### 2. Implement Core Interfaces

**Go** - Implement `domain.Adapter` interface:
```go
type MySQLAdapter struct {
    // implementation
}

func (a *MySQLAdapter) Connect(ctx context.Context, config domain.Config) (domain.Storage, error) {
    // implementation
}

// Implement all domain.Adapter methods
```

**Python** - Implement `Storage` and `Adapter` ABCs:
```python
class MySQLAdapter(Adapter):
    async def connect(self, config: Config) -> Storage:
        # implementation
        pass

    # Implement all Adapter methods
```

#### 3. Add Factory Function

**Go** - In `pkg/storage/adapters.go`:
```go
func NewMySQLAdapter() Adapter {
    return mysql.NewAdapter()
}
```

**Python** - In `src/storage/adapters/__init__.py`:
```python
from .mysql import MySQLAdapter

__all__ = ['MySQLAdapter']
```

#### 4. Create Tests

- Unit tests for adapter
- Integration tests with testcontainers
- Performance benchmarks

#### 5. Update Documentation

- Add to README.md supported databases section
- Create adapter-specific documentation
- Add usage examples

### Extending Query Builder

#### 1. Add to Domain Interface

In `internal/domain/interfaces.go`:
```go
type QueryBuilder interface {
    // Existing methods...

    // New method
    Union(query QueryBuilder) QueryBuilder
}
```

#### 2. Implement in Application Layer

In `internal/application/query/builder.go`:
```go
func (b *Builder) Union(query QueryBuilder) QueryBuilder {
    // implementation
    return b
}
```

#### 3. Update Public API

In `pkg/storage/query.go`:
```go
// Re-export if needed
type QueryBuilder = domain.QueryBuilder
```

#### 4. Add Tests

```go
func TestQueryBuilder_Union(t *testing.T) {
    // Test implementation
}
```

### Running Performance Benchmarks

```bash
# Go benchmarks
go test -bench=. -benchmem ./...
make benchmark

# Python benchmarks
python examples/async_performance.py
```

### Building Docker Images

```bash
# Development image
docker build -f Dockerfile.dev -t hybridcache:dev .

# Production image
docker build -t hybridcache:latest .

# Test image
docker build -f Dockerfile.test -t hybridcache:test .
```

### Generating Documentation

```bash
# Go documentation
make docs

# Python documentation
cd python && make docs
```

---

## Important Files & Locations

### Configuration Files

| File | Purpose | When to Modify |
|------|---------|----------------|
| `go.mod` | Go dependencies | Adding/updating Go packages |
| `.golangci.yml` | Go linting config | Changing code quality rules |
| `Makefile` | Build automation | Adding new build targets |
| `docker-compose.yml` | Dev environment | Adding new services |
| `.github/workflows/ci.yml` | CI/CD pipeline | Changing CI process |
| `python/pyproject.toml` | Python config | Python dependencies/settings |

### Documentation Files

| File | Purpose | Keep Updated |
|------|---------|--------------|
| `README.md` | Project overview | Yes - main entry point |
| `ARCHITECTURE.md` | Architecture details | Yes - on structural changes |
| `PROJECT_STATUS.md` | Implementation status | Yes - on feature completion |
| `REORGANIZATION_REPORT.md` | Historical record | No - historical |
| `CLAUDE.md` | This file | Yes - on workflow changes |

### Key Source Files

#### Go Implementation

| File | Purpose |
|------|---------|
| `internal/domain/interfaces.go` | Core interface definitions |
| `internal/domain/types.go` | Domain types and constants |
| `internal/domain/errors.go` | Error type definitions |
| `internal/application/query/builder.go` | Query builder implementation |
| `internal/infrastructure/adapters/postgres/adapter.go` | PostgreSQL adapter |
| `internal/infrastructure/adapters/mongodb/adapter.go` | MongoDB adapter |
| `pkg/storage/storage.go` | Public API exports |

#### Python Implementation

| File | Purpose |
|------|---------|
| `python/src/storage/interfaces.py` | Async interface definitions |
| `python/src/storage/types.py` | Type definitions |
| `python/src/storage/errors.py` | Error classes |
| `python/src/storage/query/builder.py` | Query builder |
| `python/src/storage/adapters/postgresql/adapter.py` | PostgreSQL adapter |
| `python/src/storage/adapters/mongodb/adapter.py` | MongoDB adapter |

---

## Multi-Language Implementation

### Architecture Parity

Both Go and Python implementations maintain **identical architectural patterns**:

- Same interface definitions
- Same type mappings
- Same error handling structure
- Same query builder API
- Same adapter pattern

### When to Modify Both Implementations

**Always update both** when changing:
- Core interfaces
- Domain types
- Error types
- Query builder API
- Adapter interface

**Language-specific changes**:
- Performance optimizations
- Language-specific idioms
- Testing frameworks
- Build systems

### Cross-Language Consistency Checklist

When adding a feature:
- [ ] Interface defined in both languages
- [ ] Types mapped correctly
- [ ] Errors handled consistently
- [ ] Tests written for both
- [ ] Documentation updated for both
- [ ] Examples provided for both

---

## AI Assistant Guidelines

### When Working on This Repository

1. **ALWAYS read before writing**
   - Read existing implementations before suggesting changes
   - Understand the clean architecture pattern
   - Follow established conventions

2. **Follow the layer architecture**
   - Domain layer: Pure business logic, no external dependencies
   - Application layer: Orchestrate domain objects
   - Infrastructure layer: External dependencies (DB drivers, etc.)
   - Public API: Re-export from domain through `pkg/storage`

3. **Import path corrections**
   - Module: `github.com/AnandSGit/HybridCache.io`
   - Internal imports: Use full module path
   - Public API: Import from `pkg/storage` only
   - Never create import cycles

4. **Testing requirements**
   - Write tests for all new code
   - Maintain 95%+ coverage
   - Include unit and integration tests
   - Test error paths

5. **Code quality**
   - Run `make quality` before committing
   - Fix all linting errors
   - Ensure type safety (Python)
   - No security vulnerabilities

6. **Documentation**
   - Update relevant documentation files
   - Add examples for new features
   - Include inline code comments for complex logic
   - Update this CLAUDE.md if workflows change

### Common Pitfalls to Avoid

❌ **DON'T**:
- Create import cycles
- Put business logic in infrastructure layer
- Skip writing tests
- Modify public API without updating internal implementation
- Commit without running quality checks
- Create backward-incompatible changes without discussion
- Add dependencies without updating go.mod/pyproject.toml

✅ **DO**:
- Follow clean architecture principles
- Write comprehensive tests
- Document non-obvious decisions
- Keep both Go and Python implementations in sync
- Run quality checks before committing
- Ask for clarification when requirements are unclear

### Understanding the Codebase

**To understand a feature**:
1. Start with interfaces in `internal/domain/interfaces.go`
2. Check types in `internal/domain/types.go`
3. Review implementation in `internal/application/` or `internal/infrastructure/`
4. Look at public API in `pkg/storage/`
5. Check examples in `examples/` or `cmd/example/`
6. Read tests for usage patterns

**To find where to make changes**:
- **Adding business logic** → `internal/domain/`
- **Implementing use case** → `internal/application/`
- **Database-specific code** → `internal/infrastructure/adapters/`
- **Public API changes** → `pkg/storage/`
- **Examples** → `examples/` or `cmd/example/`
- **Tests** → `tests/`

### Performance Considerations

**Go Performance Targets**:
- Simple query: < 5ms (p95)
- Complex query: < 50ms (p95)
- Transaction: < 10ms (p95)
- Connection pool acquisition: < 1ms

**Python Performance Targets**:
- Simple query: < 10ms (p95)
- Complex query: < 50ms (p95)
- Transaction: < 15ms (p95)
- Async operations: Full async/await support

### Security Considerations

- **Never log sensitive data** (passwords, API keys)
- **Use parameterized queries** (prevent SQL injection)
- **Validate all input** at domain boundaries
- **Handle errors securely** (don't leak internal details)
- **Run security scans** before committing

### Getting Help

**Documentation Resources**:
- Architecture: `ARCHITECTURE.md`
- Status: `PROJECT_STATUS.md`
- Examples: `examples/` and `cmd/example/`
- Tests: Look at test files for patterns

**When Stuck**:
1. Read the relevant documentation files
2. Check existing implementations
3. Look at test files for usage patterns
4. Review git history for context

---

## Quick Reference

### Essential Commands

```bash
# Go Development
make test              # Run all tests
make lint              # Run linter
make fmt               # Format code
make quality           # All quality checks
make dev-up            # Start databases
make run-example       # Run example

# Python Development
cd python
make test              # Run all tests
make lint              # Run linter
make format            # Format code
make quality           # All quality checks
make dev-up            # Start databases

# Git Operations
git status
git add .
git commit -m "message"
git push -u origin <branch>

# Docker
docker-compose up -d   # Start services
docker-compose down    # Stop services
docker-compose logs -f # View logs
```

### Key Interfaces

```go
// Go
type Storage interface {
    Query(ctx context.Context, query Query) (Result, error)
    Execute(ctx context.Context, command Command) (ExecuteResult, error)
    BeginTx(ctx context.Context, opts *TxOptions) (Transaction, error)
}

type Adapter interface {
    Connect(ctx context.Context, config Config) (Storage, error)
    Name() string
    DatabaseType() DatabaseType
}
```

```python
# Python
class Storage(ABC):
    @abstractmethod
    async def query(self, query: Query) -> Result:
        pass

    @abstractmethod
    async def execute(self, command: Command) -> ExecuteResult:
        pass

    @abstractmethod
    async def begin_tx(self, opts: Optional[TxOptions] = None) -> Transaction:
        pass

class Adapter(ABC):
    @abstractmethod
    async def connect(self, config: Config) -> Storage:
        pass
```

---

## Conclusion

This document provides comprehensive guidance for AI assistants working on the HybridCache.io project. When in doubt:

1. **Read before writing** - Understand existing patterns
2. **Follow architecture** - Respect layer boundaries
3. **Test thoroughly** - Maintain quality standards
4. **Document changes** - Keep docs up to date
5. **Ask questions** - Clarify requirements when unclear

**Remember**: This is a production-ready library. Code quality, testing, and architectural consistency are paramount.

---

**For questions or clarifications**, refer to:
- Architecture documentation: `ARCHITECTURE.md`
- Project status: `PROJECT_STATUS.md`
- Examples: `examples/` and `python/examples/`
- Tests: `tests/` and `python/tests/`
