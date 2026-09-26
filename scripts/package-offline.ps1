[CmdletBinding()]
param(
    [string]$OutputDir = "offline-bundles",
    [string]$EnvFile = "",
    [switch]$Full,
    [switch]$IncludeAi = $true,
    [switch]$IncludeHarness = $true,
    [string]$OllamaModel = "",
    [string]$DeepSeekModel = "deepseek-flash",
    [string]$OllamaEmbeddingModel = "nomic-embed-text",
    [switch]$SkipOllamaModel,
    [switch]$SkipDockerRuntime,
    [string]$DockerPackagesDir = "",
    [ValidateSet("generic", "openeuler-24.03-lts-sp4")]
    [string]$TargetOS = "generic"
)

# 执行当前脚本步骤。
Set-StrictMode -Version Latest
# 执行当前脚本步骤。
$ErrorActionPreference = "Stop"

# 判断条件后执行对应操作。
if ($TargetOS -ne 'generic' -and ($SkipDockerRuntime -or $DockerPackagesDir)) {
    # 执行当前脚本步骤。
    throw '专用系统包不能与 SkipDockerRuntime 或 DockerPackagesDir 同时使用。'
# 结束当前控制块。
}

# 执行当前脚本步骤。
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
# 执行当前脚本步骤。
$projectRoot = Split-Path -Parent $scriptDir
# 执行当前脚本步骤。
. (Join-Path $scriptDir 'lib/deployment.ps1')
# 执行当前脚本步骤。
. (Join-Path $scriptDir 'lib/docker-runtime.ps1')

# 判断条件后执行对应操作。
if ($Full) {
    # 执行当前脚本步骤。
    $IncludeAi = $true
    # 执行当前脚本步骤。
    $IncludeHarness = $true
# 结束当前控制块。
}

