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
        IOT_ADMIN_PASSWORD = 'admin123'
        IOT_ADMIN_TENANTS = 'tenant_001'
        IOT_VIDEO_PLATFORM_SECRETS = ('video-platform-1:' + (New-DeploymentSecret))
        IOT_AI_HARNESS_TOKEN = (New-DeploymentSecret)
        IOT_BACKUP_ADMIN_TOKEN = (New-DeploymentSecret)
        GB26875_CONTROL_TOKEN = (New-DeploymentSecret)
        IOT_HTTP_ADDR = ':8081'
        IOT_WEB_PORT = '8080'
        IOT_API_PORT = '8081'
        IOT_CORS_ALLOWED_ORIGINS = 'http://localhost:8080,http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173'
        IOT_OLLAMA_MODEL = 'qwen3:1.7b'
        IOT_AI_PROVIDER = 'ollama'
        IOT_AI_BASE_URL = 'http://ollama:11434'
        IOT_AI_MODEL = 'qwen3:1.7b'
        IOT_AI_API_KEY = ''
        DEEPSEEK_API_KEY = ''
        DEEPSEEK_BASE_URL = 'https://api.deepseek.com'
        IOT_AI_HARNESS_ENABLED = 'true'
        IOT_AI_HARNESS_URL = 'http://deepseek-harness:8091'
        IOT_AI_HARNESS_MCP_URL = 'http://platform-api:8080/mcp/harness'
        IOT_AI_HARNESS_PROVIDER = 'ollama'
        IOT_AI_HARNESS_OLLAMA_BASE_URL = 'http://ollama:11434/v1'
        IOT_AI_HARNESS_CONTEXT_WINDOW = '8192'
        IOT_AI_HARNESS_MODEL = 'qwen3:1.7b'
        IOT_BACKUP_TIME = '00:05'
        IOT_BACKUP_ENABLED = 'true'
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
    $lines = @('# 此配置只在首次部署时生成，后续运行不会轮换凭据。', '# 文件包含敏感凭据，请勿提交到 Git 或公开分享。')
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
        & git -C $target config core.autocrlf false
        & git -C $target config core.fileMode false
        & git -C $target reset --hard HEAD *> $null
        & git -C $target clean -fd *> $null
    }
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
        & git -c http.version=HTTP/1.1 clone --depth 1 'https://github.com/deepseek-ai/deepseek-harness.git' $target
        if ($LASTEXITCODE -ne 0) { throw "Harness 源码重新下载失败，原目录保存在：$backup" }
        & git -C $target config core.autocrlf false
        & git -C $target config core.fileMode false
        & git -C $target reset --hard HEAD *> $null
        & git -C $target clean -fd *> $null
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
