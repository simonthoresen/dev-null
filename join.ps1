$ErrorActionPreference = 'Stop'

# The NS environment variable contains a base64url-encoded binary token with
# the server's connection endpoints. Format (variable-length):
#   Bytes 0-1:   SSH port (uint16 big-endian)
#   Bytes 2-5:   LAN IP (4 bytes; 0.0.0.0 = absent)
#   Bytes 6-9:   Public/UPnP IP (4 bytes; 0.0.0.0 = absent)
#   Bytes 10-11: Pinggy port (uint16 big-endian; 0 = no Pinggy)
#   Bytes 12+:   Pinggy hostname (UTF-8, remaining bytes)
# Localhost is always tried first (not encoded in the token).

if (-not $env:NS) {
    Write-Host "Error: NS not set. Use the invite command from a dev-null server." -ForegroundColor Red
    exit 1
}

# Decode base64url to bytes.
$b64 = $env:NS.Replace('-','+').Replace('_','/')
switch ($b64.Length % 4) {
    2 { $b64 += '==' }
    3 { $b64 += '=' }
}
$bytes = [Convert]::FromBase64String($b64)

if ($bytes.Length -lt 2) {
    Write-Host "Error: invalid invite token." -ForegroundColor Red
    exit 1
}

# Parse SSH port (big-endian uint16).
# Use multiplication instead of -shl to avoid [byte] overflow.
$sshPort = [int]$bytes[0] * 256 + [int]$bytes[1]

# Build endpoint list: localhost is always first.
$endpoints = @(@{ Host = 'localhost'; Port = $sshPort })

# LAN IP (bytes 2-5).
if ($bytes.Length -ge 6) {
    $ip = "$($bytes[2]).$($bytes[3]).$($bytes[4]).$($bytes[5])"
    if ($ip -ne '0.0.0.0') {
        $endpoints += @{ Host = $ip; Port = $sshPort }
    }
}

# Public/UPnP IP (bytes 6-9).
if ($bytes.Length -ge 10) {
    $ip = "$($bytes[6]).$($bytes[7]).$($bytes[8]).$($bytes[9])"
    if ($ip -ne '0.0.0.0') {
        $endpoints += @{ Host = $ip; Port = $sshPort }
    }
}

# Pinggy (bytes 10-11 port, 12+ hostname).
if ($bytes.Length -ge 12) {
    $pPort = [int]$bytes[10] * 256 + [int]$bytes[11]
    if ($pPort -ne 0 -and $bytes.Length -gt 12) {
        $pHost = [System.Text.Encoding]::UTF8.GetString($bytes, 12, $bytes.Length - 12)
        $endpoints += @{ Host = $pHost; Port = $pPort }
    }
}

$name = $env:USERNAME
$name = Read-Host "Enter your player name (default: $name)"
if ([string]::IsNullOrWhiteSpace($name)) {
    $name = $env:USERNAME
}

# Locate the dev-null install directory by checking known locations.
function Find-InstallDir {
    # 1. PATH
    $cmd = Get-Command "dev-null-client.exe" -ErrorAction SilentlyContinue
    if ($cmd) { return Split-Path $cmd.Source -Parent }

    # 2. Desktop shortcut
    $shortcut = Join-Path ([Environment]::GetFolderPath("Desktop")) "DevNull Client.lnk"
    if (Test-Path $shortcut) {
        $shell = New-Object -ComObject WScript.Shell
        $lnk   = $shell.CreateShortcut($shortcut)
        if ($lnk.Arguments -match '-File\s+"([^"]+)"') {
            $dir = Split-Path $Matches[1] -Parent
            if (Test-Path (Join-Path $dir "dev-null-client.exe")) { return $dir }
        }
    }

    # 3. %LocalAppData%\DevNull  (default install location)
    $candidate = Join-Path $env:LocalAppData "DevNull"
    if (Test-Path (Join-Path $candidate "dev-null-client.exe")) { return $candidate }

    # 4. %ProgramFiles%\DevNull  (legacy / system-wide install)
    $candidate = Join-Path $env:ProgramFiles "DevNull"
    if (Test-Path (Join-Path $candidate "dev-null-client.exe")) { return $candidate }

    return $null
}

$installDir  = Find-InstallDir
$clientExe   = $null
$startScript = $null

