param(
  [string]$Output = ".\\build\\feedmedaily-tray.exe"
)

$go = Get-Command "go" -ErrorAction SilentlyContinue
if (-not $go) {
  throw "Go is not installed or not on PATH. Install Go first, then rerun this script."
}

$root = Split-Path -Parent $PSScriptRoot
$outputPath = Join-Path $root $Output
$outputDir = Split-Path -Parent $outputPath
$goCacheDir = Join-Path $root ".tmp\\go-build-cache"
$goModCacheDir = Join-Path $root ".tmp\\go-mod-cache"

if (-not (Test-Path $outputDir)) {
  New-Item -ItemType Directory -Path $outputDir | Out-Null
}

foreach ($dir in @($goCacheDir, $goModCacheDir)) {
  if (-not (Test-Path $dir)) {
    New-Item -ItemType Directory -Path $dir -Force | Out-Null
  }
}

Push-Location $root
try {
  $env:GOCACHE = $goCacheDir
  $env:GOMODCACHE = $goModCacheDir
  & go build -ldflags "-H=windowsgui" -o $outputPath .\cmd\feedmedaily-tray
}
finally {
  Pop-Location
}
