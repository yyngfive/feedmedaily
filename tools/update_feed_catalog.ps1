param(
  [string]$SourceUrl = "https://raw.githubusercontent.com/yyngfive/sci-rss-list/main/data/feeds.json",
  [string]$SourcePath,
  [string]$OutputPath
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($OutputPath)) {
  $OutputPath = Join-Path $root "web\src\data\feedCatalog.ts"
}

function Read-FeedCatalogJson {
  if (-not [string]::IsNullOrWhiteSpace($SourcePath)) {
    return Get-Content -LiteralPath $SourcePath -Raw
  }

  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
  $lastError = $null
  foreach ($attempt in 1..3) {
    try {
      return (Invoke-WebRequest -UseBasicParsing -Uri $SourceUrl -TimeoutSec 60).Content
    }
    catch {
      $lastError = $_
      if ($attempt -eq 3) {
        break
      }
      Write-Warning "Feed catalog download failed (attempt $attempt of 3): $($_.Exception.Message)"
      Start-Sleep -Seconds (2 * $attempt)
    }
  }
  throw $lastError
}

function ConvertTo-TypeScriptString {
  param([Parameter(Mandatory = $true)][string]$Value)
  return ($Value | ConvertTo-Json -Compress)
}

$payload = Read-FeedCatalogJson | ConvertFrom-Json
$items = if ($payload -is [array]) { $payload } elseif ($payload.feeds) { @($payload.feeds) } else { throw "Feed catalog JSON must be an array or an object with a feeds array." }

$seen = @{}
$catalog = foreach ($item in $items) {
  $publisher = "$($item.publisher)".Trim()
  $journal = "$($item.journal)".Trim()
  $url = "$($item.url)".Trim()
  $status = "$($item.status)".Trim()
  if ([string]::IsNullOrWhiteSpace($publisher) -or [string]::IsNullOrWhiteSpace($journal) -or [string]::IsNullOrWhiteSpace($url)) {
    throw "Each feed entry must include publisher, journal, and url."
  }
  if ($status -ne "verified") {
    continue
  }
  if ($seen.ContainsKey($url)) {
    continue
  }
  $seen[$url] = $true
  [pscustomobject]@{
    publisher = $publisher
    journal   = $journal
    url       = $url
    subjects  = @($item.subjects | ForEach-Object { "$_".Trim() } | Where-Object { $_ })
  }
}

if ($catalog.Count -eq 0) {
  throw "Feed catalog is empty."
}

$lines = New-Object System.Collections.Generic.List[string]
$lines.Add("export type FeedCatalogEntry = {")
$lines.Add("  publisher: string;")
$lines.Add("  journal: string;")
$lines.Add("  url: string;")
$lines.Add("  subjects: string[];")
$lines.Add("};")
$lines.Add("")
$lines.Add("export const feedCatalog: FeedCatalogEntry[] = [")
foreach ($item in $catalog) {
  $subjects = @($item.subjects | ForEach-Object { ConvertTo-TypeScriptString -Value $_ }) -join ", "
  $lines.Add("  {")
  $lines.Add("    publisher: $(ConvertTo-TypeScriptString -Value $item.publisher),")
  $lines.Add("    journal: $(ConvertTo-TypeScriptString -Value $item.journal),")
  $lines.Add("    url: $(ConvertTo-TypeScriptString -Value $item.url),")
  $lines.Add("    subjects: [$subjects],")
  $lines.Add("  },")
}
$lines.Add("];")

New-Item -ItemType Directory -Force -Path (Split-Path -Parent $OutputPath) | Out-Null
[System.IO.File]::WriteAllText($OutputPath, [string]::Join("`n", $lines) + "`n", [System.Text.UTF8Encoding]::new($false))
Write-Host "Wrote $($catalog.Count) feed catalog entries to $OutputPath"
