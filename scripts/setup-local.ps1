[CmdletBinding()]
param(
    [string]$EnvFile = '.env.local',
    [switch]$SkipCodeDeps,
    [switch]$IncludeAi,
    [switch]$IncludeDeepSeek,
    [switch]$IncludeHarness,
    [switch]$NoHarness,
    [switch]$IncludeBackup,
    [switch]$IncludeOps,
    [string]$OllamaModel = 'qwen3:1.7b',
    [string]$DeepSeekModel = 'deepseek-v4-flash'
)

# 执行当前脚本步骤。
$ErrorActionPreference = 'Stop'
# 执行当前脚本步骤。
. (Join-Path $PSScriptRoot 'lib/deployment.ps1')
# 执行当前脚本步骤。
$projectRoot = Split-Path $PSScriptRoot -Parent
# 判断条件后执行对应操作。
if (-not [IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $projectRoot $EnvFile }
# 执行当前脚本步骤。
$EnvFile = [IO.Path]::GetFullPath($EnvFile)
# 判断条件后执行对应操作。
if ($EnvFile -eq (Join-Path $projectRoot '.env')) { throw '本地环境请使用 .env.local，不能覆盖在线部署的 .env。' }

# 定义可复用的脚本函数。
function Set-LocalEnvValue {
    # 执行当前脚本步骤。
    param([string]$Key, [string]$Value, [switch]$Replace)
    # 判断条件后执行对应操作。
    if ($Value -match "['\r\n]") { throw "配置 $Key 含不支持的引号或换行，未写入。" }
    # 执行当前脚本步骤。
    $content = [IO.File]::ReadAllText($EnvFile)
    # 执行当前脚本步骤。
    $pattern = '(?m)^' + [regex]::Escape($Key) + '=.*$'
    # 执行当前脚本步骤。
    $line = $Key + "='" + $Value + "'"
    # 判断条件后执行对应操作。
    if ([regex]::IsMatch($content, $pattern)) {
        # 判断条件后执行对应操作。
        if (-not $Replace) { return }
        # 执行当前脚本步骤。
        $content = [regex]::Replace($content, $pattern, [System.Text.RegularExpressions.MatchEvaluator]{ param($match) $line })
    # 结束当前控制块。
    } else {
        # 执行当前脚本步骤。
        $content = $content.TrimEnd("`r", "`n") + "`n" + $line + "`n"
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    [IO.File]::WriteAllText($EnvFile, $content, [Text.UTF8Encoding]::new($false))
# 结束当前控制块。
}

# 执行当前脚本步骤。
Assert-DockerAvailable
# 判断条件后执行对应操作。
if ($OllamaModel -notmatch '^[A-Za-z0-9][A-Za-z0-9._:/-]*$') { throw 'OllamaModel 不是有效的模型名称。' }
# 判断条件后执行对应操作。
if ($DeepSeekModel -notmatch '^[A-Za-z0-9][A-Za-z0-9._:/-]*$') { throw 'DeepSeekModel 不是有效的模型名称。' }
# 判断条件后执行对应操作。
if ($IncludeAi -and $IncludeDeepSeek) { throw 'IncludeAi 与 IncludeDeepSeek 只能二选一。' }
# 判断条件后执行对应操作。
if ($IncludeHarness -and $NoHarness) { throw 'IncludeHarness 与 NoHarness 不能同时使用。' }
# 执行当前脚本步骤。
$npmCommand = if ($env:OS -eq 'Windows_NT') { 'npm.cmd' } else { 'npm' }
# 判断条件后执行对应操作。
if (-not $SkipCodeDeps) {
    # 遍历数据并执行循环体。
    foreach ($command in @('go', $npmCommand)) {
        # 判断条件后执行对应操作。
        if (-not (Get-Command $command -ErrorAction SilentlyContinue)) { throw "请先安装 $command，或使用 -SkipCodeDeps 跳过代码依赖安装。" }
    # 结束当前控制块。
    }
# 结束当前控制块。
}
# 执行当前脚本步骤。
$newEnv = -not (Test-Path -LiteralPath $EnvFile)
# 执行当前脚本步骤。
Ensure-DeploymentEnv -Path $EnvFile
# 执行当前脚本步骤。
$postgresPassword = [Uri]::EscapeDataString((Get-DeploymentEnvValue -Path $EnvFile -Key 'POSTGRES_PASSWORD'))
# 执行当前脚本步骤。
$clickhousePassword = [Uri]::EscapeDataString((Get-DeploymentEnvValue -Path $EnvFile -Key 'CLICKHOUSE_PASSWORD'))
# 执行当前脚本步骤。
$defaults = [ordered]@{
    # 执行当前脚本步骤。
    IOT_HTTP_ADDR = ':8081'
    # 执行当前脚本步骤。
    IOT_DEV_MODE = 'false'
    # 执行当前脚本步骤。
    IOT_DATA_DIR = './data'
    # 执行当前脚本步骤。
    IOT_POSTGRES_DSN = "postgres://iot:${postgresPassword}@127.0.0.1:15432/iot?sslmode=disable"
    # 执行当前脚本步骤。
    IOT_REDIS_ADDR = '127.0.0.1:16379'
    # 执行当前脚本步骤。
    IOT_REDIS_PASSWORD = (Get-DeploymentEnvValue -Path $EnvFile -Key 'REDIS_PASSWORD')
    # 执行当前脚本步骤。
    IOT_CLICKHOUSE_URL = "http://iot:${clickhousePassword}@127.0.0.1:18123?database=iot"
    # 执行当前脚本步骤。
    IOT_MINIO_ENDPOINT = '127.0.0.1:19000'
    # 执行当前脚本步骤。
    IOT_MINIO_ACCESS_KEY = (Get-DeploymentEnvValue -Path $EnvFile -Key 'MINIO_ROOT_USER')
    # 执行当前脚本步骤。
    IOT_MINIO_SECRET_KEY = (Get-DeploymentEnvValue -Path $EnvFile -Key 'MINIO_ROOT_PASSWORD')
    # 执行当前脚本步骤。
    IOT_KAFKA_BROKERS = '127.0.0.1:19092'
    # 执行当前脚本步骤。
    IOT_MQTT_BROKER = 'tcp://127.0.0.1:1883'
    # 执行当前脚本步骤。
    IOT_MQTT_WEBSOCKET_PUBLIC_URL = 'ws://127.0.0.1:8083/mqtt'
    # 执行当前脚本步骤。
    IOT_OLLAMA_URL = 'http://127.0.0.1:11434'
    # 执行当前脚本步骤。
    IOT_AI_OLLAMA_URL = 'http://127.0.0.1:11434'
    # 执行当前脚本步骤。
    IOT_AI_PROVIDER = 'deepseek'
    # 执行当前脚本步骤。
    IOT_AI_BASE_URL = 'https://api.deepseek.com'
    # 执行当前脚本步骤。
    IOT_AI_MODEL = $DeepSeekModel
    # 执行当前脚本步骤。
    IOT_WEAVIATE_URL = 'http://127.0.0.1:18080'
    # 执行当前脚本步骤。
    IOT_BACKUP_URL = 'http://127.0.0.1:8092'
    # 执行当前脚本步骤。
    IOT_BACKUP_HTTP_ADDR = ':8092'
    # 执行当前脚本步骤。
    IOT_AI_HARNESS_ENABLED = 'true'
    # 执行当前脚本步骤。
    IOT_AI_HARNESS_URL = 'http://127.0.0.1:8091'
    # 执行当前脚本步骤。
    IOT_AI_HARNESS_MCP_URL = 'http://host.docker.internal:8081/mcp/harness'
    # 执行当前脚本步骤。
    IOT_AI_HARNESS_PROVIDER = 'deepseek-official'
    # 执行当前脚本步骤。
    IOT_AI_HARNESS_MODEL = $DeepSeekModel
# 结束当前控制块。
}
# 遍历数据并执行循环体。
foreach ($key in $defaults.Keys) { Set-LocalEnvValue -Key $key -Value $defaults[$key] -Replace:$newEnv }
# The source-debugged API and backup worker run on the same host. Keep the
# worker endpoint local even when middleware containers are remote.
# 执行当前脚本步骤。
Set-LocalEnvValue -Key 'IOT_BACKUP_URL' -Value 'http://127.0.0.1:8092' -Replace
# 执行当前脚本步骤。
Set-LocalEnvValue -Key 'IOT_BACKUP_HTTP_ADDR' -Value ':8092'
# 判断条件后执行对应操作。
if ($IncludeAi) {
    # 执行当前脚本步骤。
    $provider = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER'
    # 判断条件后执行对应操作。
    if ($provider -eq 'ollama') {
        # 执行当前脚本步骤。
        $configuredModel = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'
        # 判断条件后执行对应操作。
        if ($configuredModel) { $OllamaModel = $configuredModel }
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_PROVIDER' 'ollama' -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_OLLAMA_MODEL' $OllamaModel -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_MODEL' $OllamaModel -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_BASE_URL' 'http://127.0.0.1:11434' -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_HARNESS_PROVIDER' 'ollama' -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_HARNESS_OLLAMA_BASE_URL' 'http://ollama:11434/v1' -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_HARNESS_CONTEXT_WINDOW' '8192' -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_HARNESS_MODEL' $OllamaModel -Replace
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if ($IncludeDeepSeek) {
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_PROVIDER' 'deepseek' -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_BASE_URL' 'https://api.deepseek.com' -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_MODEL' $DeepSeekModel -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_HARNESS_PROVIDER' 'deepseek-official' -Replace
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_HARNESS_MODEL' $DeepSeekModel -Replace
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER') -eq 'deepseek') {
    # 执行当前脚本步骤。
    $deepSeekKey = Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY'
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace($deepSeekKey)) { $deepSeekKey = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_API_KEY' }
    # 判断条件后执行对应操作。
    if (-not [string]::IsNullOrWhiteSpace($deepSeekKey) -and [string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY'))) { Set-LocalEnvValue 'DEEPSEEK_API_KEY' $deepSeekKey -Replace }
    # 执行当前脚本步骤。
    $baseUrl = Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_BASE_URL'
    # 判断条件后执行对应操作。
    if (-not $baseUrl) { $baseUrl = 'https://api.deepseek.com' }
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_BASE_URL'))) { Set-LocalEnvValue 'IOT_AI_BASE_URL' $baseUrl -Replace }
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'))) { Set-LocalEnvValue 'IOT_AI_MODEL' $DeepSeekModel -Replace }
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace($deepSeekKey)) { Write-Warning '请在配置文件中填写 DEEPSEEK_API_KEY，自动研判和 AI 工作流将共用该密钥。' }
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if ($IncludeHarness) { Set-LocalEnvValue 'IOT_AI_HARNESS_ENABLED' 'true' -Replace }
# 判断条件后执行对应操作。
if ($NoHarness) { Set-LocalEnvValue 'IOT_AI_HARNESS_ENABLED' 'false' -Replace }
# 执行当前脚本步骤。
$useHarnessText = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED'
# 判断条件后执行对应操作。
if ($useHarnessText -notin @('true', 'false')) { throw 'IOT_AI_HARNESS_ENABLED 只能是 true 或 false。' }
# 执行当前脚本步骤。
$useHarness = $useHarnessText -eq 'true'
# 判断条件后执行对应操作。
if ($useHarness) {
    # 执行当前脚本步骤。
    Ensure-HarnessSource -ProjectRoot $projectRoot
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL'))) { Set-LocalEnvValue 'IOT_AI_HARNESS_URL' 'http://127.0.0.1:8091' -Replace }
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_HARNESS_MCP_URL' 'http://host.docker.internal:8081/mcp/harness' -Replace
# 结束当前控制块。
} else {
    # 执行当前脚本步骤。
    Set-LocalEnvValue 'IOT_AI_HARNESS_URL' '' -Replace
# 结束当前控制块。
}
# 执行当前脚本步骤。
# 运维中心依赖可选；规则与通知配置通过绑定挂载与容器共享。
if ($IncludeOps) {
    Set-LocalEnvValue 'IOT_OPS_PROMETHEUS_URL' 'http://127.0.0.1:19090' -Replace
    Set-LocalEnvValue 'IOT_OPS_LOKI_URL' 'http://127.0.0.1:13100' -Replace
    Set-LocalEnvValue 'IOT_OPS_GRAFANA_URL' 'http://127.0.0.1:13000' -Replace
    Set-LocalEnvValue 'IOT_OPS_ALERTMANAGER_URL' 'http://127.0.0.1:19093' -Replace
    Set-LocalEnvValue 'IOT_OPS_GRAFANA_USER' (Get-DeploymentEnvValue -Path $EnvFile -Key 'GRAFANA_ADMIN_USER') -Replace
    Set-LocalEnvValue 'IOT_OPS_GRAFANA_PASSWORD' (Get-DeploymentEnvValue -Path $EnvFile -Key 'GRAFANA_ADMIN_PASSWORD') -Replace
    Set-LocalEnvValue 'IOT_LOG_LOKI_URL' 'http://127.0.0.1:13100' -Replace
    Set-LocalEnvValue 'IOT_LOCAL_API_HOST' 'host.docker.internal' -Replace
    Set-LocalEnvValue 'IOT_OPS_CONFIG_FILE_MODE' '0644' -Replace
    Set-LocalEnvValue 'IOT_OPS_PROMETHEUS_RULES_DIR' './data/ops/prometheus-rules' -Replace
    Set-LocalEnvValue 'IOT_OPS_LOKI_RULES_DIR' './data/ops/loki/rules/fake' -Replace
    Set-LocalEnvValue 'IOT_OPS_LOKI_RUNTIME_FILE' './data/ops/loki/runtime.yaml' -Replace
    Set-LocalEnvValue 'IOT_OPS_ALERTMANAGER_CONFIG_FILE' './data/ops/alertmanager/alertmanager.yml' -Replace
    $opsDir = Join-Path $projectRoot 'data/ops'
    foreach ($dir in @('prometheus-rules', 'loki/rules/fake', 'alertmanager')) { [void](New-Item -ItemType Directory -Force -Path (Join-Path $opsDir $dir)) }
    $utf8 = [Text.UTF8Encoding]::new($false)
    $runtimeFile = Join-Path $opsDir 'loki/runtime.yaml'
    if (-not (Test-Path -LiteralPath $runtimeFile)) { [IO.File]::WriteAllText($runtimeFile, "overrides: {}`n", $utf8) }
    $alertmanagerFile = Join-Path $opsDir 'alertmanager/alertmanager.yml'
    if (-not (Test-Path -LiteralPath $alertmanagerFile)) { [IO.File]::WriteAllText($alertmanagerFile, "route:`n  receiver: platform-null`n  group_by: [alertname, severity]`nreceivers:`n  - name: platform-null`n", $utf8) }
}
Add-DeploymentEnvComments -Path $EnvFile

# 执行当前脚本步骤。
Push-Location $projectRoot
# 执行当前脚本步骤。
try {
    # 判断条件后执行对应操作。
    if (-not $SkipCodeDeps) {
        # 执行当前脚本步骤。
        Write-Host '准备 Go 依赖……'
        # 执行当前脚本步骤。
        & go mod download
        # 判断条件后执行对应操作。
        if ($LASTEXITCODE -ne 0) { throw 'go mod download 失败。' }
        # 执行当前脚本步骤。
        Push-Location (Join-Path $projectRoot 'iot_front')
        # 执行当前脚本步骤。
        try {
            # 执行当前脚本步骤。
            Write-Host '准备前端依赖……'
            # 执行当前脚本步骤。
            & $npmCommand ci
            # 判断条件后执行对应操作。
            if ($LASTEXITCODE -ne 0) { throw 'npm ci 失败。' }
        # 结束当前控制块。
        } finally { Pop-Location }
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    $compose = @('compose', '--project-name', 'iot-platform-local', '--env-file', $EnvFile, '-f', 'compose.local.yaml')
    # 判断条件后执行对应操作。
    if ($useHarness) { $compose += @('--profile', 'harness') }
    # 判断条件后执行对应操作。
    if ($IncludeBackup) { $compose += @('--profile', 'backup') }
    # 判断条件后执行对应操作。
    if ($IncludeOps) { $compose += @('--profile', 'ops') }
    # 判断条件后执行对应操作。
    if (-not $IncludeBackup) {
        # 执行当前脚本步骤。
        $backupCompose = @('compose', '--project-name', 'iot-platform-local', '--env-file', $EnvFile, '-f', 'compose.local.yaml', '--profile', 'backup')
        # 执行当前脚本步骤。
        Invoke-DockerChecked -Arguments ($backupCompose + @('stop', 'backup-service'))
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    Invoke-DockerChecked -Arguments ($compose + @('config', '--quiet'))
    # 执行当前脚本步骤。
    Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--build', '--wait', '--wait-timeout', '300'))
    # 执行当前脚本步骤。
    Wait-DeploymentHttp -Url 'http://127.0.0.1:11434/api/tags' -TimeoutSeconds 180
    # 执行当前脚本步骤。
    Invoke-DockerChecked -Arguments ($compose + @('exec', '-T', 'ollama', 'ollama', 'pull', 'nomic-embed-text'))
    # 判断条件后执行对应操作。
    if ($IncludeAi) { Invoke-DockerChecked -Arguments ($compose + @('exec', '-T', 'ollama', 'ollama', 'pull', $OllamaModel)) }
    # 判断条件后执行对应操作。
    if ($IncludeBackup) { Wait-DeploymentHttp -Url 'http://127.0.0.1:8092/health/ready' -TimeoutSeconds 180 }
    # 判断条件后执行对应操作。
    if ($useHarness) { Wait-DeploymentHttp -Url 'http://127.0.0.1:8091/health' -TimeoutSeconds 180 }
    # 判断条件后执行对应操作。
    if ($IncludeOps) {
        Wait-DeploymentHttp -Url 'http://127.0.0.1:19090/-/ready' -TimeoutSeconds 180
        Wait-DeploymentHttp -Url 'http://127.0.0.1:13000/api/health' -TimeoutSeconds 180
    }
    # 执行当前脚本步骤。
    Write-Host "本地依赖已就绪。配置和管理员账号保存在：$EnvFile（凭据不输出）。"
    # 执行当前脚本步骤。
    Write-Host "在 platform 目录启动后端：go run ./cmd/iot-platform --env-file `"$EnvFile`""
    # 执行当前脚本步骤。
    Write-Host '在 platform/iot_front 目录启动前端：npm run dev'
    # 执行当前脚本步骤。
    Write-Host '备份服务默认不启动容器；在 VS Code 选择“IoT Platform (API + Web + Backup)”进行源码调试。'
    # 判断条件后执行对应操作。
    if ($IncludeBackup) { Write-Host '已按 -IncludeBackup 启动备份容器；停止后可改用 VS Code 源码调试。' }
    # 执行当前脚本步骤。
    Write-Host '前端：http://localhost:5173；后端：http://localhost:8081'
    # 判断条件后执行对应操作。
    if ($IncludeOps) { Write-Host '运维中心依赖已启动：内置管理员可在“运维中心”菜单使用；其他账号需把所在租户加入 IOT_OPS_TENANTS。' }
# 结束当前控制块。
} finally { Pop-Location }
