param(
  [string]$SourceUrl = "https://raw.githubusercontent.com/yyngfive/sci-rss-list/main/data/feeds.json",
  [string]$SourcePath,
  [string]$OutputPath,
  [string]$JournalOutputPath,
  [string]$SourceRevision = "",
  [switch]$BundledCatalog
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
  param([Parameter(Mandatory = $true)][AllowEmptyString()][string]$Value)
  return ($Value | ConvertTo-Json -Compress)
}

function Get-FeedValue {
  param(
    [Parameter(Mandatory = $true)]$Item,
    [Parameter(Mandatory = $true)][string]$SnakeCase,
    [Parameter(Mandatory = $true)][string]$CamelCase
  )

  $value = "$($Item.$SnakeCase)".Trim()
  if ([string]::IsNullOrWhiteSpace($value)) {
    $value = "$($Item.$CamelCase)".Trim()
  }
  return $value
}

function Get-FeedScope {
  param(
    [Parameter(Mandatory = $true)]$Item,
    [Parameter(Mandatory = $true)][string]$Publisher,
    [Parameter(Mandatory = $true)][string]$Journal,
    [string]$Notes
  )

  $scope = Get-FeedValue -Item $Item -SnakeCase 'feed_scope' -CamelCase 'feedScope'
  $allowedScopes = @('single_journal', 'multi_journal', 'subject_collection', 'platform_collection')
  if (-not [string]::IsNullOrWhiteSpace($scope)) {
    $scope = $scope.ToLowerInvariant()
    if ($scope -notin $allowedScopes) {
      throw "Unsupported feed_scope '$scope' for '$Journal'."
    }
    return $scope
  }

  # Compatibility fallback for legacy entries that do not define feed_scope.
  # Current SRL records provide it explicitly; inference uses only labels and notes.
  if ($Publisher -eq 'bioRxiv/medRxiv' -and
      ($Journal -match '^(bioRxiv|medRxiv):\s*.+$' -or $Notes -match '(?i)\bsubject collection\b')) {
    return 'subject_collection'
  }
  if ($Notes -match '(?i)\bcross[- ]journal\b|\bmultiple journals\b|\bacross journals\b|\ball journals\b' -or
      $Journal -match '(?i)\bcross[- ]journal\b|\bmultiple journals\b|\bacross journals\b|\ball journals\b|\ball editors.{0,2}suggestions\b') {
    return 'multi_journal'
  }
  $latest = [regex]::Match($Journal, '^(?<prefix>[^:]+):\s*latest(?:\s+preprints)?$')
  if ($latest.Success -and
      [string]::Equals($latest.Groups['prefix'].Value.Trim(), $Publisher, [System.StringComparison]::OrdinalIgnoreCase)) {
    return 'platform_collection'
  }
  return 'single_journal'
}

