# Shared by local, online and offline entry points. Never dot-source a dotenv file.
$script:IotDeploymentEnvCommentsPath = Join-Path $PSScriptRoot 'env-comments.tsv'

function Get-DeploymentEnvComment {
    param([Parameter(Mandatory)][string]$Key)
    foreach ($line in [IO.File]::ReadAllLines($script:IotDeploymentEnvCommentsPath)) {
        $parts = $line.Split(@("`t"), 2, [StringSplitOptions]::None)
        if ($parts.Count -eq 2 -and $parts[0] -eq $Key) { return $parts[1] }
    }
    return "自定义配置项 $Key"
}

function Add-DeploymentEnvComments {
    param([Parameter(Mandatory)][string]$Path)
    $fullPath = [IO.Path]::GetFullPath($Path)
    $result = New-Object 'System.Collections.Generic.List[string]'
    foreach ($line in [IO.File]::ReadAllLines($fullPath)) {
        if ($line.StartsWith('# 配置说明：')) { continue }
        if ($line -match '^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=') {
            [void]$result.Add('# 配置说明：' + (Get-DeploymentEnvComment -Key $matches[1]))
        }
        [void]$result.Add($line.TrimEnd("`r"))
    }
    [IO.File]::WriteAllText($fullPath, ($result -join "`n") + "`n", (New-Object Text.UTF8Encoding($false)))
}

function New-DeploymentSecret {
    $bytes = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $generator.GetBytes($bytes) } finally { $generator.Dispose() }
    return [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
}

# The environment file holds credentials: on Windows only the current user may
# read or change it, the same restriction cluster-up.ps1 applies to the deploy key
# (chmod 600 on Linux). Other platforms keep the umask the file was created with.
function Protect-DeploymentEnvFile {
    param([Parameter(Mandatory)][string]$Path)
    if ($env:OS -ne 'Windows_NT' -or -not (Test-Path -LiteralPath $Path -PathType Leaf)) { return }
    & icacls ([IO.Path]::GetFullPath($Path)) /inheritance:r /grant:r "$($env:USERNAME):(R,W)" | Out-Null
    if ($LASTEXITCODE -ne 0) { Write-Warning "无法收紧配置文件权限：$Path" }
}

