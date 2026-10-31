<#
.SYNOPSIS
  Shaurma Launcher - Windows build (no signing, no secrets in this file).

.DESCRIPTION
  Mirrors the private pack-manager build, so the open-source repo can be built
  by anyone and by GitHub Actions (where SignPath signs the result).

  -Step components : frontend.zip, updater-frontend.zip, shell.exe, bootstrap.exe,
                     backend.dll, updater.dll  ->  dist\unsigned\  (PE files, to be signed)
                                                   dist\assets\    (zip archives, not signed)
  -Step installer  : takes SIGNED components (-ComponentsDir), assembles dist\app-<edition>\
                     and builds setup.exe with Inno Setup  ->  dist\*-setup.exe
  -Step all        : both, locally, with no signing in between.

  Secrets are read ONLY from environment variables (never from files in git):
    SHRM_BUILD_TOKEN_SHAURMA, SHRM_BUILD_TOKEN_BASE   CDN channel tokens
    CF_API_KEY                                       CurseForge client key (recoverable from distributed binaries)
    SHAURMA_GOOGLE_CLIENT_ID, SHAURMA_GOOGLE_CLIENT_SECRET   Google sign-in
    SHAURMA_MANIFEST_PUBKEYS                          Ed25519 public keys (base64, comma separated)
  Missing values only disable the matching feature (warning), unless -RequireSecrets is set.

.EXAMPLE
  .\scripts\build-windows.ps1                       # everything, both editions, unsigned
  .\scripts\build-windows.ps1 -Edition base -Step components
#>
[CmdletBinding()]
param(
    [ValidateSet('components', 'installer', 'all')] [string]$Step = 'all',
    [ValidateSet('shaurma', 'base', 'all')]         [string]$Edition = 'all',
    [string]$OutDir = '',
    [string]$ComponentsDir = '',
    [string]$AssetsDir = '',
    [switch]$Strip,
    [switch]$RequireSecrets
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
if (-not $OutDir) { $OutDir = Join-Path $Root 'dist' }
$OutDir = [System.IO.Path]::GetFullPath($OutDir)

# ---- constants (must match .signpath\artifact-configuration\*.xml) ----------
$ProductName = 'Shaurma Launcher'
$ProductVersion = '1.0.0'          # fixed on purpose: real release version is read at runtime
$CompanyName = 'SHAURMA TEAM'
$Copyright = ([string][char]0x00A9) + ' SHAURMA TEAM'
$WailsCliVersion = 'v3.0.0-alpha2.121'
$GoWinresVersion = 'v0.3.3'

$ShellNames = @{ shaurma = 'ShaurmLauncher.exe'; base = 'ShaurmLauncherBase.exe' }
$Editions = if ($Edition -eq 'all') { @('shaurma', 'base') } else { @($Edition) }

$UnsignedDir = Join-Path $OutDir 'unsigned'
$AssetsOut = Join-Path $OutDir 'assets'
if (-not $AssetsDir) { $AssetsDir = $AssetsOut }

# ---- helpers ---------------------------------------------------------------
function Write-Step([string]$m) { Write-Host "`n== $m ==" -ForegroundColor Cyan }
function Fail([string]$m) { throw $m }

function Get-Secret([string]$name) {
    $v = [Environment]::GetEnvironmentVariable($name)
    if ($null -eq $v) { return '' }
    return $v.Trim()
}

function Invoke-Native {
    param([string]$File, [string[]]$Arguments, [string]$WorkDir = $Root, [hashtable]$EnvVars = @{})
    # Print the command WITHOUT ldflags values (they carry tokens/keys).
    $shown = @(); $hide = $false
    foreach ($a in $Arguments) {
        if ($hide) { $shown += '<hidden>'; $hide = $false; continue }
        $shown += $a
        if ($a -eq '-ldflags') { $hide = $true }
    }
    Write-Host ("> {0} {1}" -f (Split-Path -Leaf $File), ($shown -join ' ')) -ForegroundColor DarkGray
    $saved = @{}
    foreach ($k in $EnvVars.Keys) { $saved[$k] = [Environment]::GetEnvironmentVariable($k); [Environment]::SetEnvironmentVariable($k, $EnvVars[$k]) }
    Push-Location $WorkDir
    try {
        & $File @Arguments
        if ($LASTEXITCODE -ne 0) { Fail "Command failed (exit $LASTEXITCODE): $(Split-Path -Leaf $File)" }
    }
    finally {
        Pop-Location
        foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k]) }
    }
}