function Get-CanonicalJournalName {
  param(
    [Parameter(Mandatory = $true)][string]$Journal,
    [Parameter(Mandatory = $true)][string]$Publisher,
    [string]$Notes,
    [hashtable]$FeedVariantKinds
  )

  $name = [regex]::Replace($Journal.Trim(), '\s+', ' ')
  $parenthetical = [regex]::Match($name, '^(?<base>.*)\s+\((?<suffix>[^()]*)\)$')
  if ($parenthetical.Success) {
    $base = $parenthetical.Groups['base'].Value.Trim()
    $suffix = $parenthetical.Groups['suffix'].Value.Trim()
    $variantKey = ($Publisher.Trim() + '|' + $base).ToLowerInvariant()
    $variants = $FeedVariantKinds[$variantKey]
    $hasSiblingFeedKinds = $null -ne $variants -and $variants.Count -gt 1
    $notesConfirmFeedKind = -not [string]::IsNullOrWhiteSpace($Notes) -and
      $Notes.IndexOf($suffix, [System.StringComparison]::OrdinalIgnoreCase) -ge 0
    $notesIdentifyMagazineFeed = $Notes -match '(?i)\bmagazine\b.*\bRSS feed\b|\bRSS feed\b.*\bmagazine\b'
    if ($hasSiblingFeedKinds -or $notesConfirmFeedKind -or $notesIdentifyMagazineFeed) {
      $name = $base
    }
  }

  # Some catalogs identify the publisher's latest feed with a colon label rather than parentheses.
  $colonLabel = [regex]::Match($name, '^(?<publisher>[^:]+):\s*(?<feedName>.+)$')
  if ($colonLabel.Success -and
      [string]::Equals($colonLabel.Groups['publisher'].Value.Trim(), $Publisher.Trim(), [System.StringComparison]::OrdinalIgnoreCase) -and
      $colonLabel.Groups['feedName'].Value -match '(?i)^latest(?:\s+preprints)?$' -and
      $Notes -match '(?i)latest') {
    $name = $Publisher.Trim()
  }

  $subjectCollection = [regex]::Match($name, '^(?<platform>bioRxiv|medRxiv):\s*Subject Collection:\s*(?<subject>.+)$')
  if ($subjectCollection.Success) {
    $name = "$($subjectCollection.Groups['platform'].Value): $($subjectCollection.Groups['subject'].Value.Trim())"
  }

  return $name
}

if ($BundledCatalog) {
  $sourceText = Get-Content -LiteralPath (Join-Path $root 'web/src/data/feedCatalog.ts') -Raw
  $json = ($sourceText -split 'export const feedCatalog: FeedCatalogEntry\[\] = ', 2)[1].Trim().TrimEnd(';')
  $json = $json -replace '(publisher|journal|canonicalJournal|displayJournal|feedScope|feedType|feedName|issnL|url|subjects):', '"$1":' -replace ',\s*([}\]])', '$1'
  $payload = $json | ConvertFrom-Json
  foreach ($item in $payload) { $item | Add-Member status 'verified' }
  $SourceUrl = 'bundled:web/src/data/feedCatalog.ts'
} else {
  $sourceText = Read-FeedCatalogJson
  $payload = $sourceText | ConvertFrom-Json
}
$items = if ($payload -is [array]) { $payload } elseif ($payload.feeds) { @($payload.feeds) } else { throw "Feed catalog JSON must be an array or an object with a feeds array." }