function Ensure-DeploymentEnv {
    param([Parameter(Mandatory)][string]$Path)

    if (Test-Path -LiteralPath $Path -PathType Leaf) {
        Protect-DeploymentEnvFile -Path $Path
        Write-Host "保留已有配置：$Path"
        return
    }
    if (Test-Path -LiteralPath $Path) { throw "配置路径不是文件：$Path" }
    $values = [ordered]@{
        SERVICE_ADMIN_USER = 'admin'
        SERVICE_ADMIN_PASSWORD = 'admin123'
        POSTGRES_PASSWORD = 'admin123'
        REDIS_PASSWORD = 'admin123'
        CLICKHOUSE_PASSWORD = 'admin123'
        MINIO_ROOT_USER = 'admin'
        MINIO_ROOT_PASSWORD = 'admin123'
        MINIO_DR_ROOT_USER = 'admin'
        MINIO_DR_ROOT_PASSWORD = 'admin123'
        EMQX_DASHBOARD_USER = 'admin'
        EMQX_DASHBOARD_PASSWORD = 'admin123'
        GRAFANA_ADMIN_USER = 'admin'
        GRAFANA_ADMIN_PASSWORD = 'admin123'
        IOT_JWT_SECRET = (New-DeploymentSecret)
        IOT_ADMIN_USER = 'admin'
        IOT_ADMIN_PASSWORD = 'admin123'
        IOT_ADMIN_TENANTS = 'tenant_001'
        IOT_MQTT_TOOL_USERNAME = 'admin'
        IOT_MQTT_TOOL_PASSWORD = 'admin123'
        IOT_KAFKA_SASL_USERNAME = 'admin'
        IOT_KAFKA_SASL_PASSWORD = 'admin123'
        IOT_KAFKA_SASL_MECHANISM = 'SCRAM-SHA-256'
        IOT_KAFKA_ADMIN_URL = 'http://redpanda:9644'
        IOT_KAFKA_ADMIN_USERNAME = 'admin'
        IOT_KAFKA_ADMIN_PASSWORD = 'admin123'
        IOT_KAFKA_ADVERTISED_HOST = '127.0.0.1'
        IOT_KAFKA_PUBLIC_BROKERS = '127.0.0.1:19092'
        IOT_AI_HARNESS_TOKEN = (New-DeploymentSecret)
        IOT_EMBEDDING_API_KEY = ''
        IOT_BACKUP_ADMIN_TOKEN = (New-DeploymentSecret)
        GB26875_CONTROL_TOKEN = (New-DeploymentSecret)
        IOT_HTTP_ADDR = ':8081'
        IOT_WEB_PORT = '8080'
        IOT_API_PORT = '8081'
        IOT_CORS_ALLOWED_ORIGINS = 'http://localhost:8080,http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173'
        IOT_AI_PROVIDER = 'deepseek'
        IOT_AI_BASE_URL = 'https://api.deepseek.com'
        IOT_AI_MODEL = 'deepseek-flash'
        IOT_AI_API_KEY = ''
        DEEPSEEK_API_KEY = ''
        DEEPSEEK_BASE_URL = 'https://api.deepseek.com'
        IOT_AI_HARNESS_URL = 'http://deepseek-harness:8091'
        IOT_AI_HARNESS_MCP_URL = 'http://platform-api:8080/mcp/harness'
        IOT_AI_HARNESS_PROVIDER = 'deepseek-official'
        IOT_AI_HARNESS_MODEL = 'deepseek-flash'
        IOT_BACKUP_TIME = '00:05'
        IOT_BACKUP_ENABLED = 'true'
        IOT_BACKUP_TIMEZONE = 'Asia/Shanghai'
    }
    $fullPath = [IO.Path]::GetFullPath($Path)
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($fullPath)) | Out-Null
    $lines = @('# 此配置只在首次部署时生成，后续运行不会轮换凭据。', '# 文件包含敏感凭据，请勿提交到 Git 或公开分享。')
    foreach ($entry in $values.GetEnumerator()) { $lines += "$($entry.Key)=$($entry.Value)" }
    # CreateNew fails safely if another invocation created the file meanwhile.
    $stream = New-Object IO.FileStream($fullPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
    try {
        $encoding = New-Object Text.UTF8Encoding($false)
        $bytes = $encoding.GetBytes(($lines -join "`n") + "`n")
        $stream.Write($bytes, 0, $bytes.Length)
    } finally { $stream.Dispose() }
    Protect-DeploymentEnvFile -Path $fullPath
    Write-Host "已生成配置：$Path（服务工具账号使用配置默认值，内部令牌随机生成）。"
}

function Get-DeploymentEnvValue {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Key)
    # Match Compose's precedence: exported process variables override --env-file.
    $processValue = [Environment]::GetEnvironmentVariable($Key, 'Process')
    if ($null -ne $processValue) { return $processValue }
    $value = ''
    foreach ($line in [IO.File]::ReadAllLines([IO.Path]::GetFullPath($Path))) {
        if ($line -match ('^\s*(?:export\s+)?' + [Regex]::Escape($Key) + '\s*=\s*(.*)$')) {
            $candidate = $matches[1].Trim()
            if ($candidate.StartsWith('"') -or $candidate.StartsWith("'")) {
                $quote = $candidate.Substring(0, 1)
                $end = $candidate.IndexOf($quote, 1)
                if ($end -ge 1) { $candidate = $candidate.Substring(1, $end - 1) }
            } else { $candidate = ($candidate -replace '\s+#.*$', '').TrimEnd() }
            $value = $candidate
        }
    }
    return $value
}

