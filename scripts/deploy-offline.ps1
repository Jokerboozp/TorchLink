[CmdletBinding()]
param(
    [string]$BundleDir = "",
    [switch]$SkipHashCheck,
    [switch]$SkipHealthCheck,
    [ValidateSet("keep", "on", "off")][string]$Capacity = "keep"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($BundleDir)) {
    $BundleDir = Split-Path -Parent $scriptDir
}
$BundleDir = [System.IO.Path]::GetFullPath($BundleDir)

function Invoke-Checked {
    param([Parameter(Mandatory)][string[]]$Arguments)

    Write-Host ("> docker " + ($Arguments -join " ")) -ForegroundColor DarkGray
    & docker @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Docker 命令失败，退出码 $LASTEXITCODE：docker $($Arguments -join ' ')"
    }
}

function Get-EnvValue {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Key)

    $processValue = [Environment]::GetEnvironmentVariable($Key, 'Process')
    if ($null -ne $processValue) { return $processValue }
    $value = $null
    foreach ($line in @(Get-Content -LiteralPath $Path -Encoding UTF8)) {
        if ($line -match ('^\s*' + [Regex]::Escape($Key) + '\s*=\s*(.*)$')) {
            $value = $matches[1].Trim().Trim('"').Trim("'")
        }
    }
    return $value
}

if (-not (Test-Path -LiteralPath $BundleDir -PathType Container)) {
    throw "离线包目录不存在：$BundleDir"
}
$envPath = Join-Path $BundleDir ".env.offline"
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf) -and
    (Test-Path -LiteralPath (Join-Path $BundleDir '.env.offline.template') -PathType Leaf)) {
    & (Join-Path $scriptDir 'init-offline-env.ps1') -BundleDir $BundleDir
}
$composePath = Join-Path $BundleDir "compose.yaml"
$offlineComposePath = Join-Path $BundleDir "compose.offline.yaml"
$archivePath = Join-Path $BundleDir "images.tar"
$hashPath = Join-Path $BundleDir "images.tar.sha256"
$modelArchives = @(
    @{ Archive = "embedding-models.tgz"; Volume = "iot-platform_embedding-models"; Label = "知识库向量模型" },
    @{ Archive = "llm-models.tgz"; Volume = "iot-platform_llm-models"; Label = "私有化对话模型权重" }
)
$profilesPath = Join-Path $BundleDir "profiles.txt"
foreach ($path in @($envPath, $composePath, $offlineComposePath, $archivePath, $hashPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "离线包缺少文件：$path"
    }
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "找不到 docker 命令，请先安装 Docker Engine/Desktop。"
}
& docker info *> $null
if ($LASTEXITCODE -ne 0) {
    throw "Docker Engine 不可用，请先启动 Docker。"
}

