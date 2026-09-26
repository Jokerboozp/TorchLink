<#
.SYNOPSIS
Deploy-level switch for the optional camera live module (ZLMediaKit, Compose profile "video").
.DESCRIPTION
enable  builds/starts the media server and writes IOT_VIDEO_* settings for the API
        (media secrets and the camera credential key are generated once and never rotated).
disable stops and removes the media server; camera data, live configuration, sealed
        credentials and keys are kept.
status  shows the container and configuration state.
logs    prints recent media server logs (may contain camera addresses; do not publish).
The business switch (whether live is on, who may watch) is set by the platform
administrator on the camera page.
.EXAMPLE
powershell -ExecutionPolicy Bypass -File .\scripts\video-module.ps1 enable -RtcIp 192.168.10.20 -Transcode
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory, Position = 0)][ValidateSet('enable', 'disable', 'status', 'logs')][string]$Action,
    [ValidateSet('online', 'local', 'offline')][string]$Mode = 'online',
    [string]$EnvFile = '',
    [string]$ProjectName = '',
    [string]$RtcIp = '',
    [string]$RtcPort = '',
    [switch]$Transcode,
    [switch]$NoTranscode,
    [string]$AllowedCidrs = ''
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
. (Join-Path $scriptDir 'lib/deployment.ps1')

$composeFiles = @('-f', (Join-Path $projectRoot 'compose.yaml'))
switch ($Mode) {
    'online' { if (-not $EnvFile) { $EnvFile = '.env.online' }; if (-not $ProjectName) { $ProjectName = 'iot-platform-online' } }
    'local' { if (-not $EnvFile) { $EnvFile = '.env.local' }; if (-not $ProjectName) { $ProjectName = 'iot-platform-local' }; $composeFiles = @('-f', (Join-Path $projectRoot 'compose.local.yaml')) }
    'offline' { if (-not $EnvFile) { $EnvFile = '.env.offline' }; if (-not $ProjectName) { $ProjectName = 'iot-platform' }; $composeFiles += @('-f', (Join-Path $projectRoot 'compose.offline.yaml')) }
}
if (-not [IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $projectRoot $EnvFile }
if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) { throw "配置文件不存在：$EnvFile（请先完成对应的部署或本地准备）" }
if ($RtcIp -and $RtcIp -notmatch '^[0-9A-Fa-f.:,]+$') { throw '-RtcIp 只能是 IP 地址，多个用逗号分隔。' }
if ($RtcPort -and $RtcPort -notmatch '^[1-9][0-9]{1,4}$') { throw '-RtcPort 必须是端口号。' }
if ($AllowedCidrs -and $AllowedCidrs -notmatch '^[0-9A-Fa-f.:/,]+$') { throw '-AllowedCidrs 格式无效。' }
if ($Transcode -and $NoTranscode) { throw '-Transcode 与 -NoTranscode 不能同时使用。' }

$compose = @('compose', '--project-name', $ProjectName, '--env-file', $EnvFile) + $composeFiles + @('--profile', 'video')

function Set-VideoProfile([bool]$On) {
    $current = Get-DeploymentEnvValue -Path $EnvFile -Key 'COMPOSE_PROFILES'
    $items = @(@("$current" -split ',') | ForEach-Object { $_.Trim() } | Where-Object { $_ -and $_ -ne 'video' })
    if ($On) { $items += 'video' }
    Set-DeploymentEnvValue -Path $EnvFile -Key 'COMPOSE_PROFILES' -Value ($items -join ',')
}

function Ensure-VideoSecret([string]$Key) {
    if (-not [string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key $Key))) { return }
    if ($Key -eq 'IOT_VIDEO_CREDENTIAL_KEY') {
        # 32 random bytes, standard base64. Never rotated: stored camera passwords depend on it.
        $bytes = New-Object byte[] 32
        $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
        try { $generator.GetBytes($bytes) } finally { $generator.Dispose() }
        $value = [Convert]::ToBase64String($bytes)
    } else {
        $value = New-DeploymentSecret
    }
    Set-DeploymentEnvValue -Path $EnvFile -Key $Key -Value $value
}

