<#
.SYNOPSIS
  jizhang 一键部署（可重复执行）：比对 -> 同步 -> 重建 -> 验证

.DESCRIPTION
  以"本地哈希清单 vs 服务器实际文件"的差异驱动，因此重复执行是幂等的：
  没有差异就什么都不动，有差异就只同步变了的文件，再按改动范围选择性重建容器。

  硬规则：本地 .env 永不入清单、永不上传（服务器有自己的一份，mode 600）。

.EXAMPLE
  pwsh -ExecutionPolicy Bypass -File .agents/skills/deploy/scripts/deploy.ps1
  pwsh -ExecutionPolicy Bypass -File .agents/skills/deploy/scripts/deploy.ps1 -Smoke
  pwsh -ExecutionPolicy Bypass -File .agents/skills/deploy/scripts/deploy.ps1 -Force

.PARAMETER Force
  无视差异，强制重建 web + api（用于镜像层缓存可疑、或只想重跑构建）。

.PARAMETER Smoke
  验证通过后由生产管理员创建临时账本账号，跑 frontend/e2e/prod_smoke.mjs。

.PARAMETER Purge
  同步完成后删除"服务器上有、本地已删/不入清单"的孤儿文件。
  不加此开关时只报警不删除——被删的源文件会经 `COPY . .` 继续进 Docker 构建上下文。

.PARAMETER Key
  指定 SSH 私钥；省略时按候选路径顺序选择第一个存在的文件。
#>
[CmdletBinding()]
param(
  [string]$Server = 'ubuntu@124.221.104.237',
  [string]$Key    = '',
  [string]$Remote = '/srv/jizhang',
  [string]$Site   = 'https://jizhang.zhanghongyun.cn',
  [switch]$Force,
  [switch]$Smoke,
  [switch]$Purge
)

$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }

# ---------- 定位仓库根（向上找 docker-compose.yml）----------
$probe = $PSScriptRoot
while ($probe -and -not (Test-Path (Join-Path $probe 'docker-compose.yml'))) {
  $probe = Split-Path -Parent $probe
}
if (-not $probe) { throw "无法定位仓库根：从 $PSScriptRoot 向上没找到 docker-compose.yml" }
$RepoRoot = $probe

# ---------- 选择 SSH 私钥（显式 -Key 优先，否则查找候选文件）----------
$KeyCandidates = @(
  'C:\Users\hongy\AppData\Local\Temp\opencode\id_jizhang',
  'C:\Users\Administrator\Documents\1'
)
if ([string]::IsNullOrWhiteSpace($Key)) {
  foreach ($candidate in $KeyCandidates) {
    if (Test-Path -LiteralPath $candidate -PathType Leaf) {
      $Key = $candidate
      break
    }
  }
  if ([string]::IsNullOrWhiteSpace($Key)) {
    throw "未找到 SSH 私钥，已检查: $($KeyCandidates -join ', ') （用 -Key 指定）"
  }
}
if (-not (Test-Path -LiteralPath $Key -PathType Leaf)) { throw "找不到 SSH 私钥: $Key （用 -Key 指定）" }
$SshOpts = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', '-i', $Key)
$Tmp = [System.IO.Path]::GetTempPath()

# ---------- 原生命令执行 ----------
# PowerShell 的 2>&1 会把远端 stderr 变成 ErrorRecord，在 $ErrorActionPreference='Stop'
# 下会把非致命告警放大成终止错误。所以把 stderr 落到临时文件，不在管道里生成 ErrorRecord。
function Invoke-Native([string]$Exe, [string[]]$ArgList) {
  $errFile = [System.IO.Path]::GetTempFileName()
  $out = & $Exe @ArgList 2>$errFile
  $code = $LASTEXITCODE
  # 剥掉 ANSI 转义：docker compose 会往 stdout 里塞颜色码，混进日志既难读也会干扰后续匹配
  $strip = { param($s) "$s" -replace '\x1b\[[0-9;]*[A-Za-z]', '' }
  $lines = @()
  if ($null -ne $out) { $lines += @($out | ForEach-Object { & $strip "$_" }) }
  if (Test-Path $errFile) {
    $e = Get-Content $errFile -Raw -Encoding utf8 -ErrorAction SilentlyContinue
    if ($e) { $lines += @(($e -split "`r?`n") | Where-Object { $_ -ne '' } | ForEach-Object { & $strip $_ }) }
  }
  Remove-Item $errFile -Force -ErrorAction SilentlyContinue
  return [PSCustomObject]@{ ExitCode = $code; Output = $lines }
}

