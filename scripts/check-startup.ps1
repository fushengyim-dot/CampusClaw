# 独立子进程临时配置：检查格式及不可达数据库的安全失败；不读取.env、不保存密码。
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $projectRoot 'backend/bin/campusclaw.exe'
if (-not (Test-Path -LiteralPath $exe)) { throw '请先运行dev.ps1 -Task build' }
$keys = @('APP_ORIGIN','WEB_PORT','API_PORT','SESSION_SECRET','MYSQL_HOST','MYSQL_PORT','MYSQL_DATABASE','MYSQL_USER','MYSQL_PASSWORD')
$values = @{
    APP_ORIGIN='http://127.0.0.1:5173'; WEB_PORT='8080'
    SESSION_SECRET=[guid]::NewGuid().ToString('N')
    MYSQL_HOST='unreachable.invalid'; MYSQL_PORT='3306'
    MYSQL_DATABASE='startup_test'; MYSQL_USER='startup_test'
    MYSQL_PASSWORD=[guid]::NewGuid().ToString('N')
}
function Start-CheckedProcess([bool]$Configured, [bool]$CheckOnly) {
    $info = [System.Diagnostics.ProcessStartInfo]::new($exe)
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $info.StandardOutputEncoding = [System.Text.Encoding]::UTF8
    $info.StandardErrorEncoding = [System.Text.Encoding]::UTF8
    foreach ($key in $keys) { $info.Environment.Remove($key) | Out-Null }
    if ($Configured) { foreach ($key in $values.Keys) { $info.Environment[$key] = $values[$key] } }
    if ($CheckOnly) { $info.ArgumentList.Add('--check-config') }
    $process = [System.Diagnostics.Process]::new()
    $process.StartInfo = $info
    $process.Start() | Out-Null
    return $process
}
function Assert-NoLeak([string]$Text) {
    foreach ($key in @('SESSION_SECRET','MYSQL_PASSWORD','MYSQL_HOST')) {
        if ($Text.Contains($values[$key])) { throw '检查失败：输出包含配置值（已隐藏）' }
    }
}
foreach ($configured in @($false,$true)) {
    $p = Start-CheckedProcess $configured $configured
    try {
        if (-not $p.WaitForExit(10000)) { throw '配置检查进程超时' }
        $output = $p.StandardOutput.ReadToEnd() + $p.StandardError.ReadToEnd()
        Assert-NoLeak $output
        if (-not $configured -and ($p.ExitCode -ne 1 -or -not $output.Contains('SESSION_SECRET'))) { throw '缺配置时未正确拒绝启动' }
        if ($configured -and ($p.ExitCode -ne 0 -or -not $output.Contains('尚未连接数据库'))) { throw '有效配置检查结果不正确' }
        if ($configured) { Write-Output 'PASS：有效配置仅检查格式，退出0，无配置值泄露' }
        else { Write-Output 'PASS：缺配置拒绝启动，退出1，仅报告变量名' }
    } finally { if (-not $p.HasExited) { $p.Kill() }; $p.Dispose() }
}
$probe = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback,0)
$probe.Start(); $values.API_PORT = [string]$probe.LocalEndpoint.Port; $probe.Stop()
$server = Start-CheckedProcess $true $false
try {
    if(-not $server.WaitForExit(10000)){throw '不可达数据库未在规定时间内失败'}
    if($server.ExitCode -ne 1){throw '数据库不可达时不应启动成功'}
    Write-Output 'PASS：不可达数据库使启动退出1，没有将格式检查当作真实连接成功。'
} finally {
    if (-not $server.HasExited) { $server.Kill(); $server.WaitForExit() }
    Assert-NoLeak ($server.StandardOutput.ReadToEnd()+$server.StandardError.ReadToEnd())
    $server.Dispose()
}
Write-Output '检查结束：临时进程已停止，没有保存凭据；真实MySQL及HTTP503检查使用verify-database.ps1。'
