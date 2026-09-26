# Cross-compile Linux binary and package .deb via Docker (nfpm).
# Prerequisites: Go, Docker Desktop
# Usage:  .\packaging\build-deb.ps1

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root

function Require-Command([string]$Name) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "'$Name' not found in PATH."
    }
}

Require-Command go
Require-Command docker

New-Item -ItemType Directory -Force -Path (Join-Path $Root "dist") | Out-Null

Write-Host "Building Linux amd64 binary..."
$env:GOOS = "linux"
$env:GOARCH = "amd64"
try {
    & go build -o skilltree ./cmd/skilltree
    if ($LASTEXITCODE -ne 0) { throw "go build failed (exit $LASTEXITCODE)" }
}
finally {
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
}

# Docker Desktop accepts Windows paths; normalize to forward slashes.
$Mount = ($Root.Path -replace "\\", "/")

Write-Host "Packaging .deb with nfpm (Docker)..."
& docker run --rm `
    -v "${Mount}:/work" `
    -w /work `
    goreleaser/nfpm package `
    --config packaging/nfpm.yaml `
    --packager deb `
    --target dist/
if ($LASTEXITCODE -ne 0) { throw "nfpm package failed (exit $LASTEXITCODE)" }

Write-Host "deb written to dist/"
Get-ChildItem (Join-Path $Root "dist") -Filter "*.deb" | ForEach-Object { Write-Host "  $($_.Name)" }
