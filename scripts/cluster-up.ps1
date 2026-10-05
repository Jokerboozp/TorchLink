# One-click cluster deployment and upgrade (Windows controller).
#
#   powershell -ExecutionPolicy Bypass -File .\scripts\cluster-up.ps1                    # wizard
#   ... -Name torchlink                                  # upgrade a cluster deployed before (no questions)
#   ... -Name torchlink -Capacity off                    # capacity-test module off (on by default)
#   ... -Inventory deploy\cluster\my.yaml                # hand-written inventory
#   ... -Name torchlink -Bundle cluster-images.tar       # online machine: build + pull, save all images, stop
#   ... -Name torchlink -Images cluster-images.tar       # offline controller
#   ... -DryRun
#
# The wizard asks for the node count and addresses, the SSH user (root by
# default), whether all nodes share one SSH password or each has its own, the
# unified service password (databases, Redis, ClickHouse, RustFS, EMQX console,
# platform administrator) and the video module. Passwords stay in memory: SSH
# passwords are used once to install a deployment key (.cluster\<name>\deploy_key).
# Unattended: -Nodes IP,IP,IP plus TORCHLINK_SSH_PASSWORD and
# TORCHLINK_SERVICE_PASSWORD in the environment.
# Then: build images → install the deployment key → secrets → render → check
# nodes → send missing images → start stages, initialise, check readiness.
# Requirements: Docker Desktop with Compose v2 and the OpenSSH client here;
# on every node Docker with Compose v2, usable by the SSH user without sudo.
param(
    [string]$Inventory = "",
    [string]$Name = "",
    [string]$Nodes = "",
    [ValidateSet("", "on", "off")][string]$Video = "",
    [ValidateSet("", "on", "off")][string]$Capacity = "",
    [string]$SshUser = "",
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
$interactive = (-not [Console]::IsInputRedirected)

function Say([string]$Text) { Write-Output ""; Write-Output "== $Text" }
function Invoke-Native([string[]]$Command) {
    if ($DryRun) { Write-Output ("DRY-RUN " + ($Command -join ' ')); return }
    & $Command[0] $Command[1..($Command.Length - 1)]
    if ($LASTEXITCODE -ne 0) { throw ("failed: " + ($Command -join ' ')) }
}
function Ask([string]$Question, [string]$Default = "") {
    $prompt = if ($Default) { "$Question [$Default]" } else { $Question }
    $v = Read-Host $prompt
    if (-not $v) { return $Default }
    return $v.Trim()
}
function Ask-Secret([string]$Question) {
    $secure = Read-Host $Question -AsSecureString
    $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try { return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr) } finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr) }
}
function Test-IP([string]$Value) { $ip = $null; return [Net.IPAddress]::TryParse($Value, [ref]$ip) -and $Value -match '[.:]' }
# Test-Quiet runs a command line through cmd with all output discarded.
function Test-Quiet([string]$CommandLine) {
    & cmd /c "$CommandLine >nul 2>&1"
    return ($LASTEXITCODE -eq 0)
}

