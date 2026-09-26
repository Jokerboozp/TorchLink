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
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
. (Join-Path $scriptDir 'lib/deployment.ps1')
if ($ProjectName -notmatch '^[a-z0-9][a-z0-9_-]*$') { throw 'ProjectName 必须以小写字母或数字开头，且仅包含小写字母、数字、下划线或短横线。' }
if ($HealthTimeoutSeconds -lt 1) { throw 'HealthTimeoutSeconds 必须大于 0。' }
if ($NoHarness) { throw 'AI 工作流服务（Harness）是必装组件，不能使用 -NoHarness。' }
if (-not [IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $projectRoot $EnvFile }
$EnvFile = [IO.Path]::GetFullPath($EnvFile)
Assert-DockerAvailable
Ensure-DeploymentEnv -Path $EnvFile
Set-DeepSeekDeploymentEnv -Path $EnvFile

# AI 工作流服务（Harness）为必装组件：告警研判、巡检、报告、协议助手和规则草稿都通过它运行。
if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED') -eq 'false') { Write-Warning 'Harness 已改为必装组件，已将 IOT_AI_HARNESS_ENABLED 改为 true。' }
Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_ENABLED' -Value 'true'
$useHarness = $true

if ($useHarness) {
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL' -Value 'http://deepseek-harness:8091' }
    if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL' -Value 'http://platform-api:8080/mcp/harness' }
}
Add-DeploymentEnvComments -Path $EnvFile
$compose = @('compose', '--project-name', $ProjectName, '--env-file', $EnvFile, '-f', (Join-Path $projectRoot 'compose.yaml'))
$buildServices = @('platform-api', 'platform-web', 'backup-service')
if ($useHarness) {
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

$apiPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_API_PORT'
$webPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_WEB_PORT'
if (-not $apiPort) { $apiPort = '8081' }
if (-not $webPort) { $webPort = '8080' }
Wait-DeploymentHttp -Url "http://127.0.0.1:$apiPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
Wait-DeploymentHttp -Url "http://127.0.0.1:$webPort/" -TimeoutSeconds $HealthTimeoutSeconds
Wait-DeploymentHttp -Url "http://127.0.0.1:$webPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
$backupPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_BACKUP_HTTP_PORT'
if (-not $backupPort) { $backupPort = '8092' }
Wait-DeploymentHttp -Url "http://127.0.0.1:$backupPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
if ($useHarness) {
    $harnessPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_PORT'
    if (-not $harnessPort) { $harnessPort = '8091' }
    Wait-DeploymentHttp -Url "http://127.0.0.1:$harnessPort/health" -TimeoutSeconds $HealthTimeoutSeconds
    $harnessModel = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MODEL'
    Write-Host "Harness 已启动；工作流模型为 $harnessModel。"
}
Invoke-DockerChecked -Arguments ($compose + @('ps'))
Write-Host "在线部署完成：http://127.0.0.1:$webPort/；登录账号和密码查看 $EnvFile 中 IOT_ADMIN_USER / IOT_ADMIN_PASSWORD。"
