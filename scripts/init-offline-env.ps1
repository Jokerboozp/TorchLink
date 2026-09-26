[CmdletBinding()]
param([Parameter(Mandatory)][string]$BundleDir)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$envPath = Join-Path $BundleDir '.env.offline'
if (Test-Path -LiteralPath $envPath -PathType Leaf) {
    Write-Host '保留已有 .env.offline 配置。'
    return
}
if (Test-Path -LiteralPath $envPath) { throw '配置路径已存在且不是普通文件。' }
$template = Join-Path $BundleDir '.env.offline.template'
$lines = foreach ($line in [IO.File]::ReadAllLines($template)) {
    if ($line.Contains('__TORCHLINK_RANDOM_HEX__')) {
        $bytes = New-Object byte[] 32
        $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
        try { $generator.GetBytes($bytes) } finally { $generator.Dispose() }
        $secret = [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
        $line.Replace('__TORCHLINK_RANDOM_HEX__', $secret)
    } else { $line }
}
$temporary = Join-Path $BundleDir ('.env.offline.tmp.' + [guid]::NewGuid().ToString('N'))
try {
    [IO.File]::WriteAllText($temporary, ($lines -join "`n") + "`n", (New-Object Text.UTF8Encoding($false)))
    # File.Move refuses to replace an existing configuration.
    [IO.File]::Move($temporary, $envPath)
} finally {
    if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary }
}
Write-Host '已在本机生成独立凭据；管理员用户名和密码见 .env.offline。'
