param(
  [string]$TrayOutput = ".\\build\\feedmedaily-tray.exe",
  [string]$DaemonOutput = ".\\build\\feedmedailyd.exe"
)

$go = Get-Command "go" -ErrorAction SilentlyContinue
if (-not $go) {
  throw "Go is not installed or not on PATH. Install Go first, then rerun this script."
}

$root = Split-Path -Parent $PSScriptRoot
$trayOutputPath = Join-Path $root $TrayOutput
$daemonOutputPath = Join-Path $root $DaemonOutput
$outputDirs = @(
  (Split-Path -Parent $trayOutputPath),
  (Split-Path -Parent $daemonOutputPath)
)
$goCacheDir = Join-Path $root ".tmp\\go-build-cache"
$goModCacheDir = Join-Path $root ".tmp\\go-mod-cache"

foreach ($dir in $outputDirs) {
  if (-not (Test-Path $dir)) {
    New-Item -ItemType Directory -Path $dir -Force | Out-Null
  }
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
  & go build -ldflags "-H=windowsgui" -o $trayOutputPath .\cmd\feedmedaily-tray
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }
  & go build -ldflags "-H=windowsgui" -o $daemonOutputPath .\cmd\feedmedailyd
}
finally {
  Pop-Location
}
