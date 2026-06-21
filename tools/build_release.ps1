param(
  [switch]$SkipInstaller
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$distDir = Join-Path $root "dist"
$appDist = Join-Path $distDir "FeedMeDaily"
$iconPath = Join-Path $root "assets\branding\feedmedaily.ico"
$trayBuildScript = Join-Path $root "tools\build_go_tray.ps1"
$trayBuildOutput = Join-Path $root "build\feedmedaily-tray.exe"
$daemonBuildOutput = Join-Path $root "build\feedmedailyd.exe"
$protectedVerifierBuildOutput = Join-Path $root "build\FeedMeDailyProtectedVerifier"

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

function Get-ProjectVersion {
  $packageJsonPath = Join-Path $root "web\\package.json"
  $packageJson = Get-Content $packageJsonPath -Raw | ConvertFrom-Json
  if (-not $packageJson.version) {
    throw "Failed to read version from web/package.json."
  }
  return "$($packageJson.version)".Trim()
}

Push-Location $root
try {
  Stop-ExistingReleaseProcess
  Remove-BuildArtifacts

  $projectVersion = Get-ProjectVersion

  Write-Host "Building FeedMeDaily version $projectVersion"

  Invoke-NativeStep `
    -Description "Frontend build" `
    -Command { corepack pnpm --dir web build }

  if (Test-Path $trayBuildScript) {
    Invoke-NativeStep `
      -Description "Go tray build" `
      -Command { & $trayBuildScript -Version $projectVersion }
  }

  New-Item -ItemType Directory -Force -Path $appDist | Out-Null
  $targetWeb = Join-Path $appDist "web\dist"
  New-Item -ItemType Directory -Force -Path $targetWeb | Out-Null
  Copy-Item -Recurse -Force ".\web\dist\*" $targetWeb
  Copy-Item -Force ".\assets\branding\feedmedaily.ico" (Join-Path $appDist "feedmedaily.ico")
  if (Test-Path $trayBuildOutput) {
    Copy-Item -Force $trayBuildOutput (Join-Path $appDist "FeedMeDailyTray.exe")
  }
  if (Test-Path $daemonBuildOutput) {
    Copy-Item -Force $daemonBuildOutput (Join-Path $appDist "feedmedailyd.exe")
  }
  if (Test-Path $protectedVerifierBuildOutput) {
    Copy-Item -Recurse -Force $protectedVerifierBuildOutput (Join-Path $appDist "FeedMeDailyProtectedVerifier")
  }
  if (-not $SkipInstaller) {
    $iscc = Resolve-Iscc
    if (-not $iscc) {
      Write-Warning "Inno Setup compiler (ISCC.exe) was not found. Skipping installer build."
    }
    else {
      Invoke-NativeStep `
        -Description "Inno Setup packaging" `
        -Command {
        & $iscc `
          "/DAppVersion=$projectVersion" `
          ".\installer\feedmedaily.iss"
      }
    }
  }
}
finally {
  Pop-Location
}
