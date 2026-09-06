[CmdletBinding()]
param(
    [string]$OutputDir = "offline-bundles",
    [string]$EnvFile = "",
    [switch]$Full,
    [switch]$IncludeAi,
    [switch]$IncludeHarness,
    [switch]$IncludeGb26875,
    [string]$OllamaModel = "qwen3:8b",
    [string]$OllamaEmbeddingModel = "nomic-embed-text",
    [switch]$SkipOllamaModel
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
. (Join-Path $scriptDir 'lib/deployment.ps1')

if ($Full) {
    $IncludeAi = $true
    $IncludeHarness = $true
    $IncludeGb26875 = $true
}

function Invoke-Checked {
    param([Parameter(Mandatory)][string[]]$Arguments)

    Write-Host ("> docker " + ($Arguments -join " ")) -ForegroundColor DarkGray
    & docker @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Docker 命令失败，退出码 $LASTEXITCODE：docker $($Arguments -join ' ')"
    }
}

function Invoke-Captured {
    param([Parameter(Mandatory)][string[]]$Arguments)

    $output = @(& docker @Arguments 2>$null)
    if ($LASTEXITCODE -ne 0) {
        throw "Docker 命令失败，退出码 $LASTEXITCODE：docker $($Arguments -join ' ')"
    }
    return @($output | ForEach-Object { $_.ToString().Trim() } | Where-Object { $_ })
}

function Write-Utf8NoBom {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][AllowEmptyCollection()][AllowEmptyString()][string[]]$Lines
    )

    $encoding = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllLines($Path, $Lines, $encoding)
}

function New-RandomHex {
    param([int]$Bytes = 24)

    $buffer = New-Object byte[] $Bytes
    $random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $random.GetBytes($buffer)
    } finally {
        $random.Dispose()
    }
    return ([BitConverter]::ToString($buffer).Replace("-", "").ToLowerInvariant())
}

function Get-EnvEntries {
    param([Parameter(Mandatory)][string]$Path)

    $entries = @{}
    foreach ($line in @(Get-Content -LiteralPath $Path -Encoding UTF8)) {
        if ($line -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') {
            $value = $matches[2].Trim()
            if ($value.Length -ge 2 -and $value.StartsWith('"') -and $value.EndsWith('"')) {
                $value = $value.Substring(1, $value.Length - 2)
            } elseif ($value.Length -ge 2 -and $value.StartsWith("'") -and $value.EndsWith("'")) {
                $value = $value.Substring(1, $value.Length - 2)
            }
            $entries[$matches[1]] = $value
        }
    }
    return $entries
}

function Set-OrAdd-EnvLine {
    param(
        [Parameter(Mandatory)][AllowEmptyCollection()][string[]]$Lines,
        [Parameter(Mandatory)][string]$Key,
        [Parameter(Mandatory)][string]$Value
    )

    $pattern = '^\s*' + [Regex]::Escape($Key) + '\s*='
    $result = New-Object 'System.Collections.Generic.List[string]'
    $replaced = $false
    foreach ($line in $Lines) {
        if ($line -match $pattern) {
            [void]$result.Add("$Key=$Value")
            $replaced = $true
        } else {
            [void]$result.Add($line)
        }
    }
    if (-not $replaced) {
        [void]$result.Add("$Key=$Value")
    }
    return $result.ToArray()
}