if (-not $SkipHashCheck) {
    $archives = @(,@($archivePath, $hashPath))
    foreach ($model in $modelArchives) {
        $modelArchive = Join-Path $BundleDir $model.Archive
        if (Test-Path -LiteralPath $modelArchive -PathType Leaf) { $archives += ,@($modelArchive, "$modelArchive.sha256") }
    }
    foreach ($pair in $archives) {
        if (-not (Test-Path -LiteralPath $pair[1] -PathType Leaf)) { throw "离线包缺少校验文件：$($pair[1])" }
        $expectedHash = ((Get-Content -LiteralPath $pair[1] -Encoding UTF8 | Select-Object -First 1) -split '\s+')[0].ToLowerInvariant()
        $actualHash = (Get-FileHash -LiteralPath $pair[0] -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($expectedHash -ne $actualHash) { throw "SHA256 校验失败：$($pair[0])" }
    }
    Write-Host "镜像和模型包 SHA256 校验通过。" -ForegroundColor Green
}

# Images are saved for one CPU architecture; loading them elsewhere cannot run.
$manifestPath = Join-Path $BundleDir "manifest.json"
if (Test-Path -LiteralPath $manifestPath -PathType Leaf) {
    $bundleArch = [string](Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json).arch
    $hostArch = (& docker info --format '{{.Architecture}}' 2>$null | Out-String).Trim()
    $normalize = { param($a) switch -Regex ($a) { '^(x86_64|amd64)$' { 'amd64' } '^(aarch64|arm64)$' { 'arm64' } default { $a } } }
    if ($bundleArch -and $hostArch -and ((& $normalize $bundleArch) -ne (& $normalize $hostArch))) {
        throw "离线包 CPU 架构为 $bundleArch，本机 Docker 为 $hostArch；请在相同架构的机器上重新打包。"
    }
}

# Capacity-test module: deployed by default; an explicit off (here or earlier) is kept.
if ($Capacity -eq "keep") { $Capacity = if ((Get-EnvValue -Path $envPath -Key 'IOT_CAPACITY_MODULE') -eq 'off') { "off" } else { "on" } }
$capacityAction = if ($Capacity -eq "on") { "prepare" } else { "unprepare" }
& (Join-Path $scriptDir "capacity-module.ps1") $capacityAction -Mode offline -EnvFile $envPath
$capacityOn = (Get-EnvValue -Path $envPath -Key 'IOT_CAPACITY_MODULE') -eq 'on'
$composeArguments = @(
    "compose", "--project-name", "iot-platform",
    "--env-file", $envPath,
    "-f", $composePath,
    "-f", $offlineComposePath
)
if (Test-Path -LiteralPath $profilesPath -PathType Leaf) {
    foreach ($profile in @(Get-Content -LiteralPath $profilesPath -Encoding UTF8 | Where-Object { $_.Trim() })) {
        if ($profile.Trim() -notin @("harness", "gb26875", "video", "llm")) { throw "离线包包含未知 profile：$profile" }
        # The media image is always packaged; IOT_VIDEO_MODULE=off keeps it undeployed.
        if ($profile.Trim() -eq 'video' -and (Get-EnvValue -Path $envPath -Key 'IOT_VIDEO_MODULE') -eq 'off') { continue }
        # The private chat model needs an NVIDIA GPU; IOT_PRIVATE_LLM=off keeps it undeployed.
        if ($profile.Trim() -eq 'llm' -and (Get-EnvValue -Path $envPath -Key 'IOT_PRIVATE_LLM') -eq 'off') { continue }
        $composeArguments += @("--profile", $profile.Trim())
    }
}
if ($capacityOn) { $composeArguments += @("--profile", "capacity") }
Invoke-Checked -Arguments ($composeArguments + @("config", "--quiet"))
Invoke-Checked -Arguments @("load", "-i", $archivePath)
$images = @(& docker @($composeArguments + @("config", "--images")))
if ($LASTEXITCODE -ne 0 -or $images.Count -eq 0) { throw "无法解析离线镜像清单。" }
foreach ($image in @($images | Sort-Object -Unique)) {
    & docker image inspect $image *> $null
    if ($LASTEXITCODE -ne 0) { throw "离线包缺少镜像：$image。请在有网机器重新打包。" }
}

foreach ($model in $modelArchives) {
    if (-not (Test-Path -LiteralPath (Join-Path $BundleDir $model.Archive) -PathType Leaf)) { continue }
    # 仅补齐缺失文件；重复运行以及上次中断后均可重试，不覆盖已有模型。
    Invoke-Checked -Arguments @("volume", "create", $model.Volume)
    Invoke-Checked -Arguments @(
        "run", "--rm", "--pull", "never",
        "--mount", "type=volume,source=$($model.Volume),target=/dst",
        "--mount", "type=bind,source=$BundleDir,target=/backup,readonly",
        "alpine:3.22", "sh", "/backup/scripts/lib/restore-volume-archive.sh", "/backup/$($model.Archive)", "/dst"
    )
    Write-Host "$($model.Label)已恢复（保留已有文件）。" -ForegroundColor Green
}

# The CPU embedding service loads and warms up its model on first start.
Invoke-Checked -Arguments ($composeArguments + @("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "900"))
# up does not remove profile services; drop a capacity service left from an earlier choice.
if (-not $capacityOn) {
    # Windows PowerShell turns redirected native stderr into errors under "Stop".
    & { $ErrorActionPreference = "Continue"; & docker @($composeArguments + @("--profile", "capacity", "rm", "-sf", "capacity")) *> $null }
}
Invoke-Checked -Arguments ($composeArguments + @("ps"))

if (-not $SkipHealthCheck) {
    $apiPort = Get-EnvValue -Path $envPath -Key "IOT_API_PORT"
    if ([string]::IsNullOrWhiteSpace($apiPort)) { $apiPort = "8081" }
    $healthUrl = "http://127.0.0.1:$apiPort/health/ready"
    $healthy = $false
    for ($i = 0; $i -lt 60; $i++) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 3
            if ($response.StatusCode -eq 200) {
                $healthy = $true
                break
            }
        } catch {
            # Compose health/dependency checks may still be in progress.
        }
        Start-Sleep -Seconds 2
    }
    if (-not $healthy) {
        & docker @($composeArguments + @("logs", "--tail=100", "platform-api", "postgres", "redpanda", "emqx"))
        throw "平台健康检查失败：$healthUrl"
    }
    Invoke-Checked -Arguments ($composeArguments + @("exec", "-T", "embedding", "curl", "-fsS", "-o", "/dev/null", "http://127.0.0.1:80/health"))
    Write-Host "平台健康检查与知识库向量服务检查通过：$healthUrl" -ForegroundColor Green
    $checkWebPort = Get-EnvValue -Path $envPath -Key "IOT_WEB_PORT"
    if (-not $checkWebPort) { $checkWebPort = '8080' }
    $checkBackupPort = Get-EnvValue -Path $envPath -Key "IOT_BACKUP_HTTP_PORT"
    if (-not $checkBackupPort) { $checkBackupPort = '8092' }
    foreach ($url in @("http://127.0.0.1:$checkWebPort/", "http://127.0.0.1:$checkWebPort/health/ready", "http://127.0.0.1:$checkBackupPort/health/ready")) {
        $ready = $false
        for ($i = 0; $i -lt 60; $i++) {
            try {
                if ((Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 3).StatusCode -eq 200) { $ready = $true; break }
            } catch { }
            Start-Sleep -Seconds 2
        }
        if (-not $ready) { throw "服务健康检查失败：$url" }
        Write-Host "健康检查通过：$url"
    }
}

$webPort = Get-EnvValue -Path $envPath -Key "IOT_WEB_PORT"
if ([string]::IsNullOrWhiteSpace($webPort)) { $webPort = "8080" }
Write-Host "离线部署完成。Web 地址：http://127.0.0.1:$webPort" -ForegroundColor Green
Write-Host "管理员凭据：$(Join-Path $BundleDir 'OFFLINE-CREDENTIALS.txt')"
if ((Test-Path -LiteralPath $profilesPath -PathType Leaf) -and (@(Get-Content -LiteralPath $profilesPath -Encoding UTF8 | ForEach-Object { $_.Trim() }) -contains 'video') -and (Get-EnvValue -Path $envPath -Key 'IOT_VIDEO_MODULE') -ne 'off') {
    if ([string]::IsNullOrWhiteSpace((Get-EnvValue -Path $envPath -Key 'IOT_VIDEO_RTC_EXTERN_IP'))) {
        Write-Warning '已部署摄像头直播媒体服务，但未设置 IOT_VIDEO_RTC_EXTERN_IP；浏览器将使用 HLS。可运行 scripts\video-module.ps1 enable -Mode offline -EnvFile .env.offline -RtcIp <服务器 IP>'
    }
    Write-Host '摄像头直播默认启用：为摄像头配置 ONVIF / RTSP / GB28181 接入后即可观看；GB28181 设备需能访问 SIP 端口（默认 5060）与 RTP 端口范围（默认 30000-30063）。'
}
