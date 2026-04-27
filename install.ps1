# dev-null installer
# Usage: irm https://github.com/simonthoresen/dev-null/raw/main/install.ps1 | iex
#    or: save this file and run it directly

param(
    [string]$InstallDir = (Join-Path $env:USERPROFILE "dev-null\play")
)

$repo = "simonthoresen/dev-null"
$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "  dev-null installer" -ForegroundColor Cyan
Write-Host ""

# Find the latest release
Write-Host "  Fetching latest release..." -NoNewline
$headers = @{ Accept = "application/vnd.github+json" }
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/tags/latest" -Headers $headers -TimeoutSec 15
$zipAsset = $release.assets | Where-Object { $_.name -eq "dev-null.zip" } | Select-Object -First 1
if (-not $zipAsset) { throw "No dev-null.zip found in latest release." }
Write-Host " OK" -ForegroundColor Green

# Download the zip
$tempZip = Join-Path ([System.IO.Path]::GetTempPath()) "dev-null-install.zip"
Write-Host "  Downloading dev-null.zip..." -NoNewline
Invoke-WebRequest -Uri $zipAsset.browser_download_url -OutFile $tempZip -TimeoutSec 120
Write-Host " OK" -ForegroundColor Green

# Extract to temp folder, then merge into install dir (preserves user's custom files)
Write-Host "  Installing to $InstallDir..." -NoNewline
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "dev-null-install"
if (Test-Path $tempDir) { Remove-Item $tempDir -Recurse -Force }
Expand-Archive -Path $tempZip -DestinationPath $tempDir -Force
Get-ChildItem -Path $tempDir -Recurse -File | ForEach-Object {
    $rel  = $_.FullName.Substring($tempDir.Length + 1)
    $dest = Join-Path $InstallDir $rel
    $dir  = Split-Path $dest -Parent
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
    Copy-Item -Path $_.FullName -Destination $dest -Force
}
Remove-Item $tempZip -Force
Remove-Item $tempDir -Recurse -Force
New-Item -ItemType Directory -Path (Join-Path $InstallDir "logs") -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $InstallDir "state") -Force | Out-Null
Write-Host " OK" -ForegroundColor Green

# Write version stamp
$version = ""
if ($release.body -match 'at ([0-9a-f]{40})') { $version = $Matches[1] }
if ($version) { Set-Content -Path (Join-Path $InstallDir ".version") -Value $version -NoNewline }

# Create desktop shortcuts
$desktop  = [Environment]::GetFolderPath("Desktop")
$startServerPs1 = Join-Path $InstallDir "start-server.ps1"
$startClientPs1 = Join-Path $InstallDir "start-client.ps1"
$createGamePs1  = Join-Path $InstallDir "create-game.ps1"
$shell    = New-Object -ComObject WScript.Shell

$public = $shell.CreateShortcut((Join-Path $desktop "dev-null Server (public).lnk"))
$public.TargetPath       = "powershell.exe"
$public.Arguments        = "-ExecutionPolicy Bypass -File `"$startServerPs1`""
$public.WorkingDirectory = $InstallDir
$public.Description      = "Start the dev-null server (online multiplayer)"
$public.IconLocation     = (Join-Path $InstallDir "dev-null-server.exe") + ",0"
$public.Save()

$private = $shell.CreateShortcut((Join-Path $desktop "dev-null Server (LAN).lnk"))
$private.TargetPath       = "powershell.exe"
$private.Arguments        = "-ExecutionPolicy Bypass -File `"$startServerPs1`" --lan"
$private.WorkingDirectory = $InstallDir
$private.Description      = "Start the dev-null server (LAN only, no Pinggy)"
$private.IconLocation     = (Join-Path $InstallDir "dev-null-server.exe") + ",0"
$private.Save()

$client = $shell.CreateShortcut((Join-Path $desktop "dev-null Client.lnk"))
$client.TargetPath       = "powershell.exe"
$client.Arguments        = "-ExecutionPolicy Bypass -File `"$startClientPs1`""
$client.WorkingDirectory = $InstallDir
$client.Description      = "Start the dev-null graphical client"
$client.IconLocation     = (Join-Path $InstallDir "dev-null-client.exe") + ",0"
$client.Save()

$solo = $shell.CreateShortcut((Join-Path $desktop "dev-null Solo Play.lnk"))
$solo.TargetPath       = "powershell.exe"
$solo.Arguments        = "-ExecutionPolicy Bypass -File `"$startClientPs1`" --local"
$solo.WorkingDirectory = $InstallDir
$solo.Description      = "Start a local server and connect the graphical client to it"
$solo.IconLocation     = (Join-Path $InstallDir "dev-null-client.exe") + ",0"
$solo.Save()

$create = $shell.CreateShortcut((Join-Path $desktop "dev-null Create Games.lnk"))
$create.TargetPath       = "powershell.exe"
$create.Arguments        = "-ExecutionPolicy Bypass -File `"$createGamePs1`""
$create.WorkingDirectory = $InstallDir
$create.Description      = "Set up authoring: installs gh + Copilot CLI on first run, forks the starter template, opens Copilot CLI"
$create.IconLocation     = (Join-Path $InstallDir "dev-null-server.exe") + ",0"
$create.Save()

# Print a one-line note if the legacy install dir still exists, so the user can clean it up.
$legacyInstall = Join-Path $env:LOCALAPPDATA "DevNull"
if (Test-Path $legacyInstall) {
    Write-Host ""
    Write-Host "  Legacy install found at $legacyInstall." -ForegroundColor Yellow
    Write-Host "  Safe to delete; everything moved to $InstallDir."
}

Write-Host ""
Write-Host "  Installed! Desktop shortcuts created." -ForegroundColor Green
Write-Host ""
Write-Host "  Quick start:"
Write-Host "    Double-click 'dev-null Solo Play' to play locally."
Write-Host "    Double-click 'dev-null Server (public)' to host online."
Write-Host "    Double-click 'dev-null Create Games' to set up authoring"
Write-Host "      (installs gh + GitHub Copilot CLI on first run)."
Write-Host ""
Write-Host "  To start manually:"
Write-Host "    cd `"$InstallDir`""
Write-Host "    .\start-server.ps1   # run the server"
Write-Host "    .\start-client.ps1   # run the graphical client"
Write-Host ""