function Invoke-Ssh([string]$Cmd) {
  $r = Invoke-Native 'ssh' ($SshOpts + @($Server, $Cmd))
  if ($r.ExitCode -ne 0) {
    throw "ssh 退出码 $($r.ExitCode)`n命令: $Cmd`n$($r.Output -join "`n")"
  }
  return $r.Output
}

# 非抛出版本：验证不过是"结果"不是"异常"——抛出会把整段远端输出塞进一条异常消息，
# 既丢换行又盖 ANSI 红色，读不出到底是哪一项 FAIL。
function Invoke-SshResult([string]$Cmd) {
  $r = Invoke-Native 'ssh' ($SshOpts + @($Server, $Cmd))
  return [PSCustomObject]@{ ExitCode = $r.ExitCode; Output = $r.Output }
}

function Send-ToServer([string]$Local, [string]$RemotePath) {
  $r = Invoke-Native 'scp' ($SshOpts + @($Local, "${Server}:$RemotePath"))
  if ($r.ExitCode -ne 0) {
    throw "scp 失败 ($($r.ExitCode)): $Local -> ${Server}:$RemotePath`n$($r.Output -join "`n")"
  }
}

# ---------- 阶段 1/4：生成本地哈希清单 ----------
$targets = @(
  'frontend/src', 'frontend/public', 'frontend/index.html',
  'frontend/package.json', 'frontend/package-lock.json',
  'frontend/tsconfig.json', 'frontend/vite.config.ts',
  'frontend/Dockerfile', 'frontend/nginx.conf', 'frontend/.dockerignore',
  'frontend/e2e',
  'backend', 'docker', 'docker-compose.yml', 'docker-compose.wealth.yml', 'collector', 'openspec', 'docs', 'README.md',
  '.env.example', 'generate-favicon.mjs'
)
# 构建产物与可再生文件不入清单：
#   dist/tsbuildinfo 是编译产物；shots/downloads/preview 是 e2e 副产物（preview 84MB 的截图）
# 漏掉 package-lock.json 会让服务器拿到新 package.json + 旧 lock，npm ci 直接报 out of sync
# 这份模式针对正斜杠相对路径，同时由远端 diff.sh 用 grep -E 复用——改一处等于两侧都改
$exclude = 'node_modules|/dist/|/bin/|/vendor/|\.git/|/coverage/|/shots/|/downloads/|/preview/|^collector/evidence/|tsbuildinfo|(^|/)server(\.exe)?$'

