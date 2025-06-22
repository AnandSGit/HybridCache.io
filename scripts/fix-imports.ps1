# PowerShell script to fix import paths and references
# This script replaces all storage. references with domain. references in internal packages

$files = @(
    "internal\application\query\builder.go",
    "internal\infrastructure\adapters\postgres\adapter.go",
    "tests\unit\domain\types_test.go",
    "tests\unit\domain\errors_test.go",
    "tests\integration\postgres_test.go",
    "tests\unit\postgres\adapter_test.go"
)

foreach ($file in $files) {
    if (Test-Path $file) {
        Write-Host "Processing $file..."

        # Read the file content
        $content = Get-Content $file -Raw

        # For test files, add domain prefix to types and replace query. with storage.
        if ($file -like "*test.go") {
            # Replace query. with storage. for integration tests
            if ($file -like "*integration*") {
                $content = $content -replace 'query\.', 'storage.'
            }
            # Replace NewAdapter() with storage.NewPostgreSQLAdapter() for postgres tests
            if ($file -like "*postgres*") {
                $content = $content -replace '\bNewAdapter\(\)', 'storage.NewPostgreSQLAdapter()'
            }
            # Replace type references with domain prefix (but not already prefixed ones)
            $content = $content -replace '\b(?<!domain\.)([A-Z][a-zA-Z]*Type[A-Z][a-zA-Z]*)\b', 'domain.$1'
            $content = $content -replace '\b(?<!domain\.)([A-Z][a-zA-Z]*Level[A-Z][a-zA-Z]*)\b', 'domain.$1'
            $content = $content -replace '\b(?<!domain\.)([A-Z][a-zA-Z]*Resolution[A-Z][a-zA-Z]*)\b', 'domain.$1'
            $content = $content -replace '\b(?<!domain\.)(Query|Command|Condition|Config|StorageInfo|HealthStatus|TableSchema|ColumnDefinition|IndexDefinition|TxOptions|Operation|OperationResult|ExecuteResult|ColumnType|StorageLimits)\b', 'domain.$1'
        }

        # Replace storage. with domain.
        $content = $content -replace 'storage\.', 'domain.'

        # Replace import paths
        $content = $content -replace 'github\.com/HybridCache\.io/storage/pkg/storage', 'github.com/AnandSGit/HybridCache.io/internal/domain'

        # Write back to file
        Set-Content $file -Value $content -NoNewline

        Write-Host "Fixed $file"
    }
    else {
        Write-Host "File not found: $file"
    }
}

Write-Host "Import fixing complete!"
