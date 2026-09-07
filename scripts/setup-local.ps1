[CmdletBinding()]
param(
    [string]$EnvFile = '.env.local',
    [switch]$SkipCodeDeps,
    [switch]$IncludeAi,
    [switch]$IncludeDeepSeek,
    [switch]$IncludeHarness,
    [switch]$NoHarness,
    [string]$OllamaModel = 'qwen3:8b',
    [string]$DeepSeekModel = 'deepseek-v4-flash'
)

$ErrorActionPreference = 'Stop'
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
if ($OllamaModel -notmatch '^[A-Za-z0-9][A-Za-z0-9._:/-]*$') { throw 'OllamaModel 不是有效的模型名称。' }
if ($DeepSeekModel -notmatch '^[A-Za-z0-9][A-Za-z0-9._:/-]*$') { throw 'DeepSeekModel 不是有效的模型名称。' }
if ($IncludeAi -and $IncludeDeepSeek) { throw 'IncludeAi 与 IncludeDeepSeek 只能二选一。' }
if ($IncludeHarness -and $NoHarness) { throw 'IncludeHarness 与 NoHarness 不能同时使用。' }
$npmCommand = if ($env:OS -eq 'Windows_NT') { 'npm.cmd' } else { 'npm' }
if (-not $SkipCodeDeps) {
    foreach ($command in @('go', $npmCommand)) {
        if (-not (Get-Command $command -ErrorAction SilentlyContinue)) { throw "请先安装 $command，或使用 -SkipCodeDeps 跳过代码依赖安装。" }
    }
}
$newEnv = -not (Test-Path -LiteralPath $EnvFile)
Ensure-DeploymentEnv -Path $EnvFile
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
    IOT_AI_HARNESS_ENABLED = 'true'
    IOT_AI_HARNESS_URL = 'http://127.0.0.1:8091'
    IOT_AI_HARNESS_MCP_URL = 'http://host.docker.internal:8081/mcp/harness'
}
foreach ($key in $defaults.Keys) { Set-LocalEnvValue -Key $key -Value $defaults[$key] -Replace:$newEnv }
if ($IncludeAi) {
    $provider = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER'
    if ($provider -eq 'ollama') {
        $configuredModel = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'
        if ($configuredModel) { $OllamaModel = $configuredModel }
    }
    Set-LocalEnvValue 'IOT_AI_PROVIDER' 'ollama' -Replace
    Set-LocalEnvValue 'IOT_OLLAMA_MODEL' $OllamaModel -Replace
    Set-LocalEnvValue 'IOT_AI_MODEL' $OllamaModel -Replace
    Set-LocalEnvValue 'IOT_AI_BASE_URL' 'http://127.0.0.1:11434' -Replace
}
if ($IncludeDeepSeek) {
    Set-LocalEnvValue 'IOT_AI_PROVIDER' 'deepseek' -Replace
    Set-LocalEnvValue 'IOT_AI_BASE_URL' 'https://api.deepseek.com' -Replace
    Set-LocalEnvValue 'IOT_AI_MODEL' $DeepSeekModel -Replace
}
if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER') -eq 'deepseek') {
    $deepSeekKey = Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY'
    if ([string]::IsNullOrWhiteSpace($deepSeekKey)) { $deepSeekKey = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_API_KEY' }
    if (-not [string]::IsNullOrWhiteSpace($deepSeekKey) -and [string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY'))) { Set-LocalEnvValue 'DEEPSEEK_API_KEY' $deepSeekKey -Replace }
    $baseUrl = Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_BASE_URL'
    if (-not $baseUrl) { $baseUrl = 'https://api.deepseek.com' }
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_BASE_URL'))) { Set-LocalEnvValue 'IOT_AI_BASE_URL' $baseUrl -Replace }
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'))) { Set-LocalEnvValue 'IOT_AI_MODEL' $DeepSeekModel -Replace }
    if ([string]::IsNullOrWhiteSpace($deepSeekKey)) { Write-Warning '请在配置文件中填写 DEEPSEEK_API_KEY，自动研判和 AI 工作流将共用该密钥。' }
}
if ($IncludeHarness) { Set-LocalEnvValue 'IOT_AI_HARNESS_ENABLED' 'true' -Replace }
if ($NoHarness) { Set-LocalEnvValue 'IOT_AI_HARNESS_ENABLED' 'false' -Replace }
$useHarnessText = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED'
if ($useHarnessText -notin @('true', 'false')) { throw 'IOT_AI_HARNESS_ENABLED 只能是 true 或 false。' }
$useHarness = $useHarnessText -eq 'true'
if ($useHarness) {
    Ensure-HarnessSource -ProjectRoot $projectRoot
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL'))) { Set-LocalEnvValue 'IOT_AI_HARNESS_URL' 'http://127.0.0.1:8091' -Replace }
    Set-LocalEnvValue 'IOT_AI_HARNESS_MCP_URL' 'http://host.docker.internal:8081/mcp/harness' -Replace
} else {
    Set-LocalEnvValue 'IOT_AI_HARNESS_URL' '' -Replace
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
    if ($useHarness) { $compose += @('--profile', 'harness') }
    Invoke-DockerChecked -Arguments ($compose + @('config', '--quiet'))
    Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--build', '--wait', '--wait-timeout', '300'))
    Wait-DeploymentHttp -Url 'http://127.0.0.1:11434/api/tags' -TimeoutSeconds 180
    Invoke-DockerChecked -Arguments ($compose + @('exec', '-T', 'ollama', 'ollama', 'pull', 'nomic-embed-text'))
    if ($IncludeAi) { Invoke-DockerChecked -Arguments ($compose + @('exec', '-T', 'ollama', 'ollama', 'pull', $OllamaModel)) }
    Wait-DeploymentHttp -Url 'http://127.0.0.1:8092/health/live' -TimeoutSeconds 180
    if ($useHarness) { Wait-DeploymentHttp -Url 'http://127.0.0.1:8091/health' -TimeoutSeconds 180 }
    Write-Host "本地依赖已就绪。配置和管理员账号保存在：$EnvFile（凭据不输出）。"
    Write-Host "在 platform 目录启动后端：go run ./cmd/iot-platform --env-file `"$EnvFile`""
    Write-Host '在 platform/iot_front 目录启动前端：npm run dev'
    Write-Host '前端：http://localhost:5173；后端：http://localhost:8081'
} finally { Pop-Location }
