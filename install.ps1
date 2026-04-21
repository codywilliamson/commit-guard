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
    [string]$HookMode = "auto",
    [string]$PackageManager = "",
    [string]$PRMode = "smart"
)

$ErrorActionPreference = "Stop"

$GuardRepo = if ($env:COMMIT_GUARD_REPO) { $env:COMMIT_GUARD_REPO } else { "codywilliamson/commit-guard" }
$GuardRef = if ($env:COMMIT_GUARD_REF) { $env:COMMIT_GUARD_REF } else { "v0.2.1" }
$TemplateUrl = if ($env:COMMIT_GUARD_TEMPLATE_URL) { $env:COMMIT_GUARD_TEMPLATE_URL } else { "https://raw.githubusercontent.com/$GuardRepo/$GuardRef/caller-template.yml" }
$ValidatorUrl = if ($env:COMMIT_GUARD_VALIDATOR_URL) { $env:COMMIT_GUARD_VALIDATOR_URL } else { "https://raw.githubusercontent.com/$GuardRepo/$GuardRef/scripts/validate-commit-message.sh" }
$WorkflowDir = ".github/workflows"
$WorkflowFile = "$WorkflowDir/commitlint.yml"

function Ensure-ValidPRMode {
    if ($PRMode -notin @("smart", "commits", "title")) {
        throw "Invalid PR mode '$PRMode'. Expected smart, commits, or title."
    }
}

function Resolve-HookMode {
    switch ($HookMode) {
        "auto" {
            if ($PackageManager) { return "husky" }
            return "native"
        }
        "husky" { return "husky" }
        "native" { return "native" }
        "none" { return "none" }
        default { throw "Invalid hook mode '$HookMode'. Expected auto, husky, native, or none." }
    }
}

function Install-NativeHook {
    $existingHooksPath = (git config --get core.hooksPath 2>$null)
    $hooksDir = if ($existingHooksPath) { $existingHooksPath } else { ".githooks" }

    New-Item -ItemType Directory -Path $hooksDir -Force | Out-Null
    Invoke-WebRequest -Uri $ValidatorUrl -OutFile "$hooksDir/commit-msg" -UseBasicParsing

    if (-not $existingHooksPath) {
        git config core.hooksPath $hooksDir | Out-Null
        Write-Host "  Configured core.hooksPath: $hooksDir" -ForegroundColor Green
    }

    Write-Host "  Created: $hooksDir/commit-msg" -ForegroundColor Green
}

function Install-HuskyHook {
    if (-not $PackageManager) {
        throw "Husky mode requires a Node.js repo with a detected package manager."
    }

    Write-Host "`nInstalling local hooks ($PackageManager, husky)..."

    $configPkg = "@commitlint/config-conventional"
    if ($Config -eq "angular") { $configPkg = "@commitlint/config-angular" }

    switch ($PackageManager) {
        "pnpm" { & pnpm add -Dw @commitlint/cli $configPkg husky }
        "yarn" { & yarn add -D @commitlint/cli $configPkg husky }
        "npm"  { & npm install -D @commitlint/cli $configPkg husky }
    }

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

    npx husky init 2>$null | Out-Null
    New-Item -ItemType Directory -Path ".husky" -Force | Out-Null

    $hookRunner = switch ($PackageManager) {
        "pnpm" { "pnpm exec commitlint" }
        "yarn" { "yarn commitlint" }
        "npm"  { "npx --no -- commitlint" }
    }

    @"
#!/usr/bin/env sh
$hookRunner --edit "`$1"
"@ | Set-Content -Path ".husky/commit-msg" -NoNewline
    Write-Host "  Created: .husky/commit-msg" -ForegroundColor Green

    if ((Test-Path "package.json") -and -not ((Get-Content "package.json" -Raw) -match '"prepare"')) {
        $pkg = Get-Content "package.json" -Raw | ConvertFrom-Json
        if (-not $pkg.PSObject.Properties["scripts"]) {
            $pkg | Add-Member -MemberType NoteProperty -Name scripts -Value ([pscustomobject]@{})
        }
        $pkg.scripts | Add-Member -MemberType NoteProperty -Name prepare -Value "husky" -Force
        $pkg | ConvertTo-Json -Depth 20 | Set-Content "package.json"
        Add-Content "package.json" ""
        Write-Host "  Added prepare script to package.json" -ForegroundColor Green
    }
}

try {
    git rev-parse --is-inside-work-tree 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw }
} catch {
    Write-Error "Not a git repository. Run this from your project root."
    exit 1
}

if (-not $PackageManager) {
    if (Test-Path "pnpm-lock.yaml") { $PackageManager = "pnpm" }
    elseif (Test-Path "yarn.lock") { $PackageManager = "yarn" }
    elseif ((Test-Path "package-lock.json") -or (Test-Path "package.json")) { $PackageManager = "npm" }
}

Ensure-ValidPRMode
$ResolvedHookMode = Resolve-HookMode

Write-Host "Installing CI workflow..."
New-Item -ItemType Directory -Path $WorkflowDir -Force | Out-Null
Invoke-WebRequest -Uri $TemplateUrl -OutFile $WorkflowFile -UseBasicParsing

$content = Get-Content $WorkflowFile -Raw
if ($Config -ne "conventional") {
    $content = $content -replace 'config: "conventional"', "config: `"$Config`""
}
if ($PRMode -ne "smart") {
    $content = $content -replace 'pr-mode: "smart"', "pr-mode: `"$PRMode`""
}
Set-Content -Path $WorkflowFile -Value $content -NoNewline

Write-Host "  Installed: $WorkflowFile" -ForegroundColor Green

if ($CIOnly -or $ResolvedHookMode -eq "none") {
    Write-Host "`nDone. Commit and push to activate."
    exit 0
}

switch ($ResolvedHookMode) {
    "native" {
        Write-Host "`nInstalling local hooks (native git)..."
        Install-NativeHook
    }
    "husky" {
        Install-HuskyHook
    }
}

Write-Host ""
Write-Host "Done! Installed:" -ForegroundColor Green
Write-Host "  - CI workflow: $WorkflowFile"
if ($ResolvedHookMode -eq "native") {
    $hookPath = git config --get core.hooksPath
    Write-Host "  - Local hook: $hookPath/commit-msg"
} elseif ($ResolvedHookMode -eq "husky") {
    Write-Host "  - Local hook: .husky/commit-msg"
    Write-Host "  - Config: commitlint.config.js"
}
Write-Host ""
Write-Host "Commit and push to activate CI checks."
