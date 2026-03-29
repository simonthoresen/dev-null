param(
    [string]$Game = $(if ($args.Count -ge 1) { $args[0] } else { "towerdefense" }),
    [string]$Password = $(if ($args.Count -ge 2) { $args[1] } else { "changeme" })
)

$root = Split-Path -Parent $PSScriptRoot
$serverOut = Join-Path $env:TEMP "null-space-server.out.log"
$serverErr = Join-Path $env:TEMP "null-space-server.err.log"

$server = Start-Process -FilePath "go" `
    -ArgumentList @("run", "./cmd/null-space", "--game", $Game, "--password", $Password) `
    -WorkingDirectory $root `
    -WindowStyle Hidden `
    -RedirectStandardOutput $serverOut `
    -RedirectStandardError $serverErr `
    -PassThru

Start-Sleep -Seconds 2

$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = "ssh"
$psi.Arguments = "-p 443 -o ServerAliveInterval=30 -R0:127.0.0.1:23234 tcp@a.pinggy.io"
$psi.WorkingDirectory = $root
$psi.UseShellExecute = $false
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.CreateNoWindow = $true

$pinggy = New-Object System.Diagnostics.Process
$pinggy.StartInfo = $psi
$null = $pinggy.Start()

$regex = [regex]'tcp://[^\s]+'
$connection = $null

while (-not $pinggy.HasExited -and -not $connection) {
    if (-not $pinggy.StandardOutput.EndOfStream) {
        $line = $pinggy.StandardOutput.ReadLine()
        $match = $regex.Match($line)
        if ($match.Success) {
            $connection = $match.Value
            break
        }
    }

    if (-not $pinggy.StandardError.EndOfStream) {
        $line = $pinggy.StandardError.ReadLine()
        $match = $regex.Match($line)
        if ($match.Success) {
            $connection = $match.Value
            break
        }
    }

    Start-Sleep -Milliseconds 100
}

if (-not $connection) {
    Write-Error "Pinggy did not return a TCP tunnel address."
    if (-not $server.HasExited) {
        Stop-Process -Id $server.Id -Force
    }
    exit 1
}

$uri = [Uri]$connection
$sshCommand = "ssh {0}@{1} -p {2}" -f "your-name", $uri.Host, $uri.Port

Clear-Host
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host "                 LOBBY OPEN                  " -ForegroundColor Black -BackgroundColor Green
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host "Game:      $Game"
Write-Host "Endpoint:  $connection"
Write-Host "Join with: $sshCommand"
Write-Host "Server PID: $($server.Id)"
Write-Host "Tunnel PID: $($pinggy.Id)"
Write-Host ""

Wait-Process -Id $pinggy.Id

if (-not $server.HasExited) {
    Stop-Process -Id $server.Id -Force
}