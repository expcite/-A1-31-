$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
go version
if ($LASTEXITCODE -ne 0) { throw "Go is missing" }
go test ./...
if ($LASTEXITCODE -ne 0) { throw "tests failed" }
go vet ./...
if ($LASTEXITCODE -ne 0) { throw "vet failed" }
go build ./cmd/...
if ($LASTEXITCODE -ne 0) { throw "build failed" }
go run ./cmd/lab -devices 20 -messages 3
if ($LASTEXITCODE -ne 0) { throw "loopback lab failed" }
Write-Host "Starter checks passed. Secure handshake and competition benchmarks remain unimplemented."
