#Requires -Version 5.1
param(
    [switch]$Nightly,
    [string]$Version,
    [string]$Dir,
    [switch]$Force
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = 'sonquer/opendba'
$binary = 'opendba'
$version = if ($Nightly) { 'nightly' } elseif ($Version) { $Version } elseif ($env:OPENDBA_VERSION) { $env:OPENDBA_VERSION } else { 'latest' }
$installDir = if ($Dir) { $Dir } elseif ($env:OPENDBA_INSTALL_DIR) { $env:OPENDBA_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\opendba' }
$force = $Force -or [bool]$env:OPENDBA_FORCE

function Say($text) { Write-Host $text }
function Die($text) { Write-Error $text; exit 1 }

function Get-Target {
    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { Die "no build for $($env:PROCESSOR_ARCHITECTURE). There are builds for amd64 and arm64." }
    }
    "windows_$arch"
}

function Get-Release {
    $url = if ($version -eq 'latest') {
        "https://api.github.com/repos/$repo/releases/latest"
    } else {
        "https://api.github.com/repos/$repo/releases/tags/$version"
    }
    $headers = @{ 'Accept' = 'application/vnd.github+json' }
    if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $($env:GITHUB_TOKEN)" }
    try {
        Invoke-RestMethod -Uri $url -Headers $headers
    } catch {
        Die "no release called $version"
    }
}

function Get-Installed {
    $candidate = Join-Path $installDir "$binary.exe"
    if (-not (Test-Path $candidate)) {
        $found = Get-Command $binary -ErrorAction SilentlyContinue
        if (-not $found) { return $null }
        $candidate = $found.Source
    }
    try {
        (& $candidate version 2>$null | Select-Object -First 1).Split(' ')[0]
    } catch {
        $null
    }
}

function Test-Checksum($archive, $sums) {
    $name = Split-Path $archive -Leaf
    $line = Get-Content $sums | Where-Object { $_ -match "\s$([regex]::Escape($name))$" } | Select-Object -First 1
    if (-not $line) { Die "$name is not in checksums.txt" }
    $want = ($line -split '\s+')[0]
    $got = (Get-FileHash -Path $archive -Algorithm SHA256).Hash.ToLower()
    if ($want -ne $got) {
        Die "$name does not match its checksum`n  expected $want`n  got      $got"
    }
    Say '  checksum ok'
}

function Test-Signature($sums, $bundle) {
    if (-not (Test-Path $bundle)) { return }
    if (-not (Get-Command cosign -ErrorAction SilentlyContinue)) { return }
    & cosign verify-blob $sums `
        --bundle $bundle `
        --certificate-identity-regexp "^https://github.com/$repo/" `
        --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) { Die 'the signature on checksums.txt does not verify' }
    Say '  signature ok'
}

$target = Get-Target
$work = New-Item -ItemType Directory -Path (Join-Path $env:TEMP ([System.Guid]::NewGuid().ToString()))

try {
    Say 'opendba'
    Say "  looking up $version"
    $release = Get-Release

    $archiveAsset = $release.assets | Where-Object { $_.name -like "*_$target.zip" } | Select-Object -First 1
    if (-not $archiveAsset) { Die "$($release.tag_name) has no build for $target" }
    $sumsAsset = $release.assets | Where-Object { $_.name -eq 'checksums.txt' } | Select-Object -First 1
    $bundleAsset = $release.assets | Where-Object { $_.name -eq 'checksums.txt.sigstore.json' } | Select-Object -First 1

    $want = $archiveAsset.name -replace "^${binary}_", '' -replace "_$target\.zip$", ''
    $have = Get-Installed
    if ($have -and $have -eq $want -and -not $force) {
        Say "  $want is already installed"
        exit 0
    }
    if ($have) { Say "  $have is installed, $want is what $($release.tag_name) holds" }

    $archive = Join-Path $work $archiveAsset.name
    Say "  downloading $($archiveAsset.name) from $($release.tag_name)"
    Invoke-WebRequest -Uri $archiveAsset.browser_download_url -OutFile $archive -UseBasicParsing

    if ($sumsAsset) {
        $sums = Join-Path $work 'checksums.txt'
        Invoke-WebRequest -Uri $sumsAsset.browser_download_url -OutFile $sums -UseBasicParsing
        $bundle = Join-Path $work 'checksums.txt.sigstore.json'
        if ($bundleAsset) {
            Invoke-WebRequest -Uri $bundleAsset.browser_download_url -OutFile $bundle -UseBasicParsing
        }
        Test-Signature $sums $bundle
        Test-Checksum $archive $sums
    } else {
        Say '  this release publishes no checksums, so nothing was checked'
    }

    Expand-Archive -Path $archive -DestinationPath $work -Force
    $unpacked = Join-Path $work "$binary.exe"
    if (-not (Test-Path $unpacked)) { Die "the archive holds no $binary.exe" }

    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    Copy-Item -Path $unpacked -Destination (Join-Path $installDir "$binary.exe") -Force

    Say "  installed to $(Join-Path $installDir "$binary.exe")"

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -notlike "*$installDir*") {
        [Environment]::SetEnvironmentVariable('Path', "$userPath;$installDir", 'User')
        Say ''
        Say "$installDir was added to your PATH. Open a new terminal, then run: $binary"
    } else {
        Say ''
        Say "Run: $binary"
    }
} finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