function Require-Command([string]$name, [string]$hint) {
    $c = Get-Command $name -ErrorAction SilentlyContinue
    if (-not $c) { Fail "'$name' not found. $hint" }
    return $c.Source
}

function Get-GoBinDir {
    $gp = (& go env GOPATH).Trim()
    return (Join-Path $gp 'bin')
}

function Ensure-GoTool([string]$exe, [string]$pkg) {
    $bin = Join-Path (Get-GoBinDir) $exe
    if (Test-Path $bin) { return $bin }
    $c = Get-Command $exe -ErrorAction SilentlyContinue
    if ($c) { return $c.Source }
    Write-Host "Installing $pkg ..."
    Invoke-Native 'go' @('install', $pkg)
    if (-not (Test-Path $bin)) { Fail "$exe was not installed to $bin" }
    return $bin
}

# C compiler for cgo (backend.dll / updater.dll). Order: CC env, clang64, mingw gcc, PATH gcc, LLVM clang.
function Find-CCompiler {
    $envCC = Get-Secret 'CC'
    if ($envCC) { return $envCC }
    $candidates = @(
        'C:\msys64\clang64\bin\clang.exe',
        'C:\msys64\ucrt64\bin\gcc.exe',
        'C:\msys64\mingw64\bin\gcc.exe',
        'C:\mingw64\bin\gcc.exe',
        'C:\ProgramData\mingw64\mingw64\bin\gcc.exe'
    )
    foreach ($c in $candidates) { if (Test-Path $c) { return $c } }
    $g = Get-Command gcc -ErrorAction SilentlyContinue
    if ($g) { return $g.Source }
    foreach ($c in @('C:\Program Files\LLVM\bin\clang.exe')) { if (Test-Path $c) { return $c } }
    return ''
}

function Find-ISCC {
    foreach ($p in @(
            "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
            "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
            "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe")) {
        if ($p -and (Test-Path $p)) { return $p }
    }
    $c = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($c) { return $c.Source }
    return ''
}

function New-ZipFromDir([string]$SrcDir, [string]$ZipPath) {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    if (Test-Path $ZipPath) { Remove-Item $ZipPath -Force }
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $ZipPath) | Out-Null
    [System.IO.Compression.ZipFile]::CreateFromDirectory($SrcDir, $ZipPath, [System.IO.Compression.CompressionLevel]::Optimal, $false)
}

# ---- secrets / ldflags -----------------------------------------------------
$TokenShaurma = Get-Secret 'SHRM_BUILD_TOKEN_SHAURMA'
$TokenBase = Get-Secret 'SHRM_BUILD_TOKEN_BASE'
$CfKey = Get-Secret 'CF_API_KEY'
$GoogleId = Get-Secret 'SHAURMA_GOOGLE_CLIENT_ID'
$GoogleSecret = Get-Secret 'SHAURMA_GOOGLE_CLIENT_SECRET'
$PubKeys = Get-Secret 'SHAURMA_MANIFEST_PUBKEYS'

function Check-Secrets {
    if ($GoogleSecret -and (Get-Secret 'SHAURMA_GOOGLE_CLIENT_KIND') -ne 'desktop') {
        Fail 'Only public Desktop OAuth client credentials may be embedded; set SHAURMA_GOOGLE_CLIENT_KIND=desktop after verifying the client type'
    }
    $missing = @()
    if (($Editions -contains 'shaurma') -and -not $TokenShaurma) { $missing += 'SHRM_BUILD_TOKEN_SHAURMA' }
    if (($Editions -contains 'base') -and -not $TokenBase) { $missing += 'SHRM_BUILD_TOKEN_BASE' }
    if (-not $PubKeys) { $missing += 'SHAURMA_MANIFEST_PUBKEYS' }
    if ($missing.Count -gt 0) {
        $msg = 'Not set: ' + ($missing -join ', ') + ' (matching features will be disabled in this build)'
        if ($RequireSecrets) { Fail $msg }
        Write-Warning $msg
    }
    if ($PubKeys -and ($PubKeys -notmatch '^[A-Za-z0-9+/=,]+$')) { Fail 'SHAURMA_MANIFEST_PUBKEYS: only base64 keys separated by commas are allowed' }
    if ($PubKeys) {
        foreach ($key in $PubKeys.Split(',')) {
            try { $decoded = [Convert]::FromBase64String($key) }
            catch { Fail 'SHAURMA_MANIFEST_PUBKEYS: invalid base64 public key' }
            if ($decoded.Length -ne 32) { Fail 'SHAURMA_MANIFEST_PUBKEYS: each Ed25519 public key must contain 32 bytes' }
        }
    }
}

