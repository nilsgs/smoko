#Requires -Version 5.1
$ErrorActionPreference = 'Stop'

$RepoDir = Split-Path -Parent $PSScriptRoot
$InstallDir = Join-Path $env:USERPROFILE '.smoko\bin'
$Artifact = Join-Path $RepoDir 'dist\smoko.exe'

if (-not (Test-Path $Artifact)) {
    throw "Build artifact not found: $Artifact. Run scripts/build.ps1 first."
}

Write-Host "Installing to $InstallDir..."
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item -Force $Artifact (Join-Path $InstallDir 'smoko.exe')
