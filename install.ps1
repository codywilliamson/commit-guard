#Requires -Version 5.1
<#
.SYNOPSIS
    Install commit-guard conventional commit linting into the current repository.
.PARAMETER Config
    Commitlint config preset: conventional (default), angular
.PARAMETER CIOnly
    Only install CI workflow, skip local hooks
.PARAMETER PackageManager
    Package manager: pnpm, npm, yarn (auto-detected if omitted)
.EXAMPLE
    irm https://raw.githubusercontent.com/codywilliamson/commit-guard/main/install.ps1 | iex
.EXAMPLE
    ./install.ps1 -Config "conventional" -PackageManager "pnpm"
#>
param(
    [string]$Config = "conventional",
    [switch]$CIOnly,
    [string]$PackageManager = ""
)

$ErrorActionPreference = "Stop"

$GuardRepo = "codywilliamson/commit-guard"
$Branch = "main"
$TemplateUrl = "https://raw.githubusercontent.com/$GuardRepo/$Branch/caller-template.yml"
$WorkflowDir = ".github/workflows"
$WorkflowFile = "$WorkflowDir/commitlint.yml"

# check we're in a git repo
try {
    git rev-parse --is-inside-work-tree 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw }
} catch {
    Write-Error "Not a git repository. Run this from your project root."
    exit 1
}

# detect package manager
if (-not $PackageManager) {
    if (Test-Path "pnpm-lock.yaml") { $PackageManager = "pnpm" }
    elseif (Test-Path "yarn.lock") { $PackageManager = "yarn" }
    elseif (Test-Path "package-lock.json" -or (Test-Path "package.json")) { $PackageManager = "npm" }
}

# install CI workflow
Write-Host "Installing CI workflow..."
New-Item -ItemType Directory -Path $WorkflowDir -Force | Out-Null
Invoke-WebRequest -Uri $TemplateUrl -OutFile $WorkflowFile -UseBasicParsing

if ($Config -ne "conventional") {
    $content = Get-Content $WorkflowFile -Raw
    $content = $content -replace 'config: "conventional"', "config: `"$Config`""
    Set-Content -Path $WorkflowFile -Value $content -NoNewline
}

Write-Host "  Installed: $WorkflowFile" -ForegroundColor Green

if ($CIOnly) {
    Write-Host "`nDone (CI-only mode). Commit and push to activate."
    exit 0
}

if (-not $PackageManager) {
    Write-Host "`nNo package.json found, skipping local hooks (CI-only)."
    Write-Host "Done. Commit and push to activate."
    exit 0
}

Write-Host "`nInstalling local hooks ($PackageManager)..."

$ConfigPkg = "@commitlint/config-conventional"
if ($Config -eq "angular") { $ConfigPkg = "@commitlint/config-angular" }

switch ($PackageManager) {
    "pnpm" { & pnpm add -Dw @commitlint/cli $ConfigPkg husky }
    "yarn" { & yarn add -D @commitlint/cli $ConfigPkg husky }
    "npm"  { & npm install -D @commitlint/cli $ConfigPkg husky }
}

# commitlint config
$configFiles = @("commitlint.config.js", "commitlint.config.mjs", "commitlint.config.cjs", ".commitlintrc.yml", ".commitlintrc.json")
$hasConfig = $configFiles | Where-Object { Test-Path $_ }

if (-not $hasConfig) {
    $extends = "@commitlint/config-conventional"
    if ($Config -eq "angular") { $extends = "@commitlint/config-angular" }

    @"
export default {
  extends: ["$extends"],
};
"@ | Set-Content -Path "commitlint.config.js"
    Write-Host "  Created: commitlint.config.js" -ForegroundColor Green
} else {
    Write-Host "  Commitlint config already exists, skipping"
}

# husky setup
npx husky init 2>$null

New-Item -ItemType Directory -Path ".husky" -Force | Out-Null

$hookCmd = switch ($PackageManager) {
    "pnpm" { 'pnpm exec commitlint --edit "$1"' }
    "npm"  { 'npx commitlint --edit "$1"' }
    "yarn" { 'yarn commitlint --edit "$1"' }
}

Set-Content -Path ".husky/commit-msg" -Value $hookCmd
Write-Host "  Created: .husky/commit-msg" -ForegroundColor Green

Write-Host ""
Write-Host "Done! Installed:" -ForegroundColor Green
Write-Host "  - CI workflow: $WorkflowFile"
Write-Host "  - Local hook: .husky/commit-msg"
Write-Host "  - Config: commitlint.config.js"
Write-Host ""
Write-Host "Commit and push to activate CI checks."
