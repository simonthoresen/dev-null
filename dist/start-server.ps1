param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$CliArgs
)

$Password = ""
$Force = $false
$Lan = $false
$NoUpdate = $false
$LogLevel = ""
$Port = "23234"
$Term = ""

$positionals = @()
for ($i = 0; $i -lt $CliArgs.Count; $i++) {
    $arg = $CliArgs[$i]
    switch -Regex ($arg) {
        '^--?force$'         { $Force = $true; continue }
        '^--?lan$'           { $Lan = $true; continue }
        '^--?(no-?update|skip-?update)$' { $NoUpdate = $true; continue }
        '^--?log-?level$'    { $i++; if ($i -lt $CliArgs.Count) { $LogLevel = $CliArgs[$i] }; continue }
        '^--?log-?level=(.+)$' { $LogLevel = $Matches[1]; continue }
        '^--?debug$'         { $LogLevel = "debug"; continue }
        '^--?port$'          { $i++; if ($i -lt $CliArgs.Count) { $Port = $CliArgs[$i] }; continue }
        '^--?port=(.+)$'     { $Port = $Matches[1]; continue }
        '^--?term$'          { $i++; if ($i -lt $CliArgs.Count) { $Term = $CliArgs[$i] }; continue }
        '^--?term=(.+)$'     { $Term = $Matches[1]; continue }
        default              { $positionals += $arg }
    }
}

if ($positionals.Count -ge 1 -and $positionals[0]) {
    $Password = $positionals[0]
}

# Password is optional — pass via first positional arg or --password in the future.
# If not provided, the server starts without one (can be set at runtime via /password).

$root = $PSScriptRoot
$logsDir = Join-Path $root "logs"
$repo = "simonthoresen/dev-null"
$script:tunnelShell = $null
$script:tunnelWatcher = $null
$script:tunnelStatus = $null
$script:runLog = $null
$script:bootStepLabel = $null
$script:bootStepWidth = 80

# ── boot-step helpers ────────────────────────────────────────────────────────

function Get-TermWidth {
    try {
        $w = $Host.UI.RawUI.WindowSize.Width
        if ($w -gt 0) { return $w }
    } catch {}
    try { return $Host.UI.RawUI.BufferSize.Width } catch {}
    return 80
}

function Get-StatusToken {
    param([string]$Status)
    $inner = 4  # widest status (DONE/FAIL/SKIP) = 4 chars; token is always 8 chars total
    $pad   = $inner - $Status.Length
    if ($pad -lt 0) { $pad = 0 }
    $left  = [Math]::Floor($pad / 2)
    $right = $pad - $left
    return '[ ' + (' ' * $left) + $Status + (' ' * $right) + ' ]'
}

function Write-BootStepStart {
    param([string]$Label)
    $script:bootStepLabel = $Label
    $script:bootStepWidth = Get-TermWidth
    # layout: label + " " + dots + " " + token(8)
    $dots = $script:bootStepWidth - $Label.Length - 2 - 8
    if ($dots -lt 1) { $dots = 1 }
    Write-Host -NoNewline ($Label + ' ' + ('.' * $dots))
}

function Write-BootStepEnd {
    param([string]$Status)
    $label = $script:bootStepLabel
    $width = $script:bootStepWidth
    $dots  = $width - $label.Length - 2 - 8
    if ($dots -lt 1) { $dots = 1 }
    $paddedStatus = Get-StatusToken $Status
    $inner = $paddedStatus.Substring(2, $paddedStatus.Length - 4)
    $noColor = $Term -in @('none', 'ascii', 'no-color')
    if ($noColor) {
        Write-Host ("`r" + $label + ' ' + ('.' * $dots) + ' [ ' + $inner + ' ]')
    } else {
        $color = switch ($Status) {
            'DONE' { 'Green'  }
            'FAIL' { 'Red'    }
            'SKIP' { 'Yellow' }
            default { 'White' }
        }
        Write-Host -NoNewline ("`r" + $label + ' ' + ('.' * $dots) + ' [ ')
        Write-Host -NoNewline $inner -ForegroundColor $color
        Write-Host ' ]'
    }
}

# ── logging ──────────────────────────────────────────────────────────────────

New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
$script:runLog = Join-Path $logsDir ((Get-Date -Format "yyyyMMdd-HHmmss") + ".log")
New-Item -ItemType File -Path $script:runLog -Force | Out-Null

