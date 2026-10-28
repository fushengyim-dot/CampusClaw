param(
    [ValidateSet('check', 'install', 'build', 'build-linux', 'preview', 'test', 'check-config', 'check-db', 'serve')]
    [string]$Task = 'check'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$cacheRoot = 'F:/CodexCache/CampusClaw'
$goCommand = 'F:/CodexTools/CampusClaw/go1.27.3/go/bin/go.exe'

# 只影响本次PowerShell进程；不写系统环境变量，不改全局PATH。
foreach ($name in @('npm', 'go-build', 'go-mod', 'go-work', 'tmp')) {
    New-Item -ItemType Directory -Path (Join-Path $cacheRoot $name) -Force | Out-Null
}
$env:npm_config_cache = Join-Path $cacheRoot 'npm'
$env:GOCACHE = Join-Path $cacheRoot 'go-build'
$env:GOMODCACHE = Join-Path $cacheRoot 'go-mod'
$env:GOPATH = Join-Path $cacheRoot 'go-work'
$env:GOTOOLCHAIN = 'local'
$env:CGO_ENABLED = '0'
$env:TEMP = Join-Path $cacheRoot 'tmp'
$env:TMP = $env:TEMP

if (-not (Test-Path -LiteralPath $goCommand)) { throw "缺少项目Go工具：$goCommand" }

switch ($Task) {
    'check' {
        & $goCommand version
        if ($LASTEXITCODE -ne 0) { throw 'Go版本检查失败' }
        node --version
        if ($LASTEXITCODE -ne 0) { throw 'Node版本检查失败' }
        npm.cmd --version
        if ($LASTEXITCODE -ne 0) { throw 'npm版本检查失败' }
    }
    'install' {
        Push-Location (Join-Path $projectRoot 'frontend')
        try {
            if (Test-Path -LiteralPath 'package-lock.json') {
                npm.cmd ci --ignore-scripts --no-audit --no-fund
            } else {
                npm.cmd install --ignore-scripts --no-audit --no-fund
            }
            if ($LASTEXITCODE -ne 0) { throw '前端依赖安装失败' }
        } finally { Pop-Location }
    }
    'build' {
        Push-Location (Join-Path $projectRoot 'frontend')
        try {
            npm.cmd run build
            if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }
        } finally { Pop-Location }
        New-Item -ItemType Directory -Path (Join-Path $projectRoot 'backend/bin') -Force | Out-Null
        Push-Location (Join-Path $projectRoot 'backend')
        try {
            & $goCommand build -o bin/campusclaw.exe .
            if ($LASTEXITCODE -ne 0) { throw '后端编译失败' }
        } finally { Pop-Location }
    }
    'build-linux' {
        $previousGoOS=$env:GOOS; $previousGoArch=$env:GOARCH
        Push-Location (Join-Path $projectRoot 'backend')
        try {
            $env:GOOS='linux'; $env:GOARCH='amd64'
            New-Item -ItemType Directory -Path bin -Force | Out-Null
            & $goCommand build -o bin/campusclaw-linux .
            if ($LASTEXITCODE -ne 0) { throw 'Linux检查工具编译失败' }
            & $goCommand test -c -o bin/database-tests ./database
            if ($LASTEXITCODE -ne 0) { throw 'Linux数据库检查程序编译失败' }
            & $goCommand test -c -o bin/services-tests ./services
            if ($LASTEXITCODE -ne 0) { throw 'Linux材料恢复检查程序编译失败' }
        } finally { $env:GOOS=$previousGoOS; $env:GOARCH=$previousGoArch; Pop-Location }
    }
    { $_ -in @('test', 'check-config', 'check-db', 'serve') } {
        if ($Task -eq 'test') {
            Push-Location (Join-Path $projectRoot 'frontend')
            try {
                npm.cmd run test:sse
                if ($LASTEXITCODE -ne 0) { throw '前端SSE分段测试失败' }
            } finally { Pop-Location }
        }
        Push-Location (Join-Path $projectRoot 'backend')
        try {
            if ($Task -eq 'test') { & $goCommand test ./... -count=1 }
            elseif ($Task -eq 'check-config') { & $goCommand run . --check-config }
            elseif ($Task -eq 'check-db') { & $goCommand run . --check-db }
            else { & $goCommand run . }
            if ($LASTEXITCODE -ne 0) { throw "后端任务未通过：$Task" }
        } finally { Pop-Location }
    }
    'preview' {
        Push-Location (Join-Path $projectRoot 'frontend')
        try {
            npm.cmd run preview
            if ($LASTEXITCODE -ne 0) { throw '预览启动失败，请检查5173端口及构建产物' }
        } finally { Pop-Location }
    }
}
