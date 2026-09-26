$ErrorActionPreference = 'Stop'
$scripts = Split-Path $PSScriptRoot -Parent
$root = Join-Path ([IO.Path]::GetTempPath()) ('public-bundle-' + [guid]::NewGuid().ToString('N'))
try {
    $configurations = @()
    foreach ($name in @('first', 'second')) {
        $bundle = Join-Path $root $name
        [IO.Directory]::CreateDirectory($bundle) | Out-Null
        $template = "IOT_ADMIN_PASSWORD=__TORCHLINK_RANDOM_HEX__`nPOSTGRES_PASSWORD=__TORCHLINK_RANDOM_HEX__`nIOT_VIDEO_PLATFORM_SECRETS=video-platform-1:__TORCHLINK_RANDOM_HEX__`n"
        [IO.File]::WriteAllText((Join-Path $bundle '.env.offline.template'), $template)
        & (Join-Path $scripts 'init-offline-env.ps1') -BundleDir $bundle
        $path = Join-Path $bundle '.env.offline'
        $original = [IO.File]::ReadAllText($path)
        if ($original -notmatch 'IOT_ADMIN_PASSWORD=[a-f0-9]{64}' -or $original.Contains('__TORCHLINK_RANDOM_HEX__')) {
            throw '首次初始化未生成随机凭据'
        }
        & (Join-Path $scripts 'init-offline-env.ps1') -BundleDir $bundle
        if ([IO.File]::ReadAllText($path) -ne $original) { throw '重复部署更改了凭据' }
        $configurations += $original
    }
    if ($configurations[0] -eq $configurations[1]) { throw '两台目标机器共享了凭据' }
    Write-Host 'PASS PowerShell: independent credentials and unchanged configuration on rerun'
} finally {
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
}
