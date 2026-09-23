[CmdletBinding()]
param(
    [string]$BundleDir = "",
    [switch]$SkipHashCheck,
    [switch]$SkipHealthCheck
)

# 执行当前脚本步骤。
Set-StrictMode -Version Latest
# 执行当前脚本步骤。
$ErrorActionPreference = "Stop"

# 执行当前脚本步骤。
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
# 判断条件后执行对应操作。
if ([string]::IsNullOrWhiteSpace($BundleDir)) {
    # 执行当前脚本步骤。
    $BundleDir = Split-Path -Parent $scriptDir
# 结束当前控制块。
}
# 执行当前脚本步骤。
$BundleDir = [System.IO.Path]::GetFullPath($BundleDir)

# 定义可复用的脚本函数。
function Invoke-Checked {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string[]]$Arguments)

    # 执行当前脚本步骤。
    Write-Host ("> docker " + ($Arguments -join " ")) -ForegroundColor DarkGray
    # 执行当前脚本步骤。
    & docker @Arguments
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0) {
        # 执行当前脚本步骤。
        throw "Docker 命令失败，退出码 $LASTEXITCODE：docker $($Arguments -join ' ')"
    # 结束当前控制块。
    }
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Get-EnvValue {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Key)

    # 执行当前脚本步骤。
    $processValue = [Environment]::GetEnvironmentVariable($Key, 'Process')
    # 判断条件后执行对应操作。
    if ($null -ne $processValue) { return $processValue }
    # 执行当前脚本步骤。
    $value = $null
    # 遍历数据并执行循环体。
    foreach ($line in @(Get-Content -LiteralPath $Path -Encoding UTF8)) {
        # 判断条件后执行对应操作。
        if ($line -match ('^\s*' + [Regex]::Escape($Key) + '\s*=\s*(.*)$')) {
            # 执行当前脚本步骤。
            $value = $matches[1].Trim().Trim('"').Trim("'")
        # 结束当前控制块。
        }
    # 结束当前控制块。
    }
    # 返回结果或结束当前脚本。
    return $value
# 结束当前控制块。
}

# 判断条件后执行对应操作。
if (-not (Test-Path -LiteralPath $BundleDir -PathType Container)) {
    # 执行当前脚本步骤。
    throw "离线包目录不存在：$BundleDir"
# 结束当前控制块。
}
# 执行当前脚本步骤。
$envPath = Join-Path $BundleDir ".env.offline"
# 执行当前脚本步骤。
$composePath = Join-Path $BundleDir "compose.yaml"
# 执行当前脚本步骤。
$offlineComposePath = Join-Path $BundleDir "compose.offline.yaml"
# 执行当前脚本步骤。
$archivePath = Join-Path $BundleDir "images.tar"
# 执行当前脚本步骤。
$hashPath = Join-Path $BundleDir "images.tar.sha256"
# 执行当前脚本步骤。
$ollamaArchive = Join-Path $BundleDir "ollama-data.tgz"
# 执行当前脚本步骤。
$modelHashPath = Join-Path $BundleDir "ollama-data.tgz.sha256"
# 执行当前脚本步骤。
$profilesPath = Join-Path $BundleDir "profiles.txt"
# 执行当前脚本步骤。
$ollamaVolumePath = Join-Path $BundleDir "ollama-volume.txt"
# 遍历数据并执行循环体。
foreach ($path in @($envPath, $composePath, $offlineComposePath, $archivePath, $hashPath)) {
    # 判断条件后执行对应操作。
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        # 执行当前脚本步骤。
        throw "离线包缺少文件：$path"
    # 结束当前控制块。
    }
# 结束当前控制块。
}

# 判断条件后执行对应操作。
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    # 执行当前脚本步骤。
    throw "找不到 docker 命令，请先安装 Docker Engine/Desktop。"
# 结束当前控制块。
}
# 执行当前脚本步骤。
& docker info *> $null
# 判断条件后执行对应操作。
if ($LASTEXITCODE -ne 0) {
    # 执行当前脚本步骤。
    throw "Docker Engine 不可用，请先启动 Docker。"
# 结束当前控制块。
}

