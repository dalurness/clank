# Install Clank — https://github.com/dalurness/clank
# Usage: irm https://raw.githubusercontent.com/dalurness/clank/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = "dalurness/clank"
$installDir = "$env:USERPROFILE\.local\bin"
$binary = "clank-windows-amd64.exe"

# Get latest release tag
Write-Host "Fetching latest release..."
$release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
$tag = $release.tag_name
$asset = $release.assets | Where-Object { $_.name -eq $binary }

if (-not $asset) {
    Write-Error "Could not find $binary in release $tag"
    exit 1
}

$url = $asset.browser_download_url

# Create install directory
if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

$dest = Join-Path $installDir "clank.exe"

Write-Host "Installing clank $tag..."
Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing

# Add to PATH if not already there
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    Write-Host "Added $installDir to your PATH."
    Write-Host "Restart your terminal for PATH changes to take effect."
}

Write-Host "Installed to $dest"
Write-Host "Done! Run 'clank --help' to get started."
