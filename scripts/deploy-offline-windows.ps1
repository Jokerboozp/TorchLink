[CmdletBinding()]
param(
    [string]$BundleDir = "",
    [switch]$SkipHashCheck,
    [switch]$SkipHealthCheck
)

$ErrorActionPreference = "Stop"
if ($env:OS -ne "Windows_NT") { throw "此脚本只能在 Windows 上运行。" }
& (Join-Path $PSScriptRoot "deploy-offline.ps1") @PSBoundParameters
