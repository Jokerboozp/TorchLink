# One-click cluster deployment and upgrade from a cluster inventory (Windows).
#
#   powershell -ExecutionPolicy Bypass -File .\scripts\cluster-up.ps1 -Inventory deploy\cluster\my-cluster.yaml -SshUser deploy
#   ... -Bundle cluster-images.tar      # online machine: build + pull, save all images, stop
#   ... -Images cluster-images.tar      # offline bundle instead of building
#   ... -DryRun
#
# Steps: build the platform images (or load an offline bundle) → generate
# missing secrets → render every node → check the nodes over SSH (Docker,
# Compose, disk, clock, free ports) → send each node only the images it lacks
# → start stages in order, initialise databases and topics, check readiness.
# Running it again upgrades in place with the same secrets; the previous
# rendering is kept for rollback (cluster-deploy.ps1 -Rendered <state>\rendered.prev).
# Requirements: Docker Desktop with Compose v2 and the OpenSSH client here;
# on every node Docker with Compose v2, usable by the SSH user without sudo.
param(
    [Parameter(Mandatory = $true)][string]$Inventory,
    [string]$SshUser = $env:USERNAME,
    [string]$SshKey = "",
    [int]$SshPort = 0,
    [string]$Secrets = "",
    [string]$StateDir = "",
    [string]$Images = "",
    [string]$Bundle = "",
    [switch]$NoBuild,
    [switch]$DryRun,
    [switch]$Yes
)
$ErrorActionPreference = "Stop"
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if (-not (Test-Path $Inventory)) { throw "-Inventory must point at a cluster inventory file" }
$Inventory = (Resolve-Path $Inventory).Path

function Say([string]$Text) { Write-Output ""; Write-Output "== $Text" }
function Invoke-Native([string[]]$Command) {
    if ($DryRun) { Write-Output ("DRY-RUN " + ($Command -join ' ')); return }
    & $Command[0] $Command[1..($Command.Length - 1)]
    if ($LASTEXITCODE -ne 0) { throw ("failed: " + ($Command -join ' ')) }
}
$sshOpts = @("-o", "BatchMode=yes", "-o", "ConnectTimeout=10")
if ($SshKey) { $sshOpts += @("-i", $SshKey) }
if ($SshPort -gt 0) { $sshOpts += @("-p", "$SshPort") }
function Invoke-Remote([string]$Address, [string]$RemoteCommand) {
    # Windows PowerShell turns redirected native stderr into errors under "Stop".
    $ErrorActionPreference = "Continue"
    $out = & ssh -n @sshOpts "$SshUser@$Address" $RemoteCommand 2>&1
    return [pscustomobject]@{ Ok = ($LASTEXITCODE -eq 0); Output = ($out | ForEach-Object { "$_" } | Out-String) }
}
# Test-Quiet runs a command line through cmd with all output discarded.
function Test-Quiet([string]$CommandLine) {
    & cmd /c "$CommandLine >nul 2>&1"
    return ($LASTEXITCODE -eq 0)
}

# Top-level scalar or images.<key> from the inventory, without a YAML parser.
$inventoryLines = Get-Content $Inventory
function Get-Top([string]$Key) {
    foreach ($l in $inventoryLines) { if ($l -match "^${Key}:\s*([^\s#]+)") { return $Matches[1].Trim('"', "'") } }
    return ""
}
function Get-Image([string]$Key) {
    $in = $false
    foreach ($l in $inventoryLines) {
        if ($l -match '^images:') { $in = $true; continue }
        if ($in -and $l -match '^[^\s#]') { break }
        if ($in -and $l -match "^\s+${Key}:\s*([^\s#]+)") { return $Matches[1].Trim('"', "'") }
    }
    return ""
}
$name = Get-Top "name"
if (-not $name) { throw "inventory has no name" }
$platformImage = Get-Image "platform"
if (-not $platformImage) { throw "inventory has no images.platform" }
if (-not $StateDir) { $StateDir = Join-Path $projectRoot ".cluster\$name" }
New-Item -ItemType Directory -Force -Path $StateDir | Out-Null
$StateDir = (Resolve-Path $StateDir).Path
if (-not $Secrets) { $Secrets = Join-Path $StateDir "secrets.yaml" }
New-Item -ItemType Directory -Force -Path (Split-Path $Secrets) | Out-Null
$Secrets = Join-Path (Resolve-Path (Split-Path $Secrets)).Path (Split-Path $Secrets -Leaf)
$renderedName = if ($DryRun) { "rendered.dry-run" } else { "rendered" }
$rendered = Join-Path $StateDir $renderedName