function Get-Token([string]$ed) { if ($ed -eq 'base') { return $TokenBase } else { return $TokenShaurma } }

# ---- resources (version info, icon, manifest) -------------------------------
# shell + bootstrap: Wails template (icon + manifest + version info).
function New-WailsSyso([string]$DestDir) {
    $wails3 = Ensure-GoTool 'wails3.exe' "github.com/wailsapp/wails/v3/cmd/wails3@$WailsCliVersion"
    $winDir = Join-Path $Root 'shell\build\windows'
    $render = Join-Path $OutDir '_render'
    New-Item -ItemType Directory -Force -Path $render | Out-Null
    $utf8 = New-Object System.Text.UTF8Encoding($false)   # no BOM: wails3 rejects BOM

    $manifest = (Get-Content (Join-Path $winDir 'wails.exe.manifest') -Raw -Encoding UTF8).
        Replace('{{.Info.ProductVersion}}', $ProductVersion).Replace('{{.Name}}', 'shell')
    [IO.File]::WriteAllText((Join-Path $render 'wails.exe.manifest'), $manifest, $utf8)

    $info = (Get-Content (Join-Path $winDir 'info.json') -Raw -Encoding UTF8).
        Replace('{{.Info.ProductVersion}}', $ProductVersion).
        Replace('{{.Info.ProductName}}', $ProductName).
        Replace('{{.Info.CompanyName}}', $CompanyName).
        Replace('{{.Info.Copyright}}', $Copyright).
        Replace('{{.Info.Comments}}', $ProductName)
    [IO.File]::WriteAllText((Join-Path $render 'info.json'), $info, $utf8)

    $out = Join-Path $DestDir 'wails_windows_amd64.syso'
    Invoke-Native $wails3 @('generate', 'syso', '-arch', 'amd64',
        '-manifest', (Join-Path $render 'wails.exe.manifest'),
        '-info', (Join-Path $render 'info.json'),
        '-icon', (Join-Path $winDir 'icon.ico'), '-out', $out) $Root
    return $out
}

# DLLs: version info only (no icon / manifest), via go-winres.
function New-VersionSyso([string]$DestDir, [string]$OriginalName, [string]$Description) {
    $gw = Ensure-GoTool 'go-winres.exe' "github.com/tc-hib/go-winres@$GoWinresVersion"
    Invoke-Native $gw @('simply', '--arch', 'amd64', '--manifest', 'none', '--out', 'rsrc',
        '--product-name', $ProductName, '--file-description', $Description,
        '--product-version', $ProductVersion, '--file-version', $ProductVersion,
        '--original-filename', $OriginalName, '--copyright', $Copyright) $DestDir
    return (Join-Path $DestDir 'rsrc_windows_amd64.syso')
}

function Build-Dll([string]$ModuleDir, [string]$Tags, [string]$Ldflags, [string]$Out, [string]$OriginalName, [string]$Description) {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Out) | Out-Null
    if (Test-Path $Out) { Remove-Item $Out -Force }
    $syso = New-VersionSyso $ModuleDir $OriginalName $Description
    try {
        $goArgs = @('build')
        if ($Tags) { $goArgs += @('-tags', $Tags) }
        $goArgs += @('-buildmode=c-shared', '-trimpath', '-buildvcs=false')
        if ($Ldflags) { $goArgs += @('-ldflags', $Ldflags) }
        $goArgs += @('-o', $Out, '.')
        $envs = @{ CGO_ENABLED = '1' }
        $cc = Find-CCompiler
        if ($cc) {
            $envs['CC'] = $cc
            $envs['PATH'] = (Split-Path -Parent $cc) + [IO.Path]::PathSeparator + $env:PATH   # compiler dir first
        }
        else { Write-Warning 'No C compiler found (install MSYS2 clang64/mingw gcc); relying on gcc from PATH.' }
        Invoke-Native 'go' $goArgs $ModuleDir $envs
    }
    finally { Remove-Item $syso -Force -ErrorAction SilentlyContinue }
    Remove-Item ([IO.Path]::ChangeExtension($Out, '.h')) -Force -ErrorAction SilentlyContinue
}

