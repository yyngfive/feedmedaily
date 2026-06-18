param(
  [string]$TrayOutput = ".\\build\\feedmedaily-tray.exe",
  [string]$DaemonOutput = ".\\build\\feedmedailyd.exe",
  [string]$ProtectedVerifierOutputDir = ".\\build\\FeedMeDailyProtectedVerifier",
  [string]$Version = ""
)

$go = Get-Command "go" -ErrorAction SilentlyContinue
if (-not $go) {
  throw "Go is not installed or not on PATH. Install Go first, then rerun this script."
}

$root = Split-Path -Parent $PSScriptRoot
$trayOutputPath = Join-Path $root $TrayOutput
$daemonOutputPath = Join-Path $root $DaemonOutput
$protectedVerifierOutputDirPath = Join-Path $root $ProtectedVerifierOutputDir
$protectedVerifierOutputPath = Join-Path $protectedVerifierOutputDirPath "FeedMeDailyProtectedVerifier.exe"
$outputDirs = @(
  (Split-Path -Parent $trayOutputPath),
  (Split-Path -Parent $daemonOutputPath),
  $protectedVerifierOutputDirPath
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
  if (-not $Version) {
    $packageJson = Get-Content (Join-Path $root "web\\package.json") -Raw | ConvertFrom-Json
    $Version = "$($packageJson.version)".Trim()
  }
  $env:GOCACHE = $goCacheDir
  $env:GOMODCACHE = $goModCacheDir
  if (-not $env:GOPROXY) {
    $env:GOPROXY = "https://goproxy.cn,direct"
  }
  & go build -ldflags "-H=windowsgui -X github.com/yyngfive/scirssagent/internal/runtime.buildVersion=$Version" -o $trayOutputPath .\cmd\feedmedaily-tray
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }
  & go build -ldflags "-H=windowsgui -X github.com/yyngfive/scirssagent/internal/runtime.buildVersion=$Version" -o $daemonOutputPath .\cmd\feedmedailyd
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }
  & go build -tags production -ldflags "-H=windowsgui -X github.com/yyngfive/scirssagent/internal/runtime.buildVersion=$Version" -o $protectedVerifierOutputPath .\cmd\feedmedaily-protected-verifier
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }
}
finally {
  Pop-Location
}