Say "local tools"
foreach ($tool in @("docker", "ssh", "scp")) { if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is not installed on this machine" } }
if (-not (Test-Quiet "docker info")) { throw "Docker is not running" }
if (-not (Test-Quiet "docker compose version")) { throw "Docker Compose v2 (docker compose) is required" }

$ownKeys = @("platform", "web", "harness", "backup", "video")
if ($Images) {
    Say "loading offline image bundle $Images"
    Invoke-Native @("docker", "load", "-i", $Images)
} elseif (-not $NoBuild) {
    Say "building platform image $platformImage"
    Invoke-Native @("docker", "build", "-t", $platformImage, "-f", (Join-Path $projectRoot "Dockerfile"), $projectRoot)
}
if (-not (Test-Quiet "docker image inspect $platformImage")) {
    if ($DryRun) { Write-Output "DRY-RUN platform image not built yet; later steps are listed without the rendered plan"; exit 0 }
    throw "platform image $platformImage is missing (build it, or pass -Images)"
}

function Invoke-Tool([string[]]$ToolArgs) {
    # cluster-render runs from the platform image; no Go toolchain is needed.
    # Windows bind mounts show every file as mode 0777, so the POSIX mode
    # check is replaced by the ACL set on the secrets file below.
    $mounts = @("-v", "$(Split-Path $Inventory):/in/inventory:ro", "-v", "$(Split-Path $Secrets):/in/secrets", "-v", "${StateDir}:/state")
    & docker run --rm @mounts --entrypoint /app/cluster-render $platformImage -inventory "/in/inventory/$(Split-Path $Inventory -Leaf)" -no-mode-check @ToolArgs
    if ($LASTEXITCODE -ne 0) { throw "cluster-render failed" }
}
$imageList = @(Invoke-Tool @("-print-images") | ForEach-Object { $p = $_ -split ' ', 2; [pscustomobject]@{ Key = $p[0]; Image = $p[1] } })

if (-not $Images -and -not $NoBuild) {
    $buildServices = @()
    $envNames = @{ web = @("IOT_PLATFORM_WEB_IMAGE", "platform-web"); harness = @("IOT_DEEPSEEK_HARNESS_IMAGE", "deepseek-harness"); backup = @("IOT_BACKUP_IMAGE", "backup-service"); video = @("IOT_ZLMEDIAKIT_IMAGE", "zlmediakit") }
    foreach ($i in $imageList) {
        if (-not $envNames.ContainsKey($i.Key) -or $i.Image -like "*@sha256:*") { continue }
        Set-Item -Path "env:$($envNames[$i.Key][0])" -Value $i.Image
        $buildServices += $envNames[$i.Key][1]
    }
    if ($buildServices.Count -gt 0) {
        Say "building $($buildServices -join ' ')"
        Invoke-Native (@("docker", "compose", "-f", (Join-Path $projectRoot "compose.yaml"), "--project-directory", $projectRoot, "--profile", "video", "build") + $buildServices)
    }
}

Say "checking third-party images"
$missing = @()
foreach ($i in $imageList) {
    if (Test-Quiet "docker image inspect $($i.Image)") { continue }
    if ($ownKeys -contains $i.Key -and $i.Image -notlike "*@sha256:*") { $missing += $i.Image; continue }
    if ($DryRun) { Write-Output "DRY-RUN docker pull $($i.Image)"; continue }
    & docker pull $i.Image | Out-Null
    if ($LASTEXITCODE -ne 0) { $missing += $i.Image }
}
if ($missing.Count -gt 0) {
    $msg = "images not available locally: $($missing -join ' ') (connect this machine to the registry or pass an offline bundle with -Images)"
    if ($DryRun) { Write-Output "DRY-RUN $msg" } else { throw $msg }
}

if ($Bundle) {
    Say "saving every cluster image into $Bundle"
    Invoke-Native (@("docker", "save", "-o", $Bundle) + @($imageList | ForEach-Object { $_.Image } | Sort-Object -Unique))
    Write-Output "copy $Bundle, the checkout and the inventory to the offline controller and run with -Images $Bundle"
    exit 0
}

Say "rendering $name into $rendered"
if ($DryRun) { Remove-Item -Recurse -Force $rendered -ErrorAction SilentlyContinue }
elseif (Test-Path $rendered) {
    Remove-Item -Recurse -Force "$rendered.prev" -ErrorAction SilentlyContinue
    Move-Item $rendered "$rendered.prev"
}
Invoke-Tool @("-secrets", "/in/secrets/$(Split-Path $Secrets -Leaf)", "-init-secrets", "-out", "/state/$renderedName")
# Only the current user may read the secrets file.
& icacls $Secrets /inheritance:r /grant:r "$($env:USERNAME):(R,W)" | Out-Null
$planPath = Join-Path $rendered "deploy-plan.txt"
if (-not (Test-Path $planPath)) { throw "rendering produced no deploy plan" }
$plan = Get-Content $planPath
$remoteDir = "/opt/$name"

Say "checking nodes"
# The check script is sent base64-encoded so no line endings or quoting
# change on the way from Windows to the node's shell.
$nodeCheck = @'
project="$1"; shift
docker info --format 'docker={{.ServerVersion}}' 2>/dev/null || echo 'docker=unusable'
echo "compose=$(docker compose version --short 2>/dev/null || echo missing)"
usage="$(df -Pk /var/lib/docker 2>/dev/null || df -Pk /)"
echo "disk=$(printf '%s\n' "$usage" | awk 'NR==2 {print int($4/1048576)}')"
echo "clock=$(date +%s)"
echo "running=$(docker ps -q --filter "label=com.docker.compose.project=$project" 2>/dev/null | wc -l | tr -d ' ')"
listening="$( (ss -ltnH 2>/dev/null || netstat -ltn 2>/dev/null) | awk '{print $4}')"
busy=""
for p in "$@"; do
  if printf '%s\n' "$listening" | grep -Eq "[:.]$p\$"; then busy="$busy $p"; fi
done
echo "busy=$busy"
'@ -replace "`r", ""
$checkB64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($nodeCheck))
$problems = @()
$upgradeNodes = @()
$localNow = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
foreach ($line in $plan | Where-Object { $_ -match '^ports ' }) {
    $f = $line -split ' '
    $node, $address, $ports = $f[1], $f[2], ($f[3..($f.Length - 1)] -join ' ')
    if ($DryRun) { Write-Output "DRY-RUN ssh $SshUser@$address (docker, compose, disk, clock, ports)"; continue }
    $r = Invoke-Remote $address "echo $checkB64 | base64 -d | sh -s -- $name $ports"
    if (-not $r.Ok) { $problems += "${node} (${address}): SSH failed as $SshUser — $($r.Output.Trim())"; continue }
    $v = @{}
    foreach ($l in ($r.Output -split "`n")) { if ($l -match '^(\w+)=(.*)$') { $v[$Matches[1]] = $Matches[2].Trim() } }
    if ($v["docker"] -eq "unusable") { $problems += "${node}: Docker is not installed or $SshUser cannot use it without sudo (add the user to the docker group)" }
    if ($v["compose"] -eq "missing") { $problems += "${node}: Docker Compose v2 plugin is missing" }
    if ($v["disk"] -and [int]$v["disk"] -lt 20) { $problems += "${node}: only $($v["disk"])GiB free for Docker (need at least 20GiB)" }
    if ($v["clock"] -and [math]::Abs([long]$v["clock"] - $localNow) -gt 5) { $problems += "${node}: clock differs from this machine by more than 5s; enable NTP/chrony on all nodes" }
    if ($v["running"] -and [int]$v["running"] -gt 0) { $upgradeNodes += $node }
    elseif ($v["busy"]) { $problems += "${node}: ports already in use by other programs: $($v["busy"])" }
}
if ($problems.Count -gt 0) { throw ("node check failed:`n  - " + ($problems -join "`n  - ")) }

