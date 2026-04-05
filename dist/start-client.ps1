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
$Terminal = $false
$Term     = ""

$positionals = @()
for ($i = 0; $i -lt $CliArgs.Count; $i++) {
    $arg = $CliArgs[$i]
    switch -Regex ($arg) {
        '^--?(no-?update|skip-?update)$' { $NoUpdate = $true; continue }
        '^--?local$'         { $Local = $true; continue }
        '^--?terminal$'      { $Terminal = $true; continue }
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
    try { return $Host.UI.RawUI.BufferSize.Width } catch { return 80 }
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

        if ($localVersion -eq "") {
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

# ── build client args ────────────────────────────────────────────────────────

$clientArgs = @()
if ($Local)    { $clientArgs += "--local"; $clientArgs += "--data-dir"; $clientArgs += $root }
if ($Terminal) { $clientArgs += "--terminal" }
if ($Host_ -and -not $Local) { $clientArgs += "--host"; $clientArgs += $Host_ }
if ($Port)     { $clientArgs += "--port";   $clientArgs += $Port }
if ($Player)   { $clientArgs += "--player"; $clientArgs += $Player }
if ($Term)     { $clientArgs += "--term";   $clientArgs += $Term }
$clientArgs += $positionals

# ── launch client ────────────────────────────────────────────────────────────

Push-Location $root
try {
    Write-RunLogLine "starting dev-null client (host=$Host_ port=$Port local=$Local terminal=$Terminal)"
    & (Join-Path $root "dev-null-client.exe") @clientArgs
    if ($LASTEXITCODE) { exit $LASTEXITCODE }
} finally {
    Pop-Location
    Write-RunLogLine "client process finished"
    if ($null -eq $previousLogLevel)  { Remove-Item Env:DEV_NULL_LOG_LEVEL  -ErrorAction SilentlyContinue }
    else { $env:DEV_NULL_LOG_LEVEL = $previousLogLevel }
    if ($null -eq $previousTermWidth) { Remove-Item Env:DEV_NULL_TERM_WIDTH -ErrorAction SilentlyContinue }
    else { $env:DEV_NULL_TERM_WIDTH = $previousTermWidth }
}