$feedVariantKinds = @{}
foreach ($item in $items) {
  if ("$($item.status)".Trim() -ne 'verified') { continue }
  $publisher = "$($item.publisher)".Trim()
  $journal = "$($item.journal)".Trim()
  $match = [regex]::Match($journal, '^(?<base>.*)\s+\((?<suffix>[^()]*)\)$')
  if (-not $match.Success) { continue }
  $variantKey = ($publisher + '|' + $match.Groups['base'].Value.Trim()).ToLowerInvariant()
  if (-not $feedVariantKinds.ContainsKey($variantKey)) {
    $feedVariantKinds[$variantKey] = @{}
  }
  $feedVariantKinds[$variantKey][$match.Groups['suffix'].Value.Trim()] = $true
}

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
  $notes = "$($item.notes)".Trim()
  $scope = Get-FeedScope -Item $item -Publisher $publisher -Journal $journal -Notes $notes
  $rawCanonical = Get-FeedValue -Item $item -SnakeCase 'canonical_journal' -CamelCase 'canonicalJournal'
  $feedType = (Get-FeedValue -Item $item -SnakeCase 'feed_type' -CamelCase 'feedType').ToLowerInvariant()
  $feedName = Get-FeedValue -Item $item -SnakeCase 'feed_name' -CamelCase 'feedName'
  $issnL = Get-FeedValue -Item $item -SnakeCase 'issn_l' -CamelCase 'issnL'
  $hasExplicitScope = -not [string]::IsNullOrWhiteSpace((Get-FeedValue -Item $item -SnakeCase 'feed_scope' -CamelCase 'feedScope'))
  $canonicalJournal = $null
  if ($scope -eq 'single_journal') {
    $canonicalJournal = $rawCanonical
    if ([string]::IsNullOrWhiteSpace($canonicalJournal)) {
      if ($hasExplicitScope) {
        throw "Single-journal feed '$journal' must define canonical_journal."
      }
      $canonicalJournal = Get-CanonicalJournalName -Journal $journal -Publisher $publisher -Notes $notes -FeedVariantKinds $feedVariantKinds
    }
  }

  $displayJournal = Get-FeedValue -Item $item -SnakeCase 'display_journal' -CamelCase 'displayJournal'
  if ($scope -eq 'subject_collection') {
    $collection = $item.collection
    $platform = "$($collection.platform)".Trim()
    if ([string]::IsNullOrWhiteSpace($platform)) { $platform = "$($item.platform)".Trim() }
    $collectionName = "$($collection.name)".Trim()
    if ([string]::IsNullOrWhiteSpace($collectionName)) { $collectionName = "$($item.collection_name)".Trim() }
    if (-not [string]::IsNullOrWhiteSpace($platform) -and -not [string]::IsNullOrWhiteSpace($collectionName)) {
      $displayJournal = "$platform`: $collectionName"
    }
  }
  if ([string]::IsNullOrWhiteSpace($displayJournal)) {
    if ($scope -eq 'single_journal') {
      $displayJournal = $canonicalJournal
    } elseif ($scope -eq 'platform_collection') {
      $displayJournal = $publisher
    } else {
      $displayJournal = Get-CanonicalJournalName -Journal $journal -Publisher $publisher -Notes $notes -FeedVariantKinds $feedVariantKinds
      if ([string]::IsNullOrWhiteSpace($displayJournal) -and -not [string]::IsNullOrWhiteSpace($feedName)) {
        $displayJournal = "$publisher`: $feedName"
      }
    }
  }

  $collectionId = "$($item.collection.id)".Trim()
  if ([string]::IsNullOrWhiteSpace($collectionId)) { $collectionId = "$($item.collection_id)".Trim() }
  [pscustomobject]@{
    publisher         = $publisher
    journal           = $journal
    canonical_journal = $canonicalJournal
    display_journal   = $displayJournal
    feed_scope        = $scope
    feed_type         = $feedType
    feed_name         = $feedName
    issn_l            = $issnL
    collection_id     = $collectionId
    url               = $url
    subjects          = @($item.subjects | ForEach-Object { "$_".Trim() } | Where-Object { $_ })
    notes             = $notes
  }
}

if ($catalog.Count -eq 0) {
  throw "Feed catalog is empty."
}

