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

$Dist = Join-Path $Root "dist"
New-Item -ItemType Directory -Force -Path $Dist | Out-Null

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
# Explicit file path avoids nfpm's "packager required for directory target" edge case.
$DebTarget = "/work/dist/skilltree_0.1.0_amd64.deb"

Write-Host "Packaging .deb with nfpm (Docker)..."
$nfpmArgs = @(
    "run", "--rm",
    "-v", "${Mount}:/work",
    "-w", "/work",
    "goreleaser/nfpm",
    "package",
    "--config", "packaging/nfpm.yaml",
    "--packager", "deb",
    "--target", $DebTarget
)
& docker @nfpmArgs
if ($LASTEXITCODE -ne 0) { throw "nfpm package failed (exit $LASTEXITCODE)" }

Write-Host "deb written to dist/"
Get-ChildItem $Dist -Filter "*.deb" | ForEach-Object { Write-Host "  $($_.Name)" }
