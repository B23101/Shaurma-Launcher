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
    SHAURMA_ROOT_PUBKEYS, SHAURMA_KEYSET_B64, SHAURMA_KEYSET_SIG   new update system (public: root key, keyset signed by root)
  Missing values only disable the matching feature (warning), unless -RequireSecrets is set.

.PARAMETER Bundle
  setup.exe ставить УСІ компоненти одразу (без bootstrap-докачки з CDN), але
  БЕЗ local-only.flag: далі лаунчер оновлюється сам за підписаним маніфестом v2.
  Потрібно єдиному лаунчеру без токена CDN (build-launcher.yml).

.PARAMETER LocalInstall
  Локальна ПОВНА (shaurma) збірка для встановлення з диска: усі компоненти
  кладуться в setup.exe, CDN-токен вшивається ТІЛЬКИ в backend.dll,
  shell/updater лишаються без токена, а local-only.flag вимикає апдейтер.
  Використовуйте через build-local-full.ps1; базова редакція не збирається.

.EXAMPLE
  .\scripts\build-windows.ps1                       # everything, both editions, unsigned
  .\scripts\build-windows.ps1 -Edition base -Step components
  .\scripts\build-windows.ps1 -Edition shaurma -LocalInstall   # офлайн setup.exe
#>
[CmdletBinding()]
param(
    [ValidateSet('components', 'installer', 'all')] [string]$Step = 'all',
    [ValidateSet('shaurma', 'base', 'all')]         [string]$Edition = 'all',
    [string]$OutDir = '',
    [string]$ComponentsDir = '',
    [string]$AssetsDir = '',
    [string]$Version = '1.0.0',
    [switch]$Strip,
    [switch]$LocalInstall,
    [switch]$Bundle,
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
$Editions = if ($LocalInstall) { @('shaurma') } elseif ($Edition -eq 'all') { @('shaurma', 'base') } else { @($Edition) }
if ($LocalInstall -and $Edition -eq 'base') { throw '-LocalInstall builds only the full (shaurma) edition' }

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
# New update system (plan "single launcher"): public values only. master.cert is never embedded here:
# the full bridge build (R1) is made locally by Pack Manager.
$RootKeys = Get-Secret 'SHAURMA_ROOT_PUBKEYS'
$KeysetB64 = Get-Secret 'SHAURMA_KEYSET_B64'
$KeysetSig = Get-Secret 'SHAURMA_KEYSET_SIG'
if ($RootKeys -and ($RootKeys -notmatch '^[A-Za-z0-9+/=,]+$')) { throw 'SHAURMA_ROOT_PUBKEYS: only base64 keys separated by commas are allowed' }
if ($KeysetB64 -and ($KeysetB64 -notmatch '^[A-Za-z0-9+/=]+$')) { throw 'SHAURMA_KEYSET_B64: base64 only' }
if ($KeysetSig -and ($KeysetSig -notmatch '^[A-Za-z0-9+/=]+$')) { throw 'SHAURMA_KEYSET_SIG: base64 only' }
if (($RootKeys -or $KeysetB64 -or $KeysetSig) -and -not ($RootKeys -and $KeysetB64 -and $KeysetSig)) { throw 'SHAURMA_ROOT_PUBKEYS, SHAURMA_KEYSET_B64 and SHAURMA_KEYSET_SIG must be set together' }
function Get-NewUpdateLdflags {
    if (-not $RootKeys) { return '' }
    $pkg = 'shrm-updater-backend/internal/newupdate'
    return " -X $pkg.RootKeys=$RootKeys -X $pkg.EmbeddedKeyset=$KeysetB64 -X $pkg.EmbeddedKeysetSig=$KeysetSig"
}

function Check-Secrets {
    if ($LocalInstall) {
        # Локальна повна збірка не оновлюється з CDN, тому ключі підпису
        # маніфесту не потрібні. Потрібен ЛИШЕ токен Shaurma: він
        # вшивається в backend.dll, щоб лаунчер качав збірки Шаурми.
        if (-not $TokenShaurma) { Fail 'SHRM_BUILD_TOKEN_SHAURMA is required: the full backend needs it to download Shaurma packs' }
        Write-Warning 'Local build: no CDN token/keys in shell and updater; updates are disabled by local-only.flag.'
        return
    }
    if (-not $PubKeys) { Fail 'SHAURMA_MANIFEST_PUBKEYS is required for production builds' }
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
# shell + bootstrap: icon + manifest + version info.
# Через go-winres, а не `wails3 generate syso`: wails3 alpha пише таблицю
# рядків версії так, що Windows (і SignPath) бачать порожні ProductName/
# ProductVersion. go-winres дає ту саму іконку й маніфест і читабельну версію.
function New-WailsSyso([string]$DestDir, [string]$OriginalName = 'ShaurmLauncher.exe') {
    $gw = Ensure-GoTool 'go-winres.exe' "github.com/tc-hib/go-winres@$GoWinresVersion"
    $winDir = Join-Path $Root 'shell\build\windows'
    $render = Join-Path $OutDir ('_render/exe-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Force -Path $render | Out-Null
    $utf8 = New-Object System.Text.UTF8Encoding($false)

    $manifest = (Get-Content (Join-Path $winDir 'wails.exe.manifest') -Raw -Encoding UTF8).
        Replace('{{.Info.ProductVersion}}', $ProductVersion).Replace('{{.Name}}', 'shell')
    [IO.File]::WriteAllText((Join-Path $render 'app.manifest'), $manifest, $utf8)
    Copy-Item (Join-Path $winDir 'icon.ico') (Join-Path $render 'icon.ico') -Force

    # Шляхи у go-winres — відносно JSON, тому все лежить поруч у $render.
    $resource = [ordered]@{
        RT_GROUP_ICON = @{ '#1' = @{ '0000' = 'icon.ico' } }
        RT_MANIFEST   = @{ '#1' = @{ '0409' = 'app.manifest' } }
        RT_VERSION    = @{ '#1' = @{ '0000' = @{
            fixed = @{ file_version = $ProductVersion; product_version = $ProductVersion }
            info  = @{ '0000' = @{
                CompanyName = $CompanyName; ProductName = $ProductName
                ProductVersion = $ProductVersion; FileVersion = $ProductVersion
                OriginalFilename = $OriginalName; FileDescription = $ProductName
                LegalCopyright = $Copyright; Comments = $ProductName
            } }
        } } }
    }
    $resourceFile = Join-Path $render 'winres.json'
    [IO.File]::WriteAllText($resourceFile, ($resource | ConvertTo-Json -Depth 10), $utf8)
    Invoke-Native $gw @('make', '--arch', 'amd64', '--in', $resourceFile, '--out', 'rsrc') $DestDir
    return (Join-Path $DestDir 'rsrc_windows_amd64.syso')
}

# DLLs: version info only (no icon / manifest), via go-winres.
function New-VersionSyso([string]$DestDir, [string]$OriginalName, [string]$Description) {
    $gw = Ensure-GoTool 'go-winres.exe' "github.com/tc-hib/go-winres@$GoWinresVersion"
    $resourceFile = Join-Path $OutDir "_render/$OriginalName-version.json"
    New-Item -ItemType Directory -Force (Split-Path -Parent $resourceFile) | Out-Null
    $resource = @{ RT_VERSION = @{ '#1' = @{ '0000' = @{
        fixed = @{ file_version = $ProductVersion; product_version = $ProductVersion }
        info = @{ '0000' = @{
            CompanyName = $CompanyName; ProductName = $ProductName
            ProductVersion = $ProductVersion; FileVersion = $ProductVersion
            OriginalFilename = $OriginalName; FileDescription = $Description
            LegalCopyright = $Copyright
        } }
    } } } }
    [IO.File]::WriteAllText($resourceFile, ($resource | ConvertTo-Json -Depth 10), [Text.UTF8Encoding]::new($false))
    Invoke-Native $gw @('make', '--arch', 'amd64', '--in', $resourceFile, '--out', 'rsrc') $DestDir
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
        $goArgs += @('-ldflags', "-buildid= $Ldflags")
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
    $syso = New-WailsSyso $dir $ShellNames[$ed]
    try {
        $ld = '-H windowsgui -buildid='
        if ($Strip) { $ld = "-w -s $ld" }
        if ($LocalInstall) {
            # Локальна збірка: shell не має жодного токена/ключа — усі
            # компоненти ставить інсталятор, bootstrap-качка не потрібна.
            $ld += " -X main.editionChannel=$ed"
        }
        else {
            $ld += " -X main.editionChannel=$ed -X main.cdnToken=$(Get-Token $ed) -X main.trustedKeys=$PubKeys"
            if ($RootKeys) { $ld += " -X main.rootKeys=$RootKeys -X main.embeddedKeyset=$KeysetB64 -X main.embeddedKeysetSig=$KeysetSig" }
        }
        Invoke-Native 'go' @('build', '-tags', 'production', '-buildmode=pie', '-trimpath', '-buildvcs=false',
            '-ldflags', $ld, '-o', $out, '.') $dir @{ CGO_ENABLED = '0' }
    }
    finally { Remove-Item $syso -Force -ErrorAction SilentlyContinue }
}

function Build-Bootstrap([string]$ed) {
    Write-Step "bootstrap ($ed)"
    # Локальний інсталятор ставить ГОТОВИЙ shell і всі компоненти, тож
    # bootstrap-качка (перший запуск з CDN) у такій збірці не потрібна.
    # Повний набір у setup (-Bundle) теж обходиться без bootstrap-докачки.
    if ($LocalInstall -or $Bundle) { return }
    $dir = Join-Path $Root 'bootstrap'
    $out = Join-Path $UnsignedDir "$ed\bootstrap\$($ShellNames[$ed])"
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $out) | Out-Null
    $syso = New-WailsSyso $dir $ShellNames[$ed]
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
    if ($RootKeys) { $ld += " -X shaurma-launcher-backend/internal/access.RootKeys=$RootKeys -X shaurma-launcher-backend/internal/access.EmbeddedKeyset=$KeysetB64 -X shaurma-launcher-backend/internal/access.EmbeddedKeysetSig=$KeysetSig" }
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
    if ($LocalInstall) {
        # Токен Shaurma вшивається ЛИШЕ в backend (див. Build-Backend).
        # Апдейтер у локальній збірці не має жодного CDN-токена/ключа:
        # оновлення вимикає local-only.flag у теці встановлення.
        $ld = ''
    }
    else {
        $tokenField = if ($ed -eq 'base') { 'tokenBase' } else { 'tokenShaurma' }
        $ld = "-X shrm-updater-backend/internal/core.$tokenField=$(Get-Token $ed) -X shrm-updater-backend/internal/sigverify.TrustedKeys=$PubKeys" + (Get-NewUpdateLdflags)
    }
    Build-Dll (Join-Path $Root 'updater-backend') 'production' $ld (Join-Path $UnsignedDir "$ed\updater.dll") 'updater.dll' "$ProductName updater"
}

# After building, prove that metadata is what SignPath will enforce.
function Test-PeMetadata([string]$Dir) {
    Write-Step 'checking PE metadata (SignPath enforces product name/version)'
    $bad = 0
    foreach ($f in Get-ChildItem $Dir -Recurse -Include *.exe, *.dll) {
        $vi = (Get-Item $f.FullName).VersionInfo
        $ok = ($vi.ProductName -eq $ProductName) -and ($vi.ProductVersion -eq $ProductVersion) -and ($vi.CompanyName -eq $CompanyName)
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
    if ($LocalInstall) {
        # Перевірка існує для SignPath: підписані релізні exe/dll мусять мати
        # однакові ProductName/ProductVersion. Локальна збірка не підписується
        # і не публікується, тому метадані не блокують її (див. README).
        Write-Warning 'Local build: PE metadata check skipped (release/SignPath only).'
    }
    else {
        Test-PeMetadata $UnsignedDir
    }
    Write-Host "`nUnsigned components: $UnsignedDir" -ForegroundColor Green
}

function New-AsciiBuildRoot {
    # ISCC не вміє вбудувати іконку, якщо шлях до неї містить не-ASCII
    # символи (наприклад, кирилиця в теці проєкту): "Error on line ...
    # SetupIconFile: The system cannot find the path specified". Тому
    # staging-тека для локального setup.exe завжди в ASCII-шляху.
    foreach ($base in @($env:PUBLIC, $env:ProgramData, [IO.Path]::GetTempPath())) {
        if (-not $base -or $base -match '[^\x00-\x7F]') { continue }
        $dir = Join-Path $base ("shaurma-local-build-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        return $dir
    }
    Fail 'Cannot find an ASCII path for the local build (ISCC cannot embed the icon from a non-ASCII path). Move the repository to an ASCII-only path.'
}

# ---- local full bundle (no CDN) -------------------------------------------
# Розпаковує frontend.zip/updater-frontend.zip у staging, кладе поруч
# local-only.flag (апдейтер бачить його і не робить жодних запитів до CDN)
# і пише локальний launcher-version.json, щоб backend показував версію.
function New-LocalBundle([string]$AppDir, [string]$ed, [bool]$LocalOnly = $true) {
    Write-Step 'full bundle (frontend dirs + launcher-version.json)'
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    foreach ($pair in @(@{ zip = 'frontend.zip'; dir = 'frontend' }, @{ zip = 'updater-frontend.zip'; dir = 'updater-frontend' })) {
        $dest = Join-Path $AppDir $pair.dir
        if (Test-Path $dest) { Remove-Item $dest -Recurse -Force }
        [System.IO.Compression.ZipFile]::ExtractToDirectory((Join-Path $AppDir $pair.zip), $dest)
    }
    if ($LocalOnly) { Set-Content -Path (Join-Path $AppDir 'local-only.flag') -Value 'local build' -Encoding ascii }
    $specs = @(
        @{ path = $ShellNames[$ed]; kind = ''; extractTo = '' },
        @{ path = 'backend.dll'; kind = ''; extractTo = '' },
        @{ path = 'updater.dll'; kind = ''; extractTo = '' },
        @{ path = 'frontend.zip'; kind = 'archive'; extractTo = 'frontend' },
        @{ path = 'updater-frontend.zip'; kind = 'archive'; extractTo = 'updater-frontend' }
    )
    $components = @()
    foreach ($spec in $specs) {
        $file = Join-Path $AppDir $spec.path
        $component = [ordered]@{
            path   = $spec.path
            sha256 = (Get-FileHash -Path $file -Algorithm SHA256).Hash.ToLowerInvariant()
            size   = (Get-Item $file).Length
            url    = "/launcher/$($spec.path)"
        }
        if ($spec.kind) { $component.kind = $spec.kind; $component.extractTo = $spec.extractTo }
        $components += $component
    }
    $notes = if ($LocalOnly) { 'Local build (no CDN)' } else { '' }
    $manifest = [ordered]@{ version = $Version; releaseNotes = $notes; components = $components }
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [IO.File]::WriteAllText((Join-Path $AppDir 'launcher-version.json'), ($manifest | ConvertTo-Json -Depth 6), $utf8)
}

# ---- installer -------------------------------------------------------------
function Invoke-Installer {
    if (-not $ComponentsDir) { $ComponentsDir = $UnsignedDir }
    $ComponentsDir = [System.IO.Path]::GetFullPath($ComponentsDir)
    $iscc = Find-ISCC
    if (-not $iscc) { Fail 'ISCC.exe not found. Install Inno Setup 6 (choco install innosetup).' }
    $asciiRoot = $null
    try {
        # Повний набір у setup: локальна офлайн-збірка АБО єдиний лаунчер (-Bundle).
        $bundleAll = $LocalInstall -or $Bundle
        foreach ($ed in $Editions) {
            Write-Step "installer ($ed)"
            # Локальна збірка має власну staging-теку й власну OutputDir, щоб не
            # перетирати офіційний dist\app-<edition> і dist\*-setup.exe.
            # Staging — в ASCII-шляху: так ISCC вбудує іконку навіть тоді,
            # коли сам репозиторій лежить у теці з кирилицею.
            if ($LocalInstall) {
                if (-not $asciiRoot) { $asciiRoot = New-AsciiBuildRoot }
                $app = Join-Path $asciiRoot "app-$ed-local"
            }
            else {
                $app = Join-Path $OutDir "app-$ed"
            }
            if (Test-Path $app) { Remove-Item $app -Recurse -Force }
            New-Item -ItemType Directory -Force -Path (Join-Path $app 'build\windows') | Out-Null
            if (-not $bundleAll) { New-Item -ItemType Directory -Force -Path (Join-Path $app 'bootstrap') | Out-Null }
            $name = $ShellNames[$ed]
            Copy-Item (Join-Path $ComponentsDir "$ed\$name") $app
            if (-not $bundleAll) { Copy-Item (Join-Path $ComponentsDir "$ed\bootstrap\$name") (Join-Path $app 'bootstrap') }
            Copy-Item (Join-Path $ComponentsDir "$ed\backend.dll") $app
            Copy-Item (Join-Path $ComponentsDir "$ed\updater.dll") $app
            Copy-Item (Join-Path $AssetsDir 'frontend.zip') $app
            Copy-Item (Join-Path $AssetsDir 'updater-frontend.zip') $app
            Copy-Item (Join-Path $Root 'shell\build\windows\icon.ico') (Join-Path $app 'build\windows')
            $isccArgs = @((Join-Path $Root 'installer\installer.iss'), "/DAppImageDir=$app", "/DMyEdition=$ed")
            if ($LocalInstall) {
                New-LocalBundle $app $ed $true
                $isccArgs += '/DLocalInstall=1'
                $isccArgs += '/DLocalOnly=1'
                $isccArgs += "/O$(Join-Path $OutDir 'local')"
            }
            elseif ($Bundle) {
                New-LocalBundle $app $ed $false
                $isccArgs += '/DLocalInstall=1'
                $isccArgs += "/O$OutDir"
            }
            else {
                $isccArgs += "/O$OutDir"
            }
            $th = Get-Secret 'SHAURMA_SIGNER_THUMBPRINTS'
            if ($th -and ($th -match '^[0-9a-fA-F,]+$')) { $isccArgs += "/DOfficialSignerThumbprints=$th" }
            Invoke-Native $iscc $isccArgs $Root
            if ($LocalInstall) {
                # Портативна копія поруч зі setup.exe: dist\app-<edition>-local
                $portable = Join-Path $OutDir "app-$ed-local"
                if (Test-Path $portable) { Remove-Item $portable -Recurse -Force }
                Copy-Item $app $portable -Recurse
            }
        }
    }
    finally {
        if ($asciiRoot) { Remove-Item $asciiRoot -Recurse -Force -ErrorAction SilentlyContinue }
    }
    Write-Host "`nInstallers in: $OutDir" -ForegroundColor Green
}

switch ($Step) {
    'components' { Invoke-Components }
    'installer' { Invoke-Installer }
    'all' { Invoke-Components; Invoke-Installer }
}