function Get-VideoApiUrl {
    if ($Mode -eq 'local') {
        $dependencyHost = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_LOCAL_ADVERTISED_HOST'
        if ([string]::IsNullOrWhiteSpace($dependencyHost)) { $dependencyHost = '127.0.0.1' }
        return "http://${dependencyHost}:18580"
    }
    return 'http://zlmediakit:80'
}

switch ($Action) {
    'enable' {
        Assert-DockerAvailable
        foreach ($key in @('IOT_VIDEO_MEDIA_SECRET', 'IOT_VIDEO_HOOK_SECRET', 'IOT_VIDEO_CREDENTIAL_KEY')) { Ensure-VideoSecret $key }
        if ($RtcIp) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_RTC_EXTERN_IP' -Value $RtcIp }
        if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_RTC_EXTERN_IP'))) {
            if ($Mode -eq 'local') { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_RTC_EXTERN_IP' -Value '127.0.0.1' }
            else { Write-Warning '未设置 -RtcIp，浏览器可能无法建立 WebRTC 连接，播放器会改用 HLS。' }
        }
        if ($RtcPort) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_RTC_PORT' -Value $RtcPort }
        if ($Transcode) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_TRANSCODE_ENABLED' -Value 'true' }
        if ($NoTranscode) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_TRANSCODE_ENABLED' -Value 'false' }
        if ($AllowedCidrs) { Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_ALLOWED_CIDRS' -Value $AllowedCidrs }
        Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_MEDIA_API_URL' -Value (Get-VideoApiUrl)
        Set-VideoProfile $true
        Add-DeploymentEnvComments -Path $EnvFile
        Invoke-DockerChecked -Arguments ($compose + @('config', '--quiet'))
        if ($Mode -eq 'offline') {
            $image = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_ZLMEDIAKIT_IMAGE'
            if ([string]::IsNullOrWhiteSpace($image)) { $image = 'iot-zlmediakit:offline' }
            & docker image inspect $image *> $null
            if ($LASTEXITCODE -ne 0) { throw '离线包未包含媒体服务镜像，请用 -IncludeVideo 重新打包。' }
            Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--no-build', '--pull', 'never', '--wait', '--wait-timeout', '120', 'zlmediakit'))
        } else {
            Invoke-DockerChecked -Arguments ($compose + @('build', '--pull', 'zlmediakit'))
            Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--no-build', '--wait', '--wait-timeout', '120', 'zlmediakit'))
        }
        if ($Mode -eq 'local') {
            Write-Host '媒体服务已启动。请重启本地 API（go run ./cmd/iot-platform --env-file .env.local）以加载 IOT_VIDEO_* 配置。'
        } else {
            Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--no-deps', '--no-build', 'platform-api'))
        }
        Write-Host '直播模块已部署。平台内置管理员登录后在“摄像头映射”页打开直播开关，再为摄像头配置接入。'
    }
    'disable' {
        Assert-DockerAvailable
        Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_MEDIA_API_URL' -Value ''
        Set-VideoProfile $false
        & docker @($compose + @('stop', 'zlmediakit'))
        & docker @($compose + @('rm', '-f', 'zlmediakit'))
        if ($Mode -ne 'local') {
            Invoke-DockerChecked -Arguments (@('compose', '--project-name', $ProjectName, '--env-file', $EnvFile) + $composeFiles + @('up', '-d', '--no-deps', '--no-build', 'platform-api'))
        } else {
            Write-Host '请重启本地 API 使“未部署”状态生效。'
        }
        Write-Host '媒体服务已停止并移除。摄像头资料、直播配置、加密凭据与密钥均已保留；再次 enable 即可恢复。'
    }
    'status' {
        Write-Host "配置文件：$EnvFile"
        $url = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_MEDIA_API_URL'
        if ([string]::IsNullOrWhiteSpace($url)) { $url = '（未设置，API 报告“未部署”）' }
        Write-Host "API 媒体服务地址：$url"
        Write-Host ("WebRTC 地址：{0}，端口：{1}" -f (Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_RTC_EXTERN_IP'), (Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_RTC_PORT'))
        Write-Host ("转码：{0}" -f (Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_VIDEO_TRANSCODE_ENABLED'))
        Invoke-DockerChecked -Arguments ($compose + @('ps', 'zlmediakit'))
    }
    'logs' { Invoke-DockerChecked -Arguments ($compose + @('logs', '--tail', '200', 'zlmediakit')) }
}