# 判断条件后执行对应操作。
if (-not $SkipHashCheck) {
    # 执行当前脚本步骤。
    $archives = @(,@($archivePath, $hashPath))
    # 判断条件后执行对应操作。
    if (Test-Path -LiteralPath $ollamaArchive -PathType Leaf) { $archives += ,@($ollamaArchive, $modelHashPath) }
    # 遍历数据并执行循环体。
    foreach ($pair in $archives) {
        # 判断条件后执行对应操作。
        if (-not (Test-Path -LiteralPath $pair[1] -PathType Leaf)) { throw "离线包缺少校验文件：$($pair[1])" }
        # 执行当前脚本步骤。
        $expectedHash = ((Get-Content -LiteralPath $pair[1] -Encoding UTF8 | Select-Object -First 1) -split '\s+')[0].ToLowerInvariant()
        # 执行当前脚本步骤。
        $actualHash = (Get-FileHash -LiteralPath $pair[0] -Algorithm SHA256).Hash.ToLowerInvariant()
        # 判断条件后执行对应操作。
        if ($expectedHash -ne $actualHash) { throw "SHA256 校验失败：$($pair[0])" }
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    Write-Host "镜像和模型包 SHA256 校验通过。" -ForegroundColor Green
# 结束当前控制块。
}

# 执行当前脚本步骤。
$composeArguments = @(
    # 执行当前脚本步骤。
    "compose", "--project-name", "iot-platform",
    "--env-file", $envPath,
    "-f", $composePath,
    "-f", $offlineComposePath
# 执行当前脚本步骤。
)
# 判断条件后执行对应操作。
if (Test-Path -LiteralPath $profilesPath -PathType Leaf) {
    # 遍历数据并执行循环体。
    foreach ($profile in @(Get-Content -LiteralPath $profilesPath -Encoding UTF8 | Where-Object { $_.Trim() })) {
        # 判断条件后执行对应操作。
        if ($profile.Trim() -notin @("harness", "gb26875")) { throw "离线包包含未知 profile：$profile" }
        # 执行当前脚本步骤。
        $composeArguments += @("--profile", $profile.Trim())
    # 结束当前控制块。
    }
# 结束当前控制块。
}
# 执行当前脚本步骤。
Invoke-Checked -Arguments ($composeArguments + @("config", "--quiet"))
# 执行当前脚本步骤。
Invoke-Checked -Arguments @("load", "-i", $archivePath)
# 执行当前脚本步骤。
$images = @(& docker @($composeArguments + @("config", "--images")))
# 判断条件后执行对应操作。
if ($LASTEXITCODE -ne 0 -or $images.Count -eq 0) { throw "无法解析离线镜像清单。" }
# 遍历数据并执行循环体。
foreach ($image in @($images | Sort-Object -Unique)) {
    # 执行当前脚本步骤。
    & docker image inspect $image *> $null
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0) { throw "离线包缺少镜像：$image。请在有网机器重新打包。" }
# 结束当前控制块。
}