function Set-DeploymentEnvValue {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Key, [AllowEmptyString()][string]$Value)
    if ($Key -notmatch '^[A-Za-z_][A-Za-z0-9_]*$' -or $Value -match "[\r\n]") { throw '环境变量名称或值包含不支持的字符。' }
    $found = $false
    $lines = @([IO.File]::ReadAllLines([IO.Path]::GetFullPath($Path)) | ForEach-Object {
        if ($_ -match ('^\s*(?:export\s+)?' + [Regex]::Escape($Key) + '\s*=')) {
            if (-not $found) { "$Key=$Value"; $found = $true }
        } else { $_ }
    })
    if (-not $found) { $lines += "$Key=$Value" }
    [IO.File]::WriteAllText([IO.Path]::GetFullPath($Path), ($lines -join "`n") + "`n", (New-Object Text.UTF8Encoding($false)))
}

function Ensure-HarnessSource {
    param([Parameter(Mandatory)][string]$ProjectRoot)
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) { throw 'Harness 需要 Git。' }
    $revision = [IO.File]::ReadAllText((Join-Path $ProjectRoot 'deploy/deepseek-harness/REVISION')).Trim()
    if ($revision -notmatch '^[0-9a-f]{40}$') { throw 'Harness REVISION 无效。' }
    $target = Join-Path $ProjectRoot 'upstream/deepseek-harness'
    $configureTarget = {
        & git -C $target config core.autocrlf false
        if ($LASTEXITCODE -ne 0) { throw '无法配置 Harness 换行转换。' }
        & git -C $target config core.fileMode false
        if ($LASTEXITCODE -ne 0) { throw '无法配置 Harness 文件权限检查。' }
        # Preserve exact blobs, including with Git < 2.10's text=auto/eol bug.
        $info = Join-Path $target '.git/info'
        [IO.Directory]::CreateDirectory($info) | Out-Null
        $attributes = Join-Path $info 'attributes'
        $marker = '# TorchLink: preserve Harness repository bytes'
        $existing = if (Test-Path -LiteralPath $attributes) { [IO.File]::ReadAllText($attributes) } else { '' }
        if ($existing -notmatch ('(?m)^' + [Regex]::Escape($marker) + '\r?$')) {
            [IO.File]::AppendAllText($attributes, "`n$marker`n* -text -eol`n", (New-Object Text.UTF8Encoding($false)))
        }
    }
    $cloneTarget = {
        [IO.Directory]::CreateDirectory((Split-Path -Parent $target)) | Out-Null
        & git -c http.version=HTTP/1.1 -c core.autocrlf=false -c core.fileMode=false clone --no-checkout --depth 1 'https://github.com/deepseek-ai/deepseek-harness.git' $target
        if ($LASTEXITCODE -ne 0) { throw 'Harness 源码下载失败。' }
        & $configureTarget
        & git -C $target reset --hard HEAD *> $null
        if ($LASTEXITCODE -ne 0) { throw 'Harness 源码检出失败。' }
        & git -C $target clean -fd *> $null
        if ($LASTEXITCODE -ne 0) { throw 'Harness 新克隆目录整理失败。' }
    }
    if (-not (Test-Path -LiteralPath (Join-Path $target '.git'))) {
        & $cloneTarget
    }
    & $configureTarget
    $changes = @(& git -C $target status --porcelain)
    if ($LASTEXITCODE -ne 0) { throw '无法检查 Harness 源码状态。' }
    if ($changes.Count -gt 0) {
        $timestamp = Get-Date -Format 'yyyyMMdd-HHmmss'
        $backup = "$target.backup-$timestamp"
        $suffix = 0
        while (Test-Path -LiteralPath $backup) {
            $suffix++
            $backup = "$target.backup-$timestamp-$suffix"
        }
        Move-Item -LiteralPath $target -Destination $backup
        Write-Host "DeepSeek Harness 源码目录存在修改，已备份到：$backup"
        & $cloneTarget
    }
    $current = & git -C $target rev-parse HEAD
    if ($LASTEXITCODE -ne 0) { throw '无法读取 Harness 提交。' }
    if ($current.Trim() -ne $revision) {
        & git -C $target -c http.version=HTTP/1.1 fetch --depth 1 origin $revision
        if ($LASTEXITCODE -ne 0) { throw 'Harness 提交下载失败。' }
        & git -C $target checkout --detach $revision
        if ($LASTEXITCODE -ne 0) { throw 'Harness 提交切换失败。' }
    }
    $current = & git -C $target rev-parse HEAD
    if ($LASTEXITCODE -ne 0 -or $current.Trim() -ne $revision) { throw 'Harness 提交校验失败。' }
    [IO.File]::WriteAllText((Join-Path $ProjectRoot 'upstream/deepseek-harness.revision'), "$revision`n", (New-Object Text.UTF8Encoding($false)))
}

