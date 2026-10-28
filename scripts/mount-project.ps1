# F盘为可移动NTFS，WSL没有自动挂载。只挂当前项目，不遮盖旧/mnt/host/f。
$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
if(-not $root.StartsWith('F:\',[StringComparison]::OrdinalIgnoreCase)){throw '只允许当前F盘项目'}
$mount='/tmp/docker-desktop-root/run/desktop/mnt/host/campusclaw-course'
Push-Location 'C:/'
try {
    wsl.exe -d docker-desktop --exec mountpoint -q $mount
    if($LASTEXITCODE -ne 0){
        # 已存在但非空的目录不覆盖，避免隐藏未知数据。
        wsl.exe -d docker-desktop --exec sh -c 'if [ -d "$1" ]; then test -z "$(ls -A "$1")"; else mkdir "$1"; fi' sh $mount
        if($LASTEXITCODE -ne 0){throw '专用挂载位置非空或无法建立，不覆盖'}
        wsl.exe -d docker-desktop --exec mount -t drvfs $root $mount -o metadata
        if($LASTEXITCODE -ne 0){throw '当前项目无法接入Docker，不改用其他磁盘'}
    }
    $linuxHash=wsl.exe -d docker-desktop --exec sha256sum "$mount/README.md"
    if($LASTEXITCODE -ne 0){throw '无法核验专用挂载'}
    $windowsHash=(Get-FileHash -LiteralPath (Join-Path $root 'README.md') -Algorithm SHA256).Hash
    if(($linuxHash -split '\s+')[0] -ne $windowsHash){throw '挂载并非当前项目，停止操作'}
} finally { Pop-Location }
$env:CAMPUSCLAW_DOCKER_ROOT='/run/desktop/mnt/host/campusclaw-course'
Write-Output '已核对：Docker专用入口与F盘当前项目是同一份文件；未覆盖旧挂载。'