# Harness 为所有模型工作流必装组件，保留参数仅兼容旧调用。
$IncludeHarness = $true

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
function Invoke-Captured {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string[]]$Arguments)

    # 执行当前脚本步骤。
    $output = @(& docker @Arguments 2>$null)
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0) {
        # 执行当前脚本步骤。
        throw "Docker 命令失败，退出码 $LASTEXITCODE：docker $($Arguments -join ' ')"
    # 结束当前控制块。
    }
    # 返回结果或结束当前脚本。
    return @($output | ForEach-Object { $_.ToString().Trim() } | Where-Object { $_ })
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Write-Utf8NoBom {
    # 执行当前脚本步骤。
    param(
        # 执行当前脚本步骤。
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][AllowEmptyCollection()][AllowEmptyString()][string[]]$Lines
    # 执行当前脚本步骤。
    )

    # 执行当前脚本步骤。
    $encoding = New-Object System.Text.UTF8Encoding($false)
    # 执行当前脚本步骤。
    [System.IO.File]::WriteAllLines($Path, $Lines, $encoding)
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function New-RandomHex {
    # 执行当前脚本步骤。
    param([int]$Bytes = 24)

    # 执行当前脚本步骤。
    $buffer = New-Object byte[] $Bytes
    # 执行当前脚本步骤。
    $random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    # 执行当前脚本步骤。
    try {
        # 执行当前脚本步骤。
        $random.GetBytes($buffer)
    # 结束当前控制块。
    } finally {
        # 执行当前脚本步骤。
        $random.Dispose()
    # 结束当前控制块。
    }
    # 返回结果或结束当前脚本。
    return ([BitConverter]::ToString($buffer).Replace("-", "").ToLowerInvariant())
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Get-EnvEntries {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string]$Path)

    # 执行当前脚本步骤。
    $entries = @{}
    # 遍历数据并执行循环体。
    foreach ($line in @(Get-Content -LiteralPath $Path -Encoding UTF8)) {
        # 判断条件后执行对应操作。
        if ($line -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') {
            # 执行当前脚本步骤。
            $value = $matches[2].Trim()
            # 判断条件后执行对应操作。
            if ($value.Length -ge 2 -and $value.StartsWith('"') -and $value.EndsWith('"')) {
                # 执行当前脚本步骤。
                $value = $value.Substring(1, $value.Length - 2)
            # 结束当前控制块。
            } elseif ($value.Length -ge 2 -and $value.StartsWith("'") -and $value.EndsWith("'")) {
                # 执行当前脚本步骤。
                $value = $value.Substring(1, $value.Length - 2)
            # 结束当前控制块。
            }
            # 执行当前脚本步骤。
            $entries[$matches[1]] = $value
        # 结束当前控制块。
        }
    # 结束当前控制块。
    }
    # 返回结果或结束当前脚本。
    return $entries
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Set-OrAdd-EnvLine {
    # 执行当前脚本步骤。
    param(
        # 执行当前脚本步骤。
        [Parameter(Mandatory)][AllowEmptyCollection()][string[]]$Lines,
        [Parameter(Mandatory)][string]$Key,
        [Parameter(Mandatory)][string]$Value
    # 执行当前脚本步骤。
    )

    # 执行当前脚本步骤。
    $pattern = '^\s*' + [Regex]::Escape($Key) + '\s*='
    # 执行当前脚本步骤。
    $result = New-Object 'System.Collections.Generic.List[string]'
    # 执行当前脚本步骤。
    $replaced = $false
    # 遍历数据并执行循环体。
    foreach ($line in $Lines) {
        # 判断条件后执行对应操作。
        if ($line -match $pattern) {
            # 执行当前脚本步骤。
            [void]$result.Add("$Key=$Value")
            # 执行当前脚本步骤。
            $replaced = $true
        # 结束当前控制块。
        } else {
            # 执行当前脚本步骤。
            [void]$result.Add($line)
        # 结束当前控制块。
        }
    # 结束当前控制块。
    }
    # 判断条件后执行对应操作。
    if (-not $replaced) {
        # 执行当前脚本步骤。
        [void]$result.Add("$Key=$Value")
    # 结束当前控制块。
    }
    # 返回结果或结束当前脚本。
    return $result.ToArray()
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function New-OfflineEnv {
    # 执行当前脚本步骤。
    param(
        # 执行当前脚本步骤。
        [Parameter(Mandatory)][string]$Destination,
        [string]$Source,
        [switch]$UseAi,
        [switch]$UseHarness
    # 执行当前脚本步骤。
    )

    # 执行当前脚本步骤。
    $generated = [string]::IsNullOrWhiteSpace($Source)
    # 执行当前脚本步骤。
    $credentialLines = New-Object 'System.Collections.Generic.List[string]'

    # 判断条件后执行对应操作。
    if (-not $generated) {
        # 判断条件后执行对应操作。
        if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
            # 执行当前脚本步骤。
            throw "指定的 EnvFile 不存在：$Source"
        # 结束当前控制块。
        }
        # 执行当前脚本步骤。
        $lines = @(Get-Content -LiteralPath $Source -Encoding UTF8)
        # 执行当前脚本步骤。
        $entries = Get-EnvEntries -Path $Source
        # 执行当前脚本步骤。
        $required = @(
            # 执行当前脚本步骤。
            "POSTGRES_PASSWORD", "REDIS_PASSWORD", "CLICKHOUSE_PASSWORD",
            "MINIO_ROOT_PASSWORD", "MINIO_DR_ROOT_PASSWORD", "IOT_JWT_SECRET",
            "IOT_ADMIN_USER", "IOT_ADMIN_PASSWORD", "IOT_ADMIN_TENANTS",
            "IOT_VIDEO_PLATFORM_SECRETS", "IOT_BACKUP_ADMIN_TOKEN",
            "EMQX_DASHBOARD_USER", "EMQX_DASHBOARD_PASSWORD",
            "GRAFANA_ADMIN_USER", "GRAFANA_ADMIN_PASSWORD"
        # 执行当前脚本步骤。
        )
        # 判断条件后执行对应操作。
        if ($UseHarness) { $required += "IOT_AI_HARNESS_TOKEN" }
        # 遍历数据并执行循环体。
        foreach ($key in $required) {
            # 判断条件后执行对应操作。
            if (-not $entries.ContainsKey($key) -or [string]::IsNullOrWhiteSpace([string]$entries[$key])) {
                # 执行当前脚本步骤。
                throw "EnvFile 缺少必填安全配置：$key。请不要直接使用 .env.example 的默认值。"
            # 结束当前控制块。
            }
        # 结束当前控制块。
        }
        # 执行当前脚本步骤。
        $unsafe = @($lines | Where-Object { $_ -notmatch '^\s*IOT_ADMIN_PASSWORD\s*=' -and $_ -match '^[A-Za-z_][A-Za-z0-9_]*=.*(change-this|local-iot-|admin123|public-change-me|change-me)' })
        # 判断条件后执行对应操作。
        if ($unsafe.Count -gt 0) {
            # 执行当前脚本步骤。
            throw "EnvFile 仍包含示例密码或默认密钥，请先替换后再打包。"
        # 结束当前控制块。
        }
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("凭据来自外部 EnvFile：$Source")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("本文件不复制外部 EnvFile 的内容，请单独保管原始凭据。")
    # 结束当前控制块。
    } else {
        # 执行当前脚本步骤。
        $postgresPassword = "pg-" + (New-RandomHex -Bytes 18)
        # 执行当前脚本步骤。
        $redisPassword = "redis-" + (New-RandomHex -Bytes 18)
        # 执行当前脚本步骤。
        $clickhousePassword = "ch-" + (New-RandomHex -Bytes 18)
        # 执行当前脚本步骤。
        $minioPassword = "minio-" + (New-RandomHex -Bytes 18)
        # 执行当前脚本步骤。
        $minioDrPassword = "minio-dr-" + (New-RandomHex -Bytes 18)
        # 执行当前脚本步骤。
        $jwtSecret = New-RandomHex -Bytes 32
        # 执行当前脚本步骤。
        $adminPassword = 'admin123'
        # 执行当前脚本步骤。
        $videoSecret = New-RandomHex -Bytes 24
        # 执行当前脚本步骤。
        $harnessToken = New-RandomHex -Bytes 32
        # 执行当前脚本步骤。
        $backupToken = New-RandomHex -Bytes 32
        # 执行当前脚本步骤。
        $emqxPassword = "Emqx-" + (New-RandomHex -Bytes 12)
        # 执行当前脚本步骤。
        $grafanaPassword = "Grafana-" + (New-RandomHex -Bytes 12)

        # 执行当前脚本步骤。
        $ollamaUrl = "http://ollama:11434"
        # 执行当前脚本步骤。
        $aiProvider = "deepseek"
        # 执行当前脚本步骤。
        $weaviateUrl = "http://weaviate:8080"
        # 执行当前脚本步骤。
        $harnessUrl = "http://deepseek-harness:8091"
        # 执行当前脚本步骤。
        $harnessEnabled = "true"

        # 执行当前脚本步骤。
        $lines = @(
            # 执行当前脚本步骤。
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
            "IOT_AI_PROVIDER=$aiProvider",
            "IOT_AI_BASE_URL=https://api.deepseek.com",
            "IOT_AI_MODEL=$DeepSeekModel",
            "IOT_AI_API_KEY=",
            "IOT_AI_OLLAMA_URL=http://ollama:11434",
            "DEEPSEEK_API_KEY=",
            "IOT_AI_HARNESS_ENABLED=$harnessEnabled",
            "IOT_AI_HARNESS_URL=$harnessUrl",
            "IOT_AI_HARNESS_TOKEN=$harnessToken",
            "IOT_AI_HARNESS_MCP_URL=http://platform-api:8080/mcp/harness",
            "IOT_AI_HARNESS_PROVIDER=deepseek-official",
            "IOT_AI_HARNESS_MODEL=$DeepSeekModel",
            "IOT_AI_HARNESS_TIMEOUT=90s",
            "IOT_WEAVIATE_URL=$weaviateUrl",
            "IOT_BACKUP_ADMIN_TOKEN=$backupToken",
            "IOT_RAW_HIGH_FREQUENCY_INTERVAL_SEC=60",
            "IOT_BACKUP_TIME=00:05",
            "IOT_BACKUP_TIMEZONE=Asia/Shanghai",
            "IOT_MQTT_WEBSOCKET_PUBLIC_URL=",
            "IOT_DEVICE_HTTP_PUBLIC_URL=",
            "IOT_DEVICE_MQTT_PUBLIC_URL=",
            "IOT_PROCESS_ROLE=combined",
            "IOT_ACCESS_GATEWAY_URL=",
            "IOT_ACCESS_COORDINATION=false",
            "IOT_ACCESS_NODE_URL=",
            "IOT_WEB_PORT=8080",
            "IOT_API_PORT=8081",
            "IOT_CORS_ALLOWED_ORIGINS=http://localhost:8080,http://127.0.0.1:8080",
            "EMQX_DASHBOARD_USER=admin",
            "EMQX_DASHBOARD_PASSWORD=$emqxPassword",
            "GRAFANA_ADMIN_USER=admin",
            "GRAFANA_ADMIN_PASSWORD=$grafanaPassword"
        # 执行当前脚本步骤。
        )

        # 执行当前脚本步骤。
        [void]$credentialLines.Add("平台管理员：admin")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("平台管理员密码：$adminPassword")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("备份服务 Token：$backupToken")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("EMQX Dashboard：admin / $emqxPassword")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("Grafana：admin / $grafanaPassword")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("PostgreSQL 密码：$postgresPassword")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("Redis 密码：$redisPassword")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("ClickHouse 密码：$clickhousePassword")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("MinIO 主密码：$minioPassword")
        # 执行当前脚本步骤。
        [void]$credentialLines.Add("MinIO 灾备密码：$minioDrPassword")
    # 结束当前控制块。
    }

    # 执行当前脚本步骤。
    $imageValues = [ordered]@{
        # 执行当前脚本步骤。
        "IOT_PLATFORM_API_IMAGE" = "iot-platform-api:offline"
        # 执行当前脚本步骤。
        "IOT_PLATFORM_WEB_IMAGE" = "iot-platform-web:offline"
        # 执行当前脚本步骤。
        "IOT_BACKUP_IMAGE" = "iot-platform-backup:offline"
        # 执行当前脚本步骤。
        "IOT_DEEPSEEK_HARNESS_IMAGE" = "iot-deepseek-harness:offline"
    # 结束当前控制块。
    }
    # 遍历数据并执行循环体。
    foreach ($item in $imageValues.GetEnumerator()) {
        # 执行当前脚本步骤。
        $lines = @(Set-OrAdd-EnvLine -Lines $lines -Key $item.Key -Value $item.Value)
    # 结束当前控制块。
    }
    # 判断条件后执行对应操作。
    if ($UseHarness) {
        # 执行当前脚本步骤。
        $lines = @(Set-OrAdd-EnvLine -Lines $lines -Key 'IOT_AI_HARNESS_ENABLED' -Value 'true')
    # 结束当前控制块。
    } elseif (-not ($lines -match '^\s*IOT_AI_HARNESS_ENABLED\s*=')) {
        # 执行当前脚本步骤。
        $lines = @(Set-OrAdd-EnvLine -Lines $lines -Key 'IOT_AI_HARNESS_ENABLED' -Value 'false')
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    Write-Utf8NoBom -Path $Destination -Lines $lines
    Set-DeepSeekDeploymentEnv -Path $Destination -Model $DeepSeekModel
    Add-DeploymentEnvComments -Path $Destination
    # 执行当前脚本步骤。
    $credentialPath = Join-Path (Split-Path -Parent $Destination) "OFFLINE-CREDENTIALS.txt"
    # 执行当前脚本步骤。
    $credentialFileLines = @(
        # 执行当前脚本步骤。
        "# 离线部署凭据",
        "# 请将本文件视为密码文件，不要提交 Git 或公开传输。",
        ""
    # 执行当前脚本步骤。
    ) + @($credentialLines)
    # 执行当前脚本步骤。
    Write-Utf8NoBom -Path $credentialPath -Lines $credentialFileLines

    # 返回结果或结束当前脚本。
    return [pscustomobject]@{
        # 执行当前脚本步骤。
        Generated = $generated
        # 执行当前脚本步骤。
        CredentialPath = $credentialPath
    # 结束当前控制块。
    }
# 结束当前控制块。
}

# 判断条件后执行对应操作。
if ($OllamaEmbeddingModel -ne "nomic-embed-text") {
    # 执行当前脚本步骤。
    throw "当前知识库使用 nomic-embed-text，OllamaEmbeddingModel 必须与其一致。"
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if ($OllamaModel) { throw '已取消打包本地对话模型，请使用 DeepSeek API。' }
if ($DeepSeekModel -notmatch '^[A-Za-z0-9][A-Za-z0-9._:/-]*$') { throw 'DeepSeek 模型名称无效。' }

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    # 执行当前脚本步骤。
    throw "找不到 docker 命令。请在安装并启动 Docker Engine/Desktop 的有网打包机执行。"
# 结束当前控制块。
}
# 执行当前脚本步骤。
& docker info *> $null
# 判断条件后执行对应操作。
if ($LASTEXITCODE -ne 0) {
    # 执行当前脚本步骤。
    throw "Docker Engine 不可用。请先启动 Docker Desktop 或 Docker Engine。"
# 结束当前控制块。
}

# 执行当前脚本步骤。
$parentPath = if ([System.IO.Path]::IsPathRooted($OutputDir)) { $OutputDir } else { Join-Path $projectRoot $OutputDir }
# 执行当前脚本步骤。
$parentPath = [System.IO.Path]::GetFullPath($parentPath)
# 执行当前脚本步骤。
New-Item -ItemType Directory -Force -Path $parentPath | Out-Null
# 执行当前脚本步骤。
$bundleName = "iot-platform-offline-$(Get-Date -Format 'yyyyMMdd-HHmmss')-$([guid]::NewGuid().ToString('N').Substring(0, 6))"
# 执行当前脚本步骤。
$bundleRoot = Join-Path $parentPath $bundleName
# 执行当前脚本步骤。
New-Item -ItemType Directory -Force -Path $bundleRoot | Out-Null

# 执行当前脚本步骤。
$sourceEnv = $EnvFile
# 判断条件后执行对应操作。
if (-not [string]::IsNullOrWhiteSpace($sourceEnv) -and -not [System.IO.Path]::IsPathRooted($sourceEnv)) {
    # 执行当前脚本步骤。
    $sourceEnv = Join-Path $projectRoot $sourceEnv
# 结束当前控制块。
}
# 执行当前脚本步骤。
$envPath = Join-Path $bundleRoot ".env.offline"
# 执行当前脚本步骤。
$envResult = New-OfflineEnv -Destination $envPath -Source $sourceEnv -UseAi:$IncludeAi -UseHarness:$IncludeHarness
# 执行当前脚本步骤。
$runtimeHarnessEnabled = Get-DeploymentEnvValue -Path $envPath -Key 'IOT_AI_HARNESS_ENABLED'
# 判断条件后执行对应操作。
if ($runtimeHarnessEnabled -eq 'true') { $IncludeHarness = $true }
# 执行当前脚本步骤。
$composeBase = @(
    # 执行当前脚本步骤。
    "compose", "--project-name", "iot-platform-offline-build",
    "--env-file", $envPath,
    "-f", (Join-Path $projectRoot "compose.yaml"),
    "-f", (Join-Path $projectRoot "compose.offline.yaml")
# 执行当前脚本步骤。
)

# 执行当前脚本步骤。
$profiles = New-Object 'System.Collections.Generic.List[string]'
# 判断条件后执行对应操作。
if ($IncludeHarness) { [void]$profiles.Add("harness") }
# 执行当前脚本步骤。
$profileArguments = New-Object 'System.Collections.Generic.List[string]'
# 遍历数据并执行循环体。
foreach ($profile in $profiles) {
    # 执行当前脚本步骤。
    [void]$profileArguments.Add("--profile")
    # 执行当前脚本步骤。
    [void]$profileArguments.Add($profile)
# 结束当前控制块。
}

# 执行当前脚本步骤。
$ollamaArchive = $null
# 执行当前脚本步骤。
$ollamaVolumeName = $null
# 执行当前脚本步骤。
$ollamaStarted = $false

# 执行当前脚本步骤。
try {
    # 执行当前脚本步骤。
    Invoke-Checked -Arguments ($composeBase + $profileArguments.ToArray() + @("config", "--quiet"))
    # 执行当前脚本步骤。
    $pullServices = @(
        # 执行当前脚本步骤。
        "postgres", "postgres-wal-init", "redis", "minio", "minio-dr",
        "redpanda", "redpanda-init", "clickhouse", "emqx", "prometheus",
        "grafana", "loki", "ollama", "weaviate", "ops-init", "alertmanager",
        "alloy", "node-exporter"
    # 执行当前脚本步骤。
    )
    # 执行当前脚本步骤。
    Invoke-Checked -Arguments ($composeBase + @("pull") + $pullServices)
    # 执行当前脚本步骤。
    Invoke-Checked -Arguments ($composeBase + @("build", "--pull", "platform-api", "platform-web", "backup-service"))

    # 只归档知识库嵌入模型；DeepSeek API 不携带模型权重。
    # 判断条件后执行对应操作。
    if (-not $SkipOllamaModel) {
        # 执行当前脚本步骤。
        $ollamaStarted = $true
        # 执行当前脚本步骤。
        Invoke-Checked -Arguments ($composeBase + @("up", "-d", "--no-deps", "ollama"))
        # 执行当前脚本步骤。
        $ollamaReady = $false
        # 遍历数据并执行循环体。
        for ($i = 0; $i -lt 30; $i++) {
            # 执行当前脚本步骤。
            & docker @($composeBase + @("exec", "-T", "ollama", "ollama", "list")) *> $null
            # 判断条件后执行对应操作。
            if ($LASTEXITCODE -eq 0) { $ollamaReady = $true; break }
            # 执行当前脚本步骤。
            Start-Sleep -Seconds 2
        # 结束当前控制块。
        }
        # 判断条件后执行对应操作。
        if (-not $ollamaReady) { throw "Ollama 容器未在规定时间内就绪。" }
        # 执行当前脚本步骤。
        Invoke-Checked -Arguments ($composeBase + @("exec", "-T", "ollama", "ollama", "pull", $OllamaEmbeddingModel))
        # 判断条件后执行对应操作。
        $ollamaSourceVolume = "iot-platform-offline-build_ollama-data"
        # 执行当前脚本步骤。
        $ollamaVolumeName = "iot-platform_ollama-data"
        # 执行当前脚本步骤。
        Invoke-Checked -Arguments @(
            # 执行当前脚本步骤。
            "run", "--rm", "--pull", "never",
            "--mount", "type=volume,source=$ollamaSourceVolume,target=/src,readonly",
            "--mount", "type=bind,source=$bundleRoot,target=/backup",
            "--mount", "type=bind,source=$scriptDir/lib,target=/helpers,readonly",
            "alpine:3.22", "sh", "/helpers/export-embedding-model.sh", "/src", "/backup/ollama-data.tgz"
        # 执行当前脚本步骤。
        )
        # 执行当前脚本步骤。
        $ollamaArchive = "ollama-data.tgz"
        # 执行当前脚本步骤。
        $modelHash = (Get-FileHash -LiteralPath (Join-Path $bundleRoot $ollamaArchive) -Algorithm SHA256).Hash.ToLowerInvariant()
        # 执行当前脚本步骤。
        Write-Utf8NoBom -Path (Join-Path $bundleRoot "ollama-data.tgz.sha256") -Lines @("$modelHash  $ollamaArchive")
    # 结束当前控制块。
    } else {
        # 执行当前脚本步骤。
        Write-Warning "已跳过模型打包：目标机必须预先具有 nomic-embed-text；否则知识库不可用。"
    # 结束当前控制块。
    }

    # 判断条件后执行对应操作。
    if ($IncludeHarness) {
        # 执行当前脚本步骤。
        Ensure-HarnessSource -ProjectRoot $projectRoot
        # 执行当前脚本步骤。
        Invoke-Checked -Arguments ($composeBase + @("--profile", "harness", "build", "--pull", "deepseek-harness"))
    # 结束当前控制块。
    }

    # 执行当前脚本步骤。
    Copy-Item -LiteralPath (Join-Path $projectRoot "compose.yaml") -Destination $bundleRoot
    # 执行当前脚本步骤。
    Copy-Item -LiteralPath (Join-Path $projectRoot "compose.offline.yaml") -Destination $bundleRoot
    # 执行当前脚本步骤。
    Copy-Item -LiteralPath (Join-Path $projectRoot "compose.access.yaml") -Destination $bundleRoot
    # 执行当前脚本步骤。
    Copy-Item -LiteralPath (Join-Path $projectRoot "docs/EDGE_AND_GATEWAY.md") -Destination $bundleRoot
    # 执行当前脚本步骤。
    Copy-Item -LiteralPath (Join-Path $projectRoot "docs/DEPLOYMENT.md") -Destination $bundleRoot
    # 执行当前脚本步骤。
    Copy-Item -LiteralPath (Join-Path $projectRoot "deploy") -Destination $bundleRoot -Recurse
    # 执行当前脚本步骤。
    New-Item -ItemType Directory -Force -Path (Join-Path $bundleRoot "scripts") | Out-Null
    # 执行当前脚本步骤。
    New-Item -ItemType Directory -Force -Path (Join-Path $bundleRoot "scripts/lib") | Out-Null
    # 执行当前脚本步骤。
    Copy-Item -LiteralPath (Join-Path $scriptDir "lib/docker-bootstrap.sh") -Destination (Join-Path $bundleRoot "scripts/lib")
    # 执行当前脚本步骤。
    Copy-Item -LiteralPath (Join-Path $scriptDir "lib/restore-ollama-models.sh") -Destination (Join-Path $bundleRoot "scripts/lib")
    # 判断条件后执行对应操作。
    if (-not $SkipDockerRuntime) {
        # 执行当前脚本步骤。
        $runtimeArch = (& docker info --format '{{.Architecture}}').Trim()
        # 判断条件后执行对应操作。
        if ($LASTEXITCODE -ne 0) { throw '无法获取打包用 Docker 架构。' }
        # 执行当前脚本步骤。
        Save-LinuxDockerRuntime -Directory (Join-Path $bundleRoot 'docker-runtime') -Architecture $runtimeArch
        # 判断条件后执行对应操作。
        if ($TargetOS -eq 'openeuler-24.03-lts-sp4') {
            # 执行当前脚本步骤。
            Save-OpenEulerPackages -Directory (Join-Path $bundleRoot 'docker-runtime/packages') -Architecture $runtimeArch -Script (Join-Path $scriptDir 'lib/prepare-openeuler-packages.sh')
        # 结束当前控制块。
        }
        # 判断条件后执行对应操作。
        if ($DockerPackagesDir) {
            # 判断条件后执行对应操作。
            if (-not (Test-Path -LiteralPath $DockerPackagesDir -PathType Container)) { throw 'DockerPackagesDir 不存在。' }
            # 执行当前脚本步骤。
            Copy-Item -LiteralPath $DockerPackagesDir -Destination (Join-Path $bundleRoot 'docker-runtime/packages') -Recurse
            # 执行当前脚本步骤。
            Get-ChildItem -LiteralPath (Join-Path $bundleRoot 'docker-runtime/packages') -File -Recurse |
                Where-Object { $_.Extension -ne '.sha256' -and ($_.Extension -in @('.rpm', '.deb') -or $_.Name -like 'RPM-GPG-KEY-*' -or $_.Directory.Name -eq 'repodata') } | ForEach-Object {
                    # 执行当前脚本步骤。
                    [IO.File]::WriteAllText(($_.FullName + '.sha256'), (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant())
                # 结束当前控制块。
                }
        # 结束当前控制块。
        }
    # 结束当前控制块。
    }
    # 遍历数据并执行循环体。
    foreach ($runtimeScript in @(
        # 执行当前脚本步骤。
        "deploy-offline.ps1",
        "deploy-offline-windows.ps1",
        "deploy-offline.sh",
        "deploy-offline-linux.sh",
        "deploy-offline-macos.sh"
    # 执行当前脚本步骤。
    )) {
        # 执行当前脚本步骤。
        Copy-Item -LiteralPath (Join-Path $scriptDir $runtimeScript) -Destination (Join-Path $bundleRoot "scripts")
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    Copy-Item -LiteralPath (Join-Path $projectRoot "docs\OFFLINE_DEPLOYMENT.md") -Destination (Join-Path $bundleRoot "OFFLINE_DEPLOYMENT.md")

    # 执行当前脚本步骤。
    $images = @(Invoke-Captured -Arguments ($composeBase + $profileArguments.ToArray() + @("config", "--images")) | Sort-Object -Unique)
    # 判断条件后执行对应操作。
    if ($images.Count -eq 0) { throw "没有解析出可导出的镜像。" }
    # 遍历数据并执行循环体。
    foreach ($image in $images) {
        # 执行当前脚本步骤。
        & docker image inspect $image *> $null
        # 判断条件后执行对应操作。
        if ($LASTEXITCODE -ne 0) {
            # 执行当前脚本步骤。
            throw "镜像不存在，无法导出：$image"
        # 结束当前控制块。
        }
    # 结束当前控制块。
    }

    # 执行当前脚本步骤。
    $archivePath = Join-Path $bundleRoot "images.tar"
    # 执行当前脚本步骤。
    Invoke-Checked -Arguments (@("save", "-o", $archivePath) + $images)
    # 执行当前脚本步骤。
    $hash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    # 执行当前脚本步骤。
    Write-Utf8NoBom -Path (Join-Path $bundleRoot "images.tar.sha256") -Lines @("$hash  images.tar")
    # 执行当前脚本步骤。
    Write-Utf8NoBom -Path (Join-Path $bundleRoot "profiles.txt") -Lines $profiles.ToArray()
    # 判断条件后执行对应操作。
    if ($ollamaVolumeName) {
        # 执行当前脚本步骤。
        Write-Utf8NoBom -Path (Join-Path $bundleRoot "ollama-volume.txt") -Lines @($ollamaVolumeName)
    # 结束当前控制块。
    }

    # 执行当前脚本步骤。
    $commit = "unknown"
    # 判断条件后执行对应操作。
    if (Get-Command git -ErrorAction SilentlyContinue) {
        # 执行当前脚本步骤。
        $commit = (& git -C $projectRoot rev-parse HEAD 2>$null | Select-Object -First 1)
        # 判断条件后执行对应操作。
        if ($LASTEXITCODE -ne 0) { $commit = "unknown" }
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    $manifest = [ordered]@{
        # 执行当前脚本步骤。
        format = 1
        # 执行当前脚本步骤。
        project = "iot-platform"
        # 执行当前脚本步骤。
        targetOS = $TargetOS
        # 执行当前脚本步骤。
        createdAtUtc = (Get-Date).ToUniversalTime().ToString("o")
        # 执行当前脚本步骤。
        gitCommit = ([string]$commit).Trim()
        # 执行当前脚本步骤。
        profiles = $profiles.ToArray()
        # 执行当前脚本步骤。
        images = $images
        # 执行当前脚本步骤。
        imageArchive = "images.tar"
        # 执行当前脚本步骤。
        imageArchiveSha256 = $hash
        # 执行当前脚本步骤。
        envFile = ".env.offline"
        # 执行当前脚本步骤。
        composeFiles = @("compose.yaml", "compose.offline.yaml")
        # 执行当前脚本步骤。
        aiProvider = "deepseek"
        aiModel = $DeepSeekModel
        aiRequiresInternet = $true
        ollamaModel = $null
        # 执行当前脚本步骤。
        ollamaEmbeddingModel = if ($ollamaArchive) { $OllamaEmbeddingModel } else { $null }
        # 执行当前脚本步骤。
        ollamaArchive = $ollamaArchive
        # 执行当前脚本步骤。
        ollamaVolume = $ollamaVolumeName
        # 执行当前脚本步骤。
        generatedCredentials = [bool]$envResult.Generated
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    $manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $bundleRoot "manifest.json") -Encoding UTF8

    # 执行当前脚本步骤。
    Write-Host ""
    # 执行当前脚本步骤。
    Write-Host "离线包已生成：$bundleRoot" -ForegroundColor Green
    # 执行当前脚本步骤。
    Write-Host "镜像数量：$($images.Count)"
    # 执行当前脚本步骤。
    Write-Host "镜像包大小：$([Math]::Round((Get-Item $archivePath).Length / 1GB, 2)) GB"
    # 执行当前脚本步骤。
    Write-Host "部署方式：按服务器系统运行 scripts/deploy-offline-windows.ps1、deploy-offline-macos.sh 或 deploy-offline-linux.sh"
    # 判断条件后执行对应操作。
    if ($envResult.Generated) {
        # 执行当前脚本步骤。
        Write-Host "自动生成的凭据：$($envResult.CredentialPath)" -ForegroundColor Yellow
    # 结束当前控制块。
    }
# 结束当前控制块。
} finally {
    # 判断条件后执行对应操作。
    if ($ollamaStarted) {
        # 执行当前脚本步骤。
        & docker @($composeBase + @("stop", "ollama")) *> $null
        # 判断条件后执行对应操作。
        if ($LASTEXITCODE -ne 0) { Write-Warning "打包用 Ollama 未能停止，请检查 iot-platform-offline-build 项目。" }
    # 结束当前控制块。
    }
# 结束当前控制块。
}
