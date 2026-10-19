param(
    [Parameter(Mandatory=$true)][string]$OutDir,
    [Parameter(Mandatory=$true)][ValidatePattern('^[0-9a-f]{64}$')][string]$SourceSHA256,
    [string]$Go = 'C:\Users\[REDACTED_USER]\.local_workspace\binaries\go\go\bin\go.exe'
)
$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskOut = [IO.Path]::GetFullPath($OutDir)
if (Test-Path -LiteralPath $taskOut) { throw '拒绝覆盖现有分发目录，请使用一个新目录。' }
$taskGo = (Resolve-Path -LiteralPath $Go -ErrorAction Stop).Path
$taskOldToolchain=$env:GOTOOLCHAIN; $taskOldGOOS=$env:GOOS; $taskOldGOARCH=$env:GOARCH; $taskOldCGO=$env:CGO_ENABLED
try {
    $env:GOTOOLCHAIN='local'; $env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
    New-Item -ItemType Directory -Path $taskOut | Out-Null
    Push-Location -LiteralPath $taskRoot
    try {
        $taskArgs=@('build','-trimpath','-ldflags',('-X example.com/edgehybrid/internal/demo.SourceSHA256='+$SourceSHA256),'-o',(Join-Path $taskOut 'EdgeHybridDemo.exe'),'./cmd/demo')
        & $taskGo @taskArgs
        if ($LASTEXITCODE -ne 0) { throw ('Go build exit '+$LASTEXITCODE) }
        $taskVersion=(& $taskGo version | Out-String).Trim()
        $taskModuleInfo=(& $taskGo version -m (Join-Path $taskOut 'EdgeHybridDemo.exe') | Out-String)
        if ($LASTEXITCODE -ne 0) { throw '无法读取二进制构建信息' }
    } finally { Pop-Location }
    Copy-Item -LiteralPath (Join-Path $taskRoot 'docs\DEMO_QUICKSTART.md') -Destination (Join-Path $taskOut '中文使用说明.md')
    Copy-Item -LiteralPath (Join-Path $taskRoot 'LICENSE.txt') -Destination $taskOut
    Copy-Item -LiteralPath (Join-Path $taskRoot 'THIRD_PARTY_NOTICES.md') -Destination $taskOut
    Copy-Item -LiteralPath (Join-Path $taskRoot 'docs\licenses') -Destination (Join-Path $taskOut 'licenses') -Recurse
    $taskAscii=[Text.Encoding]::ASCII
    [IO.File]::WriteAllText((Join-Path $taskOut '启动1台设备.cmd'),"@echo off`r`n`"%~dp0EdgeHybridDemo.exe`" -preset 1`r`nif errorlevel 1 pause`r`n",$taskAscii)
    [IO.File]::WriteAllText((Join-Path $taskOut '启动20台设备.cmd'),"@echo off`r`n`"%~dp0EdgeHybridDemo.exe`" -preset 20`r`nif errorlevel 1 pause`r`n",$taskAscii)
    [IO.File]::WriteAllText((Join-Path $taskOut '停止演示.cmd'),"@echo off`r`n`"%~dp0EdgeHybridDemo.exe`" -stop`r`npause`r`n",$taskAscii)
    $taskHash=(Get-FileHash -LiteralPath (Join-Path $taskOut 'EdgeHybridDemo.exe') -Algorithm SHA256).Hash.ToLowerInvariant()
    $taskInfo=@{source_root=$taskRoot;source_manifest_sha256=$SourceSHA256;binary='EdgeHybridDemo.exe';binary_sha256=$taskHash;go_version=$taskVersion;goos='windows';goarch='amd64';cgo_enabled='0';goto_toolchain='local';build_command=@($taskGo)+$taskArgs;module_info=$taskModuleInfo;formal_results_eligible=$false}
    $taskInfo | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $taskOut 'build_info.json') -Encoding utf8
    Write-Output ('DEMO_DIRECTORY='+$taskOut)
    Write-Output ('BINARY_SHA256='+$taskHash)
} finally {
    $env:GOTOOLCHAIN=$taskOldToolchain; $env:GOOS=$taskOldGOOS; $env:GOARCH=$taskOldGOARCH; $env:CGO_ENABLED=$taskOldCGO
}