function New-OfflineEnv {
    param(
        [Parameter(Mandatory)][string]$Destination,
        [string]$Source,
        [switch]$UseAi,
        [switch]$UseHarness
    )

    $generated = [string]::IsNullOrWhiteSpace($Source)
    $credentialLines = New-Object 'System.Collections.Generic.List[string]'

    if (-not $generated) {
        if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
            throw "指定的 EnvFile 不存在：$Source"
        }
        $lines = @(Get-Content -LiteralPath $Source -Encoding UTF8)
        $entries = Get-EnvEntries -Path $Source
        $required = @(
            "POSTGRES_PASSWORD", "REDIS_PASSWORD", "CLICKHOUSE_PASSWORD",
            "MINIO_ROOT_PASSWORD", "MINIO_DR_ROOT_PASSWORD", "IOT_JWT_SECRET",
            "IOT_ADMIN_USER", "IOT_ADMIN_PASSWORD", "IOT_ADMIN_TENANTS",
            "IOT_VIDEO_PLATFORM_SECRETS", "IOT_BACKUP_ADMIN_TOKEN",
            "EMQX_DASHBOARD_USER", "EMQX_DASHBOARD_PASSWORD",
            "GRAFANA_ADMIN_USER", "GRAFANA_ADMIN_PASSWORD"
        )
        if ($UseHarness) { $required += "IOT_AI_HARNESS_TOKEN" }
        foreach ($key in $required) {
            if (-not $entries.ContainsKey($key) -or [string]::IsNullOrWhiteSpace([string]$entries[$key])) {
                throw "EnvFile 缺少必填安全配置：$key。请不要直接使用 .env.example 的默认值。"
            }
        }
        $unsafe = @($lines | Where-Object { $_ -match '(change-this|local-iot-|admin123|public-change-me|change-me)' })
        if ($unsafe.Count -gt 0) {
            throw "EnvFile 仍包含示例密码或默认密钥，请先替换后再打包。"
        }
        [void]$credentialLines.Add("凭据来自外部 EnvFile：$Source")
        [void]$credentialLines.Add("本文件不复制外部 EnvFile 的内容，请单独保管原始凭据。")
    } else {
        $postgresPassword = "pg-" + (New-RandomHex -Bytes 18)
        $redisPassword = "redis-" + (New-RandomHex -Bytes 18)
        $clickhousePassword = "ch-" + (New-RandomHex -Bytes 18)
        $minioPassword = "minio-" + (New-RandomHex -Bytes 18)
        $minioDrPassword = "minio-dr-" + (New-RandomHex -Bytes 18)
        $jwtSecret = New-RandomHex -Bytes 32
        $adminPassword = "Admin-" + (New-RandomHex -Bytes 12)
        $videoSecret = New-RandomHex -Bytes 24
        $harnessToken = New-RandomHex -Bytes 32
        $backupToken = New-RandomHex -Bytes 32
        $emqxPassword = "Emqx-" + (New-RandomHex -Bytes 12)
        $grafanaPassword = "Grafana-" + (New-RandomHex -Bytes 12)

        $ollamaUrl = if ($UseAi) { "http://ollama:11434" } else { "" }
        $aiProvider = if ($UseAi) { "ollama" } else { "" }
        $weaviateUrl = "http://weaviate:8080"
        $harnessUrl = if ($UseHarness) { "http://deepseek-harness:8091" } else { "" }

        $lines = @(
            "# 自动生成的离线部署配置，请限制此文件权限。",
            "POSTGRES_PASSWORD=$postgresPassword",
            "REDIS_PASSWORD=$redisPassword",
            "CLICKHOUSE_PASSWORD=$clickhousePassword",
            "MINIO_ROOT_USER=iotadmin",
            "MINIO_ROOT_PASSWORD=$minioPassword",
            "MINIO_DR_ROOT_USER=iotdradmin",
            "MINIO_DR_ROOT_PASSWORD=$minioDrPassword",
            "IOT_JWT_SECRET=$jwtSecret",
            "IOT_ADMIN_USER=admin",
            "IOT_ADMIN_PASSWORD=$adminPassword",
            "IOT_ADMIN_TENANTS=tenant_001",
            "IOT_VIDEO_PLATFORM_SECRETS=video-platform-1:$videoSecret",
            "IOT_VIDEO_MEDIA_ALLOWED_HOSTS=",
            "IOT_OLLAMA_URL=$ollamaUrl",
            "IOT_OLLAMA_MODEL=$OllamaModel",
            "IOT_AI_PROVIDER=$aiProvider",
            "IOT_AI_BASE_URL=",
            "IOT_AI_MODEL=",
            "IOT_AI_API_KEY=",
            "IOT_AI_PROVIDER_TEST_ALLOWED_ORIGINS=http://ollama:11434",
            "IOT_AI_OLLAMA_URL=http://ollama:11434",
            "DEEPSEEK_API_KEY=",
            "IOT_AI_HARNESS_URL=$harnessUrl",
            "IOT_AI_HARNESS_TOKEN=$harnessToken",
            "IOT_AI_HARNESS_MCP_URL=http://platform-api:8080/mcp/harness",
            "IOT_AI_HARNESS_MODEL=deepseek-v4-flash",
            "IOT_AI_HARNESS_TIMEOUT=90s",
            "IOT_WEAVIATE_URL=$weaviateUrl",
            "IOT_BACKUP_ADMIN_TOKEN=$backupToken",
            "IOT_RAW_HIGH_FREQUENCY_INTERVAL_SEC=60",
            "IOT_BACKUP_TIME=00:05",
            "IOT_BACKUP_TIMEZONE=Asia/Shanghai",
            "IOT_MQTT_WEBSOCKET_PUBLIC_URL=",
            "IOT_WEB_PORT=8080",
            "IOT_API_PORT=8081",
            "IOT_CORS_ALLOWED_ORIGINS=http://localhost:8080,http://127.0.0.1:8080",
            "EMQX_DASHBOARD_USER=admin",
            "EMQX_DASHBOARD_PASSWORD=$emqxPassword",
            "GRAFANA_ADMIN_USER=admin",
            "GRAFANA_ADMIN_PASSWORD=$grafanaPassword"
        )

        [void]$credentialLines.Add("平台管理员：admin")
        [void]$credentialLines.Add("平台管理员密码：$adminPassword")
        [void]$credentialLines.Add("备份服务 Token：$backupToken")
        [void]$credentialLines.Add("EMQX Dashboard：admin / $emqxPassword")
        [void]$credentialLines.Add("Grafana：admin / $grafanaPassword")
        [void]$credentialLines.Add("PostgreSQL 密码：$postgresPassword")
        [void]$credentialLines.Add("Redis 密码：$redisPassword")
        [void]$credentialLines.Add("ClickHouse 密码：$clickhousePassword")
        [void]$credentialLines.Add("MinIO 主密码：$minioPassword")
        [void]$credentialLines.Add("MinIO 灾备密码：$minioDrPassword")
    }

    $imageValues = [ordered]@{
        "IOT_PLATFORM_API_IMAGE" = "iot-platform-api:offline"
        "IOT_PLATFORM_WEB_IMAGE" = "iot-platform-web:offline"
        "IOT_BACKUP_IMAGE" = "iot-platform-backup:offline"
        "IOT_DEEPSEEK_HARNESS_IMAGE" = "iot-deepseek-harness:offline"
    }
    foreach ($item in $imageValues.GetEnumerator()) {
        $lines = @(Set-OrAdd-EnvLine -Lines $lines -Key $item.Key -Value $item.Value)
    }

    Write-Utf8NoBom -Path $Destination -Lines $lines
    $credentialPath = Join-Path (Split-Path -Parent $Destination) "OFFLINE-CREDENTIALS.txt"
    $credentialFileLines = @(
        "# 离线部署凭据",
        "# 请将本文件视为密码文件，不要提交 Git 或公开传输。",
        ""
    ) + @($credentialLines)
    Write-Utf8NoBom -Path $credentialPath -Lines $credentialFileLines

    return [pscustomobject]@{
        Generated = $generated
        CredentialPath = $credentialPath
    }
}