# Current SRL entries provide source URLs and explicit identity fields.
# Use canonical_journal and feed_scope when present; retain limited inference for
# legacy entries that predate those fields.
$journalFeeds = @($catalog | ForEach-Object {
  [ordered]@{
    url              = $_.url
    journal          = $_.display_journal
    canonical_journal = $_.canonical_journal
    source_journal   = $_.journal
    publisher        = $_.publisher
    feed_scope       = $_.feed_scope
    feed_type        = $_.feed_type
    feed_name        = $_.feed_name
    issn_l           = $_.issn_l
    collection_id    = $_.collection_id
  }
})
# Preserve exact legacy URLs used by existing RSC subscriptions; their display names
# remain sourced from the verified RSC entries above.
$rscNames = @{}
foreach ($item in $catalog | Where-Object { $_.publisher -eq 'RSC' }) {
  $rscNames[$item.display_journal] = $item.display_journal
}
if ($rscNames.ContainsKey('Chemical Communications')) {
  $journalFeeds += [ordered]@{url = 'http://feeds.rsc.org/rss/cc'; journal = $rscNames['Chemical Communications']; source_journal = ''; publisher = 'RSC'}
}
if ($rscNames.ContainsKey('Chemical Science')) {
  $journalFeeds += [ordered]@{url = 'http://feeds.rsc.org/rss/sc'; journal = $rscNames['Chemical Science']; source_journal = ''; publisher = 'RSC'}
}
# Keep preprint subject labels from the source list; article-level metadata takes
# precedence over feed labels for aggregator and category subscriptions.
$subjectCollectionNames = [System.Collections.Generic.List[string]]::new()
$subjectCollectionSeen = @{}
foreach ($item in $catalog) {
  if ($item.feed_scope -ne 'subject_collection' -or $item.display_journal -cnotmatch '^(bioRxiv|medRxiv):\s*(.+)$') {
    continue
  }
  $platform = $matches[1]
  $subject = $matches[2].Trim()
  if ([string]::IsNullOrWhiteSpace($subject)) { continue }
  $name = "$($platform): $subject"
  $key = $name.ToLowerInvariant()
  if (-not $subjectCollectionSeen.ContainsKey($key)) {
    $subjectCollectionSeen[$key] = $true
    $subjectCollectionNames.Add($name)
  }
}
if ([string]::IsNullOrWhiteSpace($JournalOutputPath)) {
  $JournalOutputPath = Join-Path $root 'internal/journals/catalog.json'
}
$sha = [System.Security.Cryptography.SHA256]::Create()
try {
  $digest = -join ($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($sourceText)) | ForEach-Object { $_.ToString("x2") })
}
finally {
  $sha.Dispose()
}
$journalPayload = [ordered]@{source = $SourceUrl; source_revision = $SourceRevision; source_sha256 = $digest; feeds = $journalFeeds; subject_collections = $subjectCollectionNames.ToArray()}
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $JournalOutputPath) | Out-Null
[IO.File]::WriteAllText($JournalOutputPath, ($journalPayload | ConvertTo-Json -Depth 8) + "`n", [Text.UTF8Encoding]::new($false))
Write-Host "Wrote $($journalFeeds.Count) source-derived feed identities and $($subjectCollectionNames.Count) preprint subject labels to $JournalOutputPath"

$lines = New-Object System.Collections.Generic.List[string]
$lines.Add("export type FeedCatalogEntry = {")
$lines.Add("  publisher: string;")
$lines.Add("  journal: string;")
$lines.Add("  canonicalJournal: string | null;")
$lines.Add("  feedScope: string;")
$lines.Add("  url: string;")
$lines.Add("  subjects: string[];")
$lines.Add("};")
$lines.Add("")
$lines.Add("export const feedCatalog: FeedCatalogEntry[] = [")
foreach ($item in $catalog) {
  $subjects = @($item.subjects | ForEach-Object { ConvertTo-TypeScriptString -Value $_ }) -join ", "
  $canonicalJournalTs = if ([string]::IsNullOrWhiteSpace($item.canonical_journal)) { "null" } else { ConvertTo-TypeScriptString -Value $item.canonical_journal }
  $lines.Add("  {")
  $lines.Add("    publisher: $(ConvertTo-TypeScriptString -Value $item.publisher),")
  $lines.Add("    journal: $(ConvertTo-TypeScriptString -Value $item.journal),")
  $lines.Add("    canonicalJournal: $canonicalJournalTs,")
  $lines.Add("    feedScope: $(ConvertTo-TypeScriptString -Value $item.feed_scope),")
  $lines.Add("    url: $(ConvertTo-TypeScriptString -Value $item.url),")
  $lines.Add("    subjects: [$subjects],")
  $lines.Add("  },")
}
$lines.Add("];")

New-Item -ItemType Directory -Force -Path (Split-Path -Parent $OutputPath) | Out-Null
[System.IO.File]::WriteAllText($OutputPath, [string]::Join("`n", $lines) + "`n", [System.Text.UTF8Encoding]::new($false))
Write-Host "Wrote $($catalog.Count) feed catalog entries to $OutputPath"