function Assert-DockerAvailable {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw '找不到 docker，请先安装 Docker Engine/Desktop 和 Compose v2。'
    }
    & docker info *> $null
    if ($LASTEXITCODE -ne 0) { throw 'Docker Engine 不可用，请先启动 Docker。' }
    & docker compose version *> $null
    if ($LASTEXITCODE -ne 0) { throw 'Docker Compose v2 不可用，请先安装或升级。' }
}

function Invoke-DockerChecked {
    param([Parameter(Mandatory)][string[]]$Arguments)
    & docker @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker 命令失败，退出码 $LASTEXITCODE。" }
}

# Strips the tag from an image reference; a registry port such as host:5000/name is kept.
function Get-DeploymentImageRepository {
    param([Parameter(Mandatory)][string]$Image)
    $image = ($Image -split '@')[0]
    $last = ($image -split '/')[-1]
    if ($last.Contains(':')) { return $image.Substring(0, $image.LastIndexOf(':')) }
    return $image
}

# Tags the image each running container of the services uses as <repository>:<Tag>
# and returns the commands that put it back. A first deployment has nothing to keep.
function Save-DeploymentRollback {
    param([Parameter(Mandatory)][string]$Tag, [Parameter(Mandatory)][string[]]$Compose, [Parameter(Mandatory)][string[]]$Services)
    $commands = @()
    foreach ($service in $Services) {
        $container = @(& docker @($Compose + @('ps', '-a', '-q', $service)) 2>$null | Select-Object -First 1)
        if ($LASTEXITCODE -ne 0 -or -not $container -or -not "$($container[0])".Trim()) { continue }
        $info = "$(& docker inspect --format '{{.Image}} {{.Config.Image}}' "$($container[0])".Trim() 2>$null)".Trim()
        if ($LASTEXITCODE -ne 0 -or -not $info) { continue }
        $id, $name = $info -split ' ', 2
        if (-not $id -or -not $name) { continue }
        $repository = Get-DeploymentImageRepository -Image $name
        & docker image tag $id "${repository}:$Tag" *> $null
        if ($LASTEXITCODE -eq 0) { $commands += "docker image tag ${repository}:$Tag $name" }
    }
    return ,$commands
}

function Write-DeploymentRollback {
    param([string[]]$Commands, [Parameter(Mandatory)][string[]]$Compose)
    Write-Warning '服务未在时限内全部启动或通过健康检查。用相同的 Compose 项目和配置参数检查 ps / logs。'
    if (-not $Commands -or $Commands.Count -eq 0) { return }
    Write-Warning '如需回到本次部署前的镜像，依次执行：'
    foreach ($command in $Commands) { Write-Host $command }
    Write-Host ('docker ' + (($Compose | ForEach-Object { if ($_ -match '\s') { "'$_'" } else { $_ } }) -join ' ') + ' up -d --no-build --pull never')
    Write-Warning '数据库迁移只向前执行：新版本已完成迁移时旧镜像可能无法启动，需按 docs/OPERATIONS.md 用升级前的整库备份恢复。'
}

