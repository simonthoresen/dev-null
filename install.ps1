# DevNull installer
# Usage: irm https://github.com/simonthoresen/DevNullCore/raw/main/install.ps1 | iex
#    or: save this file and run it directly

param(
    [string]$InstallDir = (Join-Path $env:USERPROFILE "DevNull")
)

$repo = "simonthoresen/DevNullCore"
$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "  DevNull installer" -ForegroundColor Cyan
Write-Host ""

# Find the latest release
Write-Host "  Fetching latest release..." -NoNewline
$headers = @{ Accept = "application/vnd.github+json" }
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/tags/latest" -Headers $headers -TimeoutSec 15
$zipAsset = $release.assets | Where-Object { $_.name -eq "DevNull.zip" } | Select-Object -First 1
if (-not $zipAsset) { throw "No DevNull.zip found in latest release." }
Write-Host " OK" -ForegroundColor Green

# Download the zip
$tempZip = Join-Path ([System.IO.Path]::GetTempPath()) "DevNull-install.zip"
Write-Host "  Downloading DevNull.zip..." -NoNewline
Invoke-WebRequest -Uri $zipAsset.browser_download_url -OutFile $tempZip -TimeoutSec 120
Write-Host " OK" -ForegroundColor Green

# Extract to temp folder, then merge into install dir (preserves user's custom files)
Write-Host "  Installing to $InstallDir..." -NoNewline
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "DevNull-install"
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
New-Item -ItemType Directory -Path (Join-Path $InstallDir "Logs") -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $InstallDir "Core\state") -Force | Out-Null
Write-Host " OK" -ForegroundColor Green

# Write version stamp into Core/.version
$version = ""
if ($release.body -match 'at ([0-9a-f]{40})') { $version = $Matches[1] }
if ($version) { Set-Content -Path (Join-Path $InstallDir "Core\.version") -Value $version -NoNewline }

# Create desktop shortcuts pointing at the root-level launcher scripts.
$desktop          = [Environment]::GetFolderPath("Desktop")
$serverPs1        = Join-Path $InstallDir "DevNullServer.ps1"
$clientPs1        = Join-Path $InstallDir "DevNull.ps1"
$createPs1        = Join-Path $InstallDir "DevNullCreate.ps1"
$serverIcon       = (Join-Path $InstallDir "Core\DevNullServer.exe") + ",0"
$clientIcon       = (Join-Path $InstallDir "Core\DevNullClient.exe") + ",0"
$shell            = New-Object -ComObject WScript.Shell

$public = $shell.CreateShortcut((Join-Path $desktop "DevNull Server (public).lnk"))
$public.TargetPath       = "powershell.exe"
$public.Arguments        = "-ExecutionPolicy Bypass -File `"$serverPs1`""
$public.WorkingDirectory = $InstallDir
$public.Description      = "Start the DevNull server (online multiplayer)"
$public.IconLocation     = $serverIcon
$public.Save()

$private = $shell.CreateShortcut((Join-Path $desktop "DevNull Server (LAN).lnk"))
$private.TargetPath       = "powershell.exe"
$private.Arguments        = "-ExecutionPolicy Bypass -File `"$serverPs1`" --lan"
$private.WorkingDirectory = $InstallDir
$private.Description      = "Start the DevNull server (LAN only, no Pinggy)"
$private.IconLocation     = $serverIcon
$private.Save()

$client = $shell.CreateShortcut((Join-Path $desktop "DevNull Client.lnk"))
$client.TargetPath       = "powershell.exe"
$client.Arguments        = "-ExecutionPolicy Bypass -File `"$clientPs1`""
$client.WorkingDirectory = $InstallDir
$client.Description      = "Start the DevNull graphical client"
$client.IconLocation     = $clientIcon
$client.Save()

$solo = $shell.CreateShortcut((Join-Path $desktop "DevNull Solo Play.lnk"))
$solo.TargetPath       = "powershell.exe"
$solo.Arguments        = "-ExecutionPolicy Bypass -File `"$clientPs1`" --local"
$solo.WorkingDirectory = $InstallDir
$solo.Description      = "Start a local server and connect the graphical client to it"
$solo.IconLocation     = $clientIcon
$solo.Save()

$create = $shell.CreateShortcut((Join-Path $desktop "DevNull Create Games.lnk"))
$create.TargetPath       = "powershell.exe"
$create.Arguments        = "-ExecutionPolicy Bypass -File `"$createPs1`""
$create.WorkingDirectory = $InstallDir
$create.Description      = "Set up authoring: installs gh + Copilot CLI on first run, forks the starter template, opens Copilot CLI"
$create.IconLocation     = $serverIcon
$create.Save()

Write-Host ""
Write-Host "  Installed! Desktop shortcuts created." -ForegroundColor Green
Write-Host ""
Write-Host "  Quick start:"
Write-Host "    Double-click 'DevNull Solo Play' to play locally."
Write-Host "    Double-click 'DevNull Server (public)' to host online."
Write-Host "    Double-click 'DevNull Create Games' to set up authoring"
Write-Host "      (installs gh + GitHub Copilot CLI on first run)."
Write-Host ""
Write-Host "  To start manually:"
Write-Host "    cd `"$InstallDir`""
Write-Host "    .\DevNullServer.ps1   # run the server"
Write-Host "    .\DevNull.ps1         # run the graphical client"
Write-Host ""
