$ErrorActionPreference = 'Stop'
$scripts = Split-Path $PSScriptRoot -Parent
$root = Join-Path ([IO.Path]::GetTempPath()) ('public-bundle-' + [guid]::NewGuid().ToString('N'))
$passwordKeys = @(
    'IOT_ADMIN_PASSWORD', 'SERVICE_ADMIN_PASSWORD', 'POSTGRES_PASSWORD',
    'REDIS_PASSWORD', 'CLICKHOUSE_PASSWORD', 'MINIO_ROOT_PASSWORD',
    'MINIO_DR_ROOT_PASSWORD', 'EMQX_DASHBOARD_PASSWORD', 'GRAFANA_ADMIN_PASSWORD',
    'IOT_MQTT_TOOL_PASSWORD', 'IOT_KAFKA_SASL_PASSWORD', 'IOT_KAFKA_ADMIN_PASSWORD'
)
$usernameKeys = @(
    'SERVICE_ADMIN_USER', 'MINIO_ROOT_USER', 'MINIO_DR_ROOT_USER',
    'EMQX_DASHBOARD_USER', 'GRAFANA_ADMIN_USER', 'IOT_ADMIN_USER',
    'IOT_MQTT_TOOL_USERNAME', 'IOT_KAFKA_SASL_USERNAME', 'IOT_KAFKA_ADMIN_USERNAME'
)
$internalKeys = @('IOT_JWT_SECRET', 'IOT_AI_HARNESS_TOKEN', 'IOT_BACKUP_ADMIN_TOKEN')
try {
    $configurations = @()
    foreach ($name in @('first', 'second')) {
        $bundle = Join-Path $root $name
        [IO.Directory]::CreateDirectory($bundle) | Out-Null
        $lines = @($passwordKeys | ForEach-Object { "$_=admin123" })
        $lines += @($usernameKeys | ForEach-Object { "$_=admin" })
        $lines += @($internalKeys | ForEach-Object { "$_=__TORCHLINK_RANDOM_HEX__" })
        $lines += 'IOT_VIDEO_PLATFORM_SECRETS=video-platform-1:__TORCHLINK_RANDOM_HEX__'
        $lines += 'IOT_VIDEO_CREDENTIAL_KEY=__TORCHLINK_RANDOM_BASE64_32__'
        $lines += 'LITERAL=$(should-never-run)'
        $template = ($lines -join "`n") + "`n"
        [IO.File]::WriteAllText((Join-Path $bundle '.env.offline.template'), $template)
        & (Join-Path $scripts 'init-offline-env.ps1') -BundleDir $bundle
        $path = Join-Path $bundle '.env.offline'
        $original = [IO.File]::ReadAllText($path)
        foreach ($key in $passwordKeys) {
            if ($original -notmatch ("(?m)^" + $key + '=admin123\r?$')) { throw "默认密码不正确：$key" }
        }
        foreach ($key in $usernameKeys) {
            if ($original -notmatch ("(?m)^" + $key + '=admin\r?$')) { throw "默认用户名不正确：$key" }
        }
        foreach ($key in $internalKeys) {
            if ($original -notmatch ("(?m)^" + $key + '=[a-f0-9]{64}\r?$')) { throw "内部令牌未生成：$key" }
        }
        if ($original.Contains('__TORCHLINK_RANDOM_') -or -not $original.Contains('LITERAL=$(should-never-run)')) {
            throw '初始化没有完整替换内部密钥占位符或改变了字面文本'
        }
        & (Join-Path $scripts 'init-offline-env.ps1') -BundleDir $bundle
        if ([IO.File]::ReadAllText($path) -ne $original) { throw '重复部署更改了凭据' }
        $custom = $original.Replace('admin123', 'existing-custom-password')
        [IO.File]::WriteAllText($path, $custom)
        & (Join-Path $scripts 'init-offline-env.ps1') -BundleDir $bundle
        if ([IO.File]::ReadAllText($path) -ne $custom) { throw '升级覆盖了已有自定义密码' }
        $configurations += $original
    }
    foreach ($key in $internalKeys) {
        $pattern = "(?m)^" + $key + '=([a-f0-9]{64})\r?$'
        if ([regex]::Match($configurations[0], $pattern).Groups[1].Value -eq [regex]::Match($configurations[1], $pattern).Groups[1].Value) {
            throw "两台目标机器共享了内部令牌：$key"
        }
    }
    Write-Host 'PASS PowerShell: shared account defaults, independent internal secrets, existing configuration preserved'
} finally {
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
}
