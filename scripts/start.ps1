param([string]$DataDir = 'data')
$ErrorActionPreference = 'Stop'
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    if (-not (Test-Path frontend/dist/index.html)) { throw '请先执行 ./scripts/build.ps1' }
    if (Test-Path 港賬通.exe) {
        & ./港賬通.exe --desktop --data-dir $DataDir
    } else {
        go run ./backend --desktop --data-dir $DataDir
    }
} finally { Pop-Location }
