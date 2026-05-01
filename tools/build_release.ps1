param(
  [switch]$SkipInstaller
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$distDir = Join-Path $root "dist"
$appDist = Join-Path $distDir "FeedMeDaily"
$iconPath = Join-Path $root "assets\branding\feedmedaily.ico"
$workspacePython = "C:\Users\Asante\.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe"

function Invoke-NativeStep {
  param(
    [Parameter(Mandatory = $true)]
    [scriptblock]$Command,
    [Parameter(Mandatory = $true)]
    [string]$Description
  )

  & $Command
  if ($LASTEXITCODE -ne 0) {
    throw "$Description failed with exit code $LASTEXITCODE."
  }
}

function Resolve-PyInstaller {
  $candidates = @(
    (Join-Path $root ".venv\Scripts\pyinstaller.exe"),
    (Join-Path $env:USERPROFILE ".local\bin\pyinstaller.exe")
  )
  foreach ($candidate in $candidates) {
    if (Test-Path $candidate) {
      return $candidate
    }
  }

  $command = Get-Command "pyinstaller" -ErrorAction SilentlyContinue
  if ($command) {
    return $command.Source
  }
  throw "PyInstaller was not found. Install it in .venv or as a user tool first."
}

function Resolve-Iscc {
  $candidates = @(
    "C:\Program Files (x86)\Inno Setup 6\ISCC.exe",
    "C:\Program Files\Inno Setup 6\ISCC.exe"
  )
  foreach ($candidate in $candidates) {
    if (Test-Path $candidate) {
      return $candidate
    }
  }

  $command = Get-Command "ISCC.exe" -ErrorAction SilentlyContinue
  if ($command) {
    return $command.Source
  }
  return $null
}

function Stop-ExistingReleaseProcess {
  $running = Get-Process "FeedMeDaily" -ErrorAction SilentlyContinue
  if (-not $running) {
    return
  }

  foreach ($process in $running) {
    $processPath = $null
    try {
      $processPath = $process.Path
    }
    catch {
      $processPath = $null
    }

    if ($processPath -and $processPath.StartsWith($appDist, [System.StringComparison]::OrdinalIgnoreCase)) {
      Write-Host "Stopping running packaged app: $processPath"
      Stop-Process -Id $process.Id -Force
    }
  }
}

function Remove-BuildArtifacts {
  $paths = @(
    (Join-Path $root "build"),
    $appDist,
    (Join-Path $root "FeedMeDaily.spec")
  )

  foreach ($path in $paths) {
    if (-not (Test-Path $path)) {
      continue
    }
    Write-Host "Removing old build artifact: $path"
    Remove-Item -LiteralPath $path -Recurse -Force
  }
}

Push-Location $root
try {
  Stop-ExistingReleaseProcess
  Remove-BuildArtifacts

  if (Test-Path $workspacePython) {
    Invoke-NativeStep `
      -Description "Brand asset generation" `
      -Command { & $workspacePython ".\tools\generate_brand_assets.py" }
  }

  Invoke-NativeStep `
    -Description "Frontend build" `
    -Command { corepack pnpm --dir web build }

  $pyinstaller = Resolve-PyInstaller

  Invoke-NativeStep `
    -Description "PyInstaller packaging" `
    -Command {
      & $pyinstaller `
        --noconfirm `
        --clean `
        --name FeedMeDaily `
        --onedir `
        --icon $iconPath `
        --paths src `
        src\scirssagent\cli.py
    }

  $targetWeb = Join-Path $appDist "web\dist"
  New-Item -ItemType Directory -Force -Path $targetWeb | Out-Null
  Copy-Item -Recurse -Force ".\web\dist\*" $targetWeb
  Copy-Item -Force ".\assets\branding\feedmedaily.ico" (Join-Path $appDist "feedmedaily.ico")

  if (-not $SkipInstaller) {
    $iscc = Resolve-Iscc
    if (-not $iscc) {
      Write-Warning "Inno Setup compiler (ISCC.exe) was not found. Skipping installer build."
    } else {
      Invoke-NativeStep `
        -Description "Inno Setup packaging" `
        -Command { & $iscc ".\installer\feedmedaily.iss" }
    }
  }
}
finally {
  Pop-Location
}
