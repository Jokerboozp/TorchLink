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
    # Expected SHA256, kept identical to docker_runtime_pinned_hash in docker-bootstrap.sh.
    $pinned = @{
        'docker-24.0.9.tgz/x86_64' = '692ecfc28333485d184f628b74c25b2894cee9495a51a5418ba60ef95bf733ca'
        'docker-24.0.9.tgz/aarch64' = '7e999590330a15469de20ac37051407d222ea73c71c10c05d61666d42c5922d1'
        'docker-28.5.2.tgz/x86_64' = 'ea90cfd12e1eeb12aa1c971741adb8bd4ed88e2a574eaac13f5029a1dbc6300d'
        'docker-28.5.2.tgz/aarch64' = '9e4f82996ab790724094475ebed33a736434bfe5d45231b676fef22ffb80044d'
        'docker-compose/x86_64' = 'a0d30a63ddb6bc77ccb68bf0eb9adebba2f8b1d8615dc8c986b924e80b9a7aed'
        'docker-compose/aarch64' = '100f474cd86417310b3037847ff089da2103789a649be6564d169bc280d3a494'
        'docker-buildx/x86_64' = '68e4f8895331ade982de8085a8c137b8af65f3ef95040b6c6113552243638508'
        'docker-buildx/aarch64' = '82e776e50a84293c160e8c89c125b7a86295c7aa7f30751d6a7c051c171762c1'
    }
    foreach ($entry in $files.GetEnumerator()) {
        $path = Join-Path $Directory $entry.Key
        Invoke-WebRequest -Uri $entry.Value -OutFile $path -UseBasicParsing
        if (-not (Test-Path -LiteralPath $path) -or (Get-Item -LiteralPath $path).Length -eq 0) { throw "Docker 安装文件下载失败：$($entry.Key)" }
        $actual = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $pinned["$($entry.Key)/$arch"]) {
            Remove-Item -LiteralPath $path -Force
            throw "下载的 $($entry.Key) SHA256 与固定值不符，已删除：$($entry.Value)"
        }
        [IO.File]::WriteAllText("$path.sha256", $actual)
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
