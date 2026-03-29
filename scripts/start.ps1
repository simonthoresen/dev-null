param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$CliArgs
)

$Game = "towerdefense"
$Password = "changeme"
$Force = $false

$positionals = @()
foreach ($arg in $CliArgs) {
    switch -Regex ($arg) {
        '^--force$' {
            $Force = $true
            continue
        }
        '^-force$' {
            $Force = $true
            continue
        }
        default {
            $positionals += $arg
        }
    }
}

if ($positionals.Count -ge 1 -and $positionals[0]) {
    $Game = $positionals[0]
}

if ($positionals.Count -ge 2 -and $positionals[1]) {
    $Password = $positionals[1]
}

$root = Split-Path -Parent $PSScriptRoot
$logsDir = Join-Path $root "logs"
$script:tunnelShell = $null
$script:tunnelWatcher = $null
$script:tunnelStatus = $null
$script:runLog = $null

function Write-RunLogLine {
    param([string]$Message)

    if (-not $script:runLog) {
        return
    }

    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss.fff"
    Add-Content -Path $script:runLog -Value ("{0} [script] {1}" -f $timestamp, $Message)
}

New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
$script:runLog = Join-Path $logsDir ((Get-Date -Format "yyyyMMdd-HHmmss") + ".log")
New-Item -ItemType File -Path $script:runLog -Force | Out-Null
Write-Host "Log file: $script:runLog" -ForegroundColor DarkGray
Write-RunLogLine "starting null-space start script"

$previousLogFile = $env:NULL_SPACE_LOG_FILE
$previousLogLevel = $env:NULL_SPACE_LOG_LEVEL
$env:NULL_SPACE_LOG_FILE = $script:runLog
if (-not $env:NULL_SPACE_LOG_LEVEL) {
    $env:NULL_SPACE_LOG_LEVEL = "info"
}
Write-RunLogLine "configured shared log environment"

function Stop-Tunnel {
    if ($script:tunnelShell) {
        Write-RunLogLine "stopping tunnel process tree"
        $childProcesses = Get-CimInstance Win32_Process -Filter "ParentProcessId = $($script:tunnelShell.Id)" -ErrorAction SilentlyContinue
        foreach ($child in $childProcesses) {
            Stop-Process -Id $child.ProcessId -Force -ErrorAction SilentlyContinue
        }

        Stop-Process -Id $script:tunnelShell.Id -Force -ErrorAction SilentlyContinue
    }
}

function Stop-ProcessTree {
    param([int]$RootPid)

    Write-RunLogLine "stopping process tree rooted at PID $RootPid"

    $all = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue
    if (-not $all) {
        Stop-Process -Id $RootPid -Force -ErrorAction SilentlyContinue
        return
    }

    $childrenByParent = @{}
    foreach ($proc in $all) {
        if (-not $childrenByParent.ContainsKey($proc.ParentProcessId)) {
            $childrenByParent[$proc.ParentProcessId] = @()
        }
        $childrenByParent[$proc.ParentProcessId] += $proc
    }

    $queue = [System.Collections.Generic.Queue[int]]::new()
    $toStop = [System.Collections.Generic.List[int]]::new()
    $queue.Enqueue($RootPid)

    while ($queue.Count -gt 0) {
        $parentPid = $queue.Dequeue()
        foreach ($child in ($childrenByParent[$parentPid] | Select-Object -Unique)) {
            $queue.Enqueue([int]$child.ProcessId)
            $toStop.Add([int]$child.ProcessId) | Out-Null
        }
    }

    for ($index = $toStop.Count - 1; $index -ge 0; $index--) {
        Stop-Process -Id $toStop[$index] -Force -ErrorAction SilentlyContinue
    }

    Stop-Process -Id $RootPid -Force -ErrorAction SilentlyContinue
}

function Remove-TunnelState {
    if ($script:tunnelStatus -and (Test-Path $script:tunnelStatus)) {
        Remove-Item $script:tunnelStatus -Force -ErrorAction SilentlyContinue
    }
}

function Read-TunnelState {
    if (-not $script:tunnelStatus -or -not (Test-Path $script:tunnelStatus)) {
        return @{}
    }

    $state = @{}
    foreach ($line in (Get-Content $script:tunnelStatus -ErrorAction SilentlyContinue)) {
        if ($line -match '^PINGGY_([^=]+)=(.*)$') {
            $state[$Matches[1]] = $Matches[2]
        }
    }

    return $state
}

