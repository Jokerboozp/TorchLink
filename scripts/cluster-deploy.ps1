# Deploy a rendered cluster (cluster-render output) to its nodes from Windows.
#
#   powershell -ExecutionPolicy Bypass -File .\scripts\cluster-deploy.ps1 -Rendered .cluster\iot-cluster\rendered -SshUser deploy
#   ... -Stage workers -Nodes n5
#   ... -DryRun
#
# Stages start in order (coordination, data, init, support, workers, edge).
# cluster-init runs on the first API node inside the platform image (no Go
# toolchain needed; pass -ClusterInit "go run ./cmd/cluster-init" to run it
# locally instead). Requires the OpenSSH client (ssh/scp). Rolling back means
# running this script with the previously rendered directory. Volumes are
# never deleted. For the complete one-click flow use cluster-up.ps1.
param(
    [Parameter(Mandatory = $true)][string]$Rendered,
    [string]$SshUser = $env:USERNAME,
    [string]$SshKey = "",
    [string]$KnownHosts = "",
    [int]$SshPort = 0,
    [string]$RemoteDir = "",
    [ValidateSet("all", "coordination", "data", "init", "support", "workers", "edge")][string]$Stage = "all",
    [string]$Nodes = "",
    [string]$Images = "",
    [string]$ClusterInit = "",
    [int]$HealthTimeout = 300,
    [int]$InitAttempts = 20,
    [switch]$DryRun
)
$ErrorActionPreference = "Stop"
$planPath = Join-Path $Rendered "deploy-plan.txt"
if (-not (Test-Path $planPath)) { throw "-Rendered must point at a cluster-render output directory" }
$plan = Get-Content $planPath
$name = ($plan | Where-Object { $_ -match '^# name ' } | Select-Object -First 1) -replace '^# name ', ''
if (-not $name) { throw "deploy plan has no cluster name" }
$platformImage = ($plan | Where-Object { $_ -match '^# platform-image ' } | Select-Object -First 1) -replace '^# platform-image ', ''
if (-not $RemoteDir) { $RemoteDir = "/opt/$name" }
$nodeFilter = @($Nodes -split ',' | Where-Object { $_ })
$sshOpts = @("-n", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10")
$scpOpts = @("-o", "BatchMode=yes", "-o", "ConnectTimeout=10")
if ($SshKey) { $sshOpts += @("-i", $SshKey, "-o", "IdentitiesOnly=yes"); $scpOpts += @("-i", $SshKey, "-o", "IdentitiesOnly=yes") }
if ($KnownHosts) { $sshOpts += @("-o", "UserKnownHostsFile=$KnownHosts", "-o", "StrictHostKeyChecking=accept-new"); $scpOpts += @("-o", "UserKnownHostsFile=$KnownHosts", "-o", "StrictHostKeyChecking=accept-new") }
if ($SshPort -gt 0) { $sshOpts += @("-p", "$SshPort"); $scpOpts += @("-P", "$SshPort") }

# With -NoThrow the outcome is left in $script:StepOk instead of throwing.
$script:StepOk = $true
function Invoke-Step([string[]]$Command, [switch]$NoThrow) {
    $script:StepOk = $true
    if ($DryRun) { Write-Output ("DRY-RUN " + ($Command -join ' ')); return }
    & $Command[0] $Command[1..($Command.Length - 1)]
    if ($LASTEXITCODE -ne 0) {
        $script:StepOk = $false
        if (-not $NoThrow) { throw ("failed: " + ($Command -join ' ')) }
    }
}
function Invoke-Ssh([string]$Address, [string]$RemoteCommand, [switch]$NoThrow) {
    Invoke-Step (@("ssh") + $sshOpts + @("$SshUser@$Address", $RemoteCommand)) -NoThrow:$NoThrow
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
    Invoke-Ssh $s.Address "mkdir -p '$RemoteDir' && chmod 700 '$RemoteDir'"
    Invoke-Step (@("scp") + $scpOpts + @("-r", "-p", (Join-Path $Rendered "$($s.Node)/."), "${target}:$RemoteDir/"))
    if ($Images) {
        Invoke-Step (@("scp") + $scpOpts + @($Images, "${target}:$RemoteDir/images.tar"))
        Invoke-Ssh $s.Address "docker load -i '$RemoteDir/images.tar'"
    }
}

# cluster-init is idempotent; Patroni and Redpanda may still be electing
# leaders right after the data stage, so it is retried.
function Invoke-Init {
    $initAddress = ""
    $initCAFile = ""
    $initCAMount = ""
    $previousCAFile = $env:IOT_KAFKA_TLS_CA_FILE
    if (Test-Path (Join-Path $Rendered "kafka/ca.pem")) {
        $initCAFile = (Resolve-Path (Join-Path $Rendered "kafka/ca.pem")).Path
        $initCAMount = " --volume '$RemoteDir/.init-kafka-ca.pem:/app/kafka/ca.pem:ro'"
    }
    if (-not $ClusterInit) {
        $health = $plan | Where-Object { $_ -match '^health ' } | Select-Object -First 1
        if (-not $health -or -not $platformImage) { throw "deploy plan lacks a platform node or image for cluster-init" }
        $initAddress = ([uri](($health -split ' ')[2])).Host
        Invoke-Step (@("scp") + $scpOpts + @("-p", (Join-Path $Rendered "init.env"), "$SshUser@${initAddress}:$RemoteDir/.init.env"))
        if ($initCAFile) {
            Invoke-Step (@("scp") + $scpOpts + @("-p", $initCAFile, "$SshUser@${initAddress}:$RemoteDir/.init-kafka-ca.pem"))
        }
    }
    try {
        for ($attempt = 1; ; $attempt++) {
            if ($ClusterInit) {
                if ($initCAFile) { $env:IOT_KAFKA_TLS_CA_FILE = $initCAFile }
                Invoke-Step (@($ClusterInit -split ' ') + @("-env-file", (Join-Path $Rendered "init.env"), "-bootstrap-postgres", "-execute")) -NoThrow
            } else {
                Invoke-Ssh $initAddress "cd '$RemoteDir' && docker run --rm --network host --env-file .init.env$initCAMount --entrypoint /app/cluster-init '$platformImage' -bootstrap-postgres -execute" -NoThrow
            }
            if ($script:StepOk) { break }
            if ($attempt -ge $InitAttempts) { throw "cluster-init did not succeed after $attempt attempts; check PostgreSQL (Patroni leader), Redpanda and ClickHouse on the data nodes" }
            Write-Warning "cluster-init not ready yet (attempt $($attempt + 1)/$InitAttempts), retrying in 15s"
            Start-Sleep -Seconds 15
        }
    } finally {
        $env:IOT_KAFKA_TLS_CA_FILE = $previousCAFile
        if ($initAddress) { Invoke-Ssh $initAddress "rm -f '$RemoteDir/.init.env' '$RemoteDir/.init-kafka-ca.pem'" -NoThrow }
    }
}

foreach ($stageName in @("coordination", "data", "init", "support", "workers", "edge")) {
    if ($Stage -ne "all" -and $Stage -ne $stageName) { continue }
    if ($stageName -eq "init") {
        Write-Output "== stage init (database, schema, topics)"
        Invoke-Init
        continue
    }
    Write-Output "== stage $stageName"
    foreach ($s in $services | Where-Object { $_.Stage -eq $stageName -and (Test-Node $_.Node) }) {
        Invoke-Ssh $s.Address "cd '$RemoteDir' && docker compose -p $name --env-file .env up -d --no-build --pull never $($s.Services -join ' ')"
    }
    foreach ($s in $services | Where-Object { $_.Stage -eq $stageName -and (Test-Node $_.Node) }) {
        Invoke-Ssh $s.Address "cd '$RemoteDir' && timeout $HealthTimeout sh -c 'until ! docker compose -p $name ps --format `"{{.Health}}`" | grep -q -E `"starting|unhealthy`"; do sleep 3; done'"
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