if ($OllamaEmbeddingModel -ne "nomic-embed-text") {
    throw "当前知识库使用 nomic-embed-text，OllamaEmbeddingModel 必须与其一致。"
}
if ($IncludeAi -and $OllamaModel -notmatch '^[A-Za-z0-9][A-Za-z0-9._:/-]*$') {
    throw "OllamaModel 不是有效的模型名称。"
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "找不到 docker 命令。请在安装并启动 Docker Engine/Desktop 的有网打包机执行。"
}
& docker info *> $null
if ($LASTEXITCODE -ne 0) {
    throw "Docker Engine 不可用。请先启动 Docker Desktop 或 Docker Engine。"
}

$parentPath = if ([System.IO.Path]::IsPathRooted($OutputDir)) { $OutputDir } else { Join-Path $projectRoot $OutputDir }
$parentPath = [System.IO.Path]::GetFullPath($parentPath)
New-Item -ItemType Directory -Force -Path $parentPath | Out-Null
$bundleName = "iot-platform-offline-$(Get-Date -Format 'yyyyMMdd-HHmmss')-$([guid]::NewGuid().ToString('N').Substring(0, 6))"
$bundleRoot = Join-Path $parentPath $bundleName
New-Item -ItemType Directory -Force -Path $bundleRoot | Out-Null

