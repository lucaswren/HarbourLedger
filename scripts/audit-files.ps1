param([string]$Root = (Split-Path $PSScriptRoot -Parent), [string]$OutputDirectory = 'build/audit')
$ErrorActionPreference = 'Stop'
$rootPath = (Resolve-Path -LiteralPath $Root).Path
$outputPath = [IO.Path]::GetFullPath((Join-Path $rootPath $OutputDirectory))
if (-not $outputPath.StartsWith($rootPath + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw '报告目录必须位于扫描目录内' }
$excluded = @('private', 'data', 'build', 'dist', 'node_modules', '.git', '.agents', '.codex', 'third_party', 'licenses')
$inventory = [Collections.Generic.List[object]]::new()
function Read-Folder([string]$Folder) {
    foreach ($item in Get-ChildItem -LiteralPath $Folder -Force) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { continue }
        if ($item.FullName -eq $outputPath) { continue }
        if ($item.PSIsContainer) {
            if ($item.Name -notin $excluded) { Read-Folder $item.FullName }
            continue
        }
        # Credentials and active SQLite files are outside the content audit.
        if ($item.Name -like '.env*' -or $item.Extension -in @('.sqlite3','.db') -or $item.Name -match '-(wal|shm)$') { continue }
        $relative = $item.FullName.Substring($rootPath.Length + 1)
        $scope = switch -Regex ($relative) {
            '\.exe$' { '构建产物'; break }
            default { '项目源码与资源' }
        }
        $inventory.Add([pscustomobject]@{Path=$relative; Bytes=$item.Length; SHA256=(Get-FileHash -LiteralPath $item.FullName -Algorithm SHA256).Hash.ToLowerInvariant(); Scope=$scope})
    }
}
Read-Folder $rootPath
New-Item -ItemType Directory -Force -Path $outputPath | Out-Null
$inventory | Sort-Object Path | Export-Csv -LiteralPath (Join-Path $outputPath 'inventory.csv') -NoTypeInformation -Encoding utf8BOM
$duplicates = @($inventory | Group-Object SHA256 | Where-Object Count -gt 1 | ForEach-Object {
    [pscustomobject]@{SHA256=$_.Name; Copies=$_.Count; RepeatedBytes=($_.Count-1)*$_.Group[0].Bytes; Files=@($_.Group | Select-Object Path,Scope)}
})
ConvertTo-Json -InputObject $duplicates -Depth 6 | Set-Content -LiteralPath (Join-Path $outputPath 'duplicates.json') -Encoding utf8
[pscustomobject]@{ScannedFiles=$inventory.Count; DuplicateGroups=$duplicates.Count; RepeatedBytes=($duplicates | Measure-Object RepeatedBytes -Sum).Sum; ReportDirectory=$outputPath} | ConvertTo-Json
# This report does not authorize removing backup/original copies or changing database paths.
