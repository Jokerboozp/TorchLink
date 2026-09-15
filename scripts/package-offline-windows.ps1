[CmdletBinding()]
param(
    [string]$OutputDir = "offline-bundles",
    [string]$EnvFile = "",
    [switch]$Full,
    [switch]$IncludeAi,
    [switch]$IncludeHarness,
    [switch]$IncludeGb26875,
    [string]$OllamaModel = "qwen3:1.7b",
    [string]$OllamaEmbeddingModel = "nomic-embed-text",
    [switch]$SkipOllamaModel,
    [switch]$SkipDockerRuntime,
    [string]$DockerPackagesDir = "",
    [ValidateSet("generic", "openeuler-24.03-lts-sp4")]
    [string]$TargetOS = "generic"
)

$ErrorActionPreference = "Stop"
if ($env:OS -ne "Windows_NT") { throw "此脚本只能在 Windows 上运行。" }
& (Join-Path $PSScriptRoot "package-offline.ps1") @PSBoundParameters
