# Shared by local, online and offline entry points. Never dot-source a dotenv file.
$script:IotDeploymentEnvCommentsPath = Join-Path $PSScriptRoot 'env-comments.tsv'

# 定义可复用的脚本函数。
function Get-DeploymentEnvComment {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string]$Key)
    # 遍历数据并执行循环体。
    foreach ($line in [IO.File]::ReadAllLines($script:IotDeploymentEnvCommentsPath)) {
        # 执行当前脚本步骤。
        $parts = $line.Split(@("`t"), 2, [StringSplitOptions]::None)
        # 判断条件后执行对应操作。
        if ($parts.Count -eq 2 -and $parts[0] -eq $Key) { return $parts[1] }
    # 结束当前控制块。
    }
    # 返回结果或结束当前脚本。
    return "自定义配置项 $Key"
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Add-DeploymentEnvComments {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string]$Path)
    # 执行当前脚本步骤。
    $fullPath = [IO.Path]::GetFullPath($Path)
    # 执行当前脚本步骤。
    $result = New-Object 'System.Collections.Generic.List[string]'
    # 遍历数据并执行循环体。
    foreach ($line in [IO.File]::ReadAllLines($fullPath)) {
        # 判断条件后执行对应操作。
        if ($line.StartsWith('# 配置说明：')) { continue }
        # 判断条件后执行对应操作。
        if ($line -match '^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=') {
            # 执行当前脚本步骤。
            [void]$result.Add('# 配置说明：' + (Get-DeploymentEnvComment -Key $matches[1]))
        # 结束当前控制块。
        }
        # 执行当前脚本步骤。
        [void]$result.Add($line.TrimEnd("`r"))
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    [IO.File]::WriteAllText($fullPath, ($result -join "`n") + "`n", (New-Object Text.UTF8Encoding($false)))
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function New-DeploymentSecret {
    # 执行当前脚本步骤。
    $bytes = New-Object byte[] 32
    # 执行当前脚本步骤。
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    # 执行当前脚本步骤。
    try { $generator.GetBytes($bytes) } finally { $generator.Dispose() }
    # 返回结果或结束当前脚本。
    return [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Ensure-DeploymentEnv {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string]$Path, [hashtable]$Defaults = @{})

    # 判断条件后执行对应操作。
    if (Test-Path -LiteralPath $Path -PathType Leaf) {
        # 执行当前脚本步骤。
        Write-Host "保留已有配置：$Path"
        # 返回结果或结束当前脚本。
        return
    # 结束当前控制块。
    }
    # 判断条件后执行对应操作。
    if (Test-Path -LiteralPath $Path) { throw "配置路径不是文件：$Path" }
    # 执行当前脚本步骤。
    $values = [ordered]@{
        # 执行当前脚本步骤。
        POSTGRES_PASSWORD = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        REDIS_PASSWORD = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        CLICKHOUSE_PASSWORD = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        MINIO_ROOT_USER = 'iotadmin'
        # 执行当前脚本步骤。
        MINIO_ROOT_PASSWORD = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        MINIO_DR_ROOT_USER = 'iotdradmin'
        # 执行当前脚本步骤。
        MINIO_DR_ROOT_PASSWORD = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        EMQX_DASHBOARD_USER = 'admin'
        # 执行当前脚本步骤。
        EMQX_DASHBOARD_PASSWORD = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        GRAFANA_ADMIN_USER = 'admin'
        # 执行当前脚本步骤。
        GRAFANA_ADMIN_PASSWORD = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        IOT_JWT_SECRET = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        IOT_ADMIN_USER = 'admin'
        # 执行当前脚本步骤。
        IOT_ADMIN_PASSWORD = 'admin123'
        # 执行当前脚本步骤。
        IOT_ADMIN_TENANTS = 'tenant_001'
        # 执行当前脚本步骤。
        IOT_VIDEO_PLATFORM_SECRETS = ('video-platform-1:' + (New-DeploymentSecret))
        # 执行当前脚本步骤。
        IOT_AI_HARNESS_TOKEN = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        IOT_BACKUP_ADMIN_TOKEN = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        GB26875_CONTROL_TOKEN = (New-DeploymentSecret)
        # 执行当前脚本步骤。
        IOT_HTTP_ADDR = ':8081'
        # 执行当前脚本步骤。
        IOT_WEB_PORT = '8080'
        # 执行当前脚本步骤。
        IOT_API_PORT = '8081'
        # 执行当前脚本步骤。
        IOT_CORS_ALLOWED_ORIGINS = 'http://localhost:8080,http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173'
        # 执行当前脚本步骤。
        IOT_OLLAMA_MODEL = 'qwen3:1.7b'
        # 执行当前脚本步骤。
        IOT_AI_PROVIDER = 'ollama'
        # 执行当前脚本步骤。
        IOT_AI_BASE_URL = 'http://ollama:11434'
        # 执行当前脚本步骤。
        IOT_AI_MODEL = 'qwen3:1.7b'
        # 执行当前脚本步骤。
        IOT_AI_API_KEY = ''
        # 执行当前脚本步骤。
        DEEPSEEK_API_KEY = ''
        # 执行当前脚本步骤。
        DEEPSEEK_BASE_URL = 'https://api.deepseek.com'
        # 执行当前脚本步骤。
        IOT_AI_HARNESS_ENABLED = 'true'
        # 执行当前脚本步骤。
        IOT_AI_HARNESS_URL = 'http://deepseek-harness:8091'
        # 执行当前脚本步骤。
        IOT_AI_HARNESS_MCP_URL = 'http://platform-api:8080/mcp/harness'
        # 执行当前脚本步骤。
        IOT_AI_HARNESS_PROVIDER = 'ollama'
        # 执行当前脚本步骤。
        IOT_AI_HARNESS_OLLAMA_BASE_URL = 'http://ollama:11434/v1'
        # 执行当前脚本步骤。
        IOT_AI_HARNESS_CONTEXT_WINDOW = '8192'
        # 执行当前脚本步骤。
        IOT_AI_HARNESS_MODEL = 'qwen3:1.7b'
        # 执行当前脚本步骤。
        IOT_BACKUP_TIME = '00:05'
        # 执行当前脚本步骤。
        IOT_BACKUP_ENABLED = 'true'
        # 执行当前脚本步骤。
        IOT_BACKUP_TIMEZONE = 'Asia/Shanghai'
    # 结束当前控制块。
    }
    # 遍历数据并执行循环体。
    foreach ($key in $Defaults.Keys) {
        # 判断条件后执行对应操作。
        if ($key -notmatch '^[A-Za-z_][A-Za-z0-9_]*$' -or [string]$Defaults[$key] -match "[\r\n]") {
            # 执行当前脚本步骤。
            throw '默认环境变量名称或值包含不支持的字符。'
        # 结束当前控制块。
        }
        # 执行当前脚本步骤。
        $values[$key] = [string]$Defaults[$key]
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    $fullPath = [IO.Path]::GetFullPath($Path)
    # 执行当前脚本步骤。
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($fullPath)) | Out-Null
    # 执行当前脚本步骤。
    $lines = @('# 此配置只在首次部署时生成，后续运行不会轮换凭据。', '# 文件包含敏感凭据，请勿提交到 Git 或公开分享。')
    # 遍历数据并执行循环体。
    foreach ($entry in $values.GetEnumerator()) { $lines += "$($entry.Key)=$($entry.Value)" }
    # CreateNew fails safely if another invocation created the file meanwhile.
    # 执行当前脚本步骤。
    $stream = New-Object IO.FileStream($fullPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
    # 执行当前脚本步骤。
    try {
        # 执行当前脚本步骤。
        $encoding = New-Object Text.UTF8Encoding($false)
        # 执行当前脚本步骤。
        $bytes = $encoding.GetBytes(($lines -join "`n") + "`n")
        # 执行当前脚本步骤。
        $stream.Write($bytes, 0, $bytes.Length)
    # 结束当前控制块。
    } finally { $stream.Dispose() }
    # 执行当前脚本步骤。
    Write-Host "已生成配置：$Path（随机凭据仅保存在文件中）。"
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Get-DeploymentEnvValue {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Key)
    # Match Compose's precedence: exported process variables override --env-file.
    # 执行当前脚本步骤。
    $processValue = [Environment]::GetEnvironmentVariable($Key, 'Process')
    # 判断条件后执行对应操作。
    if ($null -ne $processValue) { return $processValue }
    # 执行当前脚本步骤。
    $value = ''
    # 遍历数据并执行循环体。
    foreach ($line in [IO.File]::ReadAllLines([IO.Path]::GetFullPath($Path))) {
        # 判断条件后执行对应操作。
        if ($line -match ('^\s*(?:export\s+)?' + [Regex]::Escape($Key) + '\s*=\s*(.*)$')) {
            # 执行当前脚本步骤。
            $candidate = $matches[1].Trim()
            # 判断条件后执行对应操作。
            if ($candidate.StartsWith('"') -or $candidate.StartsWith("'")) {
                # 执行当前脚本步骤。
                $quote = $candidate.Substring(0, 1)
                # 执行当前脚本步骤。
                $end = $candidate.IndexOf($quote, 1)
                # 判断条件后执行对应操作。
                if ($end -ge 1) { $candidate = $candidate.Substring(1, $end - 1) }
            # 结束当前控制块。
            } else { $candidate = ($candidate -replace '\s+#.*$', '').TrimEnd() }
            # 执行当前脚本步骤。
            $value = $candidate
        # 结束当前控制块。
        }
    # 结束当前控制块。
    }
    # 返回结果或结束当前脚本。
    return $value
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Set-DeploymentEnvValue {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Key, [AllowEmptyString()][string]$Value)
    # 判断条件后执行对应操作。
    if ($Key -notmatch '^[A-Za-z_][A-Za-z0-9_]*$' -or $Value -match "[\r\n]") { throw '环境变量名称或值包含不支持的字符。' }
    # 执行当前脚本步骤。
    $found = $false
    # 执行当前脚本步骤。
    $lines = @([IO.File]::ReadAllLines([IO.Path]::GetFullPath($Path)) | ForEach-Object {
        # 判断条件后执行对应操作。
        if ($_ -match ('^\s*(?:export\s+)?' + [Regex]::Escape($Key) + '\s*=')) {
            # 判断条件后执行对应操作。
            if (-not $found) { "$Key=$Value"; $found = $true }
        # 结束当前控制块。
        } else { $_ }
    # 结束当前控制块。
    })
    # 判断条件后执行对应操作。
    if (-not $found) { $lines += "$Key=$Value" }
    # 执行当前脚本步骤。
    [IO.File]::WriteAllText([IO.Path]::GetFullPath($Path), ($lines -join "`n") + "`n", (New-Object Text.UTF8Encoding($false)))
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Ensure-HarnessSource {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string]$ProjectRoot)
    # 判断条件后执行对应操作。
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) { throw 'Harness 需要 Git。' }
    # 执行当前脚本步骤。
    $revision = [IO.File]::ReadAllText((Join-Path $ProjectRoot 'deploy/deepseek-harness/REVISION')).Trim()
    # 判断条件后执行对应操作。
    if ($revision -notmatch '^[0-9a-f]{40}$') { throw 'Harness REVISION 无效。' }
    # 执行当前脚本步骤。
    $target = Join-Path $ProjectRoot 'upstream/deepseek-harness'
    # 判断条件后执行对应操作。
    if (-not (Test-Path -LiteralPath (Join-Path $target '.git'))) {
        # 执行当前脚本步骤。
        [IO.Directory]::CreateDirectory((Split-Path -Parent $target)) | Out-Null
        # 执行当前脚本步骤。
        & git -c http.version=HTTP/1.1 clone --depth 1 'https://github.com/deepseek-ai/deepseek-harness.git' $target
        # 判断条件后执行对应操作。
        if ($LASTEXITCODE -ne 0) { throw 'Harness 源码下载失败。' }
        # 执行当前脚本步骤。
        & git -C $target config core.autocrlf false
        # 执行当前脚本步骤。
        & git -C $target config core.fileMode false
        # 执行当前脚本步骤。
        & git -C $target reset --hard HEAD *> $null
        # 执行当前脚本步骤。
        & git -C $target clean -fd *> $null
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    $changes = @(& git -C $target status --porcelain)
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0) { throw '无法检查 Harness 源码状态。' }
    # 判断条件后执行对应操作。
    if ($changes.Count -gt 0) {
        # 执行当前脚本步骤。
        $timestamp = Get-Date -Format 'yyyyMMdd-HHmmss'
        # 执行当前脚本步骤。
        $backup = "$target.backup-$timestamp"
        # 执行当前脚本步骤。
        $suffix = 0
        # 遍历数据并执行循环体。
        while (Test-Path -LiteralPath $backup) {
            # 执行当前脚本步骤。
            $suffix++
            # 执行当前脚本步骤。
            $backup = "$target.backup-$timestamp-$suffix"
        # 结束当前控制块。
        }
        # 执行当前脚本步骤。
        Move-Item -LiteralPath $target -Destination $backup
        # 执行当前脚本步骤。
        Write-Host "DeepSeek Harness 源码目录存在修改，已备份到：$backup"
        # 执行当前脚本步骤。
        & git -c http.version=HTTP/1.1 clone --depth 1 'https://github.com/deepseek-ai/deepseek-harness.git' $target
        # 判断条件后执行对应操作。
        if ($LASTEXITCODE -ne 0) { throw "Harness 源码重新下载失败，原目录保存在：$backup" }
        # 执行当前脚本步骤。
        & git -C $target config core.autocrlf false
        # 执行当前脚本步骤。
        & git -C $target config core.fileMode false
        # 执行当前脚本步骤。
        & git -C $target reset --hard HEAD *> $null
        # 执行当前脚本步骤。
        & git -C $target clean -fd *> $null
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    $current = & git -C $target rev-parse HEAD
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0) { throw '无法读取 Harness 提交。' }
    # 判断条件后执行对应操作。
    if ($current.Trim() -ne $revision) {
        # 执行当前脚本步骤。
        & git -C $target -c http.version=HTTP/1.1 fetch --depth 1 origin $revision
        # 判断条件后执行对应操作。
        if ($LASTEXITCODE -ne 0) { throw 'Harness 提交下载失败。' }
        # 执行当前脚本步骤。
        & git -C $target checkout --detach $revision
        # 判断条件后执行对应操作。
        if ($LASTEXITCODE -ne 0) { throw 'Harness 提交切换失败。' }
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    $current = & git -C $target rev-parse HEAD
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0 -or $current.Trim() -ne $revision) { throw 'Harness 提交校验失败。' }
    # 执行当前脚本步骤。
    [IO.File]::WriteAllText((Join-Path $ProjectRoot 'upstream/deepseek-harness.revision'), "$revision`n", (New-Object Text.UTF8Encoding($false)))
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Assert-DockerAvailable {
    # 判断条件后执行对应操作。
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        # 执行当前脚本步骤。
        throw '找不到 docker，请先安装 Docker Engine/Desktop 和 Compose v2。'
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    & docker info *> $null
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0) { throw 'Docker Engine 不可用，请先启动 Docker。' }
    # 执行当前脚本步骤。
    & docker compose version *> $null
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0) { throw 'Docker Compose v2 不可用，请先安装或升级。' }
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Invoke-DockerChecked {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string[]]$Arguments)
    # 执行当前脚本步骤。
    & docker @Arguments
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0) { throw "Docker 命令失败，退出码 $LASTEXITCODE。" }
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Wait-DeploymentHttp {
    # 执行当前脚本步骤。
    param([Parameter(Mandatory)][string]$Url, [int]$TimeoutSeconds = 180)
    # 执行当前脚本步骤。
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    # 执行当前脚本步骤。
    do {
        # 执行当前脚本步骤。
        try {
            # 执行当前脚本步骤。
            $response = Invoke-WebRequest -UseBasicParsing -Uri $Url -TimeoutSec 5
            # 判断条件后执行对应操作。
            if ($response.StatusCode -eq 200) { Write-Host "健康检查通过：$Url"; return }
        # 结束当前控制块。
        } catch { }
        # 执行当前脚本步骤。
        Start-Sleep -Seconds 2
    # 结束当前控制块。
    } while ([DateTime]::UtcNow -lt $deadline)
    # 执行当前脚本步骤。
    throw "健康检查超时：$Url。请用相同的 Compose 项目和配置参数检查 ps / logs。"
# 结束当前控制块。
}
