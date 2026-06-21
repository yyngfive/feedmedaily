param(
  [Parameter(Mandatory = $true)]
  [string]$Version,
  [string]$Url,
  [switch]$DryRun
)

$ErrorActionPreference = "Stop"

$endpoint = "https://alidns.aliyuncs.com/"
$domainName = "stassenger.top"
$rr = "feedmedaily-update"
$recordType = "TXT"
$ttl = 600
$envPath = Join-Path (Split-Path -Parent $PSScriptRoot) ".env"

function Get-ReleaseUrl {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$Url
  )

  if (-not [string]::IsNullOrWhiteSpace($Url)) {
    return $Url.Trim()
  }
  return "https://github.com/yyngfive/feedmedaily/releases/tag/v$($Version.Trim())"
}

function ConvertTo-AliyunPercentEncoded {
  param(
    [Parameter(Mandatory = $true)]
    [AllowEmptyString()]
    [string]$Value
  )

  $encoded = [System.Uri]::EscapeDataString($Value)
  $encoded = $encoded.Replace("+", "%20")
  $encoded = $encoded.Replace("*", "%2A")
  return $encoded.Replace("%7E", "~")
}

function New-SignatureNonce {
  return [guid]::NewGuid().ToString()
}

function Get-OrdinalSortedKeys {
  param(
    [Parameter(Mandatory = $true)]
    [hashtable]$Parameters
  )

  $keys = [string[]]@($Parameters.Keys | ForEach-Object { [string]$_ })
  [Array]::Sort($keys, [StringComparer]::Ordinal)
  return $keys
}

function New-AliyunSignature {
  param(
    [Parameter(Mandatory = $true)]
    [hashtable]$Parameters,
    [Parameter(Mandatory = $true)]
    [string]$AccessKeySecret
  )

  $encodedPairs = New-Object System.Collections.Generic.List[string]
  foreach ($key in (Get-OrdinalSortedKeys -Parameters $Parameters)) {
    $encodedKey = ConvertTo-AliyunPercentEncoded -Value ([string]$key)
    $encodedValue = ConvertTo-AliyunPercentEncoded -Value ([string]$Parameters[$key])
    $encodedPairs.Add("$encodedKey=$encodedValue")
  }
  $canonicalizedQuery = [string]::Join("&", $encodedPairs)
  $stringToSign = "GET&%2F&$(ConvertTo-AliyunPercentEncoded -Value $canonicalizedQuery)"
  $keyBytes = [System.Text.Encoding]::UTF8.GetBytes("$AccessKeySecret&")
  $messageBytes = [System.Text.Encoding]::UTF8.GetBytes($stringToSign)
  $hmac = [System.Security.Cryptography.HMACSHA1]::new($keyBytes)
  try {
    return [Convert]::ToBase64String($hmac.ComputeHash($messageBytes))
  }
  finally {
    $hmac.Dispose()
  }
}

function ConvertTo-QueryString {
  param(
    [Parameter(Mandatory = $true)]
    [hashtable]$Parameters
  )

  $encodedPairs = New-Object System.Collections.Generic.List[string]
  foreach ($key in (Get-OrdinalSortedKeys -Parameters $Parameters)) {
    $encodedKey = ConvertTo-AliyunPercentEncoded -Value ([string]$key)
    $encodedValue = ConvertTo-AliyunPercentEncoded -Value ([string]$Parameters[$key])
    $encodedPairs.Add("$encodedKey=$encodedValue")
  }
  return [string]::Join("&", $encodedPairs)
}

function Invoke-AliyunAlidns {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Action,
    [Parameter(Mandatory = $true)]
    [hashtable]$ActionParameters,
    [Parameter(Mandatory = $true)]
    [string]$AccessKeyId,
    [Parameter(Mandatory = $true)]
    [string]$AccessKeySecret
  )

  $parameters = @{
    Action           = $Action
    Version          = "2015-01-09"
    Format           = "JSON"
    AccessKeyId      = $AccessKeyId
    SignatureMethod  = "HMAC-SHA1"
    Timestamp        = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    SignatureVersion = "1.0"
    SignatureNonce   = New-SignatureNonce
  }
  foreach ($key in $ActionParameters.Keys) {
    $parameters[$key] = $ActionParameters[$key]
  }
  $parameters["Signature"] = New-AliyunSignature -Parameters $parameters -AccessKeySecret $AccessKeySecret
  $uri = "$endpoint`?$(ConvertTo-QueryString -Parameters $parameters)"
  try {
    return Invoke-RestMethod -Method Get -Uri $uri -TimeoutSec 30
  }
  catch {
    $detail = $_.Exception.Message
    if ($_.ErrorDetails -and -not [string]::IsNullOrWhiteSpace($_.ErrorDetails.Message)) {
      $detail = $_.ErrorDetails.Message
    }
    try {
      $apiError = $detail | ConvertFrom-Json
      if ($apiError.Code) {
        $requestId = if ($apiError.RequestId) { " RequestId=$($apiError.RequestId)" } else { "" }
        throw "Aliyun Alidns $Action failed: Code=$($apiError.Code)$requestId"
      }
    }
    catch {
      if ($_.Exception.Message.StartsWith("Aliyun Alidns $Action failed:")) {
        throw
      }
    }
    $detail = $detail -replace 'AccessKeyId%3D[^%&",\s]+', 'AccessKeyId%3D<redacted>'
    $detail = $detail -replace '"AccessKeyId"\s*:\s*"[^"]+"', '"AccessKeyId":"<redacted>"'
    throw "Aliyun Alidns $Action failed: $detail"
  }
}

