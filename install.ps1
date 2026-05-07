#Requires -Version 5.1
$ErrorActionPreference = 'Stop'

$InstallDir = Join-Path $env:USERPROFILE '.smoko\bin'
$RepoDir = Split-Path -Parent $MyInvocation.MyCommand.Definition

& (Join-Path $RepoDir 'scripts\build.ps1')
& (Join-Path $RepoDir 'scripts\install.ps1')

# Add to user-level PATH if not already present
$UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($UserPath -split ';' | Where-Object { $_ -eq $InstallDir }) {
    Write-Host "PATH already contains $InstallDir"
} else {
    $NewPath = $InstallDir + ';' + $UserPath
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
    $env:PATH = $InstallDir + ';' + $env:PATH
    Write-Host "Added $InstallDir to user PATH"
}

Write-Host 'Done. Restart your terminal, then run: smoko --help'
