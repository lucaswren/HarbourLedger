param()
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$stage = Join-Path $root ('build/source-' + [Guid]::NewGuid().ToString('N'))
$files = @('README.md','LICENSE','NOTICE','go.mod','go.sum','.gitignore','build.ps1',
    'frontend/package.json','frontend/package-lock.json','frontend/index.html','frontend/tsconfig.json','frontend/vite.config.ts',
    'docs/Windows客戶端使用說明.txt','docs/功能與架構分析.md')
$folders = @('backend','frontend/src','frontend/tests','third_party/pdf','assets/fonts','scripts')
try {
    New-Item -ItemType Directory -Force $stage,(Join-Path $root 'dist') | Out-Null
    foreach ($file in $files) {
        $target = Join-Path $stage $file
        New-Item -ItemType Directory -Force (Split-Path $target -Parent) | Out-Null
        Copy-Item -LiteralPath (Join-Path $root $file) -Destination $target
    }
    foreach ($folder in $folders) {
        $target = Join-Path $stage $folder
        New-Item -ItemType Directory -Force (Split-Path $target -Parent) | Out-Null
        Copy-Item -LiteralPath (Join-Path $root $folder) -Destination $target -Recurse
    }
    $unexpected = Get-ChildItem -LiteralPath $stage -Recurse -Force -File | Where-Object {
        $_.Name -like '.env*' -or $_.Extension -in @('.sqlite3','.sqlite3-wal','.sqlite3-shm','.db','.pdf','.exe','.zip','.log') -or $_.Name -eq 'verified_invoices.json'
    }
    if ($unexpected) { throw '來源目錄包含不應發布的帳簿、憑證、密鑰或二進位檔案，停止打包' }
    Compress-Archive -LiteralPath (Get-ChildItem -LiteralPath $stage -Force | Select-Object -ExpandProperty FullName) -DestinationPath (Join-Path $root 'dist/HarbourLedger-source-review.zip') -Force
    Write-Output 'dist/HarbourLedger-source-review.zip'
} finally {
    $resolved = [IO.Path]::GetFullPath($stage)
    $boundary = [IO.Path]::GetFullPath((Join-Path $root 'build')) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolved.StartsWith($boundary,[StringComparison]::OrdinalIgnoreCase)) { throw '來源暫存目錄越界' }
    if (Test-Path -LiteralPath $resolved) { Remove-Item -LiteralPath $resolved -Recurse -Force }
}
