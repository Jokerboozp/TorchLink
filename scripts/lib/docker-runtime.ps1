# Packaging only: the Windows build host can prepare Linux Docker installers.
function Save-LinuxDockerRuntime {
    param([string]$Directory, [string]$Architecture)
    $arch = switch ($Architecture) {
        { $_ -in @('amd64', 'x86_64') } { 'x86_64'; break }
        { $_ -in @('arm64', 'aarch64') } { 'aarch64'; break }
        default { throw "Docker 自动安装包不支持架构：$Architecture" }
    }
    New-Item -ItemType Directory -Force -Path $Directory | Out-Null
    [IO.File]::WriteAllText((Join-Path $Directory 'architecture'), $arch)
    $files = [ordered]@{
        'docker-24.0.9.tgz' = "https://download.docker.com/linux/static/stable/$arch/docker-24.0.9.tgz"
        'docker-28.5.2.tgz' = "https://download.docker.com/linux/static/stable/$arch/docker-28.5.2.tgz"
        'docker-compose' = "https://github.com/docker/compose/releases/download/v2.27.3/docker-compose-linux-$arch"
    }
    $buildArch = if ($arch -eq 'x86_64') { 'amd64' } else { 'arm64' }
    $files['docker-buildx'] = "https://github.com/docker/buildx/releases/download/v0.14.1/buildx-v0.14.1.linux-$buildArch"
    foreach ($entry in $files.GetEnumerator()) {
        $path = Join-Path $Directory $entry.Key
        Invoke-WebRequest -Uri $entry.Value -OutFile $path -UseBasicParsing
        if (-not (Test-Path -LiteralPath $path) -or (Get-Item -LiteralPath $path).Length -eq 0) { throw "Docker 安装文件下载失败：$($entry.Key)" }
        [IO.File]::WriteAllText("$path.sha256", (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant())
    }
}

function Save-OpenEulerPackages {
    param([string]$Directory, [string]$Architecture, [string]$Script)
    $platform = switch ($Architecture) {
        { $_ -in @('amd64', 'x86_64') } { 'amd64'; break }
        { $_ -in @('arm64', 'aarch64') } { 'arm64'; break }
        default { throw "不支持的架构：$Architecture" }
    }
    New-Item -ItemType Directory -Force -Path $Directory | Out-Null
    $packagePath = (Resolve-Path -LiteralPath $Directory).Path
    $preparePath = (Resolve-Path -LiteralPath $Script).Path
    & docker run --rm --platform "linux/$platform" `
        --mount "type=bind,source=$packagePath,target=/packages" `
        --mount "type=bind,source=$preparePath,target=/prepare.sh,readonly" `
        'openeuler/openeuler:24.03-lts-sp4' bash /prepare.sh
    if ($LASTEXITCODE -ne 0) { throw 'openEuler 系统依赖准备失败，不能交付此离线包。' }
    $marker = Join-Path $Directory 'target-os'
    if (-not (Test-Path -LiteralPath "$marker.sha256") -or -not (Test-Path -LiteralPath $marker)) { throw '系统依赖缺少目标系统信息或校验值。' }
    if ((Get-FileHash -LiteralPath $marker -Algorithm SHA256).Hash.ToLowerInvariant() -ne (Get-Content -LiteralPath "$marker.sha256" -Raw).Trim()) { throw '系统依赖元数据校验失败。' }
    foreach ($relative in @('repodata/repomd.xml', 'RPM-GPG-KEY-openEuler')) {
        $path = Join-Path $Directory $relative
        if (-not (Test-Path -LiteralPath $path) -or -not (Test-Path -LiteralPath "$path.sha256")) { throw "系统依赖缺少软件源索引或公钥：$relative" }
        if ((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne (Get-Content -LiteralPath "$path.sha256" -Raw).Trim()) { throw "软件源文件校验失败：$relative" }
    }
    if (-not (Get-ChildItem -LiteralPath $Directory -Filter 'container-selinux-*.rpm')) { throw '系统依赖缺少 container-selinux。' }
}
