# Installs the aitbase-plugin CLI and the WebAssembly toolchain it builds plugins with, on Windows.
#
#   irm https://raw.githubusercontent.com/Aitbase/aitbase-vision-plugin-sdk/main/install.ps1 | iex
#
# Settings (environment variables):
#   AITBASE_VERSION          release tag to install, for example v0.4.0 (default: the newest release)
#   AITBASE_INSTALL_DIR      where the CLI goes (default: %LOCALAPPDATA%\aitbase\bin)
#   AITBASE_SKIP_TOOLCHAIN=1 don't download the WebAssembly toolchain now (build --wasm does it later)
#
# The download is checked against the release's SHA256SUMS before it is installed.
$ErrorActionPreference = 'Stop'
$repo = 'Aitbase/aitbase-vision-plugin-sdk'
$installDir = if ($env:AITBASE_INSTALL_DIR) { $env:AITBASE_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'aitbase\bin' }

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "aitbase-plugin-windows-$arch.exe"

$version = $env:AITBASE_VERSION
if (-not $version) {
    # Preview releases are GitHub prereleases, which releases/latest skips: ask for the newest one.
    # The API is rate limited per IP, so fall back to the releases feed, which lists them too.
    try { $version = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases?per_page=1")[0].tag_name } catch { }
    if (-not $version) {
        $feed = (Invoke-WebRequest "https://github.com/$repo/releases.atom" -UseBasicParsing).Content
        if ($feed -match 'releases/tag/([^"<]+)') { $version = $Matches[1] }
    }
    if (-not $version) { throw 'Could not find the newest release; set AITBASE_VERSION, for example v0.4.0' }
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("aitbase-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $base = "https://github.com/$repo/releases/download/$version"
    Write-Host "Downloading aitbase-plugin $version for windows/$arch..."
    Invoke-WebRequest "$base/$asset" -OutFile (Join-Path $tmp $asset) -UseBasicParsing
    Invoke-WebRequest "$base/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS') -UseBasicParsing

    $line = Get-Content (Join-Path $tmp 'SHA256SUMS') | Where-Object { ($_ -split '\s+')[1] -in @($asset, "*$asset") } | Select-Object -First 1
    if (-not $line) { throw "SHA256SUMS has no entry for $asset" }
    $expected = ($line -split '\s+')[0].ToLower()
    $actual = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
    if ($actual -ne $expected) { throw "Checksum mismatch for $asset (expected $expected, got $actual); not installed" }

    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    $exe = Join-Path $installDir 'aitbase-plugin.exe'
    Move-Item -Force (Join-Path $tmp $asset) $exe
    Write-Host "Installed $exe ($(& $exe version))"
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (($userPath -split ';') -contains $installDir)) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$installDir", 'User')
    $env:Path = "$env:Path;$installDir"
    Write-Host "Added $installDir to your PATH (open a new terminal to use it everywhere)."
}

if ($env:AITBASE_SKIP_TOOLCHAIN -ne '1') {
    & $exe toolchain | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "The WebAssembly toolchain download failed; run 'aitbase-plugin toolchain' to retry" }
    Write-Host "WebAssembly toolchain ready: $(& $exe toolchain)"
}

$missing = @()
foreach ($tool in 'cmake', 'python', 'ninja') { if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { $missing += $tool } }
if ($missing) {
    Write-Host ''
    Write-Host "Also install: $($missing -join ', ') (aitbase-plugin build --wasm needs CMake, Python 3 and Ninja on Windows):"
    Write-Host '  winget install Kitware.CMake Python.Python.3.12 Ninja-build.Ninja'
}
Write-Host ''
Write-Host 'Next: aitbase-plugin init my-plugin --id yourname.my-plugin'
