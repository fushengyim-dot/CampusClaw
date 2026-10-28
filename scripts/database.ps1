param([ValidateSet('prepare','up','check','test','init-schema','test-schema','test-ngram','prepare-accounts','init-accounts','test-accounts','test-login','test-search','test-materials','test-qdrant','test-embedding','test-chat','stop','status')][string]$Task='status')
$ErrorActionPreference='Stop'
$projectRoot=[System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
if (-not $projectRoot.StartsWith('F:\',[StringComparison]::OrdinalIgnoreCase)) { throw '本项目数据库必须位于F盘' }
$dataPath=Join-Path $projectRoot 'runtime/mysql-data'
$uploadsPath=Join-Path $projectRoot 'runtime/uploads'
$qdrantPath=Join-Path $projectRoot 'runtime/qdrant-data'
$privatePath=Join-Path $projectRoot 'runtime/private'
$compose=@('--project-name','campusclaw-course','--project-directory',$projectRoot,'-f',(Join-Path $projectRoot 'compose.yaml'))

function Assert-LocalPaths {
    foreach($p in @($projectRoot,(Join-Path $projectRoot 'runtime'),$dataPath,$uploadsPath,$qdrantPath,$privatePath)) {
        if(Test-Path -LiteralPath $p) {
            if((Get-Item -LiteralPath $p).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw '数据路径不能是指向其他位置的链接' }
        }
    }
}
Assert-LocalPaths
if($Task -ne 'prepare'){ & (Join-Path $PSScriptRoot 'mount-project.ps1') }
switch($Task) {
    'prepare' {
        $coreSecretNames=@('mysql_password','mysql_root_password','session_secret')
        $secretNames=@('mysql_password','mysql_root_password','session_secret','mcp_tools_token')
        $dataExists=(Test-Path -LiteralPath $dataPath) -and (@(Get-ChildItem -LiteralPath $dataPath -Force).Count -gt 0)
        if($dataExists) {
            foreach($name in $coreSecretNames) { if(-not(Test-Path -LiteralPath (Join-Path $privatePath $name))) {throw '已有数据库但核心秘密文件缺失；停止，不能重置密码或数据'} }
        }
        New-Item -ItemType Directory -Path $dataPath,$uploadsPath,$qdrantPath,$privatePath -Force | Out-Null
        $sid=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value
        & icacls.exe $privatePath /inheritance:r /grant:r "*${sid}:(OI)(CI)F" '*S-1-5-18:(OI)(CI)F' | Out-Null
        if($LASTEXITCODE -ne 0){throw '私密目录访问权限设置失败'}
        foreach($name in $secretNames) {
            $path=Join-Path $privatePath $name
            if(-not(Test-Path -LiteralPath $path)) {
                $bytes=[Security.Cryptography.RandomNumberGenerator]::GetBytes(32)
                [IO.File]::WriteAllText($path,[Convert]::ToHexString($bytes),[Text.UTF8Encoding]::new($false))
            } elseif((Get-Item -LiteralPath $path).Length -eq 0) { throw '秘密文件为空；停止，不覆盖已有文件' }
        }
        Write-Output 'F盘独立数据目录与随机秘密文件已准备；已有文件不会覆盖，密码不显示。'
    }
    'up' {
        New-Item -ItemType Directory -Path $uploadsPath -Force | Out-Null
        $seedPath=Join-Path $privatePath 'seed'
        foreach($name in @('SEED_TEACHER_A_PASSWORD','SEED_STUDENT_A_PASSWORD','SEED_TEACHER_B_PASSWORD','SEED_STUDENT_B_PASSWORD')) {
            if(-not(Test-Path -LiteralPath (Join-Path $seedPath $name) -PathType Leaf)){throw '四份账号种子凭据未准备齐全；先运行prepare-accounts和init-accounts'}
        }
        if(Get-NetTCPConnection -State Listen -LocalPort 8080 -ErrorAction SilentlyContinue){throw '8080端口已被占用；停止，避免影响其他服务'}
        & (Join-Path $PSScriptRoot 'dev.ps1') -Task build
        if($LASTEXITCODE -ne 0){throw '前端或后端构建失败；尚未启动网站'}
        & (Join-Path $PSScriptRoot 'dev.ps1') -Task build-linux
        if($LASTEXITCODE -ne 0){throw 'Linux后端构建失败；尚未启动网站'}
        docker compose @compose config --quiet
        if($LASTEXITCODE -ne 0){throw 'Compose配置检查失败'}
        docker compose @compose up -d --build --wait --wait-timeout 300 web
        if($LASTEXITCODE -ne 0){throw '网站容器尚未通过健康检查；请查看本项目容器日志'}
    }
    'check' {
        docker compose @compose run --rm --no-deps db-check
        if($LASTEXITCODE -ne 0){throw '真实数据库连接检查未通过'}
    }
    'test' {
        docker compose @compose run --rm --no-deps db-check test
        if($LASTEXITCODE -ne 0){throw '真实数据库集成检查未通过'}
    }
    { $_ -in @('init-schema','test-schema') } {
        docker compose @compose run --rm --no-deps db-check $Task
        if($LASTEXITCODE -ne 0){throw '建表或结构检查未通过；保留已有数据，停止后续操作'}
    }
    'stop' {
        docker compose @compose stop web api db
        if($LASTEXITCODE -ne 0){throw '停止本项目服务失败'}
    }
    'prepare-accounts' {
        $seedPath=Join-Path $privatePath 'seed'
        if((Test-Path -LiteralPath $seedPath) -and ((Get-Item -LiteralPath $seedPath).Attributes -band [IO.FileAttributes]::ReparsePoint)){throw '种子凭据目录不能为链接'}
        $names=@('SEED_TEACHER_A_PASSWORD','SEED_STUDENT_A_PASSWORD','SEED_TEACHER_B_PASSWORD','SEED_STUDENT_B_PASSWORD')
        if(Test-Path -LiteralPath $seedPath){
            foreach($name in $names){
                $file=Join-Path $seedPath $name
                if(-not(Test-Path -LiteralPath $file -PathType Leaf) -or (Get-Item -LiteralPath $file).Length -eq 0){throw '已有种子凭据不完整，停止；不自动重置或补造密码'}
                if((Get-Item -LiteralPath $file).Attributes -band [IO.FileAttributes]::ReparsePoint){throw '种子凭据不能为链接'}
            }
            Write-Output '四份种子凭据已存在，保持原值。'
        } else {
            New-Item -ItemType Directory -Path $seedPath | Out-Null
            $sid=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value
            & icacls.exe $seedPath /inheritance:r /grant:r "*${sid}:(OI)(CI)F" '*S-1-5-18:(OI)(CI)F' | Out-Null
            if($LASTEXITCODE -ne 0){throw '种子凭据权限设置失败'}
            foreach($name in $names){
                [IO.File]::WriteAllText((Join-Path $seedPath $name),[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)),[Text.UTF8Encoding]::new($false))
            }
            Write-Output '四份独立随机密码已保存在F盘私密目录；不显示、不覆盖。'
        }
    }
    { $_ -in @('init-accounts','test-accounts','test-login','test-materials') } {
        $seedPath=Join-Path $privatePath 'seed'
        if(-not(Test-Path -LiteralPath $seedPath -PathType Container)){throw '请先运行prepare-accounts'}
        if((Get-Item -LiteralPath $seedPath).Attributes -band [IO.FileAttributes]::ReparsePoint){throw '种子凭据目录不能为链接'}
        docker compose @compose run --rm --no-deps --volume "${env:CAMPUSCLAW_DOCKER_ROOT}/runtime/private/seed:/run/seed:ro" db-check $Task
        if($LASTEXITCODE -ne 0){throw '本项目初始化或集成验收未通过；保留现有数据并停止'}
    }
    'test-search' {
        docker compose @compose up -d --wait qdrant
        if($LASTEXITCODE -ne 0){throw '本地向量库未能启动'}
        & (Join-Path $PSScriptRoot 'dev.ps1') -Task build-linux
        if($LASTEXITCODE -ne 0){throw '检索集成检查程序编译失败'}
        docker compose @compose run --rm --no-deps db-check test-search
        if($LASTEXITCODE -ne 0){throw '本班全文检索集成检查未通过；保留已有数据并停止'}
    }
    'test-qdrant' {
        if(-not(Test-Path -LiteralPath $qdrantPath)){New-Item -ItemType Directory -Path $qdrantPath | Out-Null}
        if((Get-Item -LiteralPath $qdrantPath).Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Qdrant持久化目录不能为链接'}
        docker compose @compose config --quiet
        if($LASTEXITCODE -ne 0){throw 'Qdrant Compose配置检查失败'}
        docker compose @compose up -d --wait qdrant
        if($LASTEXITCODE -ne 0){throw '本地向量库未能启动'}
        & (Join-Path $PSScriptRoot 'dev.ps1') -Task build-linux
        if($LASTEXITCODE -ne 0){throw '向量验收程序编译失败'}
        docker compose @compose build api
        if($LASTEXITCODE -ne 0){throw '后端镜像构建失败'}
        docker compose @compose run --rm --no-deps --entrypoint /usr/local/bin/campusclaw api --check-qdrant-test
        if($LASTEXITCODE -ne 0){throw '合成向量写入、班级过滤或查询验收失败'}
        docker compose @compose restart qdrant
        if($LASTEXITCODE -ne 0){throw '本地向量库重启验收失败'}
        docker compose @compose run --rm --no-deps --entrypoint /usr/local/bin/campusclaw api --check-qdrant-persistence
        if($LASTEXITCODE -ne 0){throw '向量库重启后持久性验收失败'}
    }
    'test-embedding' {
        docker compose @compose config --quiet
        if($LASTEXITCODE -ne 0){throw 'Compose配置检查失败；没有发送模型请求'}
        docker compose @compose build api
        if($LASTEXITCODE -ne 0){throw '后端构建失败；没有发送模型请求'}
        docker compose @compose run --rm --no-deps api --test-embedding
        if($LASTEXITCODE -ne 0){throw '单次课程嵌入连通检查未通过；没有自动重试'}
    }
    'test-chat' {
        docker compose @compose config --quiet
        if($LASTEXITCODE -ne 0){throw 'Compose配置检查失败；没有发送模型请求'}
        & (Join-Path $PSScriptRoot 'dev.ps1') -Task build-linux
        if($LASTEXITCODE -ne 0){throw 'Linux后端构建失败；没有发送模型请求'}
        docker compose @compose build api
        if($LASTEXITCODE -ne 0){throw '后端镜像构建失败；没有发送模型请求'}
        docker compose @compose run --rm --no-deps api --test-chat
        if($LASTEXITCODE -ne 0){throw '单次课程对话连通检查未通过；没有自动重试'}
    }
    'status' { docker compose @compose ps -a }
}