if ($installDir) {
    $clientExe = Join-Path $installDir "dev-null-client.exe"
    $s = Join-Path $installDir "start-client.ps1"
    if (Test-Path $s) { $startScript = $s }
} else {
    Write-Host ""
    Write-Host "dev-null client is not installed." -ForegroundColor Yellow
    $answer = Read-Host "Install it now? [Y/n]"
    if ($answer -eq '' -or $answer -match '^[Yy]') {
        $installDir = Join-Path $env:LocalAppData "DevNull"
        Write-Host "Installing to $installDir ..." -ForegroundColor Cyan
        & ([scriptblock]::Create((Invoke-RestMethod 'https://raw.githubusercontent.com/simonthoresen/dev-null/main/install.ps1'))) -InstallDir $installDir
        # Re-check after install.
        $installDir = Find-InstallDir
        if ($installDir) {
            $clientExe = Join-Path $installDir "dev-null-client.exe"
            $s = Join-Path $installDir "start-client.ps1"
            if (Test-Path $s) { $startScript = $s }
        }
    }
}

# Quick TCP reachability check — used as pre-flight before launching the GUI client
# so unreachable endpoints are skipped silently instead of showing an error dialog.
function Test-TcpEndpoint {
    param([string]$HostName, [int]$Port, [int]$TimeoutMs = 3000)
    $tcp = $null
    try {
        $tcp = [System.Net.Sockets.TcpClient]::new()
        $ar  = $tcp.BeginConnect($HostName, $Port, $null, $null)
        return $ar.AsyncWaitHandle.WaitOne($TimeoutMs) -and $tcp.Connected
    } catch { return $false }
    finally { if ($tcp) { try { $tcp.Close() } catch {} } }
}

# Read init commands from ~/.dev-null/client.txt if it exists (SSH fallback only).
$devNullInit = ""
$initFile = Join-Path $HOME ".dev-null" "client.txt"
if (Test-Path $initFile) {
    $initContent = Get-Content $initFile -Raw -ErrorAction SilentlyContinue
    if ($initContent) {
        $devNullInit = [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($initContent))
    }
}

foreach ($ep in $endpoints) {
    Write-Host "Trying $($ep.Host):$($ep.Port) ..." -ForegroundColor DarkGray

    if ($clientExe) {
        # TCP pre-flight: skip silently if the endpoint is unreachable.
        if (-not (Test-TcpEndpoint -HostName $ep.Host -Port $ep.Port)) { continue }

        if ($startScript) {
            powershell.exe -ExecutionPolicy Bypass -File $startScript `
                --host $ep.Host --port $ep.Port --player $name --no-update
        } else {
            & $clientExe --host $ep.Host --port $ep.Port --player $name
        }
    } else {
        # SSH fallback: set terminal environment variables then restore them on exit.
        $prev_TERM      = $env:TERM
        $prev_LANG      = $env:LANG
        $prev_COLOR     = $env:COLORTERM
        $prev_INIT      = $env:DEV_NULL_INIT
        $env:TERM       = "xterm-256color"
        $env:LANG       = "en_US.UTF-8"
        $env:COLORTERM  = "truecolor"
        if ($devNullInit) { $env:DEV_NULL_INIT = $devNullInit }

        $sshOpts = @(
            "-tt",
            "-o", "ConnectTimeout=5",
            "-o", "StrictHostKeyChecking=no",
            "-o", "UserKnownHostsFile=$( if ($IsWindows -or $env:OS -eq 'Windows_NT') { 'NUL' } else { '/dev/null' } )",
            "-o", "SendEnv=TERM",
            "-o", "SendEnv=LANG",
            "-o", "SendEnv=COLORTERM",
            "-o", "SendEnv=DEV_NULL_INIT"
        )
        ssh @sshOpts -p $ep.Port "${name}@$($ep.Host)"

        if ($null -eq $prev_TERM)  { Remove-Item Env:TERM          -ErrorAction SilentlyContinue } else { $env:TERM = $prev_TERM }
        if ($null -eq $prev_LANG)  { Remove-Item Env:LANG          -ErrorAction SilentlyContinue } else { $env:LANG = $prev_LANG }
        if ($null -eq $prev_COLOR) { Remove-Item Env:COLORTERM     -ErrorAction SilentlyContinue } else { $env:COLORTERM = $prev_COLOR }
        if ($null -eq $prev_INIT)  { Remove-Item Env:DEV_NULL_INIT -ErrorAction SilentlyContinue } else { $env:DEV_NULL_INIT = $prev_INIT }
    }

    if ($LASTEXITCODE -eq 0) { exit 0 }
}

Write-Host "Could not connect to any endpoint." -ForegroundColor Red
exit 1
