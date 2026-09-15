$ErrorActionPreference = 'Stop'
$scriptsDir = Split-Path $PSScriptRoot -Parent
. (Join-Path $scriptsDir 'lib/docker-runtime.ps1')
$testDir = Join-Path ([IO.Path]::GetTempPath()) ('iot-policy-test-' + [guid]::NewGuid().ToString('N'))
$global:fixtureDirectory = $testDir
$global:fixtureFail = $false
$global:fixtureCalls = @()
function global:docker {
    $global:fixtureCalls += ,@($args)
    if ($global:fixtureFail) { $global:LASTEXITCODE = 5; return }
    [IO.File]::WriteAllText((Join-Path $global:fixtureDirectory 'target-os'), "openeuler`n24.03`n24.03 (LTS-SP4)`nx86_64`n")
    $marker = Join-Path $global:fixtureDirectory 'target-os'
    [IO.File]::WriteAllText("$marker.sha256", (Get-FileHash $marker).Hash.ToLowerInvariant())
    [IO.File]::WriteAllText((Join-Path $global:fixtureDirectory 'container-selinux-fixture.rpm'), 'fixture')
    New-Item -ItemType Directory -Force -Path (Join-Path $global:fixtureDirectory 'repodata') | Out-Null
    foreach ($relative in @('repodata/repomd.xml', 'RPM-GPG-KEY-openEuler')) {
        $path = Join-Path $global:fixtureDirectory $relative
        [IO.File]::WriteAllText($path, 'fixture')
        [IO.File]::WriteAllText("$path.sha256", (Get-FileHash $path).Hash.ToLowerInvariant())
    }
    $global:LASTEXITCODE = 0
}
try {
    Save-OpenEulerPackages -Directory $testDir -Architecture x86_64 -Script (Join-Path $scriptsDir 'lib/prepare-openeuler-packages.sh')
    if ($global:fixtureCalls[0] -notcontains 'linux/amd64' -or $global:fixtureCalls[0] -notcontains 'openeuler/openeuler:24.03-lts-sp4') { throw 'wrong build target' }
    $global:fixtureFail = $true
    $rejected = $false
    try { Save-OpenEulerPackages -Directory $testDir -Architecture x86_64 -Script (Join-Path $scriptsDir 'lib/prepare-openeuler-packages.sh') } catch { $rejected = $true }
    if (-not $rejected) { throw 'accepted failed preparation' }
    'PASS PowerShell OS package preparation, target selection and failure propagation (Docker mocked)'
} finally {
    # Delete only this test-created, fixed-prefix directory inside the resolved temp root.
    $resolved = [IO.Path]::GetFullPath($testDir)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    if ($resolved.StartsWith($tempRoot) -and (Split-Path $resolved -Leaf).StartsWith('iot-policy-test-')) { Remove-Item -LiteralPath $resolved -Recurse -Force }
    Remove-Item Function:/docker
}
