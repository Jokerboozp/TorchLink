<#
.SYNOPSIS
Build and deploy the platform with internet access (Docker + Compose v2 required).
.DESCRIPTION
Creates .env.online once with random credentials, pulls dependency images,
builds the application, PostgreSQL with pgvector and the knowledge embedding /
rerank image (models downloaded and checksum-verified at build time, about
1.3 GB), and checks HTTP readiness. Chat inference calls an external API. Set
IOT_HF_ENDPOINT (e.g. https://hf-mirror.com) or IOT_LLAMA_CPP_IMAGE (a mirror of
the same image) where huggingface.co or ghcr.io is unreachable.
.PARAMETER EnvFile
Environment file, relative to the repository root. Credentials are never replaced.
.PARAMETER Video
on/off deploys or removes the camera live media server; keep (default) reuses the
last choice, and new environments deploy it.
.PARAMETER Capacity
on/off deploys or removes the capacity-test module (运维中心 → 容量测试); keep (default)
reuses the last choice, and new environments leave it off.
.PARAMETER Ops
on/off deploys or removes the monitoring stack (Prometheus, Loki, Grafana,
Alertmanager, Alloy, node-exporter); keep (default) reuses the last choice, and
new environments deploy it.
.PARAMETER ClickHouse
on/off deploys or removes ClickHouse (high-frequency raw messages and telemetry);
off keeps everything in PostgreSQL. keep (default) reuses the last choice, and
new environments deploy it.
#>
[CmdletBinding()]
param(
    [string]$EnvFile = '.env.online',
    [string]$ProjectName = 'iot-platform-online',
    [int]$HealthTimeoutSeconds = 180,
    [ValidateSet('keep', 'on', 'off')][string]$Video = 'keep',
    [ValidateSet('keep', 'on', 'off')][string]$Capacity = 'keep',
    [ValidateSet('keep', 'on', 'off')][string]$Ops = 'keep',
    [ValidateSet('keep', 'on', 'off')][string]$ClickHouse = 'keep'
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
. (Join-Path $scriptDir 'lib/deployment.ps1')
if ($ProjectName -notmatch '^[a-z0-9][a-z0-9_-]*$') { throw 'ProjectName 必须以小写字母或数字开头，且仅包含小写字母、数字、下划线或短横线。' }
if ($HealthTimeoutSeconds -lt 1) { throw 'HealthTimeoutSeconds 必须大于 0。' }
if (-not [IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $projectRoot $EnvFile }
$EnvFile = [IO.Path]::GetFullPath($EnvFile)
Assert-DockerAvailable
Ensure-DeploymentEnv -Path $EnvFile
Ensure-EmqxAdminEnv -Path $EnvFile -DefaultUrl 'http://emqx:18083'
Ensure-MetricsToken -Path $EnvFile
Ensure-ServiceTokens -Path $EnvFile
Ensure-KafkaBindAddress -Path $EnvFile
# HTTPS / MQTTS turn on when tls\tls.crt and tls\tls.key exist (scripts/generate-tls-cert.ps1).
New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot 'tls') | Out-Null
Set-DeepSeekDeploymentEnv -Path $EnvFile
Set-EmbeddingDeploymentEnv -Path $EnvFile

# AI 工作流服务（Harness）为必装组件：告警研判、巡检、报告、协议助手和规则草稿都通过它运行。

if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_URL' -Value 'http://deepseek-harness:8091' }
if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL'))) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MCP_URL' -Value 'http://platform-api:8080/mcp/harness' }
# Live video is deployed by default; an earlier -Video off is kept.
if ($Video -eq 'keep') { $Video = if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_MODULE') -eq 'off') { 'off' } else { 'on' } }
if ($Video -eq 'on') {
    & (Join-Path $scriptDir 'video-module.ps1') prepare -Mode online -EnvFile $EnvFile -ProjectName $ProjectName
} else {
    Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_MEDIA_API_URL' -Value ''
    Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_MODULE' -Value 'off'
    $profiles = @(@("$(Get-DeploymentEnvValue -Path $EnvFile -Key 'COMPOSE_PROFILES')" -split ',') | ForEach-Object { $_.Trim() } | Where-Object { $_ -and $_ -ne 'video' })
    Set-DeploymentEnvValue -Path $EnvFile -Key 'COMPOSE_PROFILES' -Value ($profiles -join ',')
}
# The capacity-test module puts real load on the platform, so it stays off
# unless it was turned on here or earlier.
if ($Capacity -eq 'keep') { $Capacity = if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_CAPACITY_MODULE') -eq 'on') { 'on' } else { 'off' } }
# The monitoring stack is deployed by default; an earlier -Ops off is kept.
if ($Ops -eq 'keep') { $Ops = if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_OPS_MODULE') -eq 'off') { 'off' } else { 'on' } }
Set-OpsModule -Path $EnvFile -State $Ops
# ClickHouse is deployed by default; an earlier -ClickHouse off is kept.
if ($ClickHouse -eq 'keep') { $ClickHouse = if ((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_CLICKHOUSE_MODULE') -eq 'off') { 'off' } else { 'on' } }
Set-ClickHouseModule -Path $EnvFile -State $ClickHouse
$capacityAction = if ($Capacity -eq 'on') { 'prepare' } else { 'unprepare' }
& (Join-Path $scriptDir 'capacity-module.ps1') $capacityAction -Mode online -EnvFile $EnvFile -ProjectName $ProjectName
Add-DeploymentEnvComments -Path $EnvFile
$compose = @('compose', '--project-name', $ProjectName, '--env-file', $EnvFile, '-f', (Join-Path $projectRoot 'compose.yaml'))
$buildServices = @('platform-api', 'platform-web', 'backup-service', 'postgres')
$buildServices += 'deepseek-harness'
# 知识库向量计算与重排（同一镜像，模型在构建时下载并校验）。
$buildServices += @('embedding', 'reranker')
Ensure-HarnessSource -ProjectRoot $projectRoot
Invoke-DockerChecked -Arguments ($compose + @('config', '--quiet'))
$allServices = @(& docker @($compose + @('config', '--services')))
if ($LASTEXITCODE -ne 0) { throw '无法读取 Compose 服务列表。' }
# 摄像头直播媒体服务（video profile，默认启用），由固定 digest 的官方镜像构建。
if (@($allServices | ForEach-Object { $_.Trim() }) -contains 'zlmediakit') { $buildServices += 'zlmediakit' }
# The capacity module runs from the platform image built here; it is never pulled.
$pullServices = @($allServices | ForEach-Object { $_.Trim() } | Where-Object { $_ -and $_ -notin $buildServices -and $_ -ne 'capacity' })
Write-Host '拉取运行依赖镜像……'
Invoke-DockerChecked -Arguments ($compose + @('pull') + $pullServices)
Write-Host '构建 API、前端、备份服务和知识库模型镜像……'
Invoke-DockerChecked -Arguments ($compose + @('build', '--pull') + $buildServices)
Write-Host '启动服务……'
Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--no-build', '--pull', 'never'))
if ($Video -eq 'off') {
    # Profile services are not removed by up; stop a media server left from an earlier deployment.
    Invoke-DockerChecked -Arguments ($compose + @('--profile', 'video', 'rm', '-sf', 'zlmediakit'))
}
if ($Capacity -eq 'off') {
    Invoke-DockerChecked -Arguments ($compose + @('--profile', 'capacity', 'rm', '-sf', 'capacity'))
}
if ($Ops -eq 'off') {
    Invoke-DockerChecked -Arguments ($compose + @('--profile', 'ops', 'rm', '-sf', 'prometheus', 'loki', 'alloy', 'grafana', 'alertmanager', 'node-exporter'))
}
if ($ClickHouse -eq 'off') {
    # The data volume is kept, so turning ClickHouse on again restores its data.
    Invoke-DockerChecked -Arguments ($compose + @('--profile', 'clickhouse', 'rm', '-sf', 'clickhouse', 'clickhouse-tool-admin'))
}

$apiPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_API_PORT'
$webPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_WEB_PORT'
if (-not $apiPort) { $apiPort = '8081' }
if (-not $webPort) { $webPort = '8080' }
Wait-DeploymentHttp -Url "http://127.0.0.1:$apiPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
Wait-DeploymentHttp -Url "http://127.0.0.1:$webPort/" -TimeoutSeconds $HealthTimeoutSeconds
Wait-DeploymentHttp -Url "http://127.0.0.1:$webPort/health/live" -TimeoutSeconds $HealthTimeoutSeconds
$backupPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_BACKUP_HTTP_PORT'
if (-not $backupPort) { $backupPort = '8092' }
Wait-DeploymentHttp -Url "http://127.0.0.1:$backupPort/health/ready" -TimeoutSeconds $HealthTimeoutSeconds
$harnessPort = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_PORT'
if (-not $harnessPort) { $harnessPort = '8091' }
Wait-DeploymentHttp -Url "http://127.0.0.1:$harnessPort/health" -TimeoutSeconds $HealthTimeoutSeconds
$harnessModel = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_AI_HARNESS_MODEL'
Write-Host "Harness 已启动；工作流模型为 $harnessModel。"
Invoke-DockerChecked -Arguments ($compose + @('ps'))
Write-Host "在线部署完成：http://127.0.0.1:$webPort/；登录账号和密码查看 $EnvFile 中 IOT_ADMIN_USER / IOT_ADMIN_PASSWORD。"
if ($Capacity -eq 'on') { Write-Host '容量测试模块已部署：在“运维中心 → 容量测试”选择预设即可运行；关闭用 -Capacity off 或 scripts\capacity-module.ps1 disable。' }
