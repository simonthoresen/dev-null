param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$CliArgs
)

$NoUpdate = $false
$LogLevel = ""
$Host_    = "localhost"
$Port     = "23234"
$Player   = ""
$Local    = $false
$NoGUI    = $false
$Term     = ""
$Game     = ""
$Resume   = ""

$positionals = @()
for ($i = 0; $i -lt $CliArgs.Count; $i++) {
    $arg = $CliArgs[$i]
    switch -Regex ($arg) {
        '^--?(no-?update|skip-?update)$' { $NoUpdate = $true; continue }
        '^--?local$'         { $Local = $true; continue }
        '^--?no-?gui$'       { $NoGUI = $true; continue }
        '^--?debug$'         { $LogLevel = "debug"; continue }
        '^--?log-?level$'    { $i++; if ($i -lt $CliArgs.Count) { $LogLevel = $CliArgs[$i] }; continue }
        '^--?log-?level=(.+)$' { $LogLevel = $Matches[1]; continue }
        '^--?host$'          { $i++; if ($i -lt $CliArgs.Count) { $Host_ = $CliArgs[$i] }; continue }
        '^--?host=(.+)$'     { $Host_ = $Matches[1]; continue }
        '^--?port$'          { $i++; if ($i -lt $CliArgs.Count) { $Port = $CliArgs[$i] }; continue }
        '^--?port=(.+)$'     { $Port = $Matches[1]; continue }
        '^--?player$'        { $i++; if ($i -lt $CliArgs.Count) { $Player = $CliArgs[$i] }; continue }
        '^--?player=(.+)$'   { $Player = $Matches[1]; continue }
        '^--?term$'          { $i++; if ($i -lt $CliArgs.Count) { $Term = $CliArgs[$i] }; continue }
        '^--?term=(.+)$'     { $Term = $Matches[1]; continue }
        '^--?game$'          { $i++; if ($i -lt $CliArgs.Count) { $Game = $CliArgs[$i] }; continue }
        '^--?game=(.+)$'     { $Game = $Matches[1]; continue }
        '^--?resume$'        { $i++; if ($i -lt $CliArgs.Count) { $Resume = $CliArgs[$i] }; continue }
        '^--?resume=(.+)$'   { $Resume = $Matches[1]; continue }
        default              { $positionals += $arg }
    }
}

$root = $PSScriptRoot
$logsDir = Join-Path $root "logs"
$repo = "simonthoresen/dev-null"
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
    $inner = 4
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

# ── logging ──────────────────────────────────────────────────────────────────

New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
$script:runLog = Join-Path $logsDir ((Get-Date -Format "yyyyMMdd-HHmmss") + "-client.log")
New-Item -ItemType File -Path $script:runLog -Force | Out-Null

