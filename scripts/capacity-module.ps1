<#
.SYNOPSIS
Deploy-level switch for the capacity-test module (Compose profile "capacity").
.DESCRIPTION
This script manages online/offline containers; source runs use the API's local controller.
enable    starts the capacity service (platform image) and points the API at it; the
          platform then shows 运维中心 → 容量测试, where tests run with the operator's
          own permissions. The service token is generated once.
disable   stops and removes the service and hides the page; results and token are kept.
prepare   writes the enable settings without touching containers (used by deploy scripts).
unprepare writes the disable settings without touching containers.
status    shows the container and configuration state.
logs      prints recent service logs.
.EXAMPLE
powershell -ExecutionPolicy Bypass -File .\scripts\capacity-module.ps1 enable
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory, Position = 0)][ValidateSet('enable', 'disable', 'prepare', 'unprepare', 'status', 'logs')][string]$Action,
    [ValidateSet('online', 'offline')][string]$Mode = 'online',
    [string]$EnvFile = '',
    [string]$ProjectName = ''
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
. (Join-Path $scriptDir 'lib/deployment.ps1')

$composeFiles = @('-f', (Join-Path $projectRoot 'compose.yaml'))
switch ($Mode) {
    'online' { if (-not $EnvFile) { $EnvFile = '.env.online' }; if (-not $ProjectName) { $ProjectName = 'iot-platform-online' } }
    'offline' { if (-not $EnvFile) { $EnvFile = '.env.offline' }; if (-not $ProjectName) { $ProjectName = 'iot-platform' }; $composeFiles += @('-f', (Join-Path $projectRoot 'compose.offline.yaml')) }
}
if (-not [IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $projectRoot $EnvFile }
if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) { throw "配置文件不存在：$EnvFile（请先完成部署）" }
$compose = @('compose', '--project-name', $ProjectName, '--env-file', $EnvFile) + $composeFiles + @('--profile', 'capacity')

function Set-CapacityProfile([bool]$On) {
    $current = Get-DeploymentEnvValue -Path $EnvFile -Key 'COMPOSE_PROFILES'
    $items = @(@("$current" -split ',') | ForEach-Object { $_.Trim() } | Where-Object { $_ -and $_ -ne 'capacity' })
    if ($On) { $items += 'capacity' }
    Set-DeploymentEnvValue -Path $EnvFile -Key 'COMPOSE_PROFILES' -Value ($items -join ',')
}
function Write-CapacityConfig([bool]$On) {
    if ($On) {
        if ([string]::IsNullOrWhiteSpace((Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_OPS_CAPACITY_TOKEN'))) {
            Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_OPS_CAPACITY_TOKEN' -Value (New-DeploymentSecret)
        }
        Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_OPS_CAPACITY_URL' -Value 'http://capacity:7080'
        Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_CAPACITY_MODULE' -Value 'on'
    } else {
        Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_OPS_CAPACITY_URL' -Value ''
        Set-DeploymentEnvValue -Path $EnvFile -Key 'IOT_CAPACITY_MODULE' -Value 'off'
    }
    Set-CapacityProfile $On
    Add-DeploymentEnvComments -Path $EnvFile
}

switch ($Action) {
    'prepare' { Write-CapacityConfig $true }
    'unprepare' { Write-CapacityConfig $false }
    'enable' {
        Assert-DockerAvailable
        Write-CapacityConfig $true
        Invoke-DockerChecked -Arguments ($compose + @('config', '--quiet'))
        Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--no-build', '--pull', 'never', 'capacity'))
        Invoke-DockerChecked -Arguments ($compose + @('up', '-d', '--no-deps', '--no-build', 'platform-api'))
        Write-Host '容量测试模块已启用：登录平台后在“运维中心 → 容量测试”选择预设即可运行（需要运维租户的容量测试权限）。'
    }
    'disable' {
        Assert-DockerAvailable
        Write-CapacityConfig $false
        & docker @($compose + @('stop', 'capacity'))
        & docker @($compose + @('rm', '-f', 'capacity'))
        Invoke-DockerChecked -Arguments (@('compose', '--project-name', $ProjectName, '--env-file', $EnvFile) + $composeFiles + @('up', '-d', '--no-deps', '--no-build', 'platform-api'))
        Write-Host '容量测试服务已停止并移除，页面已隐藏。测试结果卷与服务令牌保留；再次 enable 即可恢复。'
    }
    'status' {
        Write-Host "配置文件：$EnvFile"
        $module = Get-DeploymentEnvValue -Path $EnvFile -Key 'IOT_CAPACITY_MODULE'
        if (-not $module) { $module = 'off' }
        Write-Host "模块：$module"
        Invoke-DockerChecked -Arguments ($compose + @('ps', 'capacity'))
    }
    'logs' { Invoke-DockerChecked -Arguments ($compose + @('logs', '--tail', '200', 'capacity')) }
}