# Keeps the two newest prev-* tags of each repository named in the commands.
function Remove-OldDeploymentRollback {
    param([string[]]$Commands)
    foreach ($command in @($Commands)) {
        if (-not $command) { continue }
        $repository = Get-DeploymentImageRepository -Image (($command -replace '^docker image tag ', '') -split ' ')[0]
        $old = @(& docker image ls --format '{{.Tag}}' $repository 2>$null | Where-Object { $_ -like 'prev-*' } | Sort-Object -Descending | Select-Object -Skip 2)
        foreach ($tag in $old) { & docker image rm "${repository}:$tag" *> $null }
    }
}

function Wait-DeploymentHttp {
    param([Parameter(Mandatory)][string]$Url, [int]$TimeoutSeconds = 180)
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $Url -TimeoutSec 5
            if ($response.StatusCode -eq 200) { Write-Host "健康检查通过：$Url"; return }
        } catch { }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "健康检查超时：$Url。请用相同的 Compose 项目和配置参数检查 ps / logs。"
}

function Test-DeploymentEnvKey {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Key)
    if (-not (Test-Path -LiteralPath $Path)) { return $false }
    return [bool](Select-String -LiteralPath $Path -Pattern ('^\s*(export\s+)?' + [Regex]::Escape($Key) + '\s*=') -Quiet)
}

# Gives an existing deployment a /metrics token once; an operator who set it
# empty keeps that choice.
function Ensure-MetricsToken {
    param([Parameter(Mandatory)][string]$Path)
    if (-not (Test-DeploymentEnvKey -Path $Path -Key 'IOT_METRICS_TOKEN')) {
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_METRICS_TOKEN' -Value (New-DeploymentSecret)
    }
}

# Replaces missing or published placeholder service tokens (Harness gateway,
# backup restore API) with generated secrets; the platform refuses
# placeholders outside development mode.
function Ensure-ServiceTokens {
    param([Parameter(Mandatory)][string]$Path)
    foreach ($key in @('IOT_AI_HARNESS_TOKEN', 'IOT_BACKUP_ADMIN_TOKEN')) {
        $value = [string](Get-DeploymentEnvValue -Path $Path -Key $key)
        if ($value.Length -lt 32 -or $value -like '*change-me*') {
            Set-DeploymentEnvValue -Path $Path -Key $key -Value (New-DeploymentSecret)
        }
    }
}

# Knowledge vectors and reranking use the embedding / reranker services
# deployed with the platform. Earlier releases defaulted to the DashScope
# cloud API; that default is replaced, an operator-chosen API is kept.
function Set-EmbeddingDeploymentEnv {
    param([Parameter(Mandatory)][string]$Path, [string]$EmbeddingUrl = 'http://embedding:8080/v1', [string]$RerankUrl = 'http://reranker:8080', [string]$ExtraHosts = '')
    $url = Get-DeploymentEnvValue -Path $Path -Key 'IOT_EMBEDDING_URL'
    $model = Get-DeploymentEnvValue -Path $Path -Key 'IOT_EMBEDDING_MODEL'
    if (-not $url -or $url -eq 'https://dashscope.aliyuncs.com/compatible-mode/v1' -or $url -eq 'http://embedding:8080/v1' -or $url -match '^http://[^/]+:18093/v1$') {
        if ($url -eq 'https://dashscope.aliyuncs.com/compatible-mode/v1') {
            Write-Warning '知识库向量计算改为随平台部署的 embedding 服务（bge-m3），已有文档会在后台自动重建索引。若“模型管理”里保存过云端向量配置，请在该页切换为本地服务。'
        }
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_EMBEDDING_URL' -Value $EmbeddingUrl
        if (-not $model -or $model -eq 'text-embedding-v4') { Set-DeploymentEnvValue -Path $Path -Key 'IOT_EMBEDDING_MODEL' -Value 'bge-m3' }
    }
    foreach ($setting in @{ IOT_EMBEDDING_DIMENSIONS='1024'; IOT_EMBEDDING_BATCH_SIZE='10' }.GetEnumerator()) {
        if (-not (Get-DeploymentEnvValue -Path $Path -Key $setting.Key)) { Set-DeploymentEnvValue -Path $Path -Key $setting.Key -Value $setting.Value }
    }
    if (-not (Test-DeploymentEnvKey -Path $Path -Key 'IOT_EMBEDDING_API_KEY')) { Set-DeploymentEnvValue -Path $Path -Key 'IOT_EMBEDDING_API_KEY' -Value '' }
    # An explicitly empty IOT_RERANK_URL turns reranking off and is kept.
    if (-not (Test-DeploymentEnvKey -Path $Path -Key 'IOT_RERANK_URL') -or (Get-DeploymentEnvValue -Path $Path -Key 'IOT_RERANK_URL') -match '^http://[^/]+:18094$') {
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_RERANK_URL' -Value $RerankUrl
    }
    $hosts = 'embedding,reranker'
    if ($ExtraHosts) { $hosts = "$hosts,$ExtraHosts" }
    Set-DeploymentEnvValue -Path $Path -Key 'IOT_LOCAL_AI_HOSTS' -Value $hosts
}

