# Windows service helpers (run elevated PowerShell from repo after building skilltree.exe)
#
# Build:
#   go build -o dist/skilltree.exe ./cmd/skilltree
#
# Install / start / stop / uninstall:
#   .\dist\skilltree.exe -service install
#   .\dist\skilltree.exe -service start
#   .\dist\skilltree.exe -service stop
#   .\dist\skilltree.exe -service uninstall
#
# Set environment for the service (system-wide or service-specific):
#   [System.Environment]::SetEnvironmentVariable("SKILLTREE_DATABASE_URL", "postgres://...", "Machine")
#   [System.Environment]::SetEnvironmentVariable("SKILLTREE_HTTP_ADDR", ":8088", "Machine")
#   [System.Environment]::SetEnvironmentVariable("SKILLTREE_SMTP_HOST", "smtp.example.com", "Machine")
#   [System.Environment]::SetEnvironmentVariable("SKILLTREE_SMTP_FROM", "noreply@example.com", "Machine")
#
# Or use services.msc / sc.exe to configure. Default DB URL expects local Postgres.
# Interactive runs also load a local .env next to the working directory.

$ErrorActionPreference = "Stop"
$Exe = Join-Path $PSScriptRoot "..\..\dist\skilltree.exe"
if (-not (Test-Path $Exe)) {
  Write-Error "Missing $Exe – build first: go build -o dist/skilltree.exe ./cmd/skilltree"
}

& $Exe -service install
& $Exe -service start
Write-Host "Skilltree Windows service installed and started."