# ---- components ------------------------------------------------------------
function Build-Frontends {
    Write-Step 'frontend -> frontend.zip'
    Require-Command 'npm' 'Install Node.js 20+.' | Out-Null
    $fe = Join-Path $Root 'frontend'
    $flags = @('--no-audit', '--no-fund', '--progress=false', '--loglevel=error')
    Invoke-Native 'npm.cmd' (@('ci') + $flags) $fe
    Invoke-Native 'npm.cmd' @('run', 'build') $fe
    & (Join-Path $PSScriptRoot 'collect-licenses.ps1') -Destination (Join-Path $fe 'dist/licenses/dependencies')
    New-ZipFromDir (Join-Path $fe 'dist') (Join-Path $AssetsOut 'frontend.zip')

    Write-Step 'updater-frontend -> updater-frontend.zip'
    New-ZipFromDir (Join-Path $Root 'updater-frontend') (Join-Path $AssetsOut 'updater-frontend.zip')
}

function Build-Shell([string]$ed) {
    Write-Step "shell ($ed)"
    $name = $ShellNames[$ed]
    $dir = Join-Path $Root 'shell'
    $out = Join-Path $UnsignedDir "$ed\$name"
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $out) | Out-Null
    $syso = New-WailsSyso $dir
    try {
        $ld = '-H windowsgui -buildid='
        if ($Strip) { $ld = "-w -s $ld" }
        $ld += " -X main.editionChannel=$ed -X main.cdnToken=$(Get-Token $ed) -X main.trustedKeys=$PubKeys"
        Invoke-Native 'go' @('build', '-tags', 'production', '-buildmode=pie', '-trimpath', '-buildvcs=false',
            '-ldflags', $ld, '-o', $out, '.') $dir @{ CGO_ENABLED = '0' }
    }
    finally { Remove-Item $syso -Force -ErrorAction SilentlyContinue }
}

function Build-Bootstrap([string]$ed) {
    Write-Step "bootstrap ($ed)"
    $dir = Join-Path $Root 'bootstrap'
    $out = Join-Path $UnsignedDir "$ed\bootstrap\$($ShellNames[$ed])"
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $out) | Out-Null
    $syso = New-WailsSyso $dir
    try {
        $ld = "-H windowsgui -buildid= -X main.cdnToken=$(Get-Token $ed) -X main.trustedKeys=$PubKeys"
        Invoke-Native 'go' @('build', '-tags', 'production', '-buildmode=pie', '-trimpath', '-buildvcs=false',
            '-ldflags', $ld, '-o', $out, '.') $dir @{ CGO_ENABLED = '0' }
    }
    finally { Remove-Item $syso -Force -ErrorAction SilentlyContinue }
}

function Build-Backend([string]$ed) {
    Write-Step "backend.dll ($ed)"
    $tags = if ($ed -eq 'shaurma') { 'production,shaurma' } else { 'production' }
    $ld = "-X shaurma-launcher-backend/internal/update.token=$(Get-Token $ed)"
    if ($ed -eq 'shaurma') {
        $ld += " -X shaurma-launcher-backend/internal/api.tokenShaurma=$TokenShaurma"

    }
    if ($GoogleId) {
        $ld += " -X main.shaurmaGoogleClientID=$GoogleId"
        if ($GoogleSecret) { $ld += " -X main.shaurmaGoogleClientSecret=$GoogleSecret" }
    }
    if ($CfKey) { $ld += " -X shaurma-launcher-backend/internal/custompack.curseForgeAPIKey=$CfKey" }
    Build-Dll (Join-Path $Root 'backend') $tags $ld (Join-Path $UnsignedDir "$ed\backend.dll") 'backend.dll' "$ProductName backend"
}