function Stop-TunnelWatcher {
    if ($script:tunnelWatcher) {
        Write-RunLogLine "stopping tunnel watcher"
        Stop-Job -Job $script:tunnelWatcher -ErrorAction SilentlyContinue
        Remove-Job -Job $script:tunnelWatcher -Force -ErrorAction SilentlyContinue
    }
}

function Start-TunnelWatcher {
    param(
        [int]$TunnelShellPid,
        [int]$ConsoleShellPid
    )

    Write-RunLogLine "starting tunnel watcher for tunnel PID $TunnelShellPid"
    $script:tunnelWatcher = Start-Job -ScriptBlock {
        param(
            [int]$ObservedTunnelPid,
            [int]$ObservedConsolePid
        )

        function Get-DescendantProcesses {
            param([int]$RootPid)

            $all = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue
            if (-not $all) {
                return @()
            }

            $childrenByParent = @{}
            foreach ($proc in $all) {
                if (-not $childrenByParent.ContainsKey($proc.ParentProcessId)) {
                    $childrenByParent[$proc.ParentProcessId] = @()
                }
                $childrenByParent[$proc.ParentProcessId] += $proc
            }

            $queue = [System.Collections.Generic.Queue[object]]::new()
            $results = [System.Collections.Generic.List[object]]::new()
            $queue.Enqueue($RootPid)

            while ($queue.Count -gt 0) {
                $parentPid = [int]$queue.Dequeue()
                foreach ($child in ($childrenByParent[$parentPid] | Select-Object -Unique)) {
                    $results.Add($child) | Out-Null
                    $queue.Enqueue([int]$child.ProcessId)
                }
            }

            return $results
        }

        while ($true) {
            Start-Sleep -Milliseconds 500

            if (-not (Get-Process -Id $ObservedTunnelPid -ErrorAction SilentlyContinue)) {
                $targets = Get-DescendantProcesses -RootPid $ObservedConsolePid | Where-Object {
                    $_.Name -in @('go.exe', 'null-space.exe')
                }

                foreach ($target in $targets) {
                    Stop-Process -Id $target.ProcessId -Force -ErrorAction SilentlyContinue
                }

                break
            }

            if (-not (Get-Process -Id $ObservedConsolePid -ErrorAction SilentlyContinue)) {
                break
            }
        }
    } -ArgumentList $TunnelShellPid, $ConsoleShellPid
}

function Wait-ForTunnelReady {
    param([int]$TimeoutSeconds = 45)

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)

    while ((Get-Date) -lt $deadline) {
        $state = Read-TunnelState
        $tcpAddress = $state['TCP']
        $joinCommand = $state['JOIN']

        if ($joinCommand) {
            return [pscustomobject]@{
                TcpAddress  = $tcpAddress
                JoinCommand = $joinCommand
            }
        }

        if (-not (Get-Process -Id $script:tunnelShell.Id -ErrorAction SilentlyContinue)) {
            break
        }

        Start-Sleep -Milliseconds 250
    }

    $state = Read-TunnelState
    $details = @()
    if ($state['ERROR']) {
        $details += "Helper error: $($state['ERROR'])"
    }

    if ($state['LOG']) {
        $details += "Last Pinggy line: $($state['LOG'])"
    }

    $message = "Pinggy helper did not produce a join command within $TimeoutSeconds seconds."
    if ($details.Count -gt 0) {
        $message += "`n`n" + ($details -join "`n`n")
    }

    throw $message
}

$existingListener = Get-NetTCPConnection -LocalPort 23234 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
if ($existingListener) {
    if ($Force) {
        Write-Host "Port 23234 is in use by PID $($existingListener.OwningProcess). Stopping it because --force was specified..." -ForegroundColor Yellow
        Write-RunLogLine "force enabled; stopping existing listener on port 23234 (PID $($existingListener.OwningProcess))"
        Stop-ProcessTree -RootPid $existingListener.OwningProcess
        Start-Sleep -Milliseconds 500
        $existingListener = Get-NetTCPConnection -LocalPort 23234 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
    }

    if ($existingListener) {
        Write-RunLogLine "startup blocked; port 23234 still in use by PID $($existingListener.OwningProcess)"
        Write-Error "Port 23234 is already in use by PID $($existingListener.OwningProcess). Stop that process or use a different listen port before starting null-space."
        exit 1
    }
}

$script:tunnelStatus = Join-Path ([System.IO.Path]::GetTempPath()) ("null-space-pinggy-{0}.status.log" -f ([guid]::NewGuid().ToString("N")))