# 0. Which cluster: a hand-written inventory, a cluster deployed before, or
# the wizard for a new one.
$generateNodes = ""
if ($Inventory) {
    if (-not (Test-Path $Inventory)) { throw "-Inventory must point at a cluster inventory file" }
    $Inventory = (Resolve-Path $Inventory).Path
    foreach ($l in (Get-Content $Inventory)) { if ($l -match '^name:\s*([^\s#]+)') { $Name = $Matches[1].Trim('"', "'"); break } }
    if (-not $Name) { throw "inventory has no name" }
} else {
    if (-not $Name) { $Name = if ($interactive) { Ask "集群名称" "torchlink" } else { "torchlink" } }
    if ($Name -notmatch '^[a-z][a-z0-9-]*$') { throw "集群名称只能包含小写字母、数字和短横线，并以字母开头" }
    if (-not $StateDir) { $StateDir = Join-Path $projectRoot ".cluster\$Name" }
    $Inventory = Join-Path $StateDir "inventory.yaml"
    if (-not (Test-Path $Inventory)) {
        if ($Nodes) { $generateNodes = $Nodes }
        elseif ($interactive) {
            Say "新集群 $Name"
            while ($true) {
                $count = Ask "节点数量（至少 3 台）" "3"
                if ($count -eq "1") { throw "单台服务器请使用单机部署：scripts\deploy-online.ps1（离线为 scripts\deploy-offline.ps1）" }
                if ($count -eq "2") { Write-Warning "2 个节点无法形成数据库与消息的仲裁，请输入 3 或更多"; continue }
                if ($count -match '^\d+$' -and [int]$count -ge 3) { break }
                Write-Warning "请输入不小于 3 的整数"
            }
            $addresses = @()
            for ($i = 1; $i -le [int]$count; $i++) {
                while ($true) {
                    $ip = Ask "第 $i 个节点的 IP"
                    if (-not (Test-IP $ip)) { Write-Warning "不是有效的 IP 地址"; continue }
                    if ($addresses -contains $ip) { Write-Warning "该 IP 已输入过"; continue }
                    $addresses += $ip; break
                }
            }
            $generateNodes = $addresses -join ','
            if (-not $Video) { $Video = if ((Ask "部署摄像头直播模块？(y/n)" "y") -match '^(n|no)$') { "off" } else { "on" } }
            if (-not $Capacity) { $Capacity = if ((Ask "部署容量测试模块？(y/n)" "y") -match '^(n|no)$') { "off" } else { "on" } }
        } else { throw "no cluster named $Name yet: run interactively, or pass -Nodes IP,IP,IP (or -Inventory)" }
    }
}
if (-not $StateDir) { $StateDir = Join-Path $projectRoot ".cluster\$Name" }
New-Item -ItemType Directory -Force -Path $StateDir | Out-Null
$StateDir = (Resolve-Path $StateDir).Path
if (-not $Secrets) { $Secrets = Join-Path $StateDir "secrets.yaml" }
New-Item -ItemType Directory -Force -Path (Split-Path $Secrets) | Out-Null
$Secrets = Join-Path (Resolve-Path (Split-Path $Secrets)).Path (Split-Path $Secrets -Leaf)
$renderedName = if ($DryRun) { "rendered.dry-run" } else { "rendered" }
$rendered = Join-Path $StateDir $renderedName
$name = $Name

# SSH user and port are remembered per cluster (not secret).
$sshConf = Join-Path $StateDir "ssh.conf"
if (Test-Path $sshConf) {
    foreach ($l in (Get-Content $sshConf)) {
        if (-not $SshUser -and $l -match '^user=(.+)$') { $SshUser = $Matches[1] }
        if ($SshPort -le 0 -and $l -match '^port=(\d+)$') { $SshPort = [int]$Matches[1] }
    }
} elseif ($generateNodes -and $interactive) {
    if (-not $SshUser) { $SshUser = Ask "SSH 用户名" "root" }
    if ($SshPort -le 0) { $SshPort = [int](Ask "SSH 端口" "22") }
}
if (-not $SshUser) { $SshUser = "root" }
if ($SshPort -le 0) { $SshPort = 22 }
$knownHosts = Join-Path $StateDir "known_hosts"
$deployKey = if ($SshKey) { $SshKey } else { Join-Path $StateDir "deploy_key" }

# SSH passwords: asked up front for a new cluster; for an existing one only
# when the deployment key no longer logs in (checked after the image build).
$sshDefaultPassword = $env:TORCHLINK_SSH_PASSWORD
$sshNodePasswords = @()
$sshPasswordsReady = [bool]$sshDefaultPassword
function Get-SshPasswords([string[]]$List) {
    if ($script:sshDefaultPassword) { $script:sshPasswordsReady = $true; return }
    if (-not $interactive) { throw "SSH 登录需要密码：交互运行，或设置 TORCHLINK_SSH_PASSWORD" }
    Write-Output "SSH 登录方式：1) 所有节点统一密码  2) 每个节点独立密码"
    if ((Ask "请选择" "1") -eq "2") {
        foreach ($ip in $List) {
            $pw = Ask-Secret "$SshUser@$ip 的 SSH 密码"
            if (-not $pw) { throw "密码不能为空" }
            $script:sshNodePasswords += "$ip=$pw"
        }
    } else {
        $script:sshDefaultPassword = Ask-Secret "所有节点的 SSH 密码（$SshUser）"
        if (-not $script:sshDefaultPassword) { throw "密码不能为空" }
    }
    $script:sshPasswordsReady = $true
}
if ($generateNodes -and -not $SshKey -and -not (Test-Path $deployKey) -and -not $DryRun) { Get-SshPasswords ($generateNodes -split ',') }

