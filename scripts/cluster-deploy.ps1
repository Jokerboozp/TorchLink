# Deploy a rendered cluster (go run ./cmd/cluster-render ...) to its nodes from Windows.
#
#   powershell -ExecutionPolicy Bypass -File .\scripts\cluster-deploy.ps1 -Rendered dist\cluster\iot-cluster -SshUser deploy
#   ... -Stage workers -Nodes n5
#   ... -DryRun
#
# Stages start in order (coordination, data, init, support, workers, edge).
# Requires the OpenSSH client (ssh/scp). Rolling back means running this script
# with the previously rendered directory. Volumes are never deleted.
param(
    [Parameter(Mandatory = $true)][string]$Rendered,
    [string]$SshUser = $env:USERNAME,
    [string]$RemoteDir = "",
    [ValidateSet("all", "coordination", "data", "init", "support", "workers", "edge")][string]$Stage = "all",
    [string]$Nodes = "",
    [string]$Images = "",
    [string]$ClusterInit = "go run ./cmd/cluster-init",
    [int]$HealthTimeout = 300,
    [switch]$DryRun
)
$ErrorActionPreference = "Stop"
$planPath = Join-Path $Rendered "deploy-plan.txt"
if (-not (Test-Path $planPath)) { throw "-Rendered must point at a cluster-render output directory" }
$plan = Get-Content $planPath
$name = ($plan | Where-Object { $_ -match '^# name ' } | Select-Object -First 1) -replace '^# name ', ''
if (-not $name) { throw "deploy plan has no cluster name" }
if (-not $RemoteDir) { $RemoteDir = "/opt/$name" }
$nodeFilter = @($Nodes -split ',' | Where-Object { $_ })

function Invoke-Step([string[]]$Command) {
    if ($DryRun) { Write-Output ("DRY-RUN " + ($Command -join ' ')); return }
    & $Command[0] $Command[1..($Command.Length - 1)]
    if ($LASTEXITCODE -ne 0) { throw ("failed: " + ($Command -join ' ')) }
}
function Test-Node([string]$Node) { return ($nodeFilter.Count -eq 0) -or ($nodeFilter -contains $Node) }
$services = foreach ($line in $plan) {
    $f = $line -split ' '
    if ($f[0] -eq 'service') { [pscustomobject]@{ Stage = $f[1]; Node = $f[2]; Address = $f[3]; Services = $f[4..($f.Length - 1)] } }
}
$copied = @{}
foreach ($s in $services) {
    if (-not (Test-Node $s.Node) -or $copied.ContainsKey($s.Node)) { continue }
    $copied[$s.Node] = $true
    $target = "$SshUser@$($s.Address)"
    Invoke-Step @("ssh", $target, "mkdir -p '$RemoteDir' && chmod 700 '$RemoteDir'")
    Invoke-Step @("scp", "-r", "-p", (Join-Path $Rendered "$($s.Node)/."), "${target}:$RemoteDir/")
    if ($Images) {
        Invoke-Step @("scp", $Images, "${target}:$RemoteDir/images.tar")
        Invoke-Step @("ssh", $target, "docker load -i '$RemoteDir/images.tar'")
    }
}
foreach ($stageName in @("coordination", "data", "init", "support", "workers", "edge")) {
    if ($Stage -ne "all" -and $Stage -ne $stageName) { continue }
    if ($stageName -eq "init") {
        Invoke-Step (@($ClusterInit -split ' ') + @("-env-file", (Join-Path $Rendered "init.env"), "-bootstrap-postgres", "-execute"))
        continue
    }
    Write-Output "== stage $stageName"
    foreach ($s in $services | Where-Object { $_.Stage -eq $stageName -and (Test-Node $_.Node) }) {
        Invoke-Step @("ssh", "$SshUser@$($s.Address)", "cd '$RemoteDir' && docker compose -p $name --env-file .env up -d --no-build --pull never $($s.Services -join ' ')")
    }
    foreach ($s in $services | Where-Object { $_.Stage -eq $stageName -and (Test-Node $_.Node) }) {
        Invoke-Step @("ssh", "$SshUser@$($s.Address)", "cd '$RemoteDir' && timeout $HealthTimeout sh -c 'until ! docker compose -p $name ps --format `"{{.Health}}`" | grep -q -E `"starting|unhealthy`"; do sleep 3; done'")
    }
}
$failed = $false
foreach ($line in $plan) {
    $f = $line -split ' '
    if ($f[0] -ne 'health' -or -not (Test-Node $f[1])) { continue }
    if ($DryRun) { Write-Output "DRY-RUN GET $($f[2])"; continue }
    try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 5 -Uri $f[2] | Out-Null } catch { Write-Warning "not ready: $($f[1]) $($f[2])"; $failed = $true }
}
if ($failed) { throw "some platform processes are not ready; see docker compose logs on those nodes" }
Write-Output "cluster $name deployed; run a quick capacity check before opening traffic (docs/DEPLOYMENT.md)"
