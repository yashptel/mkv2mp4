# mkv2mp4 installer for Windows.
#
#   iwr -useb https://raw.githubusercontent.com/yashptel/mkv2mp4/main/install.ps1 | iex
#
# Optional overrides (set before running):
#   $env:MKV2MP4_VERSION = 'v0.1.1'                    # specific release; default is latest
#   $env:MKV2MP4_PREFIX  = "$env:USERPROFILE\bin"      # install dir; default %LOCALAPPDATA%\Programs\mkv2mp4

#Requires -Version 5.1
$ErrorActionPreference = 'Stop'

# Speed up Invoke-WebRequest dramatically by suppressing the progress UI.
$ProgressPreference = 'SilentlyContinue'

$repo    = 'yashptel/mkv2mp4'
$prefix  = if ($env:MKV2MP4_PREFIX) { $env:MKV2MP4_PREFIX } else { Join-Path $env:LOCALAPPDATA 'Programs\mkv2mp4' }
$version = if ($env:MKV2MP4_VERSION) { $env:MKV2MP4_VERSION } else { 'latest' }

function Get-Arch {
    if (-not [Environment]::Is64BitOperatingSystem) {
        throw 'mkv2mp4 only ships a 64-bit Windows build.'
    }
    # goreleaser currently produces windows/amd64 only (arm64 is ignored).
    return 'amd64'
}

function Resolve-LatestVersion {
    Write-Host 'Resolving latest release...'
    $api = "https://api.github.com/repos/$repo/releases/latest"
    $resp = Invoke-RestMethod -Uri $api -UseBasicParsing
    return $resp.tag_name
}

function Verify-Checksum($zipPath, $asset) {
    $csumUrl = "https://github.com/$repo/releases/download/$version/checksums.txt"
    try {
        $csumPath = Join-Path (Split-Path $zipPath) 'checksums.txt'
        Invoke-WebRequest -Uri $csumUrl -OutFile $csumPath -UseBasicParsing
        $line = Get-Content $csumPath | Where-Object { $_ -match "\s$([regex]::Escape($asset))$" } | Select-Object -First 1
        if (-not $line) {
            Write-Warning "Checksum for $asset not found in checksums.txt; skipping verification"
            return
        }
        $expected = ($line -split '\s+')[0].ToLower()
        $actual   = (Get-FileHash -Algorithm SHA256 -Path $zipPath).Hash.ToLower()
        if ($actual -ne $expected) {
            throw "SHA256 mismatch: got $actual, expected $expected"
        }
        Write-Host "Verified SHA256 $expected"
    } catch [System.Net.WebException] {
        Write-Warning "Could not fetch checksums.txt; skipping verification"
    }
}

function Add-ToUserPath($dir) {
    $userPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
    $existing = if ($userPath) { $userPath -split ';' | Where-Object { $_ } } else { @() }
    if ($existing -contains $dir) {
        return $false
    }
    $newPath = if ($userPath) { "$userPath;$dir" } else { $dir }
    [Environment]::SetEnvironmentVariable('PATH', $newPath, 'User')
    # Also update the current process so the user can invoke mkv2mp4 immediately.
    $env:PATH = "$env:PATH;$dir"
    return $true
}

$arch = Get-Arch
if ($version -eq 'latest') {
    $version = Resolve-LatestVersion
}
if (-not $version) {
    throw 'Could not resolve a release version.'
}

$plainVersion = $version.TrimStart('v')
$asset = "mkv2mp4_${plainVersion}_windows_${arch}.zip"
$url   = "https://github.com/$repo/releases/download/$version/$asset"

$tmpRoot = Join-Path $env:TEMP ("mkv2mp4-install-" + [guid]::NewGuid().Guid)
New-Item -ItemType Directory -Force -Path $tmpRoot | Out-Null

try {
    $zipPath = Join-Path $tmpRoot $asset
    Write-Host "Downloading $url"
    Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing

    Verify-Checksum -zipPath $zipPath -asset $asset

    Write-Host "Extracting to $prefix"
    New-Item -ItemType Directory -Force -Path $prefix | Out-Null
    $extractDir = Join-Path $tmpRoot 'extracted'
    Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force

    $srcExe = Get-ChildItem -Path $extractDir -Filter 'mkv2mp4.exe' -Recurse | Select-Object -First 1
    if (-not $srcExe) {
        throw 'extracted archive did not contain mkv2mp4.exe'
    }
    $destExe = Join-Path $prefix 'mkv2mp4.exe'
    Move-Item -Force $srcExe.FullName $destExe

    if (Add-ToUserPath $prefix) {
        Write-Host "Added $prefix to user PATH" -ForegroundColor Yellow
        Write-Host '  (open a new terminal for it to take effect globally)'
    }

    Write-Host ''
    Write-Host "Installed $destExe" -ForegroundColor Green
    & $destExe --version
} finally {
    Remove-Item -Recurse -Force $tmpRoot -ErrorAction SilentlyContinue
}
