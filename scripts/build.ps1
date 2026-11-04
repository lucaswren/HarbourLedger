param([switch]$SkipInstall)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
$previousCgo = $env:CGO_ENABLED
$previousTemp = $env:TEMP
$previousTmp = $env:TMP
$previousCache = $env:GOCACHE
$previousOS = $env:GOOS
$previousArch = $env:GOARCH
$bundleDir = $null
Push-Location $projectRoot
try {
    New-Item -ItemType Directory -Force build/test-temp, build/go-cache, dist | Out-Null
    $env:TEMP = Join-Path $projectRoot 'build/test-temp'
    $env:TMP = $env:TEMP
    $env:GOCACHE = Join-Path $projectRoot 'build/go-cache'
    if (-not $SkipInstall) {
        npm ci --prefix frontend --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw '前端依赖安装失败' }
    }
    npm run build --prefix frontend
    if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }
    npm test --prefix frontend
    if ($LASTEXITCODE -ne 0) { throw '前端分類測試失敗' }
    go test ./backend
    if ($LASTEXITCODE -ne 0) { throw 'Go回归测试失败' }
    go vet ./backend
    if ($LASTEXITCODE -ne 0) { throw 'Go靜態檢查失敗' }
    # Every archive starts from a fresh allowlisted staging directory.
    $bundleDir = Join-Path $projectRoot ('build/package-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force $bundleDir | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    go build -trimpath -ldflags '-s -w -H windowsgui -X main.defaultDesktop=true' -o "$bundleDir/港賬通.exe" ./backend
    if ($LASTEXITCODE -ne 0) { throw 'Go构建失败' }
    New-Item -ItemType Directory -Force "$bundleDir/frontend", "$bundleDir/assets/fonts" | Out-Null
    $webOutput = [IO.Path]::GetFullPath((Join-Path $bundleDir 'frontend/dist'))
    $bundleRoot = [IO.Path]::GetFullPath($bundleDir) + [IO.Path]::DirectorySeparatorChar
    if (-not $webOutput.StartsWith($bundleRoot, [StringComparison]::OrdinalIgnoreCase)) { throw '构建目录越界' }
    if (Test-Path -LiteralPath $webOutput) { Remove-Item -LiteralPath $webOutput -Recurse -Force }
    Copy-Item -LiteralPath frontend/dist -Destination "$bundleDir/frontend" -Recurse -Force
    Copy-Item -LiteralPath assets/fonts/NotoSansSC-Regular.ttf, assets/fonts/OFL.txt -Destination "$bundleDir/assets/fonts" -Force
    Copy-Item -LiteralPath docs/Windows客戶端使用說明.txt -Destination "$bundleDir/使用說明.txt" -Force
    Copy-Item -LiteralPath .env.example, LICENSE, NOTICE -Destination $bundleDir -Force
    New-Item -ItemType Directory -Force "$bundleDir/licenses" | Out-Null
    Copy-Item -LiteralPath third_party/pdf/LICENSE -Destination "$bundleDir/licenses/pdf-BSD.txt" -Force
    Copy-Item -LiteralPath third_party/pdf/LICENSE-font-metrics -Destination "$bundleDir/licenses/font-metrics-MIT.txt" -Force
    go version -m "$bundleDir/港賬通.exe" | Set-Content "$bundleDir/licenses/Go组件版本.txt" -Encoding utf8
    $moduleLines = go list -m -f '{{if .Dir}}{{.Path}}|{{.Dir}}{{end}}' all
    if ($LASTEXITCODE -ne 0) { throw 'Go许可清单读取失败' }
    foreach ($moduleLine in $moduleLines) {
        $parts = $moduleLine -split '\|', 2
        if ($parts.Count -ne 2 -or $parts[1] -eq $projectRoot -or -not (Test-Path -LiteralPath $parts[1])) { continue }
        $licenseDir = Join-Path "$bundleDir/licenses/go" ($parts[0] -replace '[/\\]', '_')
        New-Item -ItemType Directory -Force $licenseDir | Out-Null
        Get-ChildItem -LiteralPath $parts[1] -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)' } | Copy-Item -Destination $licenseDir -Force
    }
    $packageDirs = npm list --prefix frontend --omit=dev --all --parseable
    if ($LASTEXITCODE -ne 0) { throw '前端运行时依赖清单读取失败' }
    $nodeModulesRoot = [IO.Path]::GetFullPath("$projectRoot/frontend/node_modules") + [IO.Path]::DirectorySeparatorChar
    foreach ($packageDir in $packageDirs) {
        $resolvedPackageDir = [IO.Path]::GetFullPath($packageDir)
        if (-not $resolvedPackageDir.StartsWith($nodeModulesRoot, [StringComparison]::OrdinalIgnoreCase)) { continue }
        $relativePackage = $resolvedPackageDir.Substring($nodeModulesRoot.Length)
        $licenseDir = Join-Path "$bundleDir/licenses/npm" ($relativePackage -replace '[/\\]', '_')
        New-Item -ItemType Directory -Force $licenseDir | Out-Null
        Get-ChildItem -LiteralPath $packageDir -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)' } | Copy-Item -Destination $licenseDir -Force
    }
    Write-Host "构建完成：$bundleDir"
    # Only program files enter the distributable archive, even after the user starts using this folder.
    $archive = Join-Path $projectRoot 'dist/港賬通-Windows免安裝.zip'
    Compress-Archive -LiteralPath "$bundleDir/港賬通.exe", "$bundleDir/frontend", "$bundleDir/assets", "$bundleDir/licenses", "$bundleDir/.env.example", "$bundleDir/使用說明.txt", "$bundleDir/LICENSE", "$bundleDir/NOTICE" -DestinationPath $archive -Force
    Copy-Item -LiteralPath "$bundleDir/港賬通.exe" -Destination (Join-Path $projectRoot '港賬通.exe') -Force
    Write-Host "免安裝壓縮包：$archive（不包含帳簿、原件、密鑰或視窗快取）"
} finally {
    $env:CGO_ENABLED = $previousCgo
    $env:TEMP = $previousTemp
    $env:TMP = $previousTmp
    $env:GOCACHE = $previousCache
    $env:GOOS = $previousOS
    $env:GOARCH = $previousArch
    if ($bundleDir -and (Test-Path -LiteralPath $bundleDir)) {
        $resolvedStage = [IO.Path]::GetFullPath($bundleDir)
        $buildBoundary = [IO.Path]::GetFullPath((Join-Path $projectRoot 'build')) + [IO.Path]::DirectorySeparatorChar
        if (-not $resolvedStage.StartsWith($buildBoundary, [StringComparison]::OrdinalIgnoreCase)) { throw '打包暫存目錄越界' }
        Remove-Item -LiteralPath $resolvedStage -Recurse -Force
    }
    Pop-Location
}
