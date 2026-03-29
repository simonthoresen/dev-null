param(
    [string]$Game = $(if ($args.Count -ge 1) { $args[0] } else { "towerdefense" }),
    [string]$Password = $(if ($args.Count -ge 2) { $args[1] } else { "changeme" })
)

$root = Split-Path -Parent $PSScriptRoot
$script:tunnelShell = $null
$script:tunnelWatcher = $null
$script:tunnelStatus = $null

function Stop-Tunnel {
    if ($script:tunnelShell) {
        $childProcesses = Get-CimInstance Win32_Process -Filter "ParentProcessId = $($script:tunnelShell.Id)" -ErrorAction SilentlyContinue
        foreach ($child in $childProcesses) {
            Stop-Process -Id $child.ProcessId -Force -ErrorAction SilentlyContinue
        }

        Stop-Process -Id $script:tunnelShell.Id -Force -ErrorAction SilentlyContinue
    }
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
        Stop-Job -Job $script:tunnelWatcher -ErrorAction SilentlyContinue
        Remove-Job -Job $script:tunnelWatcher -Force -ErrorAction SilentlyContinue
    }
}

function Start-TunnelWatcher {
    param(
        [int]$TunnelShellPid,
        [int]$ConsoleShellPid
    )

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
    Write-Error "Port 23234 is already in use by PID $($existingListener.OwningProcess). Stop that process or use a different listen port before starting null-space."
    exit 1
}

$script:tunnelStatus = Join-Path ([System.IO.Path]::GetTempPath()) ("null-space-pinggy-{0}.status.log" -f ([guid]::NewGuid().ToString("N")))

Write-Host "Starting Pinggy helper..." -ForegroundColor Cyan
$script:tunnelShell = Start-Process -FilePath "go" `
    -ArgumentList @("run", "./cmd/pinggy-helper", "--listen", "127.0.0.1:23234", "--status-file", $script:tunnelStatus) `
    -WorkingDirectory $root `
    -NoNewWindow `
    -PassThru

Start-TunnelWatcher -TunnelShellPid $script:tunnelShell.Id -ConsoleShellPid $PID

Write-Host "Waiting for Pinggy to publish the tunnel address..." -ForegroundColor Cyan

try {
    $tunnelInfo = Wait-ForTunnelReady -TimeoutSeconds 45
}
catch {
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
    & go run ./cmd/null-space --game $Game --password $Password
    if ($LASTEXITCODE) {
        $serverExitCode = $LASTEXITCODE
    }
}
finally {
    Pop-Location
    if ($null -eq $previousPinggyStatusFile) {
        Remove-Item Env:NULL_SPACE_PINGGY_STATUS_FILE -ErrorAction SilentlyContinue
    }
    else {
        $env:NULL_SPACE_PINGGY_STATUS_FILE = $previousPinggyStatusFile
    }
    Stop-TunnelWatcher
    Stop-Tunnel
    Remove-TunnelState
}

if ($serverExitCode -ne 0) {
    exit $serverExitCode
}