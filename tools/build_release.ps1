param(
  [switch]$SkipInstaller
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$distDir = Join-Path $root "dist"
$appDist = Join-Path $distDir "FeedMeDaily"
$iconPath = Join-Path $root "assets\branding\feedmedaily.ico"
$workspacePython = "C:\Users\Asante\.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe"

Push-Location $root
try {
  if (Test-Path $workspacePython) {
    & $workspacePython ".\tools\generate_brand_assets.py"
  }

  corepack pnpm --dir web build

  pyinstaller `
    --noconfirm `
    --clean `
    --name FeedMeDaily `
    --onedir `
    --icon $iconPath `
    --paths src `
    src\scirssagent\cli.py

  $targetWeb = Join-Path $appDist "web\dist"
  New-Item -ItemType Directory -Force -Path $targetWeb | Out-Null
  Copy-Item -Recurse -Force ".\web\dist\*" $targetWeb
  Copy-Item -Force ".\assets\branding\feedmedaily.ico" (Join-Path $appDist "feedmedaily.ico")

  if (-not $SkipInstaller) {
    $iscc = Get-Command "ISCC.exe" -ErrorAction SilentlyContinue
    if (-not $iscc) {
      Write-Warning "Inno Setup compiler (ISCC.exe) was not found. Skipping installer build."
    } else {
      & $iscc.Source ".\installer\feedmedaily.iss"
    }
  }
}
finally {
  Pop-Location
}
