<#
.SYNOPSIS
Build and deploy the platform with internet access (Docker + Compose v2 required).
.DESCRIPTION
Creates .env.online once with random credentials, pulls dependency images,
builds the application, downloads the knowledge embedding model, and checks HTTP readiness.
.PARAMETER IncludeAi
Also download the chat model. A newly generated environment enables Ollama.
.PARAMETER IncludeHarness
Explicitly enable the DeepSeek Harness, which is enabled by default.
.PARAMETER NoHarness
Disable the DeepSeek Harness and persist that choice in the environment file.
.PARAMETER EnvFile
Environment file, relative to platform/. Credentials are never replaced.
#>
[CmdletBinding()]
param(
    [string]$EnvFile = '.env.online',
    [string]$ProjectName = 'iot-platform-online',
    [switch]$IncludeAi,
    [switch]$IncludeHarness,
    [switch]$NoHarness,
    [int]$HealthTimeoutSeconds = 180
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
. (Join-Path $scriptDir 'lib/deployment.ps1')
if ($ProjectName -notmatch '^[a-z0-9][a-z0-9_-]*$') { throw 'ProjectName 必须以小写字母或数字开头，且仅包含小写字母、数字、下划线或短横线。' }
if ($HealthTimeoutSeconds -lt 1) { throw 'HealthTimeoutSeconds 必须大于 0。' }
if ($IncludeHarness -and $NoHarness) { throw 'IncludeHarness 与 NoHarness 不能同时使用。' }
if (-not [IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $projectRoot $EnvFile }
$EnvFile = [IO.Path]::GetFullPath($EnvFile)
Assert-DockerAvailable
$defaults = @{}
if ($IncludeAi) {
    $defaults.IOT_AI_PROVIDER = 'ollama'
    $defaults.IOT_OLLAMA_URL = 'http://ollama:11434'
    $defaults.IOT_AI_BASE_URL = 'http://ollama:11434'
    $defaults.IOT_AI_MODEL = 'qwen3:8b'
}
Ensure-DeploymentEnv -Path $EnvFile -Defaults $defaults
if ($IncludeAi) {
    $provider = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER'
    $model = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_OLLAMA_MODEL'
    if (-not $model) { $model = 'qwen3:8b' }
    if ($provider -eq 'ollama') {
        $configuredModel = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'
        if ($configuredModel) { $model = $configuredModel }
    }
    foreach ($setting in @{'IOT_AI_PROVIDER'='ollama'; 'IOT_OLLAMA_URL'='http://ollama:11434'; 'IOT_AI_BASE_URL'='http://ollama:11434'; 'IOT_AI_MODEL'=$model}.GetEnumerator()) {
        Set-DeploymentEnvValue -Path $EnvFile -Key $setting.Key -Value $setting.Value
    }
}
if ($IncludeHarness) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED' -Value 'true' }
if ($NoHarness) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED' -Value 'false' }
$useHarnessText = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED'
if (-not $useHarnessText) {
    $useHarnessText = 'true'
    Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED' -Value $useHarnessText
}
if ($useHarnessText -notin @('true', 'false')) { throw 'IOT_AI_HARNESS_ENABLED 只能是 true 或 false。' }
$useHarness = $useHarnessText -eq 'true'
if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER') -eq 'deepseek') {
    $deepSeekKey = Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY'
    if ([string]::IsNullOrWhiteSpace($deepSeekKey)) { $deepSeekKey = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_API_KEY' }
    if (-not [string]::IsNullOrWhiteSpace($deepSeekKey) -and [string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY'))) {
        Set-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY' -Value $deepSeekKey
    }
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_BASE_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_BASE_URL' -Value 'https://api.deepseek.com' }
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL' -Value 'deepseek-v4-flash' }
    if ([string]::IsNullOrWhiteSpace($deepSeekKey)) { Write-Warning '请在配置文件中填写 DEEPSEEK_API_KEY，自动研判和 AI 工作流将共用该密钥。' }
}
if ($useHarness) {
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL' -Value 'http://deepseek-harness:8091' }
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL' -Value 'http://platform-api:8080/mcp/harness' }
} else {
    Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL' -Value ''
}
Add-DeploymentEnvComments -Path $EnvFile
$compose = @('compose', '--project-name', $ProjectName, '--env-file', $EnvFile, '-f', (Join-Path $projectRoot 'compose.yaml'))
$buildServices = @('platform-api', 'platform-web', 'backup-service')
if ($useHarness) {
    $compose += @('--profile', 'harness')
    $buildServices += 'deepseek-harness'
    Ensure-HarnessSource -ProjectRoot $projectRoot
}
Invoke-DockerChecked -Arguments ($compose + @('config', '--quiet'))
$allServices = @(& docker @($compose + @('config', '--services')))
if ($LASTEXITCODE -ne 0) { throw '无法读取 Compose 服务列表。' }
$pullServices = @($allServices | ForEach-Object { $_.Trim() } | Where-Object { $_ -and $_ -notin $buildServices })
Write-Host '拉取运行依赖镜像……'
Invoke-DockerChecked -Arguments ($compose + @('pull') + $pullServices)
Write-Host '构建 API、前端和备份服务镜像……'
Invoke-DockerChecked -Arguments ($compose + @('build', '--pull') + $buildServices)
Write-Host '启动服务……'
Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--no-build', '--pull', 'never'))
Write-Host '下载知识库嵌入模型 nomic-embed-text（首次可能需要较长时间）……'
Invoke-DockerChecked -Arguments ($compose + @('exec', '-T', 'ollama', 'ollama', 'pull', 'nomic-embed-text'))
if ($IncludeAi) {
    $model = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'
    if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER') -ne 'ollama' -or -not $model) {
        $model = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_OLLAMA_MODEL'
    }
    if (-not $model) { $model = 'qwen3:8b' }
    Invoke-DockerChecked -Arguments ($compose + @('exec', '-T', 'ollama', 'ollama', 'pull', $model))
    if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER') -ne 'ollama') {
        Write-Host "模型已下载；已有 AI Provider 保持不变。要启用 Ollama，请在 $EnvFile 设置 IOT_AI_PROVIDER=ollama、IOT_OLLAMA_URL=http://ollama:11434 后重跑。"
    }
}
$apiPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_API_PORT'
$webPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_WEB_PORT'
if (-not $apiPort) { $apiPort = '8081' }
if (-not $webPort) { $webPort = '8080' }
Wait-DeploymentHttp -Url "http://127.0.0.1:$apiPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
Wait-DeploymentHttp -Url "http://127.0.0.1:$webPort/" -TimeoutSeconds $HealthTimeoutSeconds
Wait-DeploymentHttp -Url "http://127.0.0.1:$webPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
$backupPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_BACKUP_HTTP_PORT'
if (-not $backupPort) { $backupPort = '8092' }
Wait-DeploymentHttp -Url "http://127.0.0.1:$backupPort/health/live" -TimeoutSeconds $HealthTimeoutSeconds
if ($useHarness) {
    $harnessPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_PORT'
    if (-not $harnessPort) { $harnessPort = '8091' }
    Wait-DeploymentHttp -Url "http://127.0.0.1:$harnessPort/health" -TimeoutSeconds $HealthTimeoutSeconds
    Write-Host "Harness 已启动；自动研判和工作流使用 $EnvFile 中的 DEEPSEEK_API_KEY。"
}
Invoke-DockerChecked -Arguments ($compose + @('ps'))
Write-Host "在线部署完成：http://127.0.0.1:$webPort/；登录账号和密码查看 $EnvFile 中 IOT_ADMIN_USER / IOT_ADMIN_PASSWORD。"