function Set-DeepSeekDeploymentEnv {
    param([string]$Path, [string]$Model = 'deepseek-flash')
    $oldProvider = Get-DeploymentEnvValue -Path $Path -Key 'IOT_AI_PROVIDER'
    $key = Get-DeploymentEnvValue -Path $Path -Key 'DEEPSEEK_API_KEY'
    if (-not $key -and $oldProvider -eq 'deepseek') { $key = Get-DeploymentEnvValue -Path $Path -Key 'IOT_AI_API_KEY' }
    if ($key -match "['`r`n]") { throw 'DeepSeek API Key 含不支持的引号或换行。' }
    foreach ($setting in ([ordered]@{
        DEEPSEEK_API_KEY="'$key'"; IOT_AI_API_KEY=''; DEEPSEEK_BASE_URL='https://api.deepseek.com';
        IOT_AI_PROVIDER='deepseek'; IOT_AI_BASE_URL='https://api.deepseek.com'; IOT_AI_MODEL=$Model;
        IOT_AI_HARNESS_PROVIDER='deepseek-official'; IOT_AI_HARNESS_MODEL=$Model
    }).GetEnumerator()) { Set-DeploymentEnvValue -Path $Path -Key $setting.Key -Value ([string]$setting.Value) }
    if (-not $key) { Write-Warning '请填写 DEEPSEEK_API_KEY，或启动后在“模型管理”填写密钥并保存（连接测试可选）；未配置前 AI 功能不可用。' }
}

# Kafka's external listener binds to 127.0.0.1 by default. An existing
# environment that already handed consumers a non-local address keeps it open.
function Ensure-KafkaBindAddress {
    param([string]$Path)
    if (-not [string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $Path -Key 'KAFKA_BIND_ADDRESS'))) { return }
    foreach ($broker in "$(Get-DeploymentEnvValue -Path $Path -Key 'IOT_KAFKA_PUBLIC_BROKERS')" -split ',') {
        $broker = $broker.Trim()
        if (-not $broker -or $broker -match '^(127\.0\.0\.1|localhost|\[::1\]):') { continue }
        Set-DeploymentEnvValue -Path $Path -Key 'KAFKA_BIND_ADDRESS' -Value '0.0.0.0'
        Write-Host "Kafka 对外地址为 $broker，保留外部监听（KAFKA_BIND_ADDRESS=0.0.0.0）。"
        return
    }
}

function Ensure-EmqxAdminEnv {
    param([string]$Path, [string]$DefaultUrl)
    $apiKey = Get-DeploymentEnvValue -Path $Path -Key 'IOT_EMQX_API_KEY'
    $apiSecret = Get-DeploymentEnvValue -Path $Path -Key 'IOT_EMQX_API_SECRET'
    if ([string]::IsNullOrWhiteSpace($apiKey) -and [string]::IsNullOrWhiteSpace($apiSecret)) {
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_EMQX_API_KEY' -Value (New-DeploymentSecret)
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_EMQX_API_SECRET' -Value (New-DeploymentSecret)
    } elseif ([string]::IsNullOrWhiteSpace($apiKey) -or [string]::IsNullOrWhiteSpace($apiSecret)) {
        throw 'EMQX 管理凭据不完整，请同时配置 IOT_EMQX_API_KEY 和 IOT_EMQX_API_SECRET。'
    }
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $Path -Key 'IOT_EMQX_API_URL'))) {
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_EMQX_API_URL' -Value $DefaultUrl
    }
}