Write-Host "Starting Pinggy helper..." -ForegroundColor Cyan
Write-RunLogLine "starting pinggy helper"
$script:tunnelShell = Start-Process -FilePath "go" `
    -ArgumentList @("run", "./cmd/pinggy-helper", "--listen", "127.0.0.1:23234", "--status-file", $script:tunnelStatus) `
    -WorkingDirectory $root `
    -NoNewWindow `
    -PassThru

Start-TunnelWatcher -TunnelShellPid $script:tunnelShell.Id -ConsoleShellPid $PID

Write-Host "Waiting for Pinggy to publish the tunnel address..." -ForegroundColor Cyan
Write-RunLogLine "waiting for pinggy tunnel address"

try {
    $tunnelInfo = Wait-ForTunnelReady -TimeoutSeconds 45
    Write-RunLogLine "pinggy tunnel ready: $($tunnelInfo.TcpAddress)"
}
catch {
    Write-RunLogLine ("pinggy helper failed to publish address: {0}" -f $_)
    Stop-TunnelWatcher
    Stop-Tunnel
    Remove-TunnelState
    Write-Error $_
    exit 1
}

Write-Host ""
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host "                 LOBBY OPEN                  " -ForegroundColor Black -BackgroundColor Green
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host "Game:      $Game"
Write-Host "Tunnel:    $($tunnelInfo.TcpAddress)"
Write-Host "Join:      $($tunnelInfo.JoinCommand)"
Write-Host "Local:     ssh -t -p 23234 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null localhost"

$publicIP = $null
try {
    $publicIP = (Invoke-RestMethod -Uri 'https://ifconfig.me/ip' -TimeoutSec 5).Trim()
} catch {
    try { $publicIP = (Invoke-RestMethod -Uri 'https://api.ipify.org' -TimeoutSec 5).Trim() } catch {}
}

if ($publicIP) {
    $directCmd = "ssh -t -p 23234 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null $publicIP"
    $relayCmd = $tunnelInfo.JoinCommand
    if ($relayCmd -and -not ($relayCmd -match '\s-t\s|-t$')) {
        $relayCmd = $relayCmd -replace '^ssh ', 'ssh -t '
    }
    Write-Host "Direct:    $directCmd"
    if ($relayCmd) {
        $oneLiner = "$directCmd; if(`$LASTEXITCODE -ne 0){$relayCmd}"
        Write-Host ""
        Write-Host "One-liner (paste in Discord):" -ForegroundColor Yellow
        Write-Host $oneLiner -ForegroundColor Green
    }
} else {
    Write-Host "Direct:    (could not detect public IP)"
}

Write-Host ""
Write-Host "Tunnel PID: $($script:tunnelShell.Id)"
Write-Host ""
Write-Host "Local admin console is live in this terminal." -ForegroundColor Cyan
Write-Host "Type chat text to broadcast globally, or use /commands as admin." -ForegroundColor Cyan
Write-Host "If the tunnel drops, the server will stop automatically." -ForegroundColor Cyan
Write-Host "Press Ctrl+C to stop both the server and the tunnel." -ForegroundColor Cyan
Write-Host ""

$serverExitCode = 0
$previousPinggyStatusFile = $env:NULL_SPACE_PINGGY_STATUS_FILE
$env:NULL_SPACE_PINGGY_STATUS_FILE = $script:tunnelStatus

Push-Location $root
try {
    Write-RunLogLine "starting null-space server"
    & go run ./cmd/null-space --game $Game --password $Password
    if ($LASTEXITCODE) {
        $serverExitCode = $LASTEXITCODE
    }
}
finally {
    Pop-Location
    Write-RunLogLine "server process finished"
    if ($null -eq $previousPinggyStatusFile) {
        Remove-Item Env:NULL_SPACE_PINGGY_STATUS_FILE -ErrorAction SilentlyContinue
    }
    else {
        $env:NULL_SPACE_PINGGY_STATUS_FILE = $previousPinggyStatusFile
    }
    if ($null -eq $previousLogFile) {
        Remove-Item Env:NULL_SPACE_LOG_FILE -ErrorAction SilentlyContinue
    }
    else {
        $env:NULL_SPACE_LOG_FILE = $previousLogFile
    }
    if ($null -eq $previousLogLevel) {
        Remove-Item Env:NULL_SPACE_LOG_LEVEL -ErrorAction SilentlyContinue
    }
    else {
        $env:NULL_SPACE_LOG_LEVEL = $previousLogLevel
    }
    Stop-TunnelWatcher
    Stop-Tunnel
    Remove-TunnelState
    Write-RunLogLine "cleanup completed"
}

if ($serverExitCode -ne 0) {
    Write-RunLogLine "exiting with non-zero server exit code"
    exit $serverExitCode
}