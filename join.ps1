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
    Write-Host "Error: NS not set. Use the invite command from a null-space server." -ForegroundColor Red
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
$sshPort = ($bytes[0] -shl 8) + $bytes[1]

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
    $pPort = ($bytes[10] -shl 8) + $bytes[11]
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

$sshOpts = @(
    "-t",
    "-o", "ConnectTimeout=5",
    "-o", "StrictHostKeyChecking=no",
    "-o", "UserKnownHostsFile=/dev/null"
)

foreach ($ep in $endpoints) {
    Write-Host "Trying $($ep.Host):$($ep.Port) ..." -ForegroundColor DarkGray
    ssh @sshOpts -p $ep.Port "${name}@$($ep.Host)"
    if ($LASTEXITCODE -eq 0) { exit 0 }
}

Write-Host "Could not connect to any endpoint." -ForegroundColor Red
exit 1
