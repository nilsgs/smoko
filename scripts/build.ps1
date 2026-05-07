#Requires -Version 5.1
$ErrorActionPreference = 'Stop'

$RepoDir = Split-Path -Parent $PSScriptRoot
$SrcDir = Join-Path $RepoDir 'src'
$DistDir = Join-Path $RepoDir 'dist'
$Version = (Get-Content (Join-Path $RepoDir 'VERSION') -Raw).Trim()
$Commit = try { (git -C $RepoDir rev-parse --short HEAD 2>$null) } catch { 'unknown' }
if (-not $Commit) { $Commit = 'unknown' }
$Ldflags = "-s -w -X main.version=$Version -X main.commit=$Commit"

Write-Host "Building smoko v${Version}+${Commit}..."
New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

Push-Location $SrcDir
try {
    & go build -ldflags $Ldflags -o (Join-Path $DistDir 'smoko.exe') ./cmd/smoko
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}
