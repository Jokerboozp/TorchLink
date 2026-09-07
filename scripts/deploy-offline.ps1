[CmdletBinding()]
param(
    [string]$BundleDir = "",
    [switch]$SkipHashCheck,
    [switch]$SkipHealthCheck
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
$composePath = Join-Path $BundleDir "compose.yaml"
$offlineComposePath = Join-Path $BundleDir "compose.offline.yaml"
$archivePath = Join-Path $BundleDir "images.tar"
$hashPath = Join-Path $BundleDir "images.tar.sha256"
$ollamaArchive = Join-Path $BundleDir "ollama-data.tgz"
$modelHashPath = Join-Path $BundleDir "ollama-data.tgz.sha256"
$profilesPath = Join-Path $BundleDir "profiles.txt"
$ollamaVolumePath = Join-Path $BundleDir "ollama-volume.txt"
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
    if (Test-Path -LiteralPath $ollamaArchive -PathType Leaf) { $archives += ,@($ollamaArchive, $modelHashPath) }
    foreach ($pair in $archives) {
        if (-not (Test-Path -LiteralPath $pair[1] -PathType Leaf)) { throw "离线包缺少校验文件：$($pair[1])" }
        $expectedHash = ((Get-Content -LiteralPath $pair[1] -Encoding UTF8 | Select-Object -First 1) -split '\s+')[0].ToLowerInvariant()
        $actualHash = (Get-FileHash -LiteralPath $pair[0] -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($expectedHash -ne $actualHash) { throw "SHA256 校验失败：$($pair[0])" }
    }
    Write-Host "镜像和模型包 SHA256 校验通过。" -ForegroundColor Green
}

$composeArguments = @(
    "compose", "--project-name", "iot-platform",
    "--env-file", $envPath,
    "-f", $composePath,
    "-f", $offlineComposePath
)
if (Test-Path -LiteralPath $profilesPath -PathType Leaf) {
    foreach ($profile in @(Get-Content -LiteralPath $profilesPath -Encoding UTF8 | Where-Object { $_.Trim() })) {
        if ($profile.Trim() -notin @("harness", "gb26875")) { throw "离线包包含未知 profile：$profile" }
        $composeArguments += @("--profile", $profile.Trim())
    }
}
Invoke-Checked -Arguments ($composeArguments + @("config", "--quiet"))
Invoke-Checked -Arguments @("load", "-i", $archivePath)
$images = @(& docker @($composeArguments + @("config", "--images")))
if ($LASTEXITCODE -ne 0 -or $images.Count -eq 0) { throw "无法解析离线镜像清单。" }
foreach ($image in @($images | Sort-Object -Unique)) {
    & docker image inspect $image *> $null
    if ($LASTEXITCODE -ne 0) { throw "离线包缺少镜像：$image。请在有网机器重新打包。" }
}

$ollamaVolume = "iot-platform_ollama-data"
if (Test-Path -LiteralPath $ollamaVolumePath -PathType Leaf) {
    $recordedVolume = (Get-Content -LiteralPath $ollamaVolumePath -Encoding UTF8 | Select-Object -First 1).Trim()
    if ($recordedVolume -ne $ollamaVolume) { throw "模型卷名称与部署项目不一致，请重新打包：$recordedVolume" }
}
if (Test-Path -LiteralPath $ollamaArchive -PathType Leaf) {
    # 仅补齐缺失文件；重复运行以及上次中断后均可重试，不覆盖已有模型。
    Invoke-Checked -Arguments @("volume", "create", $ollamaVolume)
    Invoke-Checked -Arguments @(
        "run", "--rm", "--pull", "never",
        "--mount", "type=volume,source=$ollamaVolume,target=/dst",
        "--mount", "type=bind,source=$BundleDir,target=/backup,readonly",
        "alpine:3.22", "sh", "-ec", "mkdir -p /tmp/restore; tar -xzf /backup/ollama-data.tgz -C /tmp/restore; cp -an /tmp/restore/. /dst/"
    )
    Write-Host "Ollama 模型已恢复（保留已有文件）。" -ForegroundColor Green
}

Invoke-Checked -Arguments ($composeArguments + @("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "180"))
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
    Invoke-Checked -Arguments ($composeArguments + @("exec", "-T", "ollama", "ollama", "show", "nomic-embed-text"))
    $aiProvider = Get-EnvValue -Path $envPath -Key "IOT_AI_PROVIDER"
    if ($aiProvider -eq "ollama") {
        $chatModel = Get-EnvValue -Path $envPath -Key "IOT_AI_MODEL"
        if ([string]::IsNullOrWhiteSpace($chatModel)) { $chatModel = Get-EnvValue -Path $envPath -Key "IOT_OLLAMA_MODEL" }
        if ([string]::IsNullOrWhiteSpace($chatModel)) { $chatModel = "qwen3:1.7b" }
        Invoke-Checked -Arguments ($composeArguments + @("exec", "-T", "ollama", "ollama", "show", $chatModel))
    }
    Write-Host "平台健康检查与本地模型检查通过：$healthUrl" -ForegroundColor Green
    $checkWebPort = Get-EnvValue -Path $envPath -Key "IOT_WEB_PORT"
    if (-not $checkWebPort) { $checkWebPort = '8080' }
    $checkBackupPort = Get-EnvValue -Path $envPath -Key "IOT_BACKUP_HTTP_PORT"
    if (-not $checkBackupPort) { $checkBackupPort = '8092' }
    foreach ($url in @("http://127.0.0.1:$checkWebPort/", "http://127.0.0.1:$checkWebPort/health/ready", "http://127.0.0.1:$checkBackupPort/health/live")) {
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
