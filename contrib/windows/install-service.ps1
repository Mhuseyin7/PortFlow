# PortFlow — install as Windows Service via NSSM.
# Run this script from an elevated PowerShell.
#
# Prereqs:
#   choco install nssm        (or) scoop install nssm
#   The portflow.exe binary built or downloaded to a stable path.

param(
    [string]$BinaryPath = "C:\Program Files\PortFlow\portflow.exe",
    [string]$ServiceName = "PortFlow"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $BinaryPath)) {
    Write-Error "Binary not found at $BinaryPath. Pass -BinaryPath <path>."
}

if (-not (Get-Command nssm -ErrorAction SilentlyContinue)) {
    Write-Error "nssm not found on PATH. Install it first: choco install nssm"
}

Write-Host "Installing $ServiceName service..."
nssm install $ServiceName $BinaryPath daemon start `
    --http 127.0.0.1:80 --https 127.0.0.1:443 --api 127.0.0.1:9280

nssm set $ServiceName AppStdout "$env:ProgramData\PortFlow\logs\portflow.out.log"
nssm set $ServiceName AppStderr "$env:ProgramData\PortFlow\logs\portflow.err.log"
nssm set $ServiceName Start SERVICE_AUTO_START
nssm set $ServiceName Description "PortFlow local development HTTPS daemon"

New-Item -ItemType Directory -Force -Path "$env:ProgramData\PortFlow\logs" | Out-Null

Write-Host "Starting $ServiceName..."
nssm start $ServiceName

Write-Host "Done. Check status with:  Get-Service $ServiceName"
Write-Host "Uninstall with:          nssm remove $ServiceName confirm"
