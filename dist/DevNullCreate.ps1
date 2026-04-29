# DevNull create-game: dev-stack setup
# Run by the "DevNull Create Games" desktop shortcut.
#
# First run: installs gh CLI (via winget) + Copilot CLI extension,
# runs gh auth login, forks the starter template to your account,
# clones it to %USERPROFILE%\DevNull\Create\, and opens Copilot CLI
# in that folder.
#
# Subsequent runs: skip the install/auth/clone and just open Copilot CLI.

param(
    [string]$CreateDir   = (Join-Path $env:USERPROFILE "DevNull\Create"),
    [string]$Template    = "simonthoresen/DevNullCoreCreateTemplate",
    [string]$RepoName    = "DevNullCreate"
)

$ErrorActionPreference = "Stop"

function Write-Step {
    param([string]$Message)
    Write-Host ("  " + $Message + " ...") -NoNewline
}
function Write-Ok    { param([string]$Status = "OK")   ; Write-Host (" " + $Status) -ForegroundColor Green }
function Write-Skip  { param([string]$Status = "SKIP") ; Write-Host (" " + $Status) -ForegroundColor DarkGray }
function Write-Fail  { param([string]$Status)          ; Write-Host (" " + $Status) -ForegroundColor Red }
function Refresh-Path {
    $machine = [System.Environment]::GetEnvironmentVariable("Path", "Machine")
    $user    = [System.Environment]::GetEnvironmentVariable("Path", "User")
    $env:Path = ($machine, $user -join ";").Trim(";")
}

Write-Host ""
Write-Host "  DevNull Create Games" -ForegroundColor Cyan
Write-Host ""

# ── 1. gh CLI ──────────────────────────────────────────────────────────────────
Write-Step "GitHub CLI"
if (Get-Command gh -ErrorAction SilentlyContinue) {
    Write-Skip "already installed"
} else {
    if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
        Write-Fail "winget unavailable"
        Write-Host ""
        Write-Host "  Install gh manually from https://cli.github.com and re-run this." -ForegroundColor Yellow
        exit 1
    }
    winget install --id GitHub.cli --silent --accept-package-agreements --accept-source-agreements | Out-Null
    Refresh-Path
    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
        Write-Fail "install failed"
        Write-Host ""
        Write-Host "  Open a fresh terminal and re-run this script (PATH may need a refresh)." -ForegroundColor Yellow
        exit 1
    }
    Write-Ok "installed"
}

# ── 2. Copilot CLI extension ───────────────────────────────────────────────────
# Newer `gh` versions ship `gh copilot` as a built-in command (not an extension).
# In that case `gh extension install github/gh-copilot` fails with
# "matches the name of a built-in command or alias", so probe for the command
# directly before falling back to the extension install.
Write-Step "Copilot CLI"
& gh copilot --help 2>&1 | Out-Null
if ($LASTEXITCODE -eq 0) {
    Write-Skip "already available"
} else {
    $extensions = & gh extension list 2>$null
    if ($extensions -match "gh-copilot") {
        Write-Skip "already installed"
    } else {
        & gh extension install github/gh-copilot 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Fail "install failed"
            Write-Host ""
            Write-Host "  Run 'gh extension install github/gh-copilot' manually." -ForegroundColor Yellow
            exit 1
        }
        Write-Ok "installed"
    }
}

# ── 3. gh auth ────────────────────────────────────────────────────────────────
Write-Step "GitHub authentication"
& gh auth status -h github.com 2>&1 | Out-Null
if ($LASTEXITCODE -eq 0) {
    Write-Skip "already signed in"
} else {
    Write-Host ""
    Write-Host "  A browser window will open for GitHub sign-in." -ForegroundColor Yellow
    & gh auth login -h github.com -w
    if ($LASTEXITCODE -ne 0) {
        Write-Host "  Auth failed; re-run when ready." -ForegroundColor Red
        exit 1
    }
}

# ── 4. Starter clone ──────────────────────────────────────────────────────────
Write-Step "Starter repo at $CreateDir"
if (Test-Path $CreateDir) {
    # Validate that the existing folder is the starter clone (or at least a git
    # repo we shouldn't clobber). If origin matches the user's <user>/DevNullCreate
    # forked from the template we treat it as already set up. Otherwise refuse.
    $origin = ""
    try {
        Push-Location $CreateDir
        $origin = (& git remote get-url origin 2>$null)
    } catch {} finally { Pop-Location }
    if ($origin -match "/$RepoName(\.git)?$") {
        Write-Skip "already cloned"
    } else {
        Write-Fail "occupied"
        Write-Host ""
        Write-Host "  $CreateDir already exists and isn't a starter clone." -ForegroundColor Yellow
        Write-Host "  Rename or remove it, or pass -CreateDir <other-path> to this script."
        exit 1
    }
} else {
    $parent = Split-Path $CreateDir -Parent
    New-Item -ItemType Directory -Path $parent -Force | Out-Null
    Push-Location $parent
    try {
        # Create a public fork of the template under <user>/<RepoName> and
        # clone into a sibling folder named <RepoName>; rename to "Create".
        & gh repo create $RepoName --template $Template --clone --public --description "My DevNull games" 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Fail "gh repo create failed"
            Write-Host ""
            Write-Host "  You may already have a <user>/$RepoName repo on GitHub." -ForegroundColor Yellow
            Write-Host "  Re-run with -RepoName <other-name> or delete the existing repo."
            exit 1
        }
        if (Test-Path (Join-Path $parent $RepoName)) {
            Rename-Item -Path (Join-Path $parent $RepoName) -NewName (Split-Path $CreateDir -Leaf)
        }
    } finally { Pop-Location }
    Write-Ok "cloned"
}

# Open Copilot CLI in the create folder.
# Run inline in the current console -- works for both launch paths:
#   - From a desktop shortcut, this is the powershell.exe window the shortcut
#     spawned, which stays open until gh copilot exits (no extra window pops).
#   - From an existing terminal, gh copilot just takes over that shell.
Write-Host ""
Write-Host "  Opening Copilot CLI in $CreateDir ..." -ForegroundColor Green
Write-Host ""
Push-Location $CreateDir
try {
    & gh copilot
} finally {
    Pop-Location
}

Write-Host ""
Write-Host "  Done. Edit Games/, Plugins/, or Shaders/ in $CreateDir." -ForegroundColor Cyan
Write-Host "  Run .\DevNullTest.ps1 in that folder to test locally."
Write-Host ""
