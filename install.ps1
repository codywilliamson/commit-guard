#Requires -Version 5.1
[CmdletBinding()]
param(
    [switch]$CIOnly,
    [switch]$NoUpdates,
    [string]$Ref = '',
    [ValidateSet('commits', 'title', 'smart')][string]$PRMode = 'commits',
    [ValidateSet('auto', 'native', 'husky', 'none')][string]$HookMode = 'auto'
)

$ErrorActionPreference = 'Stop'
$releaseRef = if ($Ref) { $Ref } elseif ($env:COMMIT_GUARD_REF) { $env:COMMIT_GUARD_REF } else { 'v0.3.0' }
if ($releaseRef -notmatch '^v\d+\.\d+\.\d+$') {
    throw 'Ref must name an exact release, such as v0.3.0.'
}
$releaseVersion = $releaseRef.Substring(1)
$architecture = if ($env:PROCESSOR_ARCHITECTURE -match 'ARM' -or $env:PROCESSOR_ARCHITEW6432 -match 'ARM') { 'arm64' } else { 'amd64' }
$asset = "commit-guard_${releaseVersion}_windows_${architecture}.exe"
$baseUrl = "https://github.com/codywilliamson/commit-guard/releases/download/$releaseRef"
$tempBase = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$tempDirectory = Join-Path $tempBase ('commit-guard-install-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDirectory | Out-Null
try {
    $binary = Join-Path $tempDirectory $asset
    $checksums = Join-Path $tempDirectory 'SHA256SUMS'
    Invoke-WebRequest -Uri "$baseUrl/$asset" -OutFile $binary -UseBasicParsing -TimeoutSec 60
    Invoke-WebRequest -Uri "$baseUrl/SHA256SUMS" -OutFile $checksums -UseBasicParsing -TimeoutSec 30
    $entries = @(Get-Content -LiteralPath $checksums | Where-Object { $_ -match "^[a-fA-F0-9]{64}\s+$([regex]::Escape($asset))$" })
    if ($entries.Count -ne 1) { throw "Missing or ambiguous checksum for $asset" }
    $expected = ($entries[0] -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw 'Release checksum verification failed.' }
    $installArgs = @('install', '--ref', $releaseRef, '--mode', $PRMode)
    if ($CIOnly -or $HookMode -eq 'none') { $installArgs += '--ci-only' }
    if ($NoUpdates) { $installArgs += '--no-updates' }
    & $binary @installArgs
    if ($LASTEXITCODE -ne 0) { throw "commit-guard installation failed (exit $LASTEXITCODE)." }
} finally {
    $resolvedTemp = [IO.Path]::GetFullPath($tempDirectory)
    if ($resolvedTemp.StartsWith($tempBase, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path $resolvedTemp -Leaf) -like 'commit-guard-install-*') {
        Remove-Item -LiteralPath $resolvedTemp -Recurse -Force -ErrorAction SilentlyContinue
    }
}