function Get-RequiredEnvironmentVariable {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Name
  )

  $value = [Environment]::GetEnvironmentVariable($Name, "Process")
  if ([string]::IsNullOrWhiteSpace($value)) {
    $value = [Environment]::GetEnvironmentVariable($Name, "User")
  }
  if ([string]::IsNullOrWhiteSpace($value)) {
    $value = [Environment]::GetEnvironmentVariable($Name, "Machine")
  }
  if ([string]::IsNullOrWhiteSpace($value)) {
    throw "Missing required environment variable: $Name"
  }
  return $value.Trim()
}

function Read-DotEnvValue {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Name
  )

  if (-not (Test-Path $envPath)) {
    return ""
  }
  foreach ($line in Get-Content $envPath) {
    $trimmed = $line.Trim()
    if ($trimmed -eq "" -or $trimmed.StartsWith("#")) {
      continue
    }
    $index = $trimmed.IndexOf("=")
    if ($index -lt 1) {
      continue
    }
    $key = $trimmed.Substring(0, $index).Trim()
    if ($key -ne $Name) {
      continue
    }
    return $trimmed.Substring($index + 1).Trim().Trim('"').Trim("'")
  }
  return ""
}

function Get-RequiredCredential {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Name
  )

  try {
    return Get-RequiredEnvironmentVariable -Name $Name
  }
  catch {
    $value = Read-DotEnvValue -Name $Name
    if (-not [string]::IsNullOrWhiteSpace($value)) {
      return $value.Trim()
    }
    throw "Missing required credential: $Name. Set it as a Windows environment variable or add it to .env."
  }
}

$cleanVersion = $Version.Trim()
if ([string]::IsNullOrWhiteSpace($cleanVersion)) {
  throw "Version must not be empty."
}

$releaseUrl = Get-ReleaseUrl -Version $cleanVersion -Url $Url
$txtValue = "version=$cleanVersion;url=$releaseUrl"
$fqdn = "$rr.$domainName"

Write-Host "Target DNS record: $fqdn"
Write-Host "Record type: $recordType"
Write-Host "TTL: $ttl"
Write-Host "TXT value: $txtValue"

if ($DryRun) {
  Write-Host "Dry run: no Aliyun API request was sent."
  return
}

$accessKeyId = Get-RequiredCredential -Name "ALIBABA_CLOUD_ACCESS_KEY_ID"
$accessKeySecret = Get-RequiredCredential -Name "ALIBABA_CLOUD_ACCESS_KEY_SECRET"

$recordsResponse = Invoke-AliyunAlidns `
  -Action "DescribeDomainRecords" `
  -ActionParameters @{
    DomainName  = $domainName
    RRKeyWord   = $rr
    TypeKeyWord = $recordType
    PageSize    = 100
  } `
  -AccessKeyId $accessKeyId `
  -AccessKeySecret $accessKeySecret

$records = @()
if ($recordsResponse.DomainRecords -and $recordsResponse.DomainRecords.Record) {
  $records = @($recordsResponse.DomainRecords.Record) | Where-Object {
    $_.RR -eq $rr -and $_.Type -eq $recordType
  }
}

if ($records.Count -gt 1) {
  $ids = ($records | ForEach-Object { $_.RecordId }) -join ", "
  throw "Found multiple $recordType records for $fqdn. Clean duplicate records first. RecordIds: $ids"
}

if ($records.Count -eq 1) {
  $recordId = [string]$records[0].RecordId
  if ([string]$records[0].Value -eq $txtValue) {
    Write-Host "DNS TXT record is already up to date."
    Write-Host "RecordId: $recordId"
    Write-Host "Done. DNS caches may take up to $ttl seconds to expire."
    return
  }
  $result = Invoke-AliyunAlidns `
    -Action "UpdateDomainRecord" `
    -ActionParameters @{
      RecordId = $recordId
      RR       = $rr
      Type     = $recordType
      Value    = $txtValue
      TTL      = $ttl
    } `
    -AccessKeyId $accessKeyId `
    -AccessKeySecret $accessKeySecret

  Write-Host "Updated DNS TXT record."
  Write-Host "RecordId: $($result.RecordId)"
}
else {
  $result = Invoke-AliyunAlidns `
    -Action "AddDomainRecord" `
    -ActionParameters @{
      DomainName = $domainName
      RR         = $rr
      Type       = $recordType
      Value      = $txtValue
      TTL        = $ttl
    } `
    -AccessKeyId $accessKeyId `
    -AccessKeySecret $accessKeySecret

  Write-Host "Created DNS TXT record."
  Write-Host "RecordId: $($result.RecordId)"
}

Write-Host "Done. DNS caches may take up to $ttl seconds to expire."
