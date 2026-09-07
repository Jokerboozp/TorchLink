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

function Assert($Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function global:docker {
    $callArgs = @($args | ForEach-Object { $_ })
    $global:IotTest_calls.Add($callArgs)
    $global:LASTEXITCODE = 0
    if ($callArgs[0] -eq 'compose' -and $callArgs -contains 'config') {
        & $global:IotTest_composeParser @($callArgs | Select-Object -Skip 1)
        $global:LASTEXITCODE = $LASTEXITCODE
    } elseif ($global:IotTest_failBuild -and $callArgs -contains 'build') {
        $global:LASTEXITCODE = 42
    } elseif ($global:IotTest_missingImage -and $callArgs[0] -eq 'image') {
        $global:LASTEXITCODE = 43
    } elseif ($callArgs[0] -eq 'save') {
        [IO.File]::WriteAllText($callArgs[2], 'mock image archive')
    } elseif ($callArgs[0] -eq 'run' -and ($callArgs[-1] -like 'tar -czf*')) {
        $mount = @($callArgs | Where-Object { $_ -like 'type=bind,*target=/backup' })[0]
        $destination = $mount -replace '^type=bind,source=', '' -replace ',target=/backup$', ''
        [IO.File]::WriteAllText((Join-Path $destination 'ollama-data.tgz'), 'mock model archive')
    }
}
function global:Invoke-WebRequest {
    param($Uri, $TimeoutSec, [switch]$UseBasicParsing)
    $global:IotTest_httpCalls.Add([string]$Uri)
    return [pscustomobject]@{StatusCode=200}
}
function global:go { $global:IotTest_calls.Add(@('go') + $args); $global:LASTEXITCODE = 0 }
function global:npm.cmd { $global:IotTest_calls.Add(@('npm') + $args); $global:LASTEXITCODE = 0 }
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
        [Environment]::SetEnvironmentVariable($item.Name, $null, 'Process')
    }
}
try {
    $localEnv = Join-Path $testRoot '.env.local'
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $localEnv
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_AI_PROVIDER') -eq 'deepseek') 'Local default AI provider is not DeepSeek'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_AI_BASE_URL') -eq 'https://api.deepseek.com') 'Local default DeepSeek URL is missing'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_AI_MODEL') -eq 'deepseek-v4-flash') 'Local default DeepSeek model is missing'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_AI_HARNESS_ENABLED') -eq 'true') 'Local Harness is not enabled by default'
    Assert ((Get-DeploymentEnvValue -Path $localEnv -Key 'IOT_AI_HARNESS_URL') -eq 'http://127.0.0.1:8091') 'Local Harness URL is missing'
    Assert-CommentedEnv $localEnv
    Assert (Contains-Call '--profile harness up -d --build --wait') 'Local setup did not start the default Harness profile'
    Assert (Contains-Call 'go mod download') 'Local setup omitted Go dependencies'
    Assert (Contains-Call 'npm ci') 'Local setup omitted npm dependencies'
    Assert (Contains-Call 'exec -T ollama ollama pull nomic-embed-text') 'Embedding model omitted'
    $localModel = & $global:IotTest_composeParser --project-name iot-platform-local --env-file $localEnv -f (Join-Path $scripts '../compose.local.yaml') config --format json | ConvertFrom-Json
    Assert ($LASTEXITCODE -eq 0) 'Local Compose model failed'
    Assert ($localModel.services.PSObject.Properties.Name -notcontains 'platform-api') 'Local setup starts API container'
    Assert ($localModel.services.PSObject.Properties.Name -notcontains 'platform-web') 'Local setup starts Web container'
    Assert ($localModel.services.postgres.image -eq 'postgres:17-alpine3.22') 'Local PostgreSQL image is not pinned to the CentOS 7 compatible Alpine release'
    Assert ($localModel.services.'backup-service'.depends_on.'redpanda-init'.condition -eq 'service_completed_successfully') 'Local backup service does not consume the successful Redpanda initialization job'
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

    $noHarnessEnv = Join-Path $testRoot '.env.no-harness'
    Copy-Item -LiteralPath $localEnv -Destination $noHarnessEnv
    Set-DeploymentEnvValue -Path $noHarnessEnv -Key 'IOT_AI_HARNESS_ENABLED' -Value 'false'
    $global:IotTest_calls.Clear()
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $noHarnessEnv -SkipCodeDeps
    Assert ((Get-DeploymentEnvValue -Path $noHarnessEnv -Key 'IOT_AI_HARNESS_URL') -eq '') 'Disabled Harness retained an active URL'
    Assert (-not (Contains-Call '--profile harness')) 'Environment file did not disable the Harness profile'
    Write-Host 'PASS local configuration: Harness can be disabled in the environment file'

    $deepSeekEnv = Join-Path $testRoot '.env.deepseek'
    Copy-Item -LiteralPath $localEnv -Destination $deepSeekEnv
    Add-Content -LiteralPath $deepSeekEnv -Value "IOT_AI_API_KEY='smoke-test-key'"
    & (Join-Path $scripts 'setup-local.ps1') -EnvFile $deepSeekEnv -SkipCodeDeps -IncludeDeepSeek
    Assert ((Get-DeploymentEnvValue -Path $deepSeekEnv -Key 'IOT_AI_PROVIDER') -eq 'deepseek') 'DeepSeek provider was not enabled'
    Assert ((Get-DeploymentEnvValue -Path $deepSeekEnv -Key 'IOT_AI_BASE_URL') -eq 'https://api.deepseek.com') 'DeepSeek base URL was not configured'
    Assert ((Get-DeploymentEnvValue -Path $deepSeekEnv -Key 'IOT_AI_MODEL') -eq 'deepseek-v4-flash') 'DeepSeek model was not configured'
    Assert ((Get-DeploymentEnvValue -Path $deepSeekEnv -Key 'DEEPSEEK_API_KEY') -eq 'smoke-test-key') 'DeepSeek key was not copied for Harness'
    Assert (-not (Contains-Call 'ollama pull qwen3:1.7b')) 'DeepSeek setup attempted an Ollama chat model download'
    Write-Host 'PASS local deepseek: provider enabled without local chat model download'

    $onlineEnv = Join-Path $testRoot '.env.online'
    & (Join-Path $scripts 'deploy-online.ps1') -EnvFile $onlineEnv
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_PROVIDER') -eq 'ollama') 'Online default AI provider is not Ollama'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_BASE_URL') -eq 'http://ollama:11434') 'Online Ollama URL is missing'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_MODEL') -eq 'qwen3:1.7b') 'Online compact Qwen model is missing'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_HARNESS_ENABLED') -eq 'true') 'Online Harness is not enabled by default'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_HARNESS_URL') -eq 'http://deepseek-harness:8091') 'Online Harness URL is missing'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_HARNESS_PROVIDER') -eq 'ollama') 'Online Harness does not use Ollama'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_HARNESS_MODEL') -eq 'qwen3:1.7b') 'Online Harness does not share the compact Qwen model'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key 'IOT_AI_HARNESS_OLLAMA_BASE_URL') -eq 'http://ollama:11434/v1') 'Online Harness Ollama endpoint is missing'
    Assert-CommentedEnv $onlineEnv
    Assert (Contains-Call 'build --pull platform-api platform-web backup-service deepseek-harness') 'Online omitted the default Harness image build'
    Assert (Contains-Call 'exec -T ollama ollama pull qwen3:1.7b') 'Online omitted the compact Qwen model'
    $onlineHash = (Get-FileHash $onlineEnv).Hash
    & (Join-Path $scripts 'deploy-online.ps1') -EnvFile $onlineEnv
    Assert ((Get-FileHash $onlineEnv).Hash -eq $onlineHash) 'Online rerun changed configuration'
    $onlineModel = & $global:IotTest_composeParser --env-file $onlineEnv -f (Join-Path $scripts '../compose.yaml') config --format json | ConvertFrom-Json
    Assert ($LASTEXITCODE -eq 0) 'Online Compose model failed'
    Assert ($onlineModel.services.postgres.image -eq 'postgres:17-alpine3.22') 'Online PostgreSQL image is not pinned to the compatible Alpine release'
    Assert ($onlineModel.services.postgres.PSObject.Properties.Name -notcontains 'ports') 'Online loaded the local override'
    Assert (Contains-Call 'build --pull platform-api platform-web backup-service') 'Online omitted application image build'
    Assert ($global:IotTest_httpCalls -contains 'http://127.0.0.1:8081/health/ready') 'API readiness was not checked'
    Assert ($global:IotTest_httpCalls -contains 'http://127.0.0.1:8080/') 'Web was not checked'
    Assert ($global:IotTest_httpCalls -contains 'http://127.0.0.1:8092/health/live') 'Backup was not checked'
    $originalPassword = Get-DeploymentEnvValue -Path $onlineEnv -Key IOT_ADMIN_PASSWORD
    & (Join-Path $scripts 'deploy-online.ps1') -EnvFile $onlineEnv -IncludeAi
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key IOT_AI_PROVIDER) -eq 'ollama') 'Explicit AI flag failed to enable Ollama'
    Assert ((Get-DeploymentEnvValue -Path $onlineEnv -Key IOT_ADMIN_PASSWORD) -eq $originalPassword) 'AI enablement rotated credentials'
    $optionalModel = & $global:IotTest_composeParser --env-file $onlineEnv -f (Join-Path $scripts '../compose.yaml') --profile '*' config --format json | ConvertFrom-Json
    Assert ($LASTEXITCODE -eq 0) 'Optional Compose profiles failed to resolve'
    Assert ($optionalModel.services.'platform-api'.environment.IOT_AI_BASE_URL -eq 'http://ollama:11434') 'Ollama Provider misconfigured'
    Assert ($optionalModel.services.'deepseek-harness'.environment.DEEPSEEK_BASE_URL -eq 'https://api.deepseek.com') 'Harness inherited the unrelated Ollama Provider URL'
    $global:IotTest_failBuild = $true
    $global:IotTest_calls.Clear()
    $rejected = $false
    try { & (Join-Path $scripts 'deploy-online.ps1') -EnvFile $onlineEnv } catch { $rejected = $true }
    Assert $rejected 'Build failure was ignored'
    Assert (-not (Contains-Call ' up ')) 'Containers started after failed build'
    $global:IotTest_failBuild = $false
    Write-Host 'PASS online: exact Compose selection, readiness, repeatability, AI and failure handling'

    $global:IotTest_calls.Clear()
    $bundleParent = Join-Path $testRoot 'bundles'
    & (Join-Path $scripts 'package-offline-windows.ps1') -OutputDir $bundleParent
    $bundle = @(Get-ChildItem -LiteralPath $bundleParent -Directory)[0].FullName
    Assert-CommentedEnv (Join-Path $bundle '.env.offline')
    $manifest = Get-Content (Join-Path $bundle 'manifest.json') -Raw | ConvertFrom-Json
    Assert ($manifest.images -contains 'ollama/ollama:0.11.4') 'Default bundle omitted Ollama'
    Assert ($manifest.images -contains 'cr.weaviate.io/semitechnologies/weaviate:1.32.8') 'Default bundle omitted Weaviate'
    Assert ($manifest.images -contains 'iot-platform-backup:offline') 'Default bundle omitted backup image'
    Assert ($manifest.ollamaEmbeddingModel -eq 'nomic-embed-text') 'Default bundle omitted embedding model'
    Assert ($manifest.ollamaModel -eq 'qwen3:1.7b') 'Default bundle omitted compact Qwen model'
    Assert ($manifest.profiles -contains 'harness') 'Default bundle omitted Harness'
    Assert (Test-Path (Join-Path $bundle 'ollama-data.tgz.sha256')) 'Model checksum omitted'
    Assert ((Get-DeploymentEnvValue -Path (Join-Path $bundle '.env.offline') -Key 'IOT_AI_PROVIDER') -eq 'ollama') 'Offline default AI provider is not Ollama'
    Assert ((Get-DeploymentEnvValue -Path (Join-Path $bundle '.env.offline') -Key 'IOT_AI_MODEL') -eq 'qwen3:1.7b') 'Offline compact Qwen model is missing'
    Assert ((Get-DeploymentEnvValue -Path (Join-Path $bundle '.env.offline') -Key 'IOT_AI_HARNESS_PROVIDER') -eq 'ollama') 'Offline Harness does not use Ollama'
    $bundleHash = (Get-FileHash (Join-Path $bundle '.env.offline')).Hash
    $global:IotTest_calls.Clear()
    & (Join-Path $scripts 'deploy-offline-windows.ps1') -BundleDir $bundle
    & (Join-Path $scripts 'deploy-offline.ps1') -BundleDir $bundle
    Assert ((Get-FileHash (Join-Path $bundle '.env.offline')).Hash -eq $bundleHash) 'Offline deploy rewrote credentials'
    Assert (Contains-Call 'up -d --no-build --pull never') 'Offline up may build or pull'
    Assert (Contains-Call '^run --rm --pull never') 'Model restore may pull images'
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
    $global:IotTest_calls.Clear()
    & (Join-Path $scripts 'package-offline.ps1') -OutputDir (Join-Path $testRoot 'existing-config') -EnvFile $onlineEnv
    Assert (Contains-Call 'exec -T ollama ollama pull qwen3:4b') 'Existing Ollama configuration omitted its active chat model'
    Write-Host 'PASS offline: complete default bundle, Windows parameter forwarding, no network, repeatability, missing/corrupt archives'
    Write-Host 'Deployment smoke tests PASS (Docker operations mocked; Compose parsing real).'
} finally {
    foreach ($key in $savedEnv.Keys) { [Environment]::SetEnvironmentVariable($key, $savedEnv[$key], 'Process') }
    Remove-Item Function:\docker,Function:\Invoke-WebRequest,Function:\go,Function:\npm.cmd -ErrorAction SilentlyContinue
    # Test fixtures contain random credentials, never real environment values.
    $resolved = [IO.Path]::GetFullPath($testRoot)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
    if ($resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path $resolved -Leaf) -like 'iot-deploy-test-*') {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