# Turns the monitoring stack (compose profile "ops") on or off. Off clears the
# API's component URLs, so the operations center reports them as not deployed.
function Set-OpsModule {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][ValidateSet('on', 'off')][string]$State)
    $profiles = @(@("$(Get-DeploymentEnvValue -Path $Path -Key 'COMPOSE_PROFILES')" -split ',') | ForEach-Object { $_.Trim() } | Where-Object { $_ -and $_ -ne 'ops' })
    $urls = [ordered]@{ IOT_OPS_PROMETHEUS_URL = 'http://prometheus:9090'; IOT_OPS_LOKI_URL = 'http://loki:3100'; IOT_OPS_GRAFANA_URL = 'http://grafana:3000'; IOT_OPS_ALERTMANAGER_URL = 'http://alertmanager:9093' }
    if ($State -eq 'on') {
        $profiles += 'ops'
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_OPS_MODULE' -Value 'on'
        foreach ($key in $urls.Keys) {
            if (-not (Get-DeploymentEnvValue -Path $Path -Key $key)) { Set-DeploymentEnvValue -Path $Path -Key $key -Value $urls[$key] }
        }
    } else {
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_OPS_MODULE' -Value 'off'
        foreach ($key in $urls.Keys) { Set-DeploymentEnvValue -Path $Path -Key $key -Value '' }
    }
    Set-DeploymentEnvValue -Path $Path -Key 'COMPOSE_PROFILES' -Value ($profiles -join ',')
}

function Remove-DeploymentEnvValue {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Key)
    $lines = @([IO.File]::ReadAllLines([IO.Path]::GetFullPath($Path)) | Where-Object { $_ -notmatch ('^\s*(?:export\s+)?' + [Regex]::Escape($Key) + '\s*=') })
    [IO.File]::WriteAllText([IO.Path]::GetFullPath($Path), ($lines -join "`n") + "`n", (New-Object Text.UTF8Encoding($false)))
}

# Turns ClickHouse (compose profile "clickhouse") on or off. Off clears
# IOT_CLICKHOUSE_URL so raw messages and telemetry stay in PostgreSQL; on
# drops an empty URL so Compose supplies the bundled server again.
function Set-ClickHouseModule {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][ValidateSet('on', 'off')][string]$State)
    $previous = Get-DeploymentEnvValue -Path $Path -Key 'IOT_CLICKHOUSE_MODULE'
    $profiles = @(@("$(Get-DeploymentEnvValue -Path $Path -Key 'COMPOSE_PROFILES')" -split ',') | ForEach-Object { $_.Trim() } | Where-Object { $_ -and $_ -ne 'clickhouse' })
    if ($State -eq 'on') {
        $profiles += 'clickhouse'
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_CLICKHOUSE_MODULE' -Value 'on'
        if (-not (Get-DeploymentEnvValue -Path $Path -Key 'IOT_CLICKHOUSE_URL')) { Remove-DeploymentEnvValue -Path $Path -Key 'IOT_CLICKHOUSE_URL' }
    } else {
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_CLICKHOUSE_MODULE' -Value 'off'
        Set-DeploymentEnvValue -Path $Path -Key 'IOT_CLICKHOUSE_URL' -Value ''
        if ($previous -ne 'off') {
            Write-Warning '关闭 ClickHouse 后，已存入 ClickHouse 的高频原文、遥测历史与属性上报的属性不再可读（数据卷保留，重新开启后恢复）；新数据写入 PostgreSQL。'
        }
    }
    Set-DeploymentEnvValue -Path $Path -Key 'COMPOSE_PROFILES' -Value ($profiles -join ',')
}