function Build-Updater([string]$ed) {
    Write-Step "updater.dll ($ed)"
    $tokenField = if ($ed -eq 'base') { 'tokenBase' } else { 'tokenShaurma' }
    $ld = "-X shrm-updater-backend/internal/core.$tokenField=$(Get-Token $ed) -X shrm-updater-backend/internal/sigverify.TrustedKeys=$PubKeys"
    Build-Dll (Join-Path $Root 'updater-backend') 'production' $ld (Join-Path $UnsignedDir "$ed\updater.dll") 'updater.dll' "$ProductName updater"
}

# After building, prove that metadata is what SignPath will enforce.
function Test-PeMetadata([string]$Dir) {
    Write-Step 'checking PE metadata (SignPath enforces product name/version)'
    $bad = 0
    foreach ($f in Get-ChildItem $Dir -Recurse -Include *.exe, *.dll) {
        $vi = (Get-Item $f.FullName).VersionInfo
        $ok = ($vi.ProductName -eq $ProductName) -and ($vi.ProductVersion -eq $ProductVersion)
        $rel = $f.FullName.Substring($Dir.Length).TrimStart('\')
        Write-Host ("{0,-45} {1} / {2}  {3}" -f $rel, $vi.ProductName, $vi.ProductVersion, $(if ($ok) { 'OK' } else { 'MISMATCH' }))
        if (-not $ok) { $bad++ }
    }
    if ($bad -gt 0) { Fail "$bad file(s) have wrong ProductName/ProductVersion - SignPath would reject them." }
}

function Invoke-Components {
    Check-Secrets
    Require-Command 'go' 'Install Go (version from backend\go.mod).' | Out-Null
    if (Test-Path $UnsignedDir) { Remove-Item $UnsignedDir -Recurse -Force }
    if (Test-Path $AssetsOut) { Remove-Item $AssetsOut -Recurse -Force }
    Build-Frontends
    foreach ($ed in $Editions) { Build-Shell $ed; Build-Bootstrap $ed; Build-Backend $ed; Build-Updater $ed }
    Test-PeMetadata $UnsignedDir
    Write-Host "`nUnsigned components: $UnsignedDir" -ForegroundColor Green
}

# ---- installer -------------------------------------------------------------
function Invoke-Installer {
    if (-not $ComponentsDir) { $ComponentsDir = $UnsignedDir }
    $ComponentsDir = [System.IO.Path]::GetFullPath($ComponentsDir)
    $iscc = Find-ISCC
    if (-not $iscc) { Fail 'ISCC.exe not found. Install Inno Setup 6 (choco install innosetup).' }
    foreach ($ed in $Editions) {
        Write-Step "installer ($ed)"
        $app = Join-Path $OutDir "app-$ed"
        if (Test-Path $app) { Remove-Item $app -Recurse -Force }
        New-Item -ItemType Directory -Force -Path (Join-Path $app 'bootstrap'), (Join-Path $app 'build\windows') | Out-Null
        $name = $ShellNames[$ed]
        Copy-Item (Join-Path $ComponentsDir "$ed\$name") $app
        Copy-Item (Join-Path $ComponentsDir "$ed\bootstrap\$name") (Join-Path $app 'bootstrap')
        Copy-Item (Join-Path $ComponentsDir "$ed\backend.dll") $app
        Copy-Item (Join-Path $ComponentsDir "$ed\updater.dll") $app
        Copy-Item (Join-Path $AssetsDir 'frontend.zip') $app
        Copy-Item (Join-Path $AssetsDir 'updater-frontend.zip') $app
        Copy-Item (Join-Path $Root 'shell\build\windows\icon.ico') (Join-Path $app 'build\windows')
        $isccArgs = @((Join-Path $Root 'installer\installer.iss'), "/DAppImageDir=$app", "/DMyEdition=$ed", "/O$OutDir")
        $th = Get-Secret 'SHAURMA_SIGNER_THUMBPRINTS'
        if ($th -and ($th -match '^[0-9a-fA-F,]+$')) { $isccArgs += "/DOfficialSignerThumbprints=$th" }
        Invoke-Native $iscc $isccArgs $Root
    }
    Write-Host "`nInstallers in: $OutDir" -ForegroundColor Green
}

switch ($Step) {
    'components' { Invoke-Components }
    'installer' { Invoke-Installer }
    'all' { Invoke-Components; Invoke-Installer }
}
