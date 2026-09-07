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
