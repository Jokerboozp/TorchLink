# Packaging only: the Windows build host can prepare Linux Docker installers.
function Save-LinuxDockerRuntime {
    # 执行当前脚本步骤。
    param([string]$Directory, [string]$Architecture)
    # 执行当前脚本步骤。
    $arch = switch ($Architecture) {
        # 执行当前脚本步骤。
        { $_ -in @('amd64', 'x86_64') } { 'x86_64'; break }
        # 执行当前脚本步骤。
        { $_ -in @('arm64', 'aarch64') } { 'aarch64'; break }
        # 执行当前脚本步骤。
        default { throw "Docker 自动安装包不支持架构：$Architecture" }
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    New-Item -ItemType Directory -Force -Path $Directory | Out-Null
    # 执行当前脚本步骤。
    [IO.File]::WriteAllText((Join-Path $Directory 'architecture'), $arch)
    # 执行当前脚本步骤。
    $files = [ordered]@{
        # 执行当前脚本步骤。
        'docker-24.0.9.tgz' = "https://download.docker.com/linux/static/stable/$arch/docker-24.0.9.tgz"
        # 执行当前脚本步骤。
        'docker-28.5.2.tgz' = "https://download.docker.com/linux/static/stable/$arch/docker-28.5.2.tgz"
        # 执行当前脚本步骤。
        'docker-compose' = "https://github.com/docker/compose/releases/download/v2.27.3/docker-compose-linux-$arch"
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    $buildArch = if ($arch -eq 'x86_64') { 'amd64' } else { 'arm64' }
    # 执行当前脚本步骤。
    $files['docker-buildx'] = "https://github.com/docker/buildx/releases/download/v0.14.1/buildx-v0.14.1.linux-$buildArch"
    # 遍历数据并执行循环体。
    foreach ($entry in $files.GetEnumerator()) {
        # 执行当前脚本步骤。
        $path = Join-Path $Directory $entry.Key
        # 执行当前脚本步骤。
        Invoke-WebRequest -Uri $entry.Value -OutFile $path -UseBasicParsing
        # 判断条件后执行对应操作。
        if (-not (Test-Path -LiteralPath $path) -or (Get-Item -LiteralPath $path).Length -eq 0) { throw "Docker 安装文件下载失败：$($entry.Key)" }
        # 执行当前脚本步骤。
        [IO.File]::WriteAllText("$path.sha256", (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant())
    # 结束当前控制块。
    }
# 结束当前控制块。
}

# 定义可复用的脚本函数。
function Save-OpenEulerPackages {
    # 执行当前脚本步骤。
    param([string]$Directory, [string]$Architecture, [string]$Script)
    # 执行当前脚本步骤。
    $platform = switch ($Architecture) {
        # 执行当前脚本步骤。
        { $_ -in @('amd64', 'x86_64') } { 'amd64'; break }
        # 执行当前脚本步骤。
        { $_ -in @('arm64', 'aarch64') } { 'arm64'; break }
        # 执行当前脚本步骤。
        default { throw "不支持的架构：$Architecture" }
    # 结束当前控制块。
    }
    # 执行当前脚本步骤。
    New-Item -ItemType Directory -Force -Path $Directory | Out-Null
    # 执行当前脚本步骤。
    $packagePath = (Resolve-Path -LiteralPath $Directory).Path
    # 执行当前脚本步骤。
    $preparePath = (Resolve-Path -LiteralPath $Script).Path
    # 执行当前脚本步骤。
    & docker run --rm --platform "linux/$platform" `
        --mount "type=bind,source=$packagePath,target=/packages" `
        --mount "type=bind,source=$preparePath,target=/prepare.sh,readonly" `
        'openeuler/openeuler:24.03-lts-sp4' bash /prepare.sh
    # 判断条件后执行对应操作。
    if ($LASTEXITCODE -ne 0) { throw 'openEuler 系统依赖准备失败，不能交付此离线包。' }
    # 执行当前脚本步骤。
    $marker = Join-Path $Directory 'target-os'
    # 判断条件后执行对应操作。
    if (-not (Test-Path -LiteralPath "$marker.sha256") -or -not (Test-Path -LiteralPath $marker)) { throw '系统依赖缺少目标系统信息或校验值。' }
    # 判断条件后执行对应操作。
    if ((Get-FileHash -LiteralPath $marker -Algorithm SHA256).Hash.ToLowerInvariant() -ne (Get-Content -LiteralPath "$marker.sha256" -Raw).Trim()) { throw '系统依赖元数据校验失败。' }
    # 遍历数据并执行循环体。
    foreach ($relative in @('repodata/repomd.xml', 'RPM-GPG-KEY-openEuler')) {
        # 执行当前脚本步骤。
        $path = Join-Path $Directory $relative
        # 判断条件后执行对应操作。
        if (-not (Test-Path -LiteralPath $path) -or -not (Test-Path -LiteralPath "$path.sha256")) { throw "系统依赖缺少软件源索引或公钥：$relative" }
        # 判断条件后执行对应操作。
        if ((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne (Get-Content -LiteralPath "$path.sha256" -Raw).Trim()) { throw "软件源文件校验失败：$relative" }
    # 结束当前控制块。
    }
    # 判断条件后执行对应操作。
    if (-not (Get-ChildItem -LiteralPath $Directory -Filter 'container-selinux-*.rpm')) { throw '系统依赖缺少 container-selinux。' }
# 结束当前控制块。
}
