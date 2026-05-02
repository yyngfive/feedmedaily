param(
  [switch]$SkipInstaller
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$distDir = Join-Path $root "dist"
$appDist = Join-Path $distDir "FeedMeDaily"
$iconPath = Join-Path $root "assets\branding\feedmedaily.ico"

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

function Resolve-BuildPython {
  $candidates = @(
    (Join-Path $root ".venv\Scripts\python.exe")
  )

  foreach ($candidate in $candidates) {
    if ($candidate -and (Test-Path $candidate)) {
      return $candidate
    }
  }

  $command = Get-Command "python" -ErrorAction SilentlyContinue
  if ($command) {
    return $command.Source
  }

  throw "Python was not found."
}

function Get-ProjectVersion {
  param(
    [Parameter(Mandatory = $true)]
    [string]$PythonExe
  )

  $version = & $PythonExe -c "import pathlib, tomllib; data = tomllib.loads(pathlib.Path('pyproject.toml').read_text(encoding='utf-8')); print(data['project']['version'])"

  if ($LASTEXITCODE -ne 0 -or -not $version) {
    throw "Failed to read version from pyproject.toml."
  }

  return $version.Trim()
}

function Write-PyInstallerVersionFile {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Version
  )

  if ($Version -notmatch "^(\d+)\.(\d+)\.(\d+)(?:\.(\d+))?") {
    throw "Version '$Version' must start with MAJOR.MINOR.PATCH, for example 0.1.1."
  }

  $major = [int]$Matches[1]
  $minor = [int]$Matches[2]
  $patch = [int]$Matches[3]
  $build = if ($Matches[4]) { [int]$Matches[4] } else { 0 }

  $versionTuple = "$major, $minor, $patch, $build"
  $versionFile = Join-Path $root "build\version_info.txt"

  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $versionFile) | Out-Null

  @"
VSVersionInfo(
  ffi=FixedFileInfo(
    filevers=($versionTuple),
    prodvers=($versionTuple),
    mask=0x3f,
    flags=0x0,
    OS=0x40004,
    fileType=0x1,
    subtype=0x0,
    date=(0, 0)
  ),
  kids=[
    StringFileInfo([
      StringTable(
        '040904B0',
        [
          StringStruct('CompanyName', 'FeedMeDaily'),
          StringStruct('FileDescription', 'FeedMeDaily'),
          StringStruct('FileVersion', '$Version'),
          StringStruct('InternalName', 'FeedMeDaily'),
          StringStruct('OriginalFilename', 'FeedMeDaily.exe'),
          StringStruct('ProductName', 'FeedMeDaily'),
          StringStruct('ProductVersion', '$Version')
        ]
      )
    ]),
    VarFileInfo([VarStruct('Translation', [1033, 1200])])
  ]
)
"@ | Set-Content -Encoding UTF8 $versionFile

  return $versionFile
}

function Write-UpdateManifest {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Version
  )

  $manifest = @{
    version           = $Version
    download_url      = "https://github.com/yyngfive/feedmedaily/releases/download/v$Version/FeedMeDaily-v$Version.exe"
    release_notes_url = "https://github.com/yyngfive/feedmedaily/releases/tag/v$Version"
  } | ConvertTo-Json -Depth 3

  $manifestPath = Join-Path $distDir "update.json"
  $manifest | Set-Content -Encoding UTF8 $manifestPath
}

Push-Location $root
try {
  Stop-ExistingReleaseProcess
  Remove-BuildArtifacts

  $buildPython = Resolve-BuildPython
  $projectVersion = Get-ProjectVersion -PythonExe $buildPython

  Write-Host "Building FeedMeDaily version $projectVersion"

  $pyinstallerVersionFile = Write-PyInstallerVersionFile -Version $projectVersion

  if (Test-Path $buildPython) {
    Invoke-NativeStep `
      -Description "Brand asset generation" `
      -Command { & $buildPython ".\tools\generate_brand_assets.py" }
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
      --version-file $pyinstallerVersionFile `
      --paths src `
      src\scirssagent\cli.py
  }

  $targetWeb = Join-Path $appDist "web\dist"
  New-Item -ItemType Directory -Force -Path $targetWeb | Out-Null
  Copy-Item -Recurse -Force ".\web\dist\*" $targetWeb
  Copy-Item -Force ".\assets\branding\feedmedaily.ico" (Join-Path $appDist "feedmedaily.ico")
  Write-UpdateManifest -Version $projectVersion
  
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