$sourceEnv = $EnvFile
if (-not [string]::IsNullOrWhiteSpace($sourceEnv) -and -not [System.IO.Path]::IsPathRooted($sourceEnv)) {
    $sourceEnv = Join-Path $projectRoot $sourceEnv
}
$envPath = Join-Path $bundleRoot ".env.offline"
$envResult = New-OfflineEnv -Destination $envPath -Source $sourceEnv -UseAi:$IncludeAi -UseHarness:$IncludeHarness
$runtimeProvider = Get-DeploymentEnvValue -Path $envPath -Key 'IOT_AI_PROVIDER'
if ($runtimeProvider -eq 'ollama' -or (-not $runtimeProvider -and (Get-DeploymentEnvValue -Path $envPath -Key 'IOT_OLLAMA_URL'))) {
    $IncludeAi = $true
    $configuredModel = Get-DeploymentEnvValue -Path $envPath -Key 'IOT_AI_MODEL'
    if (-not $configuredModel) { $configuredModel = Get-DeploymentEnvValue -Path $envPath -Key 'IOT_OLLAMA_MODEL' }
    if ($configuredModel) { $OllamaModel = $configuredModel }
}
if ($IncludeAi -and $OllamaModel -notmatch '^[A-Za-z0-9][A-Za-z0-9._:/-]*$') { throw '配置中的 Ollama 模型名称无效。' }
$composeBase = @(
    "compose", "--project-name", "iot-platform-offline-build",
    "--env-file", $envPath,
    "-f", (Join-Path $projectRoot "compose.yaml"),
    "-f", (Join-Path $projectRoot "compose.offline.yaml")
)

$profiles = New-Object 'System.Collections.Generic.List[string]'
if ($IncludeHarness) { [void]$profiles.Add("harness") }
if ($IncludeGb26875) { [void]$profiles.Add("gb26875") }
$profileArguments = New-Object 'System.Collections.Generic.List[string]'
foreach ($profile in $profiles) {
    [void]$profileArguments.Add("--profile")
    [void]$profileArguments.Add($profile)
}

$ollamaArchive = $null
$ollamaVolumeName = $null
$ollamaStarted = $false

