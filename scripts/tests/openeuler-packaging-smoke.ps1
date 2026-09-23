$ErrorActionPreference = 'Stop'
$scriptsDir = Split-Path $PSScriptRoot -Parent
# 执行当前脚本步骤。
. (Join-Path $scriptsDir 'lib/docker-runtime.ps1')
# 执行当前脚本步骤。
$testDir = Join-Path ([IO.Path]::GetTempPath()) ('iot-policy-test-' + [guid]::NewGuid().ToString('N'))
# 执行当前脚本步骤。
$global:fixtureDirectory = $testDir
# 执行当前脚本步骤。
$global:fixtureFail = $false
# 执行当前脚本步骤。
$global:fixtureCalls = @()
# 定义可复用的脚本函数。
function global:docker {
    # 执行当前脚本步骤。
    $global:fixtureCalls += ,@($args)
    # 判断条件后执行对应操作。
    if ($global:fixtureFail) { $global:LASTEXITCODE = 5; return }
    # 执行当前脚本步骤。
    [IO.File]::WriteAllText((Join-Path $global:fixtureDirectory 'target-os'), "openeuler`n24.03`n24.03 (LTS-SP4)`nx86_64`n")
    # 执行当前脚本步骤。
    $marker = Join-Path $global:fixtureDirectory 'target-os'
    # 执行当前脚本步骤。
    [IO.File]::WriteAllText("$marker.sha256", (Get-FileHash $marker).Hash.ToLowerInvariant())
    # 执行当前脚本步骤。
    [IO.File]::WriteAllText((Join-Path $global:fixtureDirectory 'container-selinux-fixture.rpm'), 'fixture')
    # 执行当前脚本步骤。
    New-Item -ItemType Directory -Force -Path (Join-Path $global:fixtureDirectory 'repodata') | Out-Null
    # 遍历数据并执行循环体。
    foreach ($relative in @('repodata/repomd.xml', 'RPM-GPG-KEY-openEuler')) {
        # 执行当前脚本步骤。
        $path = Join-Path $global:fixtureDirectory $relative
        # 执行当前脚本步骤。
        [IO.File]::WriteAllText($path, 'fixture')
        # 执行当前脚本步骤。
        [IO.File]::WriteAllText("$path.sha256", (Get-FileHash $path).Hash.ToLowerInvariant())
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    $global:LASTEXITCODE = 0
# 结束当前控制块。
}
# 执行当前脚本步骤。
try {
    # 执行当前脚本步骤。
    Save-OpenEulerPackages -Directory $testDir -Architecture x86_64 -Script (Join-Path $scriptsDir 'lib/prepare-openeuler-packages.sh')
    # 判断条件后执行对应操作。
    if ($global:fixtureCalls[0] -notcontains 'linux/amd64' -or $global:fixtureCalls[0] -notcontains 'openeuler/openeuler:24.03-lts-sp4') { throw 'wrong build target' }
    # 执行当前脚本步骤。
    $global:fixtureFail = $true
    # 执行当前脚本步骤。
    $rejected = $false
    # 执行当前脚本步骤。
    try { Save-OpenEulerPackages -Directory $testDir -Architecture x86_64 -Script (Join-Path $scriptsDir 'lib/prepare-openeuler-packages.sh') } catch { $rejected = $true }
    # 判断条件后执行对应操作。
    if (-not $rejected) { throw 'accepted failed preparation' }
    # 执行当前脚本步骤。
    'PASS PowerShell OS package preparation, target selection and failure propagation (Docker mocked)'
# 结束当前控制块。
} finally {
    # Delete only this test-created, fixed-prefix directory inside the resolved temp root.
    # 执行当前脚本步骤。
    $resolved = [IO.Path]::GetFullPath($testDir)
    # 执行当前脚本步骤。
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    # 判断条件后执行对应操作。
    if ($resolved.StartsWith($tempRoot) -and (Split-Path $resolved -Leaf).StartsWith('iot-policy-test-')) { Remove-Item -LiteralPath $resolved -Recurse -Force }
    # 执行当前脚本步骤。
    Remove-Item Function:/docker
# 结束当前控制块。
}
