param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$CliArgs
)

$Password = "changeme"
$Force = $false

$positionals = @()
foreach ($arg in $CliArgs) {
    switch -Regex ($arg) {
        '^--?force$' { $Force = $true; continue }
        default      { $positionals += $arg }
    }
}

if ($positionals.Count -ge 1 -and $positionals[0]) {
    $Password = $positionals[0]
}

$root = $PSScriptRoot
$logsDir = Join-Path $root "logs"
$script:tunnelShell = $null
$script:tunnelWatcher = $null
$script:tunnelStatus = $null
$script:runLog = $null
$script:bootStepLabel = $null
$script:bootStepWidth = 80

# ── boot-step helpers ────────────────────────────────────────────────────────

function Get-TermWidth {
    try { return $Host.UI.RawUI.BufferSize.Width } catch { return 80 }
}

function Get-StatusToken {
    param([string]$Status)
    $inner = 7  # widest status (IGNORED/SKIPPED) = 7 chars; token is always 11 chars total
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
    # layout: label + " " + dots + " " + token(11)
    $dots = $script:bootStepWidth - $Label.Length - 2 - 11
    if ($dots -lt 1) { $dots = 1 }
    Write-Host -NoNewline ($Label + ' ' + ('.' * $dots))
}

function Write-BootStepEnd {
    param([string]$Status)
    $label = $script:bootStepLabel
    $width = $script:bootStepWidth
    $token = Get-StatusToken $Status
    $dots  = $width - $label.Length - 2 - 11
    if ($dots -lt 1) { $dots = 1 }
    $color = switch ($Status) {
        'DONE'    { 'Green'    }
        'OK'      { 'Green'    }
        'FAILED'  { 'Red'      }
        'IGNORED' { 'Yellow'   }
        'SKIPPED' { 'DarkGray' }
        default   { 'White'    }
    }
    Write-Host -NoNewline ("`r" + $label + ' ' + ('.' * $dots) + ' ')
    Write-Host $token -ForegroundColor $color
}

# ── logging ──────────────────────────────────────────────────────────────────

New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
$script:runLog = Join-Path $logsDir ((Get-Date -Format "yyyyMMdd-HHmmss") + ".log")
New-Item -ItemType File -Path $script:runLog -Force | Out-Null

function Write-RunLogLine {
    param([string]$Message)
    if (-not $script:runLog) { return }
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss.fff"
    Add-Content -Path $script:runLog -Value ("{0} [script] {1}" -f $timestamp, $Message)
}

Write-RunLogLine "starting null-space start script"

$previousLogFile  = $env:NULL_SPACE_LOG_FILE
$previousLogLevel = $env:NULL_SPACE_LOG_LEVEL
$env:NULL_SPACE_LOG_FILE = $script:runLog
if (-not $env:NULL_SPACE_LOG_LEVEL) { $env:NULL_SPACE_LOG_LEVEL = "info" }

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
                    Where-Object { $_.ParentProcessId -eq $ConsolePid -and $_.Name -in @('null-space.exe') }
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

# ── pre-flight ───────────────────────────────────────────────────────────────

$existingListener = Get-NetTCPConnection -LocalPort 23234 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
if ($existingListener) {
    if ($Force) {
        Write-BootStepStart "Port 23234 (force-stopping PID $($existingListener.OwningProcess))"
        Stop-ProcessTree -RootPid $existingListener.OwningProcess
        Start-Sleep -Milliseconds 500
        $existingListener = Get-NetTCPConnection -LocalPort 23234 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($existingListener) {
            Write-BootStepEnd "FAILED"
            Write-Error "Port 23234 is still in use after --force."
            exit 1
        }
        Write-BootStepEnd "DONE"
    } else {
        Write-Error "Port 23234 is already in use by PID $($existingListener.OwningProcess). Use --force to stop it."
        exit 1
    }
}

# ── start tunnel ─────────────────────────────────────────────────────────────

$script:tunnelStatus = Join-Path ([System.IO.Path]::GetTempPath()) ("null-space-pinggy-{0}.status.log" -f ([guid]::NewGuid().ToString("N")))

Write-BootStepStart "Pinggy helper"
Write-RunLogLine "starting pinggy helper"
$script:tunnelShell = Start-Process `
    -FilePath (Join-Path $root "pinggy-helper.exe") `
    -ArgumentList @("--listen", "127.0.0.1:23234", "--status-file", $script:tunnelStatus) `
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
    Write-BootStepEnd "FAILED"
    Stop-TunnelWatcher; Stop-Tunnel; Remove-TunnelState
    Write-Error $_
    exit 1
}

# ── start server ─────────────────────────────────────────────────────────────

$serverExitCode = 0
$previousPinggyStatusFile       = $env:NULL_SPACE_PINGGY_STATUS_FILE
$env:NULL_SPACE_PINGGY_STATUS_FILE = $script:tunnelStatus

Push-Location $root
try {
    Write-RunLogLine "starting null-space server"
    & (Join-Path $root "null-space.exe") --password $Password
    if ($LASTEXITCODE) { $serverExitCode = $LASTEXITCODE }
} finally {
    Pop-Location
    Write-RunLogLine "server process finished"
    if ($null -eq $previousPinggyStatusFile) { Remove-Item Env:NULL_SPACE_PINGGY_STATUS_FILE -ErrorAction SilentlyContinue }
    else { $env:NULL_SPACE_PINGGY_STATUS_FILE = $previousPinggyStatusFile }
    if ($null -eq $previousLogFile)  { Remove-Item Env:NULL_SPACE_LOG_FILE  -ErrorAction SilentlyContinue }
    else { $env:NULL_SPACE_LOG_FILE = $previousLogFile }
    if ($null -eq $previousLogLevel) { Remove-Item Env:NULL_SPACE_LOG_LEVEL -ErrorAction SilentlyContinue }
    else { $env:NULL_SPACE_LOG_LEVEL = $previousLogLevel }

    Write-BootStepStart "Stopping Pinggy helper"
    Stop-TunnelWatcher; Stop-Tunnel; Remove-TunnelState
    Write-BootStepEnd "DONE"

    Write-RunLogLine "cleanup completed"
    Write-Host ""
}

if ($serverExitCode -ne 0) { exit $serverExitCode }
