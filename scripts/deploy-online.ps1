<#
.SYNOPSIS
Build and deploy the platform with internet access (Docker + Compose v2 required).
.DESCRIPTION
Creates .env.online once with random credentials, pulls dependency images,
builds the application, downloads the knowledge embedding model, and checks HTTP readiness.
.PARAMETER IncludeAi
Force an older environment to use the bundled local Ollama model.
.PARAMETER IncludeHarness
Explicitly enable the AI workflow Harness, which is enabled by default.
.PARAMETER NoHarness
Disable the AI workflow Harness and persist that choice in the environment file.
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
# 执行当前脚本步骤。
Set-StrictMode -Version Latest
# 执行当前脚本步骤。
$ErrorActionPreference = 'Stop'
# 执行当前脚本步骤。
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
# 执行当前脚本步骤。
$projectRoot = Split-Path -Parent $scriptDir
# 执行当前脚本步骤。
. (Join-Path $scriptDir 'lib/deployment.ps1')
# 判断条件后执行对应操作。
if ($ProjectName -notmatch '^[a-z0-9][a-z0-9_-]*$') { throw 'ProjectName 必须以小写字母或数字开头，且仅包含小写字母、数字、下划线或短横线。' }
# 判断条件后执行对应操作。
if ($HealthTimeoutSeconds -lt 1) { throw 'HealthTimeoutSeconds 必须大于 0。' }
# 判断条件后执行对应操作。
if ($IncludeHarness -and $NoHarness) { throw 'IncludeHarness 与 NoHarness 不能同时使用。' }
# 判断条件后执行对应操作。
if (-not [IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $projectRoot $EnvFile }
# 执行当前脚本步骤。
$EnvFile = [IO.Path]::GetFullPath($EnvFile)
# 执行当前脚本步骤。
Assert-DockerAvailable
# 执行当前脚本步骤。
Ensure-DeploymentEnv -Path $EnvFile
# 执行当前脚本步骤。
$provider = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER'
# 执行当前脚本步骤。
$configuredModel = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'
# 判断条件后执行对应操作。
if ($IncludeAi -or -not $provider -or $provider -eq 'disabled' -or ($provider -eq 'deepseek' -and $configuredModel -eq 'deepseek-v4-flash') -or ($provider -eq 'ollama' -and $configuredModel -eq 'qwen3:8b')) {
    # 判断条件后执行对应操作。
    if ($provider -eq 'ollama') { $model = $configuredModel } else { $model = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_OLLAMA_MODEL' }
    # 判断条件后执行对应操作。
    if (-not $model -or $model -in @('qwen3:8b', 'deepseek-v4-flash')) { $model = 'qwen3:1.7b' }
    # 遍历数据并执行循环体。
    foreach ($setting in @{'IOT_AI_PROVIDER'='ollama'; 'IOT_OLLAMA_URL'='http://ollama:11434'; 'IOT_OLLAMA_MODEL'=$model; 'IOT_AI_BASE_URL'='http://ollama:11434'; 'IOT_AI_MODEL'=$model}.GetEnumerator()) {
        # 执行当前脚本步骤。
        Set-DeploymentEnvValue -Path $EnvFile -Key $setting.Key -Value $setting.Value
    # 结束当前控制块。
    }
# 结束当前控制块。
}
# 执行当前脚本步骤。
$provider = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER'
# 判断条件后执行对应操作。
if ($provider -eq 'ollama') {
    # 执行当前脚本步骤。
    $model = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'
    # 判断条件后执行对应操作。
    if (-not $model) { $model = 'qwen3:1.7b' }
    # 遍历数据并执行循环体。
    foreach ($setting in @{
        # 执行当前脚本步骤。
        'IOT_OLLAMA_URL'='http://ollama:11434'; 'IOT_OLLAMA_MODEL'=$model;
        # 执行当前脚本步骤。
        'IOT_AI_BASE_URL'='http://ollama:11434'; 'IOT_AI_MODEL'=$model;
        # 执行当前脚本步骤。
        'IOT_AI_HARNESS_PROVIDER'='ollama'; 'IOT_AI_HARNESS_OLLAMA_BASE_URL'='http://ollama:11434/v1';
        # 执行当前脚本步骤。
        'IOT_AI_HARNESS_CONTEXT_WINDOW'='8192'; 'IOT_AI_HARNESS_MODEL'=$model
    # 结束当前控制块。
    }.GetEnumerator()) {
        # 执行当前脚本步骤。
        Set-DeploymentEnvValue -Path $EnvFile -Key $setting.Key -Value $setting.Value
    # 结束当前控制块。
    }
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if ($IncludeHarness) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED' -Value 'true' }
# 判断条件后执行对应操作。
if ($NoHarness) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED' -Value 'false' }
# 执行当前脚本步骤。
$useHarnessText = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED'
# 判断条件后执行对应操作。
if (-not $useHarnessText) {
    # 执行当前脚本步骤。
    $useHarnessText = 'true'
    # 执行当前脚本步骤。
    Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED' -Value $useHarnessText
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if ($useHarnessText -notin @('true', 'false')) { throw 'IOT_AI_HARNESS_ENABLED 只能是 true 或 false。' }
# 执行当前脚本步骤。
$useHarness = $useHarnessText -eq 'true'
# 判断条件后执行对应操作。
if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_PROVIDER') -eq 'deepseek') {
    # 执行当前脚本步骤。
    $deepSeekKey = Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY'
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace($deepSeekKey)) { $deepSeekKey = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_API_KEY' }
    # 判断条件后执行对应操作。
    if (-not [string]::IsNullOrWhiteSpace($deepSeekKey) -and [string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY'))) {
        # 执行当前脚本步骤。
        Set-DeploymentEnvValue -Path $EnvFile -Key 'DEEPSEEK_API_KEY' -Value $deepSeekKey
    # 结束当前控制块。
    }
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_BASE_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_BASE_URL' -Value 'https://api.deepseek.com' }
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_MODEL' -Value 'deepseek-v4-flash' }
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace($deepSeekKey)) { Write-Warning '请在配置文件中填写 DEEPSEEK_API_KEY，自动研判和 AI 工作流将共用该密钥。' }
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if ($useHarness) {
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL' -Value 'http://deepseek-harness:8091' }
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL' -Value 'http://platform-api:8080/mcp/harness' }
# 结束当前控制块。
} else {
    # 执行当前脚本步骤。
    Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL' -Value ''
# 结束当前控制块。
}
# 执行当前脚本步骤。
Add-DeploymentEnvComments -Path $EnvFile
# 执行当前脚本步骤。
$compose = @('compose', '--project-name', $ProjectName, '--env-file', $EnvFile, '-f', (Join-Path $projectRoot 'compose.yaml'))
# 执行当前脚本步骤。
$buildServices = @('platform-api', 'platform-web', 'backup-service')
# 判断条件后执行对应操作。
if ($useHarness) {
    # 执行当前脚本步骤。
    $compose += @('--profile', 'harness')
    # 执行当前脚本步骤。
    $buildServices += 'deepseek-harness'
    # 执行当前脚本步骤。
    Ensure-HarnessSource -ProjectRoot $projectRoot
# 结束当前控制块。
}
# 执行当前脚本步骤。
Invoke-DockerChecked -Arguments ($compose + @('config', '--quiet'))
# 执行当前脚本步骤。
$allServices = @(& docker @($compose + @('config', '--services')))
# 判断条件后执行对应操作。
if ($LASTEXITCODE -ne 0) { throw '无法读取 Compose 服务列表。' }
# 执行当前脚本步骤。
$pullServices = @($allServices | ForEach-Object { $_.Trim() } | Where-Object { $_ -and $_ -notin $buildServices })
# 执行当前脚本步骤。
Write-Host '拉取运行依赖镜像……'
# 执行当前脚本步骤。
Invoke-DockerChecked -Arguments ($compose + @('pull') + $pullServices)
# 执行当前脚本步骤。
Write-Host '构建 API、前端和备份服务镜像……'
# 执行当前脚本步骤。
Invoke-DockerChecked -Arguments ($compose + @('build', '--pull') + $buildServices)
# 执行当前脚本步骤。
Write-Host '启动服务……'
# 执行当前脚本步骤。
Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--no-build', '--pull', 'never'))
# 执行当前脚本步骤。
Write-Host '下载知识库嵌入模型 nomic-embed-text（首次可能需要较长时间）……'
# 执行当前脚本步骤。
Invoke-DockerChecked -Arguments ($compose + @('exec', '-T', 'ollama', 'ollama', 'pull', 'nomic-embed-text'))
# 判断条件后执行对应操作。
if ($provider -eq 'ollama') {
    # 判断条件后执行对应操作。
    if (-not $model) { $model = 'qwen3:1.7b' }
    # 执行当前脚本步骤。
    Write-Host "下载统一 AI 模型 $model（告警研判与工作流共用）……"
    # 执行当前脚本步骤。
    Invoke-DockerChecked -Arguments ($compose + @('exec', '-T', 'ollama', 'ollama', 'pull', $model))
# 结束当前控制块。
}
# 执行当前脚本步骤。
$apiPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_API_PORT'
# 执行当前脚本步骤。
$webPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_WEB_PORT'
# 判断条件后执行对应操作。
if (-not $apiPort) { $apiPort = '8081' }
# 判断条件后执行对应操作。
if (-not $webPort) { $webPort = '8080' }
# 执行当前脚本步骤。
Wait-DeploymentHttp -Url "http://127.0.0.1:$apiPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
# 执行当前脚本步骤。
Wait-DeploymentHttp -Url "http://127.0.0.1:$webPort/" -TimeoutSeconds $HealthTimeoutSeconds
# 执行当前脚本步骤。
Wait-DeploymentHttp -Url "http://127.0.0.1:$webPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
# 执行当前脚本步骤。
$backupPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_BACKUP_HTTP_PORT'
# 判断条件后执行对应操作。
if (-not $backupPort) { $backupPort = '8092' }
# 执行当前脚本步骤。
Wait-DeploymentHttp -Url "http://127.0.0.1:$backupPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
# 判断条件后执行对应操作。
if ($useHarness) {
    # 执行当前脚本步骤。
    $harnessPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_PORT'
    # 判断条件后执行对应操作。
    if (-not $harnessPort) { $harnessPort = '8091' }
    # 执行当前脚本步骤。
    Wait-DeploymentHttp -Url "http://127.0.0.1:$harnessPort/health" -TimeoutSeconds $HealthTimeoutSeconds
    # 执行当前脚本步骤。
    $harnessModel = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MODEL'
    # 执行当前脚本步骤。
    Write-Host "Harness 已启动；工作流模型为 $harnessModel。"
# 结束当前控制块。
}
# 执行当前脚本步骤。
Invoke-DockerChecked -Arguments ($compose + @('ps'))
# 执行当前脚本步骤。
Write-Host "在线部署完成：http://127.0.0.1:$webPort/；登录账号和密码查看 $EnvFile 中 IOT_ADMIN_USER / IOT_ADMIN_PASSWORD。"
