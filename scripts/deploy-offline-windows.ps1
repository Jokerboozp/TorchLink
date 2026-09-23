[CmdletBinding()]
param(
    [string]$BundleDir = "",
    [switch]$SkipHashCheck,
    [switch]$SkipHealthCheck
)

# 执行当前脚本步骤。
$ErrorActionPreference = "Stop"
# 判断条件后执行对应操作。
if ($env:OS -ne "Windows_NT") { throw "此脚本只能在 Windows 上运行。" }
# 执行当前脚本步骤。
& (Join-Path $PSScriptRoot "deploy-offline.ps1") @PSBoundParameters