# Unified service password and DeepSeek key: only when the secrets file is
# created; an existing cluster keeps the passwords it was initialised with.
$servicePassword = $env:TORCHLINK_SERVICE_PASSWORD
$deepseekKey = ""
if (-not (Test-Path $Secrets) -and -not $DryRun -and $interactive) {
    if (-not $servicePassword) {
        Write-Output "服务统一密码用于 PostgreSQL、Redis、ClickHouse、RustFS、MQTT、Kafka、EMQX 控制台和平台管理员 admin；至少 8 位，只能包含字母、数字和 . _ ~ -"
        while ($true) {
            $servicePassword = Ask-Secret "服务统一密码（直接回车使用 admin123，内部令牌独立随机）"
            if (-not $servicePassword) { break }
            if ($servicePassword.Length -lt 8 -or $servicePassword -notmatch '^[A-Za-z0-9._~-]+$') { Write-Warning "密码不符合要求，请重新输入"; continue }
            if ((Ask-Secret "再次输入服务统一密码") -eq $servicePassword) { break }
            Write-Warning "两次输入不一致，请重新输入"
        }
    }
    $deepseekKey = Ask-Secret "DeepSeek API Key（可留空，部署后也可在“模型管理”填写）"
}

Say "local tools"
foreach ($tool in @("docker", "ssh", "scp")) { if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is not installed on this machine" } }
if (-not (Test-Quiet "docker info")) { throw "Docker is not running" }
if (-not (Test-Quiet "docker compose version")) { throw "Docker Compose v2 (docker compose) is required" }

# The platform image name comes from the inventory, or the generator's default.
$platformImage = "iot-platform-api:offline"
if (Test-Path $Inventory) {
    $in = $false
    foreach ($l in (Get-Content $Inventory)) {
        if ($l -match '^images:') { $in = $true; continue }
        if ($in -and $l -match '^[^\s#]') { break }
        if ($in -and $l -match '^\s+platform:\s*([^\s#]+)') { $platformImage = $Matches[1].Trim('"', "'") }
    }
}
$ownKeys = @("platform", "web", "harness", "backup", "video", "localAI")
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

# Tools run from the platform image; no Go toolchain is needed. Windows bind
# mounts show every file as mode 0777, so the POSIX mode check is replaced by
# the ACL set on the secrets file below. Input lines go to the tool's stdin.
function Invoke-ImageTool([string]$Entry, [string[]]$ToolArgs, [string[]]$InputLines = @()) {
    $mounts = @("-v", "$(Split-Path $Inventory):/in/inventory", "-v", "$(Split-Path $Secrets):/in/secrets", "-v", "${StateDir}:/state")
    if ($InputLines.Count -gt 0) {
        $out = $InputLines | & docker run --rm -i @mounts --entrypoint "/app/$Entry" $platformImage @ToolArgs
    } else {
        $out = & docker run --rm @mounts --entrypoint "/app/$Entry" $platformImage @ToolArgs
    }
    $script:ToolExit = $LASTEXITCODE
    return $out
}
function Invoke-Tool([string[]]$ToolArgs) {
    $out = Invoke-ImageTool "cluster-render" (@("-inventory", "/in/inventory/$(Split-Path $Inventory -Leaf)", "-no-mode-check") + $ToolArgs)
    if ($script:ToolExit -ne 0) { throw "cluster-render failed" }
    return $out
}
if ($generateNodes) {
    Say "generating inventory $Inventory"
    $videoFlag = if ($Video -eq "off") { "-video=false" } else { "-video=true" }
    $capacityFlag = if ($Capacity -eq "off") { "-capacity=false" } else { "-capacity=true" }
    Invoke-ImageTool "cluster-render" @("-generate", "-name", $name, "-nodes", $generateNodes, $videoFlag, $capacityFlag, "-inventory", "/in/inventory/$(Split-Path $Inventory -Leaf)")
    if ($script:ToolExit -ne 0) { throw "generating the inventory failed" }
} elseif ($Capacity) {
    # Module switch on an existing cluster: only the inventory entry changes.
    Invoke-ImageTool "cluster-render" @("-set-capacity", $Capacity, "-inventory", "/in/inventory/$(Split-Path $Inventory -Leaf)")
    if ($script:ToolExit -ne 0) { throw "switching the capacity module failed" }
}
Set-Content -Path $sshConf -Value @("user=$SshUser", "port=$SshPort")
$inventoryLines = Get-Content $Inventory
$imageList = @(Invoke-Tool @("-print-images") | ForEach-Object { $p = $_ -split ' ', 2; [pscustomobject]@{ Key = $p[0]; Image = $p[1] } })
$nodeAddresses = (@(Invoke-Tool @("-print-nodes") | ForEach-Object { ($_ -split ' ')[1] }) -join ',')

if (-not $Images -and -not $NoBuild) {
    $buildServices = @()
    $envNames = @{ web = @("IOT_PLATFORM_WEB_IMAGE", "platform-web"); harness = @("IOT_DEEPSEEK_HARNESS_IMAGE", "deepseek-harness"); backup = @("IOT_BACKUP_IMAGE", "backup-service"); video = @("IOT_ZLMEDIAKIT_IMAGE", "zlmediakit"); localAI = @("IOT_LOCAL_AI_IMAGE", "embedding") }
    foreach ($i in $imageList) {
        if ($i.Key -eq 'postgres' -and $i.Image -like 'iot-platform-postgres-ha:*') {
            Invoke-Native @('docker', 'build', '--pull', '-t', $i.Image, '-f', (Join-Path $projectRoot 'deploy/postgres/Dockerfile.spilo'), (Join-Path $projectRoot 'deploy/postgres'))
            continue
        }
        if (-not $envNames.ContainsKey($i.Key) -or $i.Image -like "*@sha256:*") { continue }
        Set-Item -Path "env:$($envNames[$i.Key][0])" -Value $i.Image
        $buildServices += $envNames[$i.Key][1]
    }
    if ($buildServices.Count -gt 0) {
        Say "building $($buildServices -join ' ')"
        # compose.yaml requires these runtime secrets even to build; the
        # images never contain them, so placeholders are enough here.
        foreach ($name in 'IOT_JWT_SECRET', 'IOT_ADMIN_PASSWORD') {
            if (-not [Environment]::GetEnvironmentVariable($name)) { Set-Item -Path "env:$name" -Value 'build-only-placeholder' }
        }
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
    Write-Output "copy $Bundle, the checkout and $StateDir to the offline controller and run with -Images $Bundle"
    exit 0
}

# SSH: install the deployment key with the passwords (once), or confirm the
# existing key still works. A key passed with -SshKey is used as is.
if (-not $SshKey) {
    Say "preparing SSH access ($SshUser, port $SshPort)"
    $keyArgs = @("-key", "/state/deploy_key", "-known-hosts", "/state/known_hosts", "-user", $SshUser, "-port", "$SshPort", "-comment", "torchlink-deploy@$name")
    if ($DryRun) { Write-Output "DRY-RUN cluster-ssh bootstrap -nodes $nodeAddresses (passwords on stdin)" }
    else {
        $check = Invoke-ImageTool "cluster-ssh" (@("check") + $keyArgs + @("-nodes", $nodeAddresses))
        if ($script:ToolExit -ne 0) {
            if (-not $sshPasswordsReady) {
                $need = @($check | Where-Object { $_ -match '^fail ' } | ForEach-Object { (($_ -split ' ')[1]).TrimEnd(':') })
                Write-Output "部署密钥尚不能登录：$($need -join ' ')"
                Get-SshPasswords $need
            }
            $lines = @()
            if ($sshDefaultPassword) { $lines += "default=$sshDefaultPassword" }
            $lines += $sshNodePasswords
            $result = Invoke-ImageTool "cluster-ssh" (@("bootstrap") + $keyArgs + @("-nodes", $nodeAddresses)) $lines
            $result | ForEach-Object { Write-Output $_ }
            if ($script:ToolExit -ne 0) { throw "SSH preparation failed on the nodes above (/state/known_hosts above is $knownHosts)" }
        }
    }
    # The private key must be readable by the current user only.
    if (Test-Path $deployKey) { & icacls $deployKey /inheritance:r /grant:r "$($env:USERNAME):(R,W)" | Out-Null }
}
$sshDefaultPassword = ""; $sshNodePasswords = @()
$sshOpts = @("-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "IdentitiesOnly=yes", "-i", $deployKey, "-o", "UserKnownHostsFile=$knownHosts", "-o", "StrictHostKeyChecking=accept-new", "-p", "$SshPort")
function Invoke-Remote([string]$Address, [string]$RemoteCommand) {
    # Windows PowerShell turns redirected native stderr into errors under "Stop".
    $ErrorActionPreference = "Continue"
    $out = & ssh -n @sshOpts "$SshUser@$Address" $RemoteCommand 2>&1
    return [pscustomobject]@{ Ok = ($LASTEXITCODE -eq 0); Output = ($out | ForEach-Object { "$_" } | Out-String) }
}

Say "rendering $name into $rendered"
if ($DryRun) { Remove-Item -Recurse -Force $rendered -ErrorAction SilentlyContinue }
elseif (Test-Path $rendered) {
    Remove-Item -Recurse -Force "$rendered.prev" -ErrorAction SilentlyContinue
    Move-Item $rendered "$rendered.prev"
}
$secretLines = @()
if ($servicePassword) { $secretLines += "servicePassword=$servicePassword" }
if ($deepseekKey) { $secretLines += "deepseekApiKey=$deepseekKey" }
if ($secretLines.Count -eq 0) { $secretLines = @("none=") }
Invoke-ImageTool "cluster-render" @("-inventory", "/in/inventory/$(Split-Path $Inventory -Leaf)", "-no-mode-check", "-secrets", "/in/secrets/$(Split-Path $Secrets -Leaf)", "-init-secrets", "-secrets-stdin", "-out", "/state/$renderedName") $secretLines
if ($script:ToolExit -ne 0) { throw "cluster-render failed" }
$servicePassword = ""; $deepseekKey = ""
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
foreach ($line in $plan | Where-Object { $_ -match '^ports ' }) {
    $f = $line -split ' '
    $node, $address, $ports = $f[1], $f[2], ($f[3..($f.Length - 1)] -join ' ')
    if ($DryRun) { Write-Output "DRY-RUN ssh $SshUser@$address (docker, compose, disk, clock, ports)"; continue }
    # The node's clock is compared with this machine's time around its own check.
    $checkStart = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    $r = Invoke-Remote $address "echo $checkB64 | base64 -d | sh -s -- $name $ports"
    $checkEnd = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    if (-not $r.Ok) { $problems += "${node} (${address}): SSH failed as $SshUser — $($r.Output.Trim())"; continue }
    $v = @{}
    foreach ($l in ($r.Output -split "`n")) { if ($l -match '^(\w+)=(.*)$') { $v[$Matches[1]] = $Matches[2].Trim() } }
    if ($v["docker"] -eq "unusable") { $problems += "${node}: Docker is not installed or $SshUser cannot use it without sudo (add the user to the docker group)" }
    if ($v["compose"] -eq "missing") { $problems += "${node}: Docker Compose v2 plugin is missing" }
    if ($v["disk"] -and [int]$v["disk"] -lt 20) { $problems += "${node}: only $($v["disk"])GiB free for Docker (need at least 20GiB)" }
    if ($v["clock"] -and ([long]$v["clock"] -lt $checkStart - 5 -or [long]$v["clock"] -gt $checkEnd + 5)) { $problems += "${node}: clock differs from this machine by more than 5s; enable NTP/chrony on all nodes" }
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
    $r = Invoke-Remote $address ("for i in " + ($imgs -join ' ') + "; do id=`$(docker image inspect -f '{{.Id}}' `"`$i`" 2>/dev/null | head -n 1); echo `"`${id:-missing}`"; done")
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

$deployArgs = @{ Rendered = $rendered; SshUser = $SshUser; SshKey = $deployKey; KnownHosts = $knownHosts; SshPort = $SshPort }
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
Write-Output "  Administrator: $adminUser; password is adminPassword in $Secrets (the service password when you set one)"
Write-Output "  Back up $StateDir — the databases were initialised with these secrets; deploy_key logs in to the nodes."
Write-Output "  Upgrade: .\scripts\cluster-up.ps1 -Name $name"
Write-Output "  Rollback: .\scripts\cluster-deploy.ps1 -Rendered $rendered.prev -SshUser $SshUser -SshKey $deployKey -KnownHosts $knownHosts -SshPort $SshPort"