if (-not $DryRun -and -not $Yes) {
    $mode = if ($upgradeNodes.Count -gt 0) { "upgrade (running on: $($upgradeNodes -join ' '))" } else { "first deployment" }
    $answer = Read-Host "Ready to deploy ${name}: $mode, $(@($plan | Where-Object { $_ -match '^ports ' }).Count) nodes. Continue? [y/N]"
    if ($answer -notmatch '^(y|yes)$') { throw "cancelled" }
}

Say "distributing images"
foreach ($line in $plan | Where-Object { $_ -match '^images ' }) {
    $f = $line -split ' '
    $node, $address, $imgs = $f[1], $f[2], @($f[3..($f.Length - 1)])
    if ($DryRun) { Write-Output "DRY-RUN docker save <missing of $($imgs.Count)> | ssh $address docker load"; continue }
    $r = Invoke-Remote $address ("for i in " + ($imgs -join ' ') + "; do docker image inspect -f '{{.Id}}' `"`$i`" 2>/dev/null || echo missing; done")
    $remoteIds = @($r.Output -split "`n" | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    $send = @()
    for ($k = 0; $k -lt $imgs.Count; $k++) {
        $localId = (& docker image inspect -f '{{.Id}}' $imgs[$k]).Trim()
        if ($k -ge $remoteIds.Count -or $remoteIds[$k] -ne $localId) { $send += $imgs[$k] }
    }
    if ($send.Count -eq 0) { Write-Output "${node}: images up to date"; continue }
    Write-Output "${node}: sending $($send.Count) image(s): $($send -join ' ')"
    # cmd pipes the binary archive unchanged (PowerShell 5 pipelines re-encode text).
    $sshArgs = ($sshOpts | ForEach-Object { if ($_ -match '\s') { "`"$_`"" } else { $_ } }) -join ' '
    & cmd /c "docker save $($send -join ' ') | ssh $sshArgs $SshUser@$address docker load"
    if ($LASTEXITCODE -ne 0) { throw "sending images to $node failed" }
}

$deployArgs = @{ Rendered = $rendered; SshUser = $SshUser }
if ($SshKey) { $deployArgs.SshKey = $SshKey }
if ($SshPort -gt 0) { $deployArgs.SshPort = $SshPort }
if ($DryRun) { $deployArgs.DryRun = $true }
& (Join-Path $PSScriptRoot "cluster-deploy.ps1") @deployArgs

if (-not $DryRun) {
    $commit = "unknown"
    if (Get-Command git -ErrorAction SilentlyContinue) { $commit = (& cmd /c "git -C `"$projectRoot`" rev-parse --short HEAD 2>nul") }
    Set-Content -Path (Join-Path $StateDir "last-deploy.txt") -Value @("deployed_at=$([DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ'))", "commit=$commit")
}
$adminUser = "admin"
$inEnv = $false
foreach ($l in $inventoryLines) {
    if ($l -match '^env:') { $inEnv = $true; continue }
    if ($inEnv -and $l -match '^[^\s#]') { break }
    if ($inEnv -and $l -match '^\s+IOT_ADMIN_USER:\s*(\S+)') { $adminUser = $Matches[1] }
}
if ($DryRun) {
    Say "dry run finished: nothing was changed on the nodes (rendering in $rendered)"
    foreach ($line in $plan | Where-Object { $_ -match '^entry ' }) { $f = $line -split ' '; Write-Output ("  {0,-12} {1}" -f $f[1], $f[2]) }
    exit 0
}
Say "cluster $name is up"
foreach ($line in $plan | Where-Object { $_ -match '^entry ' }) { $f = $line -split ' '; Write-Output ("  {0,-12} {1}" -f $f[1], $f[2]) }
Write-Output "  Put DNS, a VIP or an external load balancer in front of each group above."
Write-Output "  Administrator: $adminUser; password is adminPassword in $Secrets"
Write-Output "  Back up $Secrets — the cluster's databases were initialised with these values."
Write-Output "  Rollback: .\scripts\cluster-deploy.ps1 -Rendered $rendered.prev -SshUser $SshUser"
