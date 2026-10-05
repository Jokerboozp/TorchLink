# Exercises orchestration with a real Compose parser and mocked Docker/HTTP.
# No engine, image build, model download or service restart is performed.
[CmdletBinding()]
param([Parameter(Mandatory)][string]$ComposeExe)
$ErrorActionPreference = 'Stop'
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('iot-deploy-test-' + [guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($testRoot) | Out-Null
$global:IotTest_composeParser = (Resolve-Path $ComposeExe).Path
$scripts = Split-Path $PSScriptRoot -Parent
$global:IotTest_calls = [Collections.Generic.List[object]]::new()
$global:IotTest_httpCalls = [Collections.Generic.List[string]]::new()
$global:IotTest_failBuild = $false
$global:IotTest_missingImage = $false
$global:LASTEXITCODE = 0
. (Join-Path $scripts 'lib/deployment.ps1')

# Entry points dot-source deployment.ps1 again. A global alias takes precedence
# over that function, so orchestration tests never fetch or move real sources.
function global:Invoke-IotTestHarnessSource {
    param([string]$ProjectRoot)
    $global:IotTest_calls.Add(@('mock-harness-source', $ProjectRoot))
}
Set-Alias -Scope Global -Name Ensure-HarnessSource -Value Invoke-IotTestHarnessSource

function Assert($Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function global:docker {
    $callArgs = @($args | ForEach-Object { $_ })
    $global:IotTest_calls.Add($callArgs)
    $global:LASTEXITCODE = 0
    if ($callArgs[0] -eq 'info' -and $callArgs -contains '--format') {
        return 'x86_64'
    } elseif ($callArgs[0] -eq 'compose' -and $callArgs -contains 'config') {
        & $global:IotTest_composeParser @($callArgs | Select-Object -Skip 1)
        $global:LASTEXITCODE = $LASTEXITCODE
    } elseif ($global:IotTest_failBuild -and $callArgs -contains 'build') {
        $global:LASTEXITCODE = 42
    } elseif ($global:IotTest_missingImage -and $callArgs[0] -eq 'image') {
        $global:LASTEXITCODE = 43
    } elseif ($callArgs[0] -eq 'save') {
        [IO.File]::WriteAllText($callArgs[2], 'mock image archive')

    }
}
function global:Invoke-WebRequest {
    param($Uri, $TimeoutSec, [switch]$UseBasicParsing, $OutFile)
    $global:IotTest_httpCalls.Add([string]$Uri)
    if ($OutFile) { [IO.File]::WriteAllText($OutFile, 'mock runtime'); return }
    return [pscustomobject]@{StatusCode=200}
}
function global:go { $global:IotTest_calls.Add(@('go') + $args); $global:LASTEXITCODE = 0 }
function global:npm.cmd { $global:IotTest_calls.Add(@('npm') + $args); $global:LASTEXITCODE = 0 }
function global:npm { $global:IotTest_calls.Add(@('npm') + $args); $global:LASTEXITCODE = 0 }
function Contains-Call([string]$Pattern) { return @($global:IotTest_calls | Where-Object { ($_ -join ' ') -match $Pattern }).Count -gt 0 }
function Assert-CommentedEnv([string]$Path) {
    $previous = ''
    foreach ($line in [IO.File]::ReadAllLines($Path)) {
        if ($line -match '^\s*(?:export\s+)?[A-Za-z_][A-Za-z0-9_]*\s*=') {
            Assert ($previous.StartsWith('# 配置说明：')) "Configuration assignment is missing a Chinese comment: $line"
        }
        $previous = $line
    }
}

# Avoid inherited configuration changing these isolated scenarios.
$savedEnv = @{}
foreach ($item in Get-ChildItem Env:) {
    if ($item.Name -match '^(IOT_|COMPOSE_|POSTGRES_|REDIS_|CLICKHOUSE_|MINIO_|EMQX_|GRAFANA_|DEEPSEEK_)') {
        $savedEnv[$item.Name] = $item.Value
        # PowerShell 7.5 / .NET 9 keeps an empty variable when set to $null; remove it instead.
        Remove-Item -LiteralPath "Env:$($item.Name)" -ErrorAction SilentlyContinue
    }
}
try {
    $localEnv = Join-Path $testRoot '.env.local'
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $localEnv
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_AI_PROVIDER') -eq 'deepseek') 'Local default AI provider is not DeepSeek'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_AI_BASE_URL') -eq 'https://api.deepseek.com') 'Local default DeepSeek URL is missing'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_AI_MODEL') -eq 'deepseek-flash') 'Local default DeepSeek model is missing'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_AI_HARNESS_URL') -eq 'http://127.0.0.1:8091') 'Local Harness URL is missing'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_BACKUP_URL') -eq 'http://127.0.0.1:8092') 'Local backup URL is not pointed at the source host'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_BACKUP_HARNESS_SNAPSHOT_URLS') -eq 'http://127.0.0.1:8091/v1/backup/snapshot') 'Source backup cannot reach Harness snapshot'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_BACKUP_RESTORE_MINIO_ENDPOINT') -eq '127.0.0.1:19001') 'MinIO DR restore endpoint is not the DR API port'
    Assert-CommentedEnv $localEnv
    Assert (Contains-Call 'compose.local.yaml up -d --build --wait') 'Local setup did not start the local services'
    Assert (-not (Contains-Call '--profile harness')) 'Local setup still selects the removed Harness profile'
    Assert (-not (Contains-Call ' up .*backup-service')) 'Local setup unexpectedly started backup-service'
    Assert (Contains-Call 'go mod download') 'Local setup omitted Go dependencies'
    Assert (Contains-Call 'npm ci') 'Local setup omitted npm dependencies'
    Assert (Contains-Call 'compose.local.yaml up -d --build --wait --wait-timeout 900') 'Local setup does not wait for dependency readiness'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_EMBEDDING_URL') -eq 'http://127.0.0.1:18093/v1') 'Local embedding service URL is missing'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_RERANK_URL') -eq 'http://127.0.0.1:18094') 'Local rerank service URL is missing'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_EMBEDDING_API_KEY') -eq '') 'The bundled vector service needs no API key'
    $localModel = & $global:IotTest_composeParser --project-name iot-platform-local --env-file $localEnv -f (Join-Path $scripts '../compose.local.yaml') config --format json | ConvertFrom-Json
    Assert ($LASTEXITCODE -eq 0) 'Local Compose model failed'
    Assert ($localModel.services.PSObject.Properties.Name -notcontains 'platform-api') 'Local setup starts API container'
    Assert ($localModel.services.PSObject.Properties.Name -notcontains 'platform-web') 'Local setup starts Web container'
    Assert ($localModel.services.postgres.image -eq 'iot-platform-postgres:17-pgvector-0.8.1') 'Local PostgreSQL image lacks the pinned pgvector extension'
    Assert ($localModel.services.rustfs.image -eq 'rustfs/rustfs:1.0.1') 'Local object storage is not the pinned RustFS image'
    Assert ($localModel.services.PSObject.Properties.Name -notcontains 'backup-service') 'Local default Compose includes backup-service'
    $localBackupModel = & $global:IotTest_composeParser --project-name iot-platform-local --env-file $localEnv -f (Join-Path $scripts '../compose.local.yaml') --profile backup config --format json | ConvertFrom-Json
    Assert ($LASTEXITCODE -eq 0) 'Local backup Compose model failed'
    Assert ($localBackupModel.services.'backup-service'.environment.IOT_CLICKHOUSE_URL) 'Local backup service has no device-data source'
    Assert (-not $localBackupModel.services.'backup-service'.environment.IOT_BACKUP_TOOL_MODE) 'Backup service still configures external tools'
    Assert (($localModel.services.redpanda.command -join ' ') -match 'external://127.0.0.1:19092') 'Kafka advertises unreachable address'
    foreach ($service in $localModel.services.PSObject.Properties.Value) {
        if ($service.PSObject.Properties.Name -contains 'ports') {
            foreach ($port in $service.ports) { Assert ($port.host_ip -eq '127.0.0.1') 'Local middleware exposed beyond loopback' }
        }
    }
    $localHash = (Get-FileHash $localEnv).Hash
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $localEnv -SkipCodeDeps
    Assert ((Get-FileHash $localEnv).Hash -eq $localHash) 'Local rerun changed configuration'
    Write-Host 'PASS local: code dependencies, isolated services, Kafka listener, stable credentials'

    $brokerDefaults = [ordered]@{
        IOT_KAFKA_SASL_USERNAME = 'admin'
        IOT_KAFKA_SASL_PASSWORD = 'admin123'
        IOT_KAFKA_SASL_MECHANISM = 'SCRAM-SHA-256'
        IOT_KAFKA_ADMIN_USERNAME = 'admin'
        IOT_KAFKA_ADMIN_PASSWORD = 'admin123'
        IOT_KAFKA_ADMIN_URL = 'http://127.0.0.1:19644'
        IOT_KAFKA_PUBLIC_BROKERS = '127.0.0.1:19092'
        IOT_KAFKA_ADVERTISED_HOST = '127.0.0.1'
        IOT_MQTT_TOOL_USERNAME = 'admin'
        IOT_MQTT_TOOL_PASSWORD = 'admin123'
    }
    $brokerCustom = [ordered]@{
        IOT_KAFKA_SASL_USERNAME = 'saved-kafka-user'
        IOT_KAFKA_SASL_PASSWORD = 'saved-kafka-password'
        IOT_KAFKA_SASL_MECHANISM = 'SCRAM-SHA-512'
        IOT_KAFKA_ADMIN_USERNAME = 'saved-kafka-user'
        IOT_KAFKA_ADMIN_PASSWORD = '#saved-admin-password'
        IOT_KAFKA_ADMIN_URL = 'http://broker.example:19644'
        IOT_KAFKA_PUBLIC_BROKERS = 'broker.example:19092'
        IOT_KAFKA_ADVERTISED_HOST = 'broker.example'
        IOT_MQTT_TOOL_USERNAME = 'saved-mqtt-user'
        IOT_MQTT_TOOL_PASSWORD = '#saved-mqtt-password'
    }
    foreach ($scenario in @('empty', 'custom')) {
        $brokerEnv = Join-Path $testRoot ('.env.local-broker-' + $scenario)
        Copy-Item -LiteralPath $localEnv -Destination $brokerEnv
        $assignments = @{}
        $index = 0
        foreach ($key in $brokerDefaults.Keys) {
            $value = if ($scenario -eq 'empty') { '' } else { $brokerCustom[$key] }
            $assignment = switch ($index % 3) {
                0 { "$key='$value'" }
                1 { "  $key `t= '$value'" }
                2 { "`texport `t$key = '$value'" }
            }
            if ($scenario -eq 'empty' -and $key -eq 'IOT_KAFKA_ADMIN_USERNAME') {
                $assignment = "$key= # old empty setting"
            }
            if ($scenario -eq 'custom' -and $key -eq 'IOT_KAFKA_ADMIN_PASSWORD') {
                $assignment = "$key=$value"
            }
            $assignments[$key] = $assignment
            $pattern = '(?m)^[ \t]*(?:export[ \t]+)?' + [regex]::Escape($key) + '[ \t]*=.*$'
            $content = [regex]::Replace([IO.File]::ReadAllText($brokerEnv), $pattern, [System.Text.RegularExpressions.MatchEvaluator]{ param($match) $assignment })
            [IO.File]::WriteAllText($brokerEnv, $content, [Text.UTF8Encoding]::new($false))
            $index++
        }
        Set-DeploymentEnvValue -Path $brokerEnv -Key 'IOT_KAFKA_TLS_CA_FILE' -Value ''
        Set-DeploymentEnvValue -Path $brokerEnv -Key 'IOT_MQTT_WEBSOCKET_PUBLIC_URL' -Value ''
        try {
            # An inherited override must not determine whether file values are
            # blank. In particular it must not suppress filling the empty file.
            [Environment]::SetEnvironmentVariable('IOT_KAFKA_SASL_PASSWORD', 'process-only-password', 'Process')
            & (Join-Path $scripts 'setup-local.ps1') -EnvFile $brokerEnv -SkipCodeDeps
        } finally {
            Remove-Item -LiteralPath 'Env:IOT_KAFKA_SASL_PASSWORD' -ErrorAction SilentlyContinue
        }
        $content = [IO.File]::ReadAllText($brokerEnv)
        foreach ($key in $brokerDefaults.Keys) {
            $expected = if ($scenario -eq 'empty') { $brokerDefaults[$key] } else { $brokerCustom[$key] }
            Assert ((Get-DeploymentEnvValue -Path $brokerEnv -Key $key) -eq $expected) "Broker $scenario configuration incorrect: $key"
            $pattern = '(?m)^[ \t]*(?:export[ \t]+)?' + [regex]::Escape($key) + '[ \t]*='
            Assert ([regex]::Matches($content, $pattern).Count -eq 1) "Broker setting duplicated: $key"
            if ($scenario -eq 'custom') { Assert ($content.Contains($assignments[$key])) "Existing broker assignment was rewritten: $key" }
        }
        Assert ((Get-DeploymentEnvValue -Path $brokerEnv -Key 'IOT_KAFKA_TLS_CA_FILE') -eq '') 'Optional TLS CA was filled'
        Assert ((Get-DeploymentEnvValue -Path $brokerEnv -Key 'IOT_MQTT_WEBSOCKET_PUBLIC_URL') -eq '') 'Unrelated optional MQTT URL was filled'
    }
    Write-Host 'PASS local broker: fill empty defaults, preserve custom/export/space assignments and ignore process overrides when editing'

    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_OPS_CAPACITY_LOCAL') -eq 'true') 'Local source controller is not enabled'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_CAPACITY_MODULE') -eq 'on') 'Local capacity is not on by default'
    $capacityEnv = Join-Path $testRoot '.env.local-capacity'
    Copy-Item -LiteralPath $localEnv -Destination $capacityEnv
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $capacityEnv -SkipCodeDeps -Capacity off
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $capacityEnv -SkipCodeDeps
    Assert ((Get-DeploymentEnvValue -Path $capacityEnv -Key 'IOT_CAPACITY_MODULE') -eq 'off') 'Local rerun lost capacity opt-out'
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $capacityEnv -SkipCodeDeps -Capacity on
    Assert ((Get-DeploymentEnvValue -Path $capacityEnv -Key 'IOT_CAPACITY_MODULE') -eq 'on') 'Local capacity could not be re-enabled'
    Write-Host 'PASS local capacity: default on, opt-out retained and re-enable'

    $videoEnv = Join-Path $testRoot '.env.local-video'
    Copy-Item -LiteralPath $localEnv -Destination $videoEnv
    $sharedOps = Join-Path $testRoot 'shared ops'
    Set-DeploymentEnvValue -Path $videoEnv -Key 'IOT_LOCAL_OPS_DIR' -Value $sharedOps
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $videoEnv -SkipCodeDeps -IncludeOps -Video on -RtcIp 127.0.0.1 -Transcode
    Assert (Test-Path (Join-Path $sharedOps 'alertmanager/alertmanager.yml')) 'Setup did not initialize the shared ops directory'
    Assert ((Get-DeploymentEnvValue -Path $videoEnv -Key 'IOT_OPS_ALERTMANAGER_CONFIG_FILE') -eq "$sharedOps/alertmanager/alertmanager.yml") 'API and Compose use different ops configuration directories'
    Assert ((Get-DeploymentEnvValue -Path $videoEnv -Key 'IOT_VIDEO_MEDIA_API_URL') -eq 'http://127.0.0.1:18580') 'Setup did not configure local video'
    Assert (Contains-Call 'build --pull zlmediakit') 'Setup did not build media service'
    $videoKey = Get-DeploymentEnvValue -Path $videoEnv -Key 'IOT_VIDEO_CREDENTIAL_KEY'
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $videoEnv -SkipCodeDeps -Video off
    Assert (-not (Get-DeploymentEnvValue -Path $videoEnv -Key 'IOT_VIDEO_MEDIA_API_URL')) 'Setup did not disable video'
    Assert ((Get-DeploymentEnvValue -Path $videoEnv -Key 'IOT_VIDEO_CREDENTIAL_KEY') -eq $videoKey) 'Disabling video changed the camera credential key'
    Assert (Contains-Call 'stop zlmediakit') 'Setup did not stop media service'
    Write-Host 'PASS local setup video switch: enable, disable and stable credential key'

    $deepSeekEnv = Join-Path $testRoot '.env.deepseek'
    Copy-Item -LiteralPath $localEnv -Destination $deepSeekEnv
    Add-Content -LiteralPath $deepSeekEnv -Value "IOT_AI_API_KEY='smoke-test-key'"
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $deepSeekEnv -SkipCodeDeps
    Assert ((Get-DeploymentEnvValue -Path $deepSeekEnv -Key 'IOT_AI_PROVIDER') -eq 'deepseek') 'DeepSeek provider was not enabled'
    Assert ((Get-DeploymentEnvValue -Path $deepSeekEnv -Key 'IOT_AI_BASE_URL') -eq 'https://api.deepseek.com') 'DeepSeek base URL was not configured'
    Assert ((Get-DeploymentEnvValue -Path $deepSeekEnv -Key 'IOT_AI_MODEL') -eq 'deepseek-flash') 'DeepSeek model was not configured'
    Assert ((Get-DeploymentEnvValue -Path $deepSeekEnv -Key 'DEEPSEEK_API_KEY') -eq 'smoke-test-key') 'DeepSeek key was not copied for Harness'
    Write-Host 'PASS local deepseek: provider enabled without local chat model download'

    $onlineEnv = Join-Path $testRoot '.env.online'
    & (Join-Path $scripts 'deploy-online.ps1') -EnvFile $onlineEnv
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_PROVIDER') -eq 'deepseek') 'Online default AI provider is not DeepSeek'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_BASE_URL') -eq 'https://api.deepseek.com') 'Online DeepSeek URL is missing'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_MODEL') -eq 'deepseek-flash') 'Online DeepSeek model is missing'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_HARNESS_URL') -eq 'http://deepseek-harness:8091') 'Online Harness URL is missing'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_HARNESS_PROVIDER') -eq 'deepseek-official') 'Online Harness does not use DeepSeek'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_HARNESS_MODEL') -eq 'deepseek-flash') 'Online Harness does not share the DeepSeek model'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_ADMIN_PASSWORD') -eq 'admin123') 'Online default admin password is incorrect'
    foreach ($key in @('SERVICE_ADMIN_PASSWORD', 'POSTGRES_PASSWORD', 'REDIS_PASSWORD', 'CLICKHOUSE_PASSWORD', 'MINIO_ROOT_PASSWORD', 'MINIO_DR_ROOT_PASSWORD', 'EMQX_DASHBOARD_PASSWORD', 'GRAFANA_ADMIN_PASSWORD', 'IOT_MQTT_TOOL_PASSWORD', 'IOT_KAFKA_SASL_PASSWORD', 'IOT_KAFKA_ADMIN_PASSWORD')) {
        Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key $key) -eq 'admin123') "Online tool password incorrect: $key"
    }
    foreach ($key in @('SERVICE_ADMIN_USER', 'MINIO_ROOT_USER', 'MINIO_DR_ROOT_USER', 'EMQX_DASHBOARD_USER', 'GRAFANA_ADMIN_USER', 'IOT_MQTT_TOOL_USERNAME', 'IOT_KAFKA_SASL_USERNAME', 'IOT_KAFKA_ADMIN_USERNAME')) {
        Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key $key) -eq 'admin') "Online tool username incorrect: $key"
    }
    Assert-CommentedEnv $onlineEnv
    Assert (Contains-Call 'build --pull platform-api platform-web backup-service postgres deepseek-harness embedding reranker') 'Online omitted the Harness or knowledge model image build'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_EMBEDDING_URL') -eq 'http://embedding:8080/v1') 'Online embedding service URL is missing'
    $onlineHash = (Get-FileHash $onlineEnv).Hash
    & (Join-Path $scripts 'deploy-online.ps1') -EnvFile $onlineEnv
    Assert ((Get-FileHash $onlineEnv).Hash -eq $onlineHash) 'Online rerun changed configuration'
    $onlineModel = & $global:IotTest_composeParser --env-file $onlineEnv -f (Join-Path $scripts '../compose.yaml') config --format json | ConvertFrom-Json
    Assert ($LASTEXITCODE -eq 0) 'Online Compose model failed'
    Assert ($onlineModel.services.postgres.image -eq 'iot-platform-postgres:17-pgvector-0.8.1') 'Online PostgreSQL image lacks the pinned pgvector extension'
    Assert ($onlineModel.services.postgres.PSObject.Properties.Name -notcontains 'ports') 'Online loaded the local override'
    Assert (Contains-Call 'build --pull platform-api platform-web backup-service') 'Online omitted application image build'
    Assert ($global:IotTest_httpCalls -contains 'http://127.0.0.1:8081/health/ready') 'API readiness was not checked'
    Assert ($global:IotTest_httpCalls -contains 'http://127.0.0.1:8080/') 'Web was not checked'
    Assert ($global:IotTest_httpCalls -contains 'http://127.0.0.1:8092/health/ready') 'Backup was not checked'
    $optionalModel = & $global:IotTest_composeParser --env-file $onlineEnv -f (Join-Path $scripts '../compose.yaml') --profile '*' config --format json | ConvertFrom-Json
    Assert ($LASTEXITCODE -eq 0) 'Optional Compose profiles failed to resolve'
    Assert ($optionalModel.services.'platform-api'.environment.IOT_AI_BASE_URL -eq 'https://api.deepseek.com') 'DeepSeek Provider misconfigured'
    Assert ($optionalModel.services.'deepseek-harness'.environment.DEEPSEEK_BASE_URL -eq 'https://api.deepseek.com') 'Harness inherited an unrelated Provider URL'
    Assert (@($optionalModel.services.PSObject.Properties.Name | Where-Object { $_ -in @('weaviate', 'vllm') }).Count -eq 0) 'Retired local AI services remain'
    Assert ($optionalModel.services.'platform-api'.environment.IOT_EMBEDDING_DIMENSIONS -eq '1024') 'Cloud embedding dimensions missing'
    $global:IotTest_failBuild = $true
    $global:IotTest_calls.Clear()
    $rejected = $false
    try { & (Join-Path $scripts 'deploy-online.ps1') -EnvFile $onlineEnv } catch { $rejected = $true }
    Assert $rejected 'Build failure was ignored'
    Assert (-not (Contains-Call ' up ')) 'Containers started after failed build'
    $global:IotTest_failBuild = $false
    Write-Host 'PASS online: exact Compose selection, readiness, repeatability, AI and failure handling'

    $global:IotTest_calls.Clear()
    $bundleParent = Join-Path $testRoot 'bundles with spaces'
    & (Join-Path $scripts 'package-offline.ps1') -OutputDir $bundleParent
    $bundle = @(Get-ChildItem -LiteralPath $bundleParent -Directory)[0].FullName
    $bundleTar = "$bundle.tar"
    Assert (Test-Path -LiteralPath $bundleTar) 'Complete tar archive omitted'
    $tarChecksum = ((Get-Content -LiteralPath "$bundleTar.sha256" -Raw).Trim() -split '\s+', 2)
    Assert ($tarChecksum[0] -eq (Get-FileHash -LiteralPath $bundleTar).Hash.ToLowerInvariant()) 'Complete tar checksum mismatch'
    Assert ($tarChecksum[1] -eq (Split-Path $bundleTar -Leaf)) 'Archive checksum uses a nonportable path'
    $extractRoot = Join-Path $testRoot 'extracted with spaces'
    New-Item -ItemType Directory -Path $extractRoot | Out-Null
    & tar -xf $bundleTar -C $extractRoot
    Assert ($LASTEXITCODE -eq 0) 'Complete tar extraction failed'
    $extractedBundle = Join-Path $extractRoot (Split-Path $bundle -Leaf)
    $projectRoot = Split-Path -Parent $scripts
    Assert ((Get-FileHash (Join-Path $projectRoot 'README.md')).Hash -eq (Get-FileHash (Join-Path $bundle 'README.md')).Hash) 'Bundle changed README'
    Assert ((Get-FileHash (Join-Path $projectRoot 'iot_front/public/torchlink-logo.png')).Hash -eq (Get-FileHash (Join-Path $bundle 'iot_front/public/torchlink-logo.png')).Hash) 'Bundle omitted or changed README logo'
    $docsRoot = Join-Path $projectRoot 'docs'
    $docs = @(Get-ChildItem -LiteralPath $docsRoot -Recurse -File)
    $bundledDocs = @(Get-ChildItem -LiteralPath (Join-Path $bundle 'docs') -Recurse -File)
    Assert ($docs.Count -eq $bundledDocs.Count) 'Bundle changed documentation file count'
    foreach ($doc in $docs) {
        $relative = $doc.FullName.Substring($docsRoot.Length + 1)
        $bundledDoc = Join-Path (Join-Path $bundle 'docs') $relative
        Assert (Test-Path -LiteralPath $bundledDoc) "Bundle omitted $($doc.Name)"
        Assert ((Get-FileHash -LiteralPath $doc.FullName).Hash -eq (Get-FileHash -LiteralPath $bundledDoc).Hash) "Bundle changed $($doc.Name)"
    }
    Assert (-not (Test-Path (Join-Path $bundle 'DEPLOYMENT.md')) -and -not (Test-Path (Join-Path $bundle 'PLATFORM.md'))) 'Bundle duplicates documentation at root'
    $sourceFiles = @(Get-ChildItem -LiteralPath $bundle -Recurse -Force -File)
    $extractedFiles = @(Get-ChildItem -LiteralPath $extractedBundle -Recurse -Force -File)
    Assert ($sourceFiles.Count -eq $extractedFiles.Count) 'Complete tar changed file count'
    foreach ($file in $sourceFiles) {
        $relative = $file.FullName.Substring($bundle.Length + 1)
        $extractedFile = Join-Path $extractedBundle $relative
        Assert (Test-Path -LiteralPath $extractedFile) "Complete tar omitted $relative"
        Assert ((Get-FileHash -LiteralPath $file.FullName).Hash -eq (Get-FileHash -LiteralPath $extractedFile).Hash) "Complete tar changed $relative"
    }
    Assert (Test-Path -LiteralPath (Join-Path $extractedBundle '.env.offline')) 'Complete tar omitted hidden config'
    & (Join-Path $scripts 'deploy-offline.ps1') -BundleDir $extractedBundle
    Write-Host 'PASS complete tar: checksum, hidden config, identical contents and extracted deployment'
    Assert ((Get-Content (Join-Path $bundle '.env.offline')) -contains 'IOT_VIDEO_RTC_EXTERN_IP=') 'Unconfigured WebRTC address must be written as an empty value'
    Assert ((Get-DeploymentEnvValue -Path (Join-Path $bundle '.env.offline') -Key 'IOT_ADMIN_PASSWORD') -eq 'admin123') 'Offline default admin password is incorrect'
    foreach ($key in @('SERVICE_ADMIN_PASSWORD', 'POSTGRES_PASSWORD', 'REDIS_PASSWORD', 'CLICKHOUSE_PASSWORD', 'MINIO_ROOT_PASSWORD', 'MINIO_DR_ROOT_PASSWORD', 'EMQX_DASHBOARD_PASSWORD', 'GRAFANA_ADMIN_PASSWORD', 'IOT_MQTT_TOOL_PASSWORD', 'IOT_KAFKA_SASL_PASSWORD', 'IOT_KAFKA_ADMIN_PASSWORD')) {
        Assert ((Get-DeploymentEnvValue -Path (Join-Path $bundle '.env.offline') -Key $key) -eq 'admin123') "Offline tool password incorrect: $key"
    }
    Assert (Test-Path (Join-Path $bundle 'deploy/toolaccounts/postgres.sh')) 'Bundle omitted PostgreSQL tool-account initialization'
    Assert (Test-Path (Join-Path $bundle 'deploy/toolaccounts/clickhouse.sh')) 'Bundle omitted ClickHouse tool-account initialization'
    Assert-CommentedEnv (Join-Path $bundle '.env.offline')
    $manifest = Get-Content (Join-Path $bundle 'manifest.json') -Raw | ConvertFrom-Json
    Assert ($manifest.images -contains 'iot-platform-backup:offline') 'Default bundle omitted backup image'
    Assert ($manifest.images -contains 'rustfs/rustfs:1.0.1') 'Default bundle omitted the RustFS image'
    Assert (Contains-Call 'build --pull platform-api platform-web backup-service postgres') 'Offline packaging omitted the application image build'
    Assert ($manifest.images -contains 'iot-platform-postgres:17-pgvector-0.8.1') 'Bundle omitted pgvector PostgreSQL'
    Assert ($manifest.knowledgeStore -eq 'postgres-pgvector' -and $manifest.embeddingRequiresInternet -eq $false) 'Knowledge architecture metadata missing'
    Assert (-not (Test-Path (Join-Path $bundle 'embedding-models.tgz'))) 'Bundle still contains model weights'
    Assert ($manifest.arch -eq 'x86_64') 'Bundle does not record its CPU architecture'
    Assert ($manifest.aiProvider -eq 'deepseek' -and $manifest.aiRequiresInternet) 'Bundle includes a chat model by default or omits DeepSeek metadata'
    Assert ($manifest.profiles -contains 'harness') 'Default bundle omitted Harness'
    foreach ($file in @('docker-24.0.9.tgz', 'docker-28.5.2.tgz', 'docker-compose', 'docker-buildx')) {
        Assert (Test-Path (Join-Path $bundle "docker-runtime/$file.sha256")) "Docker runtime checksum omitted: $file"
    }
    Assert (Test-Path (Join-Path $bundle 'scripts/lib/docker-bootstrap.sh')) 'Docker bootstrap helper omitted'
    Assert ((Get-DeploymentEnvValue -Path (Join-Path $bundle '.env.offline') -Key 'IOT_AI_PROVIDER') -eq 'deepseek') 'Offline default AI provider is not DeepSeek'
    Assert ((Get-DeploymentEnvValue -Path (Join-Path $bundle '.env.offline') -Key 'IOT_AI_MODEL') -eq 'deepseek-flash') 'Offline DeepSeek model is missing'
    Assert ((Get-DeploymentEnvValue -Path (Join-Path $bundle '.env.offline') -Key 'IOT_AI_HARNESS_PROVIDER') -eq 'deepseek-official') 'Offline Harness does not use DeepSeek'
    $bundleHash = (Get-FileHash (Join-Path $bundle '.env.offline')).Hash
    $global:IotTest_calls.Clear()
    & (Join-Path $scripts 'deploy-offline.ps1') -BundleDir $bundle
    & (Join-Path $scripts 'deploy-offline.ps1') -BundleDir $bundle
    Assert ((Get-FileHash (Join-Path $bundle '.env.offline')).Hash -eq $bundleHash) 'Offline deploy rewrote credentials'
    Assert (Contains-Call 'up -d --no-build --pull never') 'Offline up may build or pull'
    Assert (Contains-Call 'up -d --no-build --pull never --wait --wait-timeout 900') 'Offline start does not wait for dependency readiness'
    Assert (-not (Contains-Call ' (build|pull) (?!never)')) 'Offline operation attempted a network build/pull'
    $global:IotTest_missingImage = $true
    $global:IotTest_calls.Clear()
    $rejected = $false
    try { & (Join-Path $scripts 'deploy-offline.ps1') -BundleDir $bundle } catch { $rejected = $true }
    Assert $rejected 'Missing image was ignored'
    Assert (-not (Contains-Call ' up ')) 'Started after detecting a missing image'
    $global:IotTest_missingImage = $false
    [IO.File]::AppendAllText((Join-Path $bundle 'images.tar'), 'corruption')
    $global:IotTest_calls.Clear()
    $rejected = $false
    try { & (Join-Path $scripts 'deploy-offline.ps1') -BundleDir $bundle } catch { $rejected = $true }
    Assert $rejected 'Corrupted archive was accepted'
    Assert (-not (Contains-Call '^load ')) 'Loaded archive before checksum verification'
    Set-DeploymentEnvValue -Path $onlineEnv -Key IOT_AI_MODEL -Value 'qwen3:4b'
    Set-DeploymentEnvValue -Path $onlineEnv -Key IOT_VIDEO_MODULE -Value 'off'
    $global:IotTest_calls.Clear()
    & (Join-Path $scripts 'package-offline.ps1') -OutputDir (Join-Path $testRoot 'existing-config') -EnvFile $onlineEnv
    $disabledBundle = @(Get-ChildItem (Join-Path $testRoot 'existing-config') -Directory)[0].FullName
    $disabledEnv = Get-Content (Join-Path $disabledBundle '.env.offline')
    Assert ($disabledEnv -contains 'IOT_VIDEO_MODULE=off') 'Packaging lost the source video opt-out'
    Assert ($disabledEnv -contains 'IOT_VIDEO_MEDIA_API_URL=') 'Packaging did not clear the disabled media URL'
    $disabledManifest = Get-Content (Join-Path $disabledBundle 'manifest.json') -Raw | ConvertFrom-Json
    Assert ($disabledManifest.images -contains 'iot-zlmediakit:offline') 'Source opt-out should retain the video image for later enablement'

    & (Join-Path $scripts 'package-offline.ps1') -OutputDir (Join-Path $testRoot 'without-video') -WithoutVideo -SkipDockerRuntime -SkipBundleArchive
    $noVideoBundle = @(Get-ChildItem (Join-Path $testRoot 'without-video') -Directory)[0].FullName
    $noVideoEnv = Get-Content (Join-Path $noVideoBundle '.env.offline')
    Assert ($noVideoEnv -contains 'IOT_VIDEO_MODULE=off') 'WithoutVideo did not disable the module'
    Assert ($noVideoEnv -contains 'IOT_VIDEO_MEDIA_API_URL=') 'WithoutVideo did not write an empty media URL'
    $noVideoManifest = Get-Content (Join-Path $noVideoBundle 'manifest.json') -Raw | ConvertFrom-Json
    Assert ($noVideoManifest.profiles -notcontains 'video' -and $noVideoManifest.images -notcontains 'iot-zlmediakit:offline') 'WithoutVideo still packages the video module'
    Assert (-not (Test-Path -LiteralPath "$noVideoBundle.tar")) 'Archive opt-out was ignored'
    function global:tar {
        [IO.File]::WriteAllText($args[1], 'partial archive')
        $global:LASTEXITCODE = 48
    }
    try {
        $failedTarRoot = Join-Path $testRoot 'failed-tar'
        $rejected = $false
        try { & (Join-Path $scripts 'package-offline.ps1') -OutputDir $failedTarRoot -SkipDockerRuntime } catch { $rejected = $true }
        Assert $rejected 'Archive failure ignored'
        Assert (@(Get-ChildItem -LiteralPath $failedTarRoot -File).Count -eq 0) 'Failed tar published an archive or left partial artifacts'
        $failedBundle = @(Get-ChildItem -LiteralPath $failedTarRoot -Directory)[0].FullName
        Assert (Test-Path (Join-Path $failedBundle 'manifest.json')) 'Archive failure removed the completed directory'
    } finally { Remove-Item Function:\tar; $global:LASTEXITCODE = 0 }
    Write-Host 'PASS complete tar: explicit opt-out and cleanup after archive failure'
    Write-Host 'PASS offline: complete default bundle, host entry points, no network, repeatability, missing/corrupt archives'
    Write-Host 'Deployment smoke tests PASS (Docker operations mocked; Compose parsing real).'
} finally {
    Remove-Item Alias:Ensure-HarnessSource -ErrorAction SilentlyContinue
    Remove-Item Function:Invoke-IotTestHarnessSource -ErrorAction SilentlyContinue
    foreach ($key in $savedEnv.Keys) { [Environment]::SetEnvironmentVariable($key, $savedEnv[$key], 'Process') }
    Remove-Item Function:\docker,Function:\Invoke-WebRequest,Function:\go,Function:\npm.cmd,Function:\npm -ErrorAction SilentlyContinue
    # Test fixtures contain random credentials, never real environment values.
    $resolved = [IO.Path]::GetFullPath($testRoot)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
    if ($resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path $resolved -Leaf) -like 'iot-deploy-test-*') {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