# 执行当前脚本步骤。
$ollamaVolume = "iot-platform_ollama-data"
# 判断条件后执行对应操作。
if (Test-Path -LiteralPath $ollamaVolumePath -PathType Leaf) {
    # 执行当前脚本步骤。
    $recordedVolume = (Get-Content -LiteralPath $ollamaVolumePath -Encoding UTF8 | Select-Object -First 1).Trim()
    # 判断条件后执行对应操作。
    if ($recordedVolume -ne $ollamaVolume) { throw "模型卷名称与部署项目不一致，请重新打包：$recordedVolume" }
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if (Test-Path -LiteralPath $ollamaArchive -PathType Leaf) {
    # 仅补齐缺失文件；重复运行以及上次中断后均可重试，不覆盖已有模型。
    # 执行当前脚本步骤。
    Invoke-Checked -Arguments @("volume", "create", $ollamaVolume)
    # 执行当前脚本步骤。
    Invoke-Checked -Arguments @(
        # 执行当前脚本步骤。
        "run", "--rm", "--pull", "never",
        "--mount", "type=volume,source=$ollamaVolume,target=/dst",
        "--mount", "type=bind,source=$BundleDir,target=/backup,readonly",
        "alpine:3.22", "sh", "/backup/scripts/lib/restore-ollama-models.sh", "/backup/ollama-data.tgz", "/dst"
    # 执行当前脚本步骤。
    )
    # 执行当前脚本步骤。
    Write-Host "Ollama 模型已恢复（保留已有文件）。" -ForegroundColor Green
# 结束当前控制块。
}

# 执行当前脚本步骤。
Invoke-Checked -Arguments ($composeArguments + @("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "180"))
# 执行当前脚本步骤。
Invoke-Checked -Arguments ($composeArguments + @("ps"))

# 判断条件后执行对应操作。
if (-not $SkipHealthCheck) {
    # 执行当前脚本步骤。
    $apiPort = Get-EnvValue -Path $envPath -Key "IOT_API_PORT"
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace($apiPort)) { $apiPort = "8081" }
    # 执行当前脚本步骤。
    $healthUrl = "http://127.0.0.1:$apiPort/health/ready"
    # 执行当前脚本步骤。
    $healthy = $false
    # 遍历数据并执行循环体。
    for ($i = 0; $i -lt 60; $i++) {
        # 执行当前脚本步骤。
        try {
            # 执行当前脚本步骤。
            $response = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 3
            # 判断条件后执行对应操作。
            if ($response.StatusCode -eq 200) {
                # 执行当前脚本步骤。
                $healthy = $true
                # 执行当前脚本步骤。
                break
            # 结束当前控制块。
            }
        # 结束当前控制块。
        } catch {
            # Compose health/dependency checks may still be in progress.
        # 结束当前控制块。
        }
        # 执行当前脚本步骤。
        Start-Sleep -Seconds 2
    # 结束当前控制块。
    }
    # 判断条件后执行对应操作。
    if (-not $healthy) {
        # 执行当前脚本步骤。
        & docker @($composeArguments + @("logs", "--tail=100", "platform-api", "postgres", "redpanda", "emqx"))
        # 执行当前脚本步骤。
        throw "平台健康检查失败：$healthUrl"
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    Invoke-Checked -Arguments ($composeArguments + @("exec", "-T", "ollama", "ollama", "show", "nomic-embed-text"))
    # 执行当前脚本步骤。
    $aiProvider = Get-EnvValue -Path $envPath -Key "IOT_AI_PROVIDER"
    # 判断条件后执行对应操作。
    if ($aiProvider -eq "ollama") {
        # 执行当前脚本步骤。
        $chatModel = Get-EnvValue -Path $envPath -Key "IOT_AI_MODEL"
        # 判断条件后执行对应操作。
        if ([string]::IsNullOrWhiteSpace($chatModel)) { $chatModel = Get-EnvValue -Path $envPath -Key "IOT_OLLAMA_MODEL" }
        # 判断条件后执行对应操作。
        if ([string]::IsNullOrWhiteSpace($chatModel)) { $chatModel = "qwen3:1.7b" }
        # 执行当前脚本步骤。
        Invoke-Checked -Arguments ($composeArguments + @("exec", "-T", "ollama", "ollama", "show", $chatModel))
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    Write-Host "平台健康检查与本地模型检查通过：$healthUrl" -ForegroundColor Green
    # 执行当前脚本步骤。
    $checkWebPort = Get-EnvValue -Path $envPath -Key "IOT_WEB_PORT"
    # 判断条件后执行对应操作。
    if (-not $checkWebPort) { $checkWebPort = '8080' }
    # 执行当前脚本步骤。
    $checkBackupPort = Get-EnvValue -Path $envPath -Key "IOT_BACKUP_HTTP_PORT"
    # 判断条件后执行对应操作。
    if (-not $checkBackupPort) { $checkBackupPort = '8092' }
    # 遍历数据并执行循环体。
    foreach ($url in @("http://127.0.0.1:$checkWebPort/", "http://127.0.0.1:$checkWebPort/health/ready", "http://127.0.0.1:$checkBackupPort/health/ready")) {
        # 执行当前脚本步骤。
        $ready = $false
        # 遍历数据并执行循环体。
        for ($i = 0; $i -lt 60; $i++) {
            # 执行当前脚本步骤。
            try {
                # 判断条件后执行对应操作。
                if ((Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 3).StatusCode -eq 200) { $ready = $true; break }
            # 结束当前控制块。
            } catch { }
            # 执行当前脚本步骤。
            Start-Sleep -Seconds 2
        # 结束当前控制块。
        }
        # 判断条件后执行对应操作。
        if (-not $ready) { throw "服务健康检查失败：$url" }
        # 执行当前脚本步骤。
        Write-Host "健康检查通过：$url"
    # 结束当前控制块。
    }
# 结束当前控制块。
}

# 执行当前脚本步骤。
$webPort = Get-EnvValue -Path $envPath -Key "IOT_WEB_PORT"
# 判断条件后执行对应操作。
if ([string]::IsNullOrWhiteSpace($webPort)) { $webPort = "8080" }
# 执行当前脚本步骤。
Write-Host "离线部署完成。Web 地址：http://127.0.0.1:$webPort" -ForegroundColor Green
# 执行当前脚本步骤。
Write-Host "管理员凭据：$(Join-Path $BundleDir 'OFFLINE-CREDENTIALS.txt')"
