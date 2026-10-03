# Creates a self-signed certificate for the Web (HTTPS) and MQTT (MQTTS/WSS)
# listeners with openssl (bundled with Git for Windows). Production should use
# a CA-issued certificate saved as tls.crt (full chain) and tls.key.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string[]]$HostName,
    [string]$Dir = 'tls',
    [int]$Days = 825
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$openssl = Get-Command openssl -ErrorAction SilentlyContinue
if (-not $openssl) { throw '需要 openssl（可安装 Git for Windows 并使用其 usr\bin\openssl.exe）。' }
$crt = Join-Path $Dir 'tls.crt'
$key = Join-Path $Dir 'tls.key'
if ((Test-Path $crt) -or (Test-Path $key)) { throw "$Dir 中已有证书，未覆盖；如需重新生成请先移走旧文件。" }
New-Item -ItemType Directory -Force -Path $Dir | Out-Null
$san = ($HostName | ForEach-Object { if ($_ -match '^[0-9.]+$' -or $_.Contains(':')) { "IP:$_" } else { "DNS:$_" } }) -join ','
& $openssl.Source req -x509 -newkey rsa:2048 -sha256 -nodes -days $Days -keyout $key -out $crt -subj "/CN=$($HostName[0])" -addext "subjectAltName=$san" -addext "extendedKeyUsage=serverAuth" 2>$null
if ($LASTEXITCODE -ne 0) { throw 'openssl 生成证书失败。' }
Write-Host "已生成自签名证书：$crt（$san），有效期 $Days 天。设备和浏览器需信任该证书。"