try {
    Invoke-Checked -Arguments ($composeBase + $profileArguments.ToArray() + @("config", "--quiet"))
    $pullServices = @(
        "postgres", "postgres-wal-init", "redis", "minio", "minio-dr",
        "redpanda", "redpanda-init", "clickhouse", "emqx", "prometheus",
        "grafana", "loki", "ollama", "weaviate"
    )
    Invoke-Checked -Arguments ($composeBase + @("pull") + $pullServices)
    Invoke-Checked -Arguments ($composeBase + @("build", "--pull", "platform-api", "platform-web", "backup-service"))

    # 知识库始终需要嵌入模型；IncludeAi 额外包含对话模型。
    if (-not $SkipOllamaModel) {
        $ollamaStarted = $true
        Invoke-Checked -Arguments ($composeBase + @("up", "-d", "--no-deps", "ollama"))
        $ollamaReady = $false
        for ($i = 0; $i -lt 30; $i++) {
            & docker @($composeBase + @("exec", "-T", "ollama", "ollama", "list")) *> $null
            if ($LASTEXITCODE -eq 0) { $ollamaReady = $true; break }
            Start-Sleep -Seconds 2
        }
        if (-not $ollamaReady) { throw "Ollama 容器未在规定时间内就绪。" }
        Invoke-Checked -Arguments ($composeBase + @("exec", "-T", "ollama", "ollama", "pull", $OllamaEmbeddingModel))
        if ($IncludeAi -and $OllamaModel -ne $OllamaEmbeddingModel) {
            Invoke-Checked -Arguments ($composeBase + @("exec", "-T", "ollama", "ollama", "pull", $OllamaModel))
        }
        $ollamaSourceVolume = "iot-platform-offline-build_ollama-data"
        $ollamaVolumeName = "iot-platform_ollama-data"
        Invoke-Checked -Arguments @(
            "run", "--rm", "--pull", "never",
            "--mount", "type=volume,source=$ollamaSourceVolume,target=/src,readonly",
            "--mount", "type=bind,source=$bundleRoot,target=/backup",
            "alpine:3.22", "sh", "-ec", "tar -czf /backup/ollama-data.tgz -C /src models"
        )
        $ollamaArchive = "ollama-data.tgz"
        $modelHash = (Get-FileHash -LiteralPath (Join-Path $bundleRoot $ollamaArchive) -Algorithm SHA256).Hash.ToLowerInvariant()
        Write-Utf8NoBom -Path (Join-Path $bundleRoot "ollama-data.tgz.sha256") -Lines @("$modelHash  $ollamaArchive")
    } else {
        Write-Warning "已跳过模型打包：目标机必须预先具有 nomic-embed-text；否则知识库不可用。"
    }

    if ($IncludeHarness) {
        Ensure-HarnessSource -ProjectRoot $projectRoot
        Invoke-Checked -Arguments ($composeBase + @("--profile", "harness", "build", "--pull", "deepseek-harness"))
    }

    Copy-Item -LiteralPath (Join-Path $projectRoot "compose.yaml") -Destination $bundleRoot
    Copy-Item -LiteralPath (Join-Path $projectRoot "compose.offline.yaml") -Destination $bundleRoot
    Copy-Item -LiteralPath (Join-Path $projectRoot "deploy") -Destination $bundleRoot -Recurse
    New-Item -ItemType Directory -Force -Path (Join-Path $bundleRoot "scripts") | Out-Null
    foreach ($runtimeScript in @(
        "deploy-offline.ps1",
        "deploy-offline-windows.ps1",
        "deploy-offline.sh",
        "deploy-offline-linux.sh",
        "deploy-offline-macos.sh"
    )) {
        Copy-Item -LiteralPath (Join-Path $scriptDir $runtimeScript) -Destination (Join-Path $bundleRoot "scripts")
    }
    Copy-Item -LiteralPath (Join-Path $projectRoot "docs\OFFLINE_DEPLOYMENT.md") -Destination (Join-Path $bundleRoot "OFFLINE_DEPLOYMENT.md")

    $images = @(Invoke-Captured -Arguments ($composeBase + $profileArguments.ToArray() + @("config", "--images")) | Sort-Object -Unique)
    if ($images.Count -eq 0) { throw "没有解析出可导出的镜像。" }
    foreach ($image in $images) {
        & docker image inspect $image *> $null
        if ($LASTEXITCODE -ne 0) {
            throw "镜像不存在，无法导出：$image"
        }
    }

    $archivePath = Join-Path $bundleRoot "images.tar"
    Invoke-Checked -Arguments (@("save", "-o", $archivePath) + $images)
    $hash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    Write-Utf8NoBom -Path (Join-Path $bundleRoot "images.tar.sha256") -Lines @("$hash  images.tar")
    Write-Utf8NoBom -Path (Join-Path $bundleRoot "profiles.txt") -Lines $profiles.ToArray()
    if ($ollamaVolumeName) {
        Write-Utf8NoBom -Path (Join-Path $bundleRoot "ollama-volume.txt") -Lines @($ollamaVolumeName)
    }

    $commit = "unknown"
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $commit = (& git -C $projectRoot rev-parse HEAD 2>$null | Select-Object -First 1)
        if ($LASTEXITCODE -ne 0) { $commit = "unknown" }
    }
    $manifest = [ordered]@{
        format = 1
        project = "iot-platform"
        createdAtUtc = (Get-Date).ToUniversalTime().ToString("o")
        gitCommit = ([string]$commit).Trim()
        profiles = $profiles.ToArray()
        images = $images
        imageArchive = "images.tar"
        imageArchiveSha256 = $hash
        envFile = ".env.offline"
        composeFiles = @("compose.yaml", "compose.offline.yaml")
        ollamaModel = if ($ollamaArchive -and $IncludeAi) { $OllamaModel } else { $null }
        ollamaEmbeddingModel = if ($ollamaArchive) { $OllamaEmbeddingModel } else { $null }
        ollamaArchive = $ollamaArchive
        ollamaVolume = $ollamaVolumeName
        generatedCredentials = [bool]$envResult.Generated
    }
    $manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $bundleRoot "manifest.json") -Encoding UTF8

    Write-Host ""
    Write-Host "离线包已生成：$bundleRoot" -ForegroundColor Green
    Write-Host "镜像数量：$($images.Count)"
    Write-Host "镜像包大小：$([Math]::Round((Get-Item $archivePath).Length / 1GB, 2)) GB"
    Write-Host "部署方式：按服务器系统运行 scripts/deploy-offline-windows.ps1、deploy-offline-macos.sh 或 deploy-offline-linux.sh"
    if ($envResult.Generated) {
        Write-Host "自动生成的凭据：$($envResult.CredentialPath)" -ForegroundColor Yellow
    }
} finally {
    if ($ollamaStarted) {
        & docker @($composeBase + @("stop", "ollama")) *> $null
        if ($LASTEXITCODE -ne 0) { Write-Warning "打包用 Ollama 未能停止，请检查 iot-platform-offline-build 项目。" }
    }
}
