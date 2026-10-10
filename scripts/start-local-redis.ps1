param(
    [string]$RedisExecutable = 'C:\Program Files\Redis\redis-server.exe'
)

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot
$dataDirectory = Join-Path $workspace 'bin\redis-native'
$redisCli = Join-Path (Split-Path -Parent $RedisExecutable) 'redis-cli.exe'
if (!(Test-Path -LiteralPath $RedisExecutable) -or !(Test-Path -LiteralPath $redisCli)) {
    throw 'Windows Redis executables are missing. Specify -RedisExecutable with the installed path.'
}

$listeners = @(Get-NetTCPConnection -LocalPort 6380 -State Listen -ErrorAction SilentlyContinue)
if ($listeners.Count -gt 0) {
    foreach ($ownerId in ($listeners | Select-Object -ExpandProperty OwningProcess -Unique)) {
        if ((Get-Process -Id $ownerId).Path -ne $RedisExecutable) {
            throw 'Port 6380 is occupied by a different program.'
        }
    }
} else {
    New-Item -ItemType Directory -Path $dataDirectory -Force | Out-Null
    $configuration = @'
bind 127.0.0.1
protected-mode yes
port 6380
daemonize no
dir ./
logfile redis.log
dbfilename dump.rdb
save 60 1
appendonly yes
appendfsync everysec
'@
    [IO.File]::WriteAllText((Join-Path $dataDirectory 'redis.conf'), $configuration, [Text.UTF8Encoding]::new($false))
    # Windows Redis uses Cygwin paths. A relative config resolves from this working directory.
    Start-Process -FilePath $RedisExecutable -ArgumentList 'redis.conf' -WorkingDirectory $dataDirectory -WindowStyle Hidden | Out-Null
    for ($attempt = 0; $attempt -lt 20; $attempt++) {
        if (Get-NetTCPConnection -LocalPort 6380 -State Listen -ErrorAction SilentlyContinue) { break }
        Start-Sleep -Milliseconds 100
    }
}

$probeKey = 'jizhang:startup-probe:' + [guid]::NewGuid().ToString('N')
$ping = & $redisCli -h 127.0.0.1 -p 6380 PING
$write = & $redisCli -h 127.0.0.1 -p 6380 SET $probeKey ready EX 30
$null = & $redisCli -h 127.0.0.1 -p 6380 DEL $probeKey
$snapshot = ''
for ($attempt = 0; $attempt -lt 20; $attempt++) {
    $snapshot = & $redisCli -h 127.0.0.1 -p 6380 SAVE
    if ($snapshot -eq 'OK' -or $snapshot -notmatch 'Background save already in progress') { break }
    Start-Sleep -Milliseconds 100
}
if ($ping -ne 'PONG' -or $write -ne 'OK' -or $snapshot -ne 'OK') {
    throw 'Native Redis connectivity, session writes or snapshot persistence failed. Check bin/redis-native/redis.log.'
}
Write-Output 'Windows Redis ready at 127.0.0.1:6380; writes and persistence verified.'