$rows = @()
foreach ($t in $targets) {
  $full = Join-Path $RepoRoot $t
  if (-not (Test-Path $full)) { continue }
  $items = if (Test-Path $full -PathType Container) { Get-ChildItem $full -Recurse -File } else { Get-Item $full }
  foreach ($i in $items) {
    $rel = $i.FullName.Substring($RepoRoot.Length + 1).Replace('\', '/')
    if ($rel -match $exclude) { continue }
    $rows += [PSCustomObject]@{ Rel = $rel; Md5 = (Get-FileHash $i.FullName -Algorithm MD5).Hash.ToLower() }
  }
}
$rows = @($rows | Sort-Object Rel)

if ($rows.Count -eq 0) { throw "哈希清单为空：targets=$($targets -join ',') 在 $RepoRoot 下都没命中" }

# 扫描根目录：远端据此找出"本地已删、服务器还留着"的孤儿文件。
# 不清理的话，被删的源文件会通过 `COPY . .` 继续进 Docker 构建上下文。
$roots = @($targets | Where-Object { Test-Path (Join-Path $RepoRoot $_) -PathType Container })

function Write-Lf([string]$Path, [string]$Text) {
  [System.IO.File]::WriteAllText($Path, ($Text -replace "`r`n", "`n"), [System.Text.UTF8Encoding]::new($false))
}

$manifest = Join-Path $Tmp 'jizhang_hashes.txt'
# 强制 LF + 无 BOM：Set-Content/WriteAllLines 默认写 CRLF，远端 read 会把 \r 吞进路径，
# 表现为每个文件都"服务器缺失"。UTF-8(无BOM) 对纯 ASCII 的哈希行与路径完全等价。
$lines = $rows | ForEach-Object { '{0}  {1}' -f $_.Md5, $_.Rel }
Write-Lf $manifest ((($lines -join "`n") + "`n"))
Write-Lf (Join-Path $Tmp 'jizhang_exclude.txt') ($exclude + "`n")
Write-Lf (Join-Path $Tmp 'jizhang_roots.txt') ((($roots -join "`n") + "`n"))

Write-Host "[1/4] 本地清单 $($rows.Count) 个文件 / $($roots.Count) 个扫描根 -> 比对 $Server$Remote"
Send-ToServer $manifest '/tmp/jizhang_hashes.txt'
Send-ToServer (Join-Path $Tmp 'jizhang_exclude.txt') '/tmp/jizhang_exclude.txt'
Send-ToServer (Join-Path $Tmp 'jizhang_roots.txt') '/tmp/jizhang_roots.txt'
Send-ToServer (Join-Path $PSScriptRoot 'diff.sh') '/tmp/jizhang_diff.sh'
(Invoke-Ssh "tr -d '\r' < /tmp/jizhang_diff.sh > /tmp/d.sh && bash /tmp/d.sh $Remote") |
  ForEach-Object { Write-Host "      $_" }

function Get-RemoteList([string]$Name, [string]$Local) {
  $r = Invoke-Native 'scp' ($SshOpts + @("${Server}:/tmp/$Name", $Local))
  if ($r.ExitCode -ne 0) { throw "拉取 $Name 失败: $($r.Output -join "`n")" }
  return @(Get-Content $Local -ErrorAction SilentlyContinue)
}

$changeFile = Join-Path $Tmp 'jizhang_changes.txt'
$changes = @(Get-RemoteList 'jizhang_changes.txt' $changeFile |
  Where-Object { $_ -match '^[MA] ' } | ForEach-Object { ($_ -split '\s+', 2)[1] })

$orphanFile = Join-Path $Tmp 'jizhang_orphans.txt'
$orphans = @(Get-RemoteList 'jizhang_orphans.txt' $orphanFile | Where-Object { $_ -match '\S' })

# ---------- 阶段 2/4：同步 ----------
if ($orphans.Count -gt 0) {
  Write-Host "      服务器上有 $($orphans.Count) 个孤儿文件（本地已删/不入清单）:" -ForegroundColor Yellow
  $orphans | Select-Object -First 20 | ForEach-Object { Write-Host "        D $_" -ForegroundColor Yellow }
  if ($orphans.Count -gt 20) { Write-Host "        ... 其余 $($orphans.Count - 20) 个" -ForegroundColor Yellow }
}

if ($changes.Count -eq 0) {
  Write-Host '[2/4] 无差异，无需同步（本次为幂等空跑）。'
}
else {
  Write-Host "[2/4] 同步 $($changes.Count) 个文件"
  foreach ($rel in $changes) {
    $local = Join-Path $RepoRoot ($rel -replace '/', [string][IO.Path]::DirectorySeparatorChar)
    if (-not (Test-Path $local)) {
      Write-Warning "清单标为待同步但本地不存在，跳过: $rel"
      continue
    }
    $dir = $rel -replace '/[^/]+$', ''
    if ($dir -and $dir -ne $rel) { Invoke-Ssh "mkdir -p '$Remote/$dir'" | Out-Null }
    Send-ToServer $local "$Remote/$rel"
    Write-Host "      + $rel"
  }
}

if ($Purge) {
  # 只删 diff.sh 判定为孤儿的文件，且拒绝绝对路径与 .. （防止清单被污染后越界删除）
  $safe = @($orphans | Where-Object { $_ -notmatch '^/' -and $_ -notmatch '(^|/)\.\.(/|$)' -and $_ -ne '' })
  if ($safe.Count -eq 0) {
    Write-Host '      [-Purge] 无可清理的孤儿文件。'
  }
  else {
    $listFile = Join-Path $Tmp 'jizhang_purge.txt'
    Write-Lf $listFile ((($safe -join "`n") + "`n"))
    Send-ToServer $listFile '/tmp/jizhang_purge.txt'
    Invoke-Ssh "cd $Remote && tr -d '\r' < /tmp/jizhang_purge.txt | sed 's#^#$Remote/#' | xargs -d '\n' -r rm -f --" | Out-Null
    Write-Host "      [-Purge] 已删除 $($safe.Count) 个孤儿文件" -ForegroundColor Yellow
  }
}

# ---------- 阶段 3/4：按改动范围重建 ----------
$needWeb = [bool]($changes | Where-Object { $_ -match '^(frontend/|docker/|docker-compose\.yml$)' })
$needApi = [bool]($changes | Where-Object { $_ -match '^(backend/|docker/|docker-compose(\.wealth)?\.yml$)' })
$needCollector = [bool]($changes | Where-Object { $_ -match '^(collector/(Dockerfile|package(-lock)?\.json|products\.json|collector\.mjs|server\.mjs|smoke\.mjs|challenge-diagnostic\.mjs|qualification\.mjs)$|docker-compose\.wealth\.yml$|docker/playwright-seccomp\.json$)' })
$services = @()
if ($needWeb) { $services += 'web' }
if ($needApi) { $services += 'api' }
if ($Force) { $services = @('web', 'api') }
if ($needCollector) { $services += 'wealth-collector' }

if ($services.Count -eq 0) {
  Write-Host '[3/4] 改动不涉及构建产物（如 openspec/README），跳过重建。'
}
else {
  Write-Host "[3/4] 构建、迁移保护与健康切换 $($services -join ' ')"
  # --wait 阻塞到 healthcheck 通过；不等的话紧接着的 verify 会看到 running/starting 而误判 FAIL
  $latestMigration = (Get-ChildItem -LiteralPath (Join-Path $RepoRoot 'backend\migrations') -Filter '*.up.sql' | Sort-Object Name | Select-Object -Last 1).Name -replace '_.*$', ''
  $latestMigration = [int]$latestMigration
  $releaseName = 'jizhang_release_' + [guid]::NewGuid().ToString('N') + '.sh'
  $releaseLocal = Join-Path $Tmp $releaseName
  Write-Lf $releaseLocal (Get-Content -LiteralPath (Join-Path $PSScriptRoot 'release.sh') -Raw)
  Send-ToServer $releaseLocal ("/tmp/" + $releaseName)
  $build = Invoke-Ssh "bash /tmp/$releaseName '$Remote' $latestMigration $($services -join ' ')"
  $build | Where-Object { "$_" -match '^RELEASE:' } | ForEach-Object { Write-Host "      $_" }
  $build | Select-Object -Last 8 | ForEach-Object { Write-Host "      $_" }
}

# ---------- 阶段 4/4：验证 ----------
Write-Host "[4/4] 验证"
Send-ToServer (Join-Path $PSScriptRoot 'verify.sh') '/tmp/jizhang_verify.sh'
$verify = Invoke-SshResult "tr -d '\r' < /tmp/jizhang_verify.sh > /tmp/v.sh && bash /tmp/v.sh $Remote '$Site'"
$verify.Output | ForEach-Object { Write-Host "      $_" }
$ok = ($verify.ExitCode -eq 0) -and
  [bool]($verify.Output | Where-Object { "$_" -match 'VERIFY: ALL_GREEN' })
if (-not $ok -and $verify.ExitCode -ne 0) {
  Write-Warning "verify.sh 退出码 $($verify.ExitCode)"
}

# ---------- 可选：生产冒烟 ----------
if ($Smoke) {
  Write-Host '[5/5] 生产冒烟（prod_smoke.mjs）'
  $pw = ((Invoke-Ssh "grep '^ADMIN_PASSWORD=' $Remote/.env | cut -d= -f2") -join '').Trim()
  if (-not $pw) { Write-Warning '读不到 ADMIN_PASSWORD，跳过冒烟'; $ok = $false }
  else {
    $env:JZ_PASS = $pw
    try {
      $out = Invoke-Native 'node' @((Join-Path $RepoRoot 'frontend\e2e\prod_smoke.mjs'))
      # 失败时全量输出（要看清哪一项没过），成功时只留末尾几行
      if ($out.ExitCode -ne 0) { $out.Output | ForEach-Object { Write-Host "      $_" -ForegroundColor Red } }
      else { $out.Output | Select-Object -Last 4 | ForEach-Object { Write-Host "      $_" } }
      if ($out.ExitCode -ne 0) { Write-Warning "prod_smoke 退出码 $($out.ExitCode)"; $ok = $false }
    }
    finally { Remove-Item Env:\JZ_PASS -ErrorAction SilentlyContinue }
  }
}

Write-Host ''
if ($ok) { Write-Host 'DEPLOY: ALL_GREEN' } else { Write-Host 'DEPLOY: FAILED'; exit 1 }