function Write-RunLogLine {
    param([string]$Message)
    if (-not $script:runLog) { return }
    $timestamp = (Get-Date).ToString("yyyy-MM-ddTHH:mm:ss.fffzzz")
    Add-Content -Path $script:runLog -Value ("time={0} level=INFO msg=`"{1}`" component=script pid={2}" -f $timestamp, $Message, $PID)
}

Write-RunLogLine "starting dev-null client script"

$previousLogLevel  = $env:DEV_NULL_LOG_LEVEL
$previousTermWidth = $env:DEV_NULL_TERM_WIDTH
$env:DEV_NULL_TERM_WIDTH = Get-TermWidth
if ($LogLevel) { $env:DEV_NULL_LOG_LEVEL = $LogLevel }
elseif (-not $env:DEV_NULL_LOG_LEVEL) { $env:DEV_NULL_LOG_LEVEL = "info" }

# ── auto-update binary ───────────────────────────────────────────────────────

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

        $versionFile = Join-Path $root ".version"
        $localVersion = if (Test-Path $versionFile) { (Get-Content $versionFile -Raw).Trim() } else { "" }
        $remoteVersion = ""
        if ($release.body -match 'at ([0-9a-f]{40})') { $remoteVersion = $Matches[1] }

        if ($localVersion -eq $remoteVersion -and $localVersion -ne "") {
            Write-BootStepEnd "DONE"
            return
        }

        # No .version file: fall back to timestamp comparison.
        $localExe = Join-Path $root "dev-null-client.exe"
        if (Test-Path $localExe) {
            $localTime = (Get-Item $localExe).LastWriteTimeUtc
            $releaseTime = [DateTimeOffset]::Parse($release.published_at).UtcDateTime
            if ($localTime -ge $releaseTime) {
                Write-BootStepEnd "DONE"
                Write-RunLogLine "local binary is newer than release, skipping update"
                return
            }
        }

        Write-BootStepEnd "DONE"

        $zipAsset = $release.assets | Where-Object { $_.name -eq "dev-null.zip" } | Select-Object -First 1
        if (-not $zipAsset) {
            Write-RunLogLine "no dev-null.zip in release, skipping update"
            return
        }

        Write-BootStepStart "Downloading update"
        $tempZip = Join-Path ([System.IO.Path]::GetTempPath()) "dev-null-update.zip"
        $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "dev-null-update"
        Invoke-WebRequest -Uri $zipAsset.browser_download_url -OutFile $tempZip -TimeoutSec 120

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

# ── local mode: start headless server ────────────────────────────────────────

$script:serverProc = $null

function Stop-LocalServer {
    if ($script:serverProc -and -not $script:serverProc.HasExited) {
        Write-RunLogLine "stopping local server (PID $($script:serverProc.Id))"
        Stop-ProcessTree -RootPid $script:serverProc.Id
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

if ($Local) {
    # Generate a random password for auto-admin.
    $localPassword = -join ((1..32) | ForEach-Object { '{0:x}' -f (Get-Random -Maximum 16) })

    $serverArgs = @("--headless", "--port", $Port, "--password", $localPassword, "--data-dir", $root)
    if ($Term) { $serverArgs += "--term"; $serverArgs += $Term }

    Write-BootStepStart "Starting local server"
    $script:serverProc = Start-Process `
        -FilePath (Join-Path $root "dev-null-server.exe") `
        -ArgumentList $serverArgs `
        -WorkingDirectory $root `
        -RedirectStandardOutput (Join-Path $logsDir "local-server-stdout.log") `
        -RedirectStandardError  (Join-Path $logsDir "local-server-stderr.log") `
        -NoNewWindow -PassThru

    # Wait for SSH port to be listening.
    $deadline = (Get-Date).AddSeconds(15)
    $ready = $false
    while ((Get-Date) -lt $deadline) {
        if ($script:serverProc.HasExited) { break }
        $listener = Get-NetTCPConnection -LocalPort ([int]$Port) -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($listener) { $ready = $true; break }
        Start-Sleep -Milliseconds 100
    }
    if (-not $ready) {
        Write-BootStepEnd "FAIL"
        Stop-LocalServer
        Write-Error "Local server failed to start on port $Port within 15 seconds."
        exit 1
    }
    Write-BootStepEnd "DONE"
    Write-RunLogLine "local server ready on port $Port (PID $($script:serverProc.Id))"
}

# ── launch client ────────────────────────────────────────────────────────────

Push-Location $root
try {
    if ($NoGUI) {
        # Terminal mode: launch plain ssh instead of the GUI client binary.
        Write-RunLogLine "starting ssh client (host=$Host_ port=$Port local=$Local)"
        if ($Local) {
            $env:DEV_NULL_PASSWORD = $localPassword
            & ssh -p $Port -o SendEnv=DEV_NULL_PASSWORD -o StrictHostKeyChecking=no localhost
        } else {
            & ssh -p $Port $Host_
        }
        if ($LASTEXITCODE) { exit $LASTEXITCODE }
    } else {
        # GUI mode: build args and launch the graphical client binary.
        $clientArgs = @()
        if ($Local) {
            $clientArgs += "--host"; $clientArgs += "localhost"
            $clientArgs += "--port"; $clientArgs += $Port
            $clientArgs += "--password"; $clientArgs += $localPassword
        } else {
            if ($Host_) { $clientArgs += "--host"; $clientArgs += $Host_ }
            if ($Port)  { $clientArgs += "--port"; $clientArgs += $Port }
        }
        if ($Player)   { $clientArgs += "--player"; $clientArgs += $Player }
        if ($Term)     { $clientArgs += "--term";   $clientArgs += $Term }
        if ($Game)     { $clientArgs += "--game";   $clientArgs += $Game }
        if ($Resume)   { $clientArgs += "--resume"; $clientArgs += $Resume }
        $clientArgs += $positionals

        Write-RunLogLine "starting dev-null client (host=$Host_ port=$Port local=$Local)"
        & (Join-Path $root "dev-null-client.exe") @clientArgs
        if ($LASTEXITCODE) { exit $LASTEXITCODE }
    }
} finally {
    Pop-Location
    Write-RunLogLine "client process finished"
    if ($Local) { Stop-LocalServer }
    if ($null -eq $previousLogLevel)  { Remove-Item Env:DEV_NULL_LOG_LEVEL  -ErrorAction SilentlyContinue }
    else { $env:DEV_NULL_LOG_LEVEL = $previousLogLevel }
    if ($null -eq $previousTermWidth) { Remove-Item Env:DEV_NULL_TERM_WIDTH -ErrorAction SilentlyContinue }
    else { $env:DEV_NULL_TERM_WIDTH = $previousTermWidth }
}
