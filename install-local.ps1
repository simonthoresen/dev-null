# install-local.ps1 -- mirror dist\ into %USERPROFILE%\DevNull\ for dev parity with a real install.
#
# Used by `make install`. Builds nothing on its own -- assumes `make build`
# (and ideally `make generate-manifest`) have already populated dist\.
#
# Behavior:
#   - Copies *.ps1 launchers from dist\ root to <InstallDir>\.
#   - Strict-mirrors dist\Core\ → <InstallDir>\Core\: copies new/changed
#     files and deletes files in <InstallDir>\Core\ that no longer exist
#     in dist\Core\. Empty directories are pruned.
#   - Excludes the same artifacts release.yml excludes from DevNull.zip:
#     *_ed25519, *_ed25519.pub, .bundle-version, Core\state\.
#   - Never touches <InstallDir>\Create\, Shared\, Config\, Logs\.
#     Creates them if missing so a fresh install has the right shape.

param(
    [string]$InstallDir = (Join-Path $env:USERPROFILE "DevNull")
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$src  = Join-Path $root "dist"
if (-not (Test-Path $src)) { throw "dist\ not found at $src -- run 'make build' first." }
$coreSrc = Join-Path $src "Core"
if (-not (Test-Path $coreSrc)) { throw "dist\Core\ not found -- run 'make build' first." }

function Test-Excluded {
    param([string]$RelPath)
    if ($RelPath.EndsWith("_ed25519"))     { return $true }
    if ($RelPath.EndsWith("_ed25519.pub")) { return $true }
    if ($RelPath -eq ".bundle-version")    { return $true }
    if ($RelPath -eq "state" -or $RelPath.StartsWith("state\")) { return $true }
    return $false
}

New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
$coreDst = Join-Path $InstallDir "Core"
New-Item -ItemType Directory -Path $coreDst -Force | Out-Null

Write-Host "Installing to $InstallDir" -ForegroundColor Cyan

# ── 1. Root-level launcher .ps1 files ────────────────────────────────────────
Get-ChildItem -Path $src -File -Filter "*.ps1" | ForEach-Object {
    $dest = Join-Path $InstallDir $_.Name
    Copy-Item -Path $_.FullName -Destination $dest -Force
}

# ── 2. Strict mirror of Core ─────────────────────────────────────────────────
$srcFiles = @{}
Get-ChildItem -Path $coreSrc -Recurse -File | ForEach-Object {
    $rel = $_.FullName.Substring($coreSrc.Length + 1)
    if (Test-Excluded $rel) { return }
    $srcFiles[$rel] = $true
    $dest = Join-Path $coreDst $rel
    $destDir = Split-Path $dest -Parent
    if (-not (Test-Path $destDir)) { New-Item -ItemType Directory -Path $destDir -Force | Out-Null }
    Copy-Item -Path $_.FullName -Destination $dest -Force
}

# Delete files in <coreDst> that no longer exist in <coreSrc>, except excluded
# paths (host keys, runtime state) which are user-owned even though they live
# under Core\.
Get-ChildItem -Path $coreDst -Recurse -File | ForEach-Object {
    $rel = $_.FullName.Substring($coreDst.Length + 1)
    if (Test-Excluded $rel) { return }
    if (-not $srcFiles.ContainsKey($rel)) {
        Remove-Item -Path $_.FullName -Force
    }
}

# Prune empty directories under Core\ (deepest-first).
Get-ChildItem -Path $coreDst -Recurse -Directory |
    Sort-Object -Property FullName -Descending |
    ForEach-Object {
        if (-not (Get-ChildItem -Path $_.FullName -Force)) {
            Remove-Item -Path $_.FullName -Force
        }
    }

# ── 3. Ensure user-owned dirs exist (never deleted, never overwritten) ───────
foreach ($d in @('Logs', 'Config', 'Shared')) {
    New-Item -ItemType Directory -Path (Join-Path $InstallDir $d) -Force | Out-Null
}

Write-Host "Mirror complete." -ForegroundColor Green
