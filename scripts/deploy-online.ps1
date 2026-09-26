<#
.SYNOPSIS
Build and deploy the platform with internet access (Docker + Compose v2 required).
.DESCRIPTION
Creates .env.online once with random credentials, pulls dependency images,
builds the application, downloads the knowledge embedding model, and checks HTTP readiness.
.PARAMETER IncludeAi
Compatibility switch; inference uses DeepSeek API without a bundled chat model.
.PARAMETER IncludeHarness
Compatibility switch; the AI workflow Harness is mandatory and always started.
.PARAMETER NoHarness
Rejected: the AI workflow Harness is a mandatory component.
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
if ($NoHarness) { throw 'AI 工作流服务（Harness）是必装组件，不能使用 -NoHarness。' }
# 判断条件后执行对应操作。
if (-not [IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $projectRoot $EnvFile }
# 执行当前脚本步骤。
$EnvFile = [IO.Path]::GetFullPath($EnvFile)
# 执行当前脚本步骤。
Assert-DockerAvailable
# 执行当前脚本步骤。
Ensure-DeploymentEnv -Path $EnvFile
# 执行当前脚本步骤。
Set-DeepSeekDeploymentEnv -Path $EnvFile

# 判断条件后执行对应操作。
# AI 工作流服务（Harness）为必装组件：告警研判、巡检、报告、协议助手和规则草稿都通过它运行。
if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED') -eq 'false') { Write-Warning 'Harness 已改为必装组件，已将 IOT_AI_HARNESS_ENABLED 改为 true。' }
# 执行当前脚本步骤。
Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED' -Value 'true'
# 执行当前脚本步骤。
$useHarness = $true
# 判断条件后执行对应操作。

# 判断条件后执行对应操作。
if ($useHarness) {
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL' -Value 'http://deepseek-harness:8091' }
    # 判断条件后执行对应操作。
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL' -Value 'http://platform-api:8080/mcp/harness' }
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
