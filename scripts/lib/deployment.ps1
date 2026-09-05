# Shared by local, online and offline entry points. Never dot-source a dotenv file.
function New-DeploymentSecret {
    $bytes = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $generator.GetBytes($bytes) } finally { $generator.Dispose() }
    return [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
}

function Ensure-DeploymentEnv {
    param([Parameter(Mandatory)][string]$Path, [hashtable]$Defaults = @{})

    if (Test-Path -LiteralPath $Path -PathType Leaf) {
        Write-Host "保留已有配置：$Path"
        return
    }
    if (Test-Path -LiteralPath $Path) { throw "配置路径不是文件：$Path" }
    $values = [ordered]@{
        POSTGRES_PASSWORD = (New-DeploymentSecret)
        REDIS_PASSWORD = (New-DeploymentSecret)
        CLICKHOUSE_PASSWORD = (New-DeploymentSecret)
        MINIO_ROOT_USER = 'iotadmin'
        MINIO_ROOT_PASSWORD = (New-DeploymentSecret)
        MINIO_DR_ROOT_USER = 'iotdradmin'
        MINIO_DR_ROOT_PASSWORD = (New-DeploymentSecret)
        EMQX_DASHBOARD_USER = 'admin'
        EMQX_DASHBOARD_PASSWORD = (New-DeploymentSecret)
        GRAFANA_ADMIN_USER = 'admin'
        GRAFANA_ADMIN_PASSWORD = (New-DeploymentSecret)
        IOT_JWT_SECRET = (New-DeploymentSecret)
        IOT_ADMIN_USER = 'admin'
        IOT_ADMIN_PASSWORD = (New-DeploymentSecret)
        IOT_ADMIN_TENANTS = 'tenant_001'
        IOT_VIDEO_PLATFORM_SECRETS = ('video-platform-1:' + (New-DeploymentSecret))
        IOT_AI_HARNESS_TOKEN = (New-DeploymentSecret)
        IOT_BACKUP_ADMIN_TOKEN = (New-DeploymentSecret)
        GB26875_CONTROL_TOKEN = (New-DeploymentSecret)
        IOT_HTTP_ADDR = ':8081'
        IOT_WEB_PORT = '8080'
        IOT_API_PORT = '8081'
        IOT_CORS_ALLOWED_ORIGINS = 'http://localhost:8080,http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173'
        IOT_OLLAMA_MODEL = 'qwen3:8b'
        IOT_AI_PROVIDER = 'disabled'
        IOT_AI_BASE_URL = ''
        IOT_AI_MODEL = ''
        IOT_AI_API_KEY = ''
        DEEPSEEK_API_KEY = ''
        IOT_AI_HARNESS_URL = ''
        IOT_AI_HARNESS_MCP_URL = 'http://platform-api:8080/mcp/harness'
        IOT_AI_HARNESS_MODEL = 'deepseek-v4-flash'
        IOT_BACKUP_TIME = '00:05'
        IOT_BACKUP_TIMEZONE = 'Asia/Shanghai'
    }
    foreach ($key in $Defaults.Keys) {
        if ($key -notmatch '^[A-Za-z_][A-Za-z0-9_]*$' -or [string]$Defaults[$key] -match "[\r\n]") {
            throw '默认环境变量名称或值包含不支持的字符。'
        }
        $values[$key] = [string]$Defaults[$key]
    }
    $fullPath = [IO.Path]::GetFullPath($Path)
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($fullPath)) | Out-Null
    $lines = @('# Generated once. Credentials are never rotated by deployment scripts.', '# Credentials are stored here; do not commit or share this file.')
    foreach ($entry in $values.GetEnumerator()) { $lines += "$($entry.Key)=$($entry.Value)" }
    # CreateNew fails safely if another invocation created the file meanwhile.
    $stream = New-Object IO.FileStream($fullPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
    try {
        $encoding = New-Object Text.UTF8Encoding($false)
        $bytes = $encoding.GetBytes(($lines -join "`n") + "`n")
        $stream.Write($bytes, 0, $bytes.Length)
    } finally { $stream.Dispose() }
    Write-Host "已生成配置：$Path（随机凭据仅保存在文件中）。"
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
    if (-not (Test-Path -LiteralPath (Join-Path $target '.git'))) {
        [IO.Directory]::CreateDirectory((Split-Path -Parent $target)) | Out-Null
        & git -c http.version=HTTP/1.1 clone --depth 1 'https://github.com/deepseek-ai/deepseek-harness.git' $target
        if ($LASTEXITCODE -ne 0) { throw 'Harness 源码下载失败。' }
    }
    $changes = @(& git -C $target status --porcelain)
    if ($LASTEXITCODE -ne 0) { throw '无法检查 Harness 源码状态。' }
    if ($changes.Count -gt 0) { throw "Harness 源码存在未提交修改，停止更新：$target" }
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
