# 仅重建本项目db容器；不删除目录、卷、网络或其他项目资源。
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
& (Join-Path $PSScriptRoot 'mount-project.ps1')
$compose=@('--project-name','campusclaw-course','--project-directory',$projectRoot,'-f',(Join-Path $projectRoot 'compose.yaml'))
$beforeID=docker compose @compose ps -q db
if(-not $beforeID){throw '请先启动本项目数据库'}
$before=docker compose @compose run --rm --no-deps db-check test
if($LASTEXITCODE -ne 0){throw '重建前检查未通过'}
$beforeHash=[regex]::Match(($before -join "`n"),'PROBE_SHA256=([0-9a-f]{64})').Groups[1].Value
if(-not $beforeHash){throw '未取得测试记录指纹'}
docker compose @compose up -d --force-recreate --wait --wait-timeout 240 db
if($LASTEXITCODE -ne 0){throw '本项目数据库重建后未就绪；保留数据，不删除'}
$afterID=docker compose @compose ps -q db
$after=docker compose @compose run --rm --no-deps -e CAMPUSCLAW_PERSISTENCE_MODE=read db-check test
if($LASTEXITCODE -ne 0){throw '重建后只读记录检查未通过'}
$afterHash=[regex]::Match(($after -join "`n"),'PROBE_SHA256=([0-9a-f]{64})').Groups[1].Value
if($beforeID -eq $afterID -or $beforeHash -ne $afterHash){throw '容器更换或数据保留未得到证实'}
$inspect=docker inspect $afterID | ConvertFrom-Json
$mount=$inspect[0].Mounts | Where-Object Destination -eq '/var/lib/mysql'
if($mount.Source -ne '/run/desktop/mnt/host/campusclaw-course/runtime/mysql-data' -or $mount.Type -ne 'bind'){throw '数据挂载落点错误'}
if($inspect[0].HostConfig.PortBindings.PSObject.Properties.Count -gt 0){throw '数据库不应开放宿主端口'}
$result=[ordered]@{
    checkedAt=(Get-Date).ToUniversalTime().ToString('o')
    project='campusclaw-course'
    beforeContainer=$beforeID;afterContainer=$afterID
    beforeProbeSHA256=$beforeHash;afterProbeSHA256=$afterHash
    mountSource=$mount.Source
    windowsDataPath=(Join-Path $projectRoot 'runtime/mysql-data')
    databaseHostPorts='none'
    beforeTests=$before;afterTests=$after
}
$report=Join-Path $projectRoot 'docs/lesson-03/internal/database-verification.json'
$result | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $report -Encoding UTF8
Write-Output 'PASS：真实数据库检查通过；容器ID已变化，F盘测试记录指纹保持一致，无宿主端口。'
Write-Output 'PASS：重建后仅读取已有测试记录，不创建或补写记录来冒充数据保留。'
