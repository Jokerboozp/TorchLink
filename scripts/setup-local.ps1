[CmdletBinding()]
param(
    [string]$EnvFile = '.env.local',
    [switch]$SkipCodeDeps,
    [switch]$NoHarness,
    [switch]$IncludeBackup,
    [switch]$IncludeOps,
    [ValidateSet('keep', 'on', 'off')][string]$Video = 'keep',
    [ValidateSet('keep', 'on', 'off')][string]$Capacity = 'keep',
    [string]$RtcIp = '',
    [string]$RtcPort = '',
    [string]$AllowedCidrs = '',
    [switch]$Transcode,
    [switch]$NoTranscode,
    [string]$OllamaModel = '',
    [string]$DeepSeekModel = 'deepseek-flash'
)

$ErrorActionPreference = 'Stop'
if (($RtcIp -or $RtcPort -or $AllowedCidrs -or $Transcode -or $NoTranscode) -and $Video -eq 'off') { throw '媒体选项不能与 -Video off 同时使用。' }
if ($Transcode -and $NoTranscode) { throw '-Transcode 与 -NoTranscode 不能同时使用。' }
. (Join-Path $PSScriptRoot 'lib/deployment.ps1')
$projectRoot = Split-Path $PSScriptRoot -Parent
if (-not [IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $projectRoot $EnvFile }
$EnvFile = [IO.Path]::GetFullPath($EnvFile)
if ($EnvFile -eq (Join-Path $projectRoot '.env')) { throw '本地环境请使用 .env.local，不能覆盖在线部署的 .env。' }

function Set-LocalEnvValue {
    param([string]$Key, [string]$Value, [switch]$Replace)
    if ($Value -match "['\r\n]") { throw "配置 $Key 含不支持的引号或换行，未写入。" }
    $content = [IO.File]::ReadAllText($EnvFile)
    $pattern = '(?m)^' + [regex]::Escape($Key) + '=.*$'
    $line = $Key + "='" + $Value + "'"
    if ([regex]::IsMatch($content, $pattern)) {
        if (-not $Replace) { return }
        $content = [regex]::Replace($content, $pattern, [System.Text.RegularExpressions.MatchEvaluator]{ param($match) $line })
    } else {
        $content = $content.TrimEnd("`r", "`n") + "`n" + $line + "`n"
    }
    [IO.File]::WriteAllText($EnvFile, $content, [Text.UTF8Encoding]::new($false))
}

Assert-DockerAvailable
if ($OllamaModel) { throw '已取消部署本地对话模型，请填写 DEEPSEEK_API_KEY。' }
if ($DeepSeekModel -notmatch '^[A-Za-z0-9][A-Za-z0-9._:/-]*$') { throw 'DeepSeekModel 不是有效的模型名称。' }
if ($NoHarness) { throw 'AI 工作流服务（Harness）是必装组件，不能使用 -NoHarness。' }
$npmCommand = if ($env:OS -eq 'Windows_NT') { 'npm.cmd' } else { 'npm' }
if (-not $SkipCodeDeps) {
    foreach ($command in @('go', $npmCommand)) {
        if (-not (Get-Command $command -ErrorAction SilentlyContinue)) { throw "请先安装 $command，或使用 -SkipCodeDeps 跳过代码依赖安装。" }
    }
}
$newEnv = -not (Test-Path -LiteralPath $EnvFile)
Ensure-DeploymentEnv -Path $EnvFile
Ensure-EmqxAdminEnv -Path $EnvFile -DefaultUrl 'http://127.0.0.1:18083'
# Live video is deployed by default; an earlier -Video off is kept.
if ($Video -eq 'keep') { $Video = if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_MODULE') -eq 'off') { 'off' } else { 'on' } }
$postgresPassword = [Uri]::EscapeDataString((Get-DeploymentEnvValue -Path $EnvFile -Key 'POSTGRES_PASSWORD'))
$clickhousePassword = [Uri]::EscapeDataString((Get-DeploymentEnvValue -Path $EnvFile -Key 'CLICKHOUSE_PASSWORD'))
$defaults = [ordered]@{
    IOT_HTTP_ADDR = ':8081'
    IOT_DEV_MODE = 'false'
    IOT_DATA_DIR = './data'
    IOT_POSTGRES_DSN = "postgres://iot:${postgresPassword}@127.0.0.1:15432/iot?sslmode=disable"
    IOT_REDIS_ADDR = '127.0.0.1:16379'
    IOT_REDIS_PASSWORD = (Get-DeploymentEnvValue -Path $EnvFile -Key 'REDIS_PASSWORD')
    IOT_CLICKHOUSE_URL = "http://iot:${clickhousePassword}@127.0.0.1:18123?database=iot"
    IOT_MINIO_ENDPOINT = '127.0.0.1:19000'
    IOT_MINIO_ACCESS_KEY = (Get-DeploymentEnvValue -Path $EnvFile -Key 'MINIO_ROOT_USER')
    IOT_MINIO_SECRET_KEY = (Get-DeploymentEnvValue -Path $EnvFile -Key 'MINIO_ROOT_PASSWORD')
    IOT_KAFKA_BROKERS = '127.0.0.1:19092'
    IOT_MQTT_BROKER = 'tcp://127.0.0.1:1883'
    IOT_MQTT_WEBSOCKET_PUBLIC_URL = 'ws://127.0.0.1:8083/mqtt'
    IOT_OLLAMA_URL = 'http://127.0.0.1:11434'
    IOT_AI_OLLAMA_URL = 'http://127.0.0.1:11434'
    IOT_AI_PROVIDER = 'deepseek'
    IOT_AI_BASE_URL = 'https://api.deepseek.com'
    IOT_AI_MODEL = $DeepSeekModel
    IOT_WEAVIATE_URL = 'http://127.0.0.1:18080'
    IOT_BACKUP_URL = 'http://127.0.0.1:8092'
    IOT_BACKUP_HTTP_ADDR = ':8092'
    IOT_AI_HARNESS_ENABLED = 'true'
    IOT_AI_HARNESS_URL = 'http://127.0.0.1:8091'
    IOT_AI_HARNESS_MCP_URL = 'http://host.docker.internal:8081/mcp/harness'
    IOT_AI_HARNESS_PROVIDER = 'deepseek-official'
    IOT_AI_HARNESS_MODEL = $DeepSeekModel
}
foreach ($key in $defaults.Keys) { Set-LocalEnvValue -Key $key -Value $defaults[$key] -Replace:$newEnv }
# The source-debugged API and backup worker run on the same host. Keep the
# worker endpoint local even when middleware containers are remote.
Set-LocalEnvValue -Key 'IOT_BACKUP_URL' -Value 'http://127.0.0.1:8092' -Replace
Set-LocalEnvValue -Key 'IOT_BACKUP_HTTP_ADDR' -Value ':8092'
Set-LocalEnvValue 'IOT_LOCAL_BACKUP_METRICS_TARGET' $(if ($IncludeBackup) { 'backup-service:8090' } else { 'host.docker.internal:8092' }) -Replace
Set-DeepSeekDeploymentEnv -Path $EnvFile -Model $DeepSeekModel

# The controller follows the source API lifecycle, using its local addresses.
if ($Capacity -eq 'keep') { $Capacity = if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_CAPACITY_MODULE') -eq 'off') { 'off' } else { 'on' } }
Set-LocalEnvValue 'IOT_OPS_CAPACITY_LOCAL' 'true' -Replace
Set-LocalEnvValue 'IOT_CAPACITY_MODULE' $Capacity -Replace

# AI 工作流服务（Harness）为必装组件：告警研判、巡检、报告、协议助手和规则草稿都通过它运行。
if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED') -eq 'false') { Write-Warning 'Harness 已改为必装组件，已将 IOT_AI_HARNESS_ENABLED 改为 true。' }
Set-LocalEnvValue 'IOT_AI_HARNESS_ENABLED' 'true' -Replace
Ensure-HarnessSource -ProjectRoot $projectRoot
if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL'))) { Set-LocalEnvValue 'IOT_AI_HARNESS_URL' 'http://127.0.0.1:8091' -Replace }
Set-LocalEnvValue 'IOT_AI_HARNESS_MCP_URL' 'http://host.docker.internal:8081/mcp/harness' -Replace
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
    $opsPath = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_LOCAL_OPS_DIR'
    if (-not $opsPath) { $opsPath = './data/ops' }
    Set-LocalEnvValue 'IOT_OPS_PROMETHEUS_RULES_DIR' "$opsPath/prometheus-rules" -Replace
    Set-LocalEnvValue 'IOT_OPS_LOKI_RULES_DIR' "$opsPath/loki/rules/fake" -Replace
    Set-LocalEnvValue 'IOT_OPS_LOKI_RUNTIME_FILE' "$opsPath/loki/runtime.yaml" -Replace
    Set-LocalEnvValue 'IOT_OPS_ALERTMANAGER_CONFIG_FILE' "$opsPath/alertmanager/alertmanager.yml" -Replace
    $opsDir = if ([IO.Path]::IsPathRooted($opsPath)) { $opsPath } else { Join-Path $projectRoot $opsPath }
    foreach ($dir in @('prometheus-rules', 'loki/rules/fake', 'alertmanager')) { [void](New-Item -ItemType Directory -Force -Path (Join-Path $opsDir $dir)) }
    $utf8 = [Text.UTF8Encoding]::new($false)
    $runtimeFile = Join-Path $opsDir 'loki/runtime.yaml'
    if (-not (Test-Path -LiteralPath $runtimeFile)) { [IO.File]::WriteAllText($runtimeFile, "overrides: {}`n", $utf8) }
    $alertmanagerFile = Join-Path $opsDir 'alertmanager/alertmanager.yml'
    if (-not (Test-Path -LiteralPath $alertmanagerFile)) { [IO.File]::WriteAllText($alertmanagerFile, "route:`n  receiver: platform-null`n  group_by: [alertname, severity]`nreceivers:`n  - name: platform-null`n", $utf8) }
}
Add-DeploymentEnvComments -Path $EnvFile

Push-Location $projectRoot
try {
    if (-not $SkipCodeDeps) {
        Write-Host '准备 Go 依赖……'
        & go mod download
        if ($LASTEXITCODE -ne 0) { throw 'go mod download 失败。' }
        Push-Location (Join-Path $projectRoot 'iot_front')
        try {
            Write-Host '准备前端依赖……'
            & $npmCommand ci
            if ($LASTEXITCODE -ne 0) { throw 'npm ci 失败。' }
        } finally { Pop-Location }
    }
    $compose = @('compose', '--project-name', 'iot-platform-local', '--env-file', $EnvFile, '-f', 'compose.local.yaml')
    if ($IncludeBackup) { $compose += @('--profile', 'backup') }
    if ($IncludeOps) { $compose += @('--profile', 'ops') }
    if ($Video -eq 'off') { & (Join-Path $PSScriptRoot 'video-module.ps1') disable -Mode local -EnvFile $EnvFile }
    if (-not $IncludeBackup) {
        $backupCompose = @('compose', '--project-name', 'iot-platform-local', '--env-file', $EnvFile, '-f', 'compose.local.yaml', '--profile', 'backup')
        Invoke-DockerChecked -Arguments ($backupCompose + @('stop', 'backup-service'))
    }
    Invoke-DockerChecked -Arguments ($compose + @('config', '--quiet'))
    Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--build', '--wait', '--wait-timeout', '300'))
    Wait-DeploymentHttp -Url 'http://127.0.0.1:11434/api/tags' -TimeoutSeconds 180
    Invoke-DockerChecked -Arguments ($compose + @('exec', '-T', 'ollama', 'ollama', 'pull', 'nomic-embed-text'))
    if ($IncludeBackup) { Wait-DeploymentHttp -Url 'http://127.0.0.1:8092/health/ready' -TimeoutSeconds 180 }
    Wait-DeploymentHttp -Url 'http://127.0.0.1:8091/health' -TimeoutSeconds 180
    if ($IncludeOps) {
        Wait-DeploymentHttp -Url 'http://127.0.0.1:19090/-/ready' -TimeoutSeconds 180
        Wait-DeploymentHttp -Url 'http://127.0.0.1:13000/api/health' -TimeoutSeconds 180
        Wait-DeploymentHttp -Url 'http://127.0.0.1:13100/ready' -TimeoutSeconds 180
        Wait-DeploymentHttp -Url 'http://127.0.0.1:19093/-/ready' -TimeoutSeconds 180
    }
    if ($Video -eq 'on') {
        & (Join-Path $PSScriptRoot 'video-module.ps1') enable -Mode local -EnvFile $EnvFile -RtcIp $RtcIp -RtcPort $RtcPort -AllowedCidrs $AllowedCidrs -Transcode:$Transcode -NoTranscode:$NoTranscode
    }
    Add-DeploymentEnvComments -Path $EnvFile
    Write-Host "本地依赖已就绪。配置和管理员账号保存在：$EnvFile（凭据不输出）。"
    Write-Host "在 platform 目录启动后端：go run ./cmd/iot-platform --env-file `"$EnvFile`""
    if ($Capacity -eq 'on') { Write-Host '本地容量测试随 API 启动，在“运维中心 → 容量测试”使用；不会自动开始发压。' }
    Write-Host '在 platform/iot_front 目录启动前端：npm run dev'
    Write-Host '备份服务默认不启动容器；在 VS Code 选择“IoT Platform (API + Web + Backup)”进行源码调试。'
    if ($IncludeBackup) { Write-Host '已按 -IncludeBackup 启动备份容器；停止后可改用 VS Code 源码调试。' }
    Write-Host '前端：http://localhost:5173；后端：http://localhost:8081'
    if ($IncludeOps) { Write-Host '运维中心依赖已启动：内置管理员可在“运维中心”菜单使用；其他账号需把所在租户加入 IOT_OPS_TENANTS。' }
} finally { Pop-Location }