function Write-RunLogLine {
    param([string]$Message)
    if (-not $script:runLog) { return }
    $timestamp = (Get-Date).ToString("yyyy-MM-ddTHH:mm:ss.fffzzz")
    Add-Content -Path $script:runLog -Value ("time={0} level=INFO msg=`"{1}`" component=script pid={2}" -f $timestamp, $Message, $PID)
}

Write-RunLogLine "starting dev-null start script"

$previousLogFile      = $env:DEV_NULL_LOG_FILE
$previousLogLevel     = $env:DEV_NULL_LOG_LEVEL
$previousTermWidth    = $env:DEV_NULL_TERM_WIDTH
$env:DEV_NULL_LOG_FILE    = $script:runLog
$env:DEV_NULL_TERM_WIDTH  = Get-TermWidth
if ($LogLevel) { $env:DEV_NULL_LOG_LEVEL = $LogLevel }
elseif (-not $env:DEV_NULL_LOG_LEVEL) { $env:DEV_NULL_LOG_LEVEL = "info" }

# ── cleanup helpers ──────────────────────────────────────────────────────────

function Stop-Tunnel {
    if ($script:tunnelShell) {
        Write-RunLogLine "stopping tunnel process tree"
        $children = Get-CimInstance Win32_Process -Filter "ParentProcessId = $($script:tunnelShell.Id)" -ErrorAction SilentlyContinue
        foreach ($child in $children) {
            Stop-Process -Id $child.ProcessId -Force -ErrorAction SilentlyContinue
        }
        Stop-Process -Id $script:tunnelShell.Id -Force -ErrorAction SilentlyContinue
    }
}

function Stop-ProcessTree {
    param([int]$RootPid)
    Write-RunLogLine "stopping process tree rooted at PID $RootPid"
    $all = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue
    if (-not $all) { Stop-Process -Id $RootPid -Force -ErrorAction SilentlyContinue; return }
    $childrenByParent = @{}
    foreach ($proc in $all) {
        if (-not $childrenByParent.ContainsKey($proc.ParentProcessId)) {
            $childrenByParent[$proc.ParentProcessId] = @()
        }
        $childrenByParent[$proc.ParentProcessId] += $proc
    }
    $queue  = [System.Collections.Generic.Queue[int]]::new()
    $toStop = [System.Collections.Generic.List[int]]::new()
    $queue.Enqueue($RootPid)
    while ($queue.Count -gt 0) {
        $parentPid = $queue.Dequeue()
        foreach ($child in ($childrenByParent[$parentPid] | Select-Object -Unique)) {
            $queue.Enqueue([int]$child.ProcessId)
            $toStop.Add([int]$child.ProcessId) | Out-Null
        }
    }
    for ($i = $toStop.Count - 1; $i -ge 0; $i--) {
        Stop-Process -Id $toStop[$i] -Force -ErrorAction SilentlyContinue
    }
    Stop-Process -Id $RootPid -Force -ErrorAction SilentlyContinue
}

function Remove-TunnelState {
    if ($script:tunnelStatus -and (Test-Path $script:tunnelStatus)) {
        Remove-Item $script:tunnelStatus -Force -ErrorAction SilentlyContinue
    }
}

function Read-TunnelState {
    if (-not $script:tunnelStatus -or -not (Test-Path $script:tunnelStatus)) { return @{} }
    $state = @{}
    foreach ($line in (Get-Content $script:tunnelStatus -ErrorAction SilentlyContinue)) {
        if ($line -match '^PINGGY_([^=]+)=(.*)$') { $state[$Matches[1]] = $Matches[2] }
    }
    return $state
}

function Stop-TunnelWatcher {
    if ($script:tunnelWatcher) {
        Stop-Job  -Job $script:tunnelWatcher -ErrorAction SilentlyContinue
        Remove-Job -Job $script:tunnelWatcher -Force -ErrorAction SilentlyContinue
    }
}

function Start-TunnelWatcher {
    param([int]$TunnelShellPid, [int]$ConsoleShellPid)
    Write-RunLogLine "starting tunnel watcher for tunnel PID $TunnelShellPid"
    $script:tunnelWatcher = Start-Job -ScriptBlock {
        param([int]$TunnelPid, [int]$ConsolePid)
        while ($true) {
            Start-Sleep -Milliseconds 500
            if (-not (Get-Process -Id $TunnelPid -ErrorAction SilentlyContinue)) {
                $targets = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
                    Where-Object { $_.ParentProcessId -eq $ConsolePid -and $_.Name -in @('dev-null-server.exe') }
                foreach ($t in $targets) {
                    Stop-Process -Id $t.ProcessId -Force -ErrorAction SilentlyContinue
                }
                break
            }
            if (-not (Get-Process -Id $ConsolePid -ErrorAction SilentlyContinue)) { break }
        }
    } -ArgumentList $TunnelShellPid, $ConsoleShellPid
}

function Wait-ForTunnelReady {
    param([int]$TimeoutSeconds = 45)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        $state = Read-TunnelState
        if ($state['JOIN']) {
            return [pscustomobject]@{ TcpAddress = $state['TCP']; JoinCommand = $state['JOIN'] }
        }
        if (-not (Get-Process -Id $script:tunnelShell.Id -ErrorAction SilentlyContinue)) { break }
        Start-Sleep -Milliseconds 250
    }
    $state   = Read-TunnelState
    $details = @()
    if ($state['ERROR']) { $details += "Helper error: $($state['ERROR'])" }
    if ($state['LOG'])   { $details += "Last Pinggy line: $($state['LOG'])" }
    $message = "Pinggy helper did not produce a join command within $TimeoutSeconds seconds."
    if ($details.Count -gt 0) { $message += "`n`n" + ($details -join "`n`n") }
    throw $message
}

# ── auto-update binaries ────────────────────────────────────────────────────

function Update-FromRelease {
    if ($NoUpdate) {
        Write-BootStepStart "Checking for updates"
        Write-BootStepEnd "SKIP"
        return
    }
    Write-BootStepStart "Checking for updates"
    try {
        $headers = @{ Accept = "application/vnd.github+json" }
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/tags/latest" -Headers $headers -TimeoutSec 10

        # Compare release commit SHA with local version stamp
        $versionFile = Join-Path $root ".version"
        $localVersion = if (Test-Path $versionFile) { (Get-Content $versionFile -Raw).Trim() } else { "" }
        $remoteVersion = ""
        if ($release.body -match 'at ([0-9a-f]{40})') { $remoteVersion = $Matches[1] }

        if ($localVersion -eq $remoteVersion -and $localVersion -ne "") {
            Write-BootStepEnd "DONE"
            return
        }

        # If no .version file, check whether the local exe is newer than the release.
        # This avoids overwriting a locally-built binary with an older release.
        if ($localVersion -eq "") {
            $localExe = Join-Path $root "dev-null-server.exe"
            if (Test-Path $localExe) {
                $localTime = (Get-Item $localExe).LastWriteTimeUtc
                $releaseTime = [DateTimeOffset]::Parse($release.published_at).UtcDateTime
                if ($localTime -ge $releaseTime) {
                    Write-BootStepEnd "DONE"
                    Write-RunLogLine "local binary is newer than release, skipping update"
                    return
                }
            }
        }

        Write-BootStepEnd "DONE"

        # Download the full release zip (includes binaries, games, fonts, etc.)
        $zipAsset = $release.assets | Where-Object { $_.name -eq "dev-null.zip" } | Select-Object -First 1
        if (-not $zipAsset) {
            Write-RunLogLine "no dev-null.zip in release, skipping update"
            return
        }

        Write-BootStepStart "Downloading update"
        $tempZip = Join-Path ([System.IO.Path]::GetTempPath()) "dev-null-update.zip"
        $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "dev-null-update"
        Invoke-WebRequest -Uri $zipAsset.browser_download_url -OutFile $tempZip -TimeoutSec 120

        # Extract to temp folder, then merge into install dir (preserves user's custom files)
        if (Test-Path $tempDir) { Remove-Item $tempDir -Recurse -Force }
        Expand-Archive -Path $tempZip -DestinationPath $tempDir -Force
        Get-ChildItem -Path $tempDir -Recurse -File | ForEach-Object {
            $rel  = $_.FullName.Substring($tempDir.Length + 1)
            $dest = Join-Path $root $rel
            $dir  = Split-Path $dest -Parent
            if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
            Copy-Item -Path $_.FullName -Destination $dest -Force
        }
        Remove-Item $tempZip -Force
        Remove-Item $tempDir -Recurse -Force
        Write-BootStepEnd "DONE"

        if ($remoteVersion) { Set-Content -Path $versionFile -Value $remoteVersion -NoNewline }
    } catch {
        Write-BootStepEnd "SKIP"
        Write-RunLogLine ("auto-update check failed: {0}" -f $_)
    }
}

Update-FromRelease

# ── pre-flight ───────────────────────────────────────────────────────────────

$existingListener = Get-NetTCPConnection -LocalPort ([int]$Port) -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
if ($existingListener) {
    if ($Force) {
        Write-BootStepStart "Port $Port (force-stopping PID $($existingListener.OwningProcess))"
        Stop-ProcessTree -RootPid $existingListener.OwningProcess
        Start-Sleep -Milliseconds 500
        $existingListener = Get-NetTCPConnection -LocalPort ([int]$Port) -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($existingListener) {
            Write-BootStepEnd "FAIL"
            Write-Error "Port $Port is still in use after --force."
            exit 1
        }
        Write-BootStepEnd "DONE"
    } else {
        Write-Error "Port $Port is already in use by PID $($existingListener.OwningProcess). Use --force to stop it."
        exit 1
    }
}

# ── start tunnel ─────────────────────────────────────────────────────────────

Write-BootStepStart "Setting up network"
Write-BootStepEnd "DONE"

$serverArgs = @("--data-dir", $root, "--port", $Port)
if ($Password) { $serverArgs = @("--password", $Password) + $serverArgs }
if ($Term) { $serverArgs += "--term"; $serverArgs += $Term }

if ($Lan) {
    Write-BootStepStart "Pinggy helper"
    Write-BootStepEnd "SKIP"
    $serverArgs += "--lan"
} else {
    $script:tunnelStatus = Join-Path ([System.IO.Path]::GetTempPath()) ("dev-null-pinggy-{0}.status.log" -f ([guid]::NewGuid().ToString("N")))

    Write-BootStepStart "Pinggy helper"
    Write-RunLogLine "starting pinggy helper"
    $script:tunnelShell = Start-Process `
        -FilePath (Join-Path $root "pinggy-helper.exe") `
        -ArgumentList @("--listen", "127.0.0.1:$Port", "--status-file", $script:tunnelStatus) `
        -WorkingDirectory $root `
        -RedirectStandardOutput (Join-Path $logsDir "pinggy-stdout.log") `
        -RedirectStandardError  (Join-Path $logsDir "pinggy-stderr.log") `
        -NoNewWindow -PassThru

    Start-TunnelWatcher -TunnelShellPid $script:tunnelShell.Id -ConsoleShellPid $PID

    try {
        $tunnelInfo = Wait-ForTunnelReady -TimeoutSeconds 45
        Write-RunLogLine "pinggy tunnel ready: $($tunnelInfo.TcpAddress)"
        Write-BootStepEnd "DONE"
    } catch {
        Write-RunLogLine ("pinggy helper failed: {0}" -f $_)
        Write-BootStepEnd "FAIL"
        Stop-TunnelWatcher; Stop-Tunnel; Remove-TunnelState
        Write-Error $_
        exit 1
    }

    $previousPinggyStatusFile       = $env:DEV_NULL_PINGGY_STATUS_FILE
    $env:DEV_NULL_PINGGY_STATUS_FILE = $script:tunnelStatus
}

# ── start server ─────────────────────────────────────────────────────────────

$serverExitCode = 0

Push-Location $root
try {
    Write-RunLogLine "starting dev-null server"
    & (Join-Path $root "dev-null-server.exe") @serverArgs
    if ($LASTEXITCODE) { $serverExitCode = $LASTEXITCODE }
} finally {
    Pop-Location
    Write-RunLogLine "server process finished"
    if (-not $Lan) {
        if ($null -eq $previousPinggyStatusFile) { Remove-Item Env:DEV_NULL_PINGGY_STATUS_FILE -ErrorAction SilentlyContinue }
        else { $env:DEV_NULL_PINGGY_STATUS_FILE = $previousPinggyStatusFile }
    }
    if ($null -eq $previousLogFile)    { Remove-Item Env:DEV_NULL_LOG_FILE    -ErrorAction SilentlyContinue }
    else { $env:DEV_NULL_LOG_FILE = $previousLogFile }
    if ($null -eq $previousLogLevel)  { Remove-Item Env:DEV_NULL_LOG_LEVEL  -ErrorAction SilentlyContinue }
    else { $env:DEV_NULL_LOG_LEVEL = $previousLogLevel }
    if ($null -eq $previousTermWidth) { Remove-Item Env:DEV_NULL_TERM_WIDTH -ErrorAction SilentlyContinue }
    else { $env:DEV_NULL_TERM_WIDTH = $previousTermWidth }

    Write-BootStepStart "Shutting down network"
    Write-BootStepEnd "DONE"
    if (-not $Lan) {
        Write-BootStepStart "Stopping Pinggy helper"
        Stop-TunnelWatcher; Stop-Tunnel; Remove-TunnelState
        Write-BootStepEnd "DONE"
    }

    Write-RunLogLine "cleanup completed"
}

if ($serverExitCode -ne 0) { exit $serverExitCode }
