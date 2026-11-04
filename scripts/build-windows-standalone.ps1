# scripts/build-windows-standalone.ps1 — повна збірка Windows-лаунчера БЕЗ Pack Manager.
# Вихідні коди тільки з публічних тек: frontend, updater-frontend, shell, bootstrap,
# backend, updater-backend, installer. Результат у dist\:
#   dist\ShaurmLauncher-setup.exe, dist\ShaurmLauncherBase-setup.exe,
#   dist\app-<edition>\ (shell, bootstrap\, backend.dll, updater.dll, *.zip, launcher-version.json)
#
# Секрети беруться ЛИШЕ зі змінних середовища (у CI — GitHub Secrets), нічого не зберігається у файлах:
#   TOKEN_SHAURMA, TOKEN_BASE (обов'язкові), CF_API_KEY, GROQ_API_KEY, GEMINI_API_KEY,
#   GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, MANIFEST_PUB_KEYS, SIGN_CMD (шаблон з $f),
#   SHAURMA_SIGNER_THUMBPRINTS.
#
# Потрібні інструменти: Go, Node.js/npm, wails3 CLI (v3.0.0-alpha2.121), C-компілятор (gcc/clang)
# для cgo, Inno Setup 6 (ISCC.exe).
#
# Запускати в PowerShell 7 (pwsh).
# Приклад: pwsh scripts\build-windows-standalone.ps1 -Version 1.2.3 -Editions shaurma,base
#
# ВАЖЛИВО: файл у UTF-8 З BOM (інакше кирилицю в рядках може бути зіпсовано).

param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Editions = "shaurma,base",
    [string]$ReleaseNotes = "",
    # Не збирати setup.exe (лише компоненти).
    [switch]$SkipInstaller
)

$ErrorActionPreference = "Stop"
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$dist = Join-Path $root "dist"
$wailsCliVersion = "v3.0.0-alpha2.121"
$copyright = ([string][char]0x00A9) + " SHAURMA TEAM"

$exeNames   = @{ shaurma = "ShaurmLauncher.exe"; base = "ShaurmLauncherBase.exe" }
$setupNames = @{ shaurma = "ShaurmLauncher-setup.exe"; base = "ShaurmLauncherBase-setup.exe" }

function Step($t) { Write-Host "::group::$t" }
function EndStep { Write-Host "::endgroup::" }
function Warn($t) { Write-Host "::warning::$t" }

# Запуск зовнішньої команди з перевіркою коду виходу, тимчасовими env-змінними і текою.
function Invoke-Tool {
    param([string]$Dir, [string]$Exe, [string[]]$Arguments, [hashtable]$EnvVars = @{})
    $saved = @{}
    foreach ($k in $EnvVars.Keys) { $saved[$k] = [Environment]::GetEnvironmentVariable($k); [Environment]::SetEnvironmentVariable($k, $EnvVars[$k]) }
    Push-Location $Dir
    try {
        & $Exe @Arguments
        if ($LASTEXITCODE -ne 0) { throw "'$Exe $($Arguments -join ' ')' завершилась з кодом $LASTEXITCODE (тека $Dir)" }
    } finally {
        Pop-Location
        foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k]) }
    }
}

function Get-Secret($name) { $v = [Environment]::GetEnvironmentVariable($name); if ($null -eq $v) { "" } else { $v.Trim() } }

# ── Валідація входу ─────────────────────────────────────────────────────
$Version = $Version.Trim() -replace '^v', ''
if ($Version -notmatch '^\d+\.\d+\.\d+([-.][0-9A-Za-z.]+)?$') { throw "Некоректна версія '$Version'" }
$edList = @($Editions -replace '\s', '' -split ',' | Where-Object { $_ })
if ($edList.Count -eq 0) { throw "Редакції не задано" }
foreach ($e in $edList) { if (-not $exeNames.ContainsKey($e)) { throw "Невідома редакція '$e'" } }

$tokenShaurma = Get-Secret "TOKEN_SHAURMA"
$tokenBase    = Get-Secret "TOKEN_BASE"
if (-not $tokenShaurma -or -not $tokenBase) { throw "TOKEN_SHAURMA і TOKEN_BASE обов'язкові (потрібні обидва, навіть для однієї редакції)" }
$cfKey   = Get-Secret "CF_API_KEY"
$groqKey = Get-Secret "GROQ_API_KEY"
$gemKey  = Get-Secret "GEMINI_API_KEY"
$gClient = Get-Secret "GOOGLE_CLIENT_ID"
$gSecret = Get-Secret "GOOGLE_CLIENT_SECRET"
$signCmd = Get-Secret "SIGN_CMD"
$keys    = Get-Secret "MANIFEST_PUB_KEYS"
if ($keys) {
    if ($keys -notmatch '^[A-Za-z0-9+/=,]+$' -or $keys.EndsWith(',')) { throw "MANIFEST_PUB_KEYS: дозволені лише base64-ключі через кому" }
    foreach ($k in $keys.Split(',')) {
        if ([Convert]::FromBase64String($k).Length -ne 32) { throw "MANIFEST_PUB_KEYS: ключ Ed25519 має бути 32 байти" }
    }
} else {
    Warn "MANIFEST_PUB_KEYS не задано: клієнти не перевірятимуть підпис маніфесту (як і в поточній збірці)"
}
if (-not $cfKey)   { Warn "CF_API_KEY не задано — збірка без CurseForge" }
if (-not $groqKey) { Warn "GROQ_API_KEY не задано — без AI-перекладу/відгуків" }
if (-not $gemKey)  { Warn "GEMINI_API_KEY не задано — без AI-аналізу крашів" }
if (-not $signCmd) { Warn "SIGN_CMD не задано — exe/dll/setup будуть НЕПІДПИСАНІ" }

# ── Інструменти ─────────────────────────────────────────────────────────
function Find-Wails3 {
    $c = Join-Path (& go env GOPATH) "bin\wails3.exe"
    if (Test-Path $c) { return $c }
    $g = Get-Command wails3 -ErrorAction SilentlyContinue
    if ($g) { return $g.Source }
    throw "wails3 не знайдено. Встанови: go install github.com/wailsapp/wails/v3/cmd/wails3@$wailsCliVersion"
}

function Find-CCompiler {
    $cands = @($env:CC,
        'C:\msys64\clang64\bin\clang.exe', 'C:\msys64\ucrt64\bin\gcc.exe', 'C:\msys64\mingw64\bin\gcc.exe')
    foreach ($n in 'clang', 'gcc') { $g = Get-Command $n -ErrorAction SilentlyContinue; if ($g) { $cands += $g.Source } }
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("cc-probe-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Set-Content -Path (Join-Path $tmp "t.c") -Value "int main(void){return 0;}" -Encoding ascii
        foreach ($c in $cands) {
            if (-not $c -or -not (Test-Path $c)) { continue }
            # Тека компілятора ПЕРШОЮ в PATH: інакше cc1.exe бере DLL з Git for Windows і мовчки падає.
            $oldPath = $env:PATH
            $env:PATH = (Split-Path $c) + ";" + $oldPath
            try {
                & $c (Join-Path $tmp "t.c") -o (Join-Path $tmp "t.exe") 2>$null
                if ($LASTEXITCODE -eq 0 -and (Test-Path (Join-Path $tmp "t.exe"))) { return $c }
            } catch { } finally { $env:PATH = $oldPath }
        }
    } finally { Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
    throw "Робочого C-компілятора не знайдено (потрібен для cgo: msys2 ucrt64 gcc або clang64)"
}

function Find-ISCC {
    $c = @('C:\Program Files (x86)\Inno Setup 6\ISCC.exe', 'C:\Program Files\Inno Setup 6\ISCC.exe')
    if ($env:LOCALAPPDATA) { $c += (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe') }
    foreach ($p in $c) { if (Test-Path $p) { return $p } }
    $g = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($g) { return $g.Source }
    throw "ISCC.exe (Inno Setup 6) не знайдено"
}

foreach ($t in 'go', 'node', 'npm') { if (-not (Get-Command $t -ErrorAction SilentlyContinue)) { throw "'$t' не знайдено в PATH" } }
$wails3 = Find-Wails3
$cc = Find-CCompiler
$iscc = $null
if (-not $SkipInstaller) { $iscc = Find-ISCC }
Write-Host "wails3: $wails3"
Write-Host "cc:     $cc"
if ($iscc) { Write-Host "ISCC:   $iscc" }

# ── Хелпери файлів ──────────────────────────────────────────────────────
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)

# zip з іменами записів через "/" (бекслеші ламають WebKitGTK/апдейтер), без записів-тек.
function New-ZipFromDir([string]$SourceDir, [string]$ZipPath) {
    if (-not (Test-Path $SourceDir)) { throw "Тека для архіву не існує: $SourceDir" }
    New-Item -ItemType Directory -Force -Path (Split-Path $ZipPath) | Out-Null
    $tmp = "$ZipPath.tmp"
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    $base = (Resolve-Path $SourceDir).Path.TrimEnd('\')
    $zip = [IO.Compression.ZipFile]::Open($tmp, [IO.Compression.ZipArchiveMode]::Create)
    try {
        Get-ChildItem -Path $base -Recurse -File | Sort-Object FullName | ForEach-Object {
            $name = $_.FullName.Substring($base.Length + 1).Replace('\', '/')
            [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $_.FullName, $name, [IO.Compression.CompressionLevel]::Optimal)
        }
    } finally { $zip.Dispose() }
    Move-Item -Force $tmp $ZipPath
}

function Expand-ZipToDir([string]$ZipPath, [string]$DestDir) {
    if (Test-Path $DestDir) { Remove-Item -Recurse -Force $DestDir }
    [IO.Compression.ZipFile]::ExtractToDirectory($ZipPath, $DestDir)
}

function Invoke-Sign([string]$Path) {
    if (-not $signCmd) { return }
    if (-not (Test-Path $Path)) { throw "підписування: файл не знайдено: $Path" }
    $cmd = $signCmd.Replace('$f', '"' + $Path + '"')
    & cmd /c $cmd
    if ($LASTEXITCODE -ne 0) { throw "підписування впало: $Path" }
}

# Рендерить manifest/info.json із шаблонів shell\build\windows і генерує .syso (іконка, маніфест, version-info).
function New-Syso([string]$OutDir, [string]$Version, [string]$ProductName, [string]$Company, [string]$Copyright, [string]$Comments) {
    $shellDir = Join-Path $root "shell"
    $winDir = Join-Path $shellDir "build\windows"
    $renderDir = Join-Path $winDir "_render"
    New-Item -ItemType Directory -Force -Path $renderDir | Out-Null
    $manifest = (Get-Content (Join-Path $winDir "wails.exe.manifest") -Raw -Encoding UTF8).
        Replace("{{.Info.ProductVersion}}", $Version).Replace("{{.Name}}", "shell")
    if ($manifest.Contains('{{')) { throw "wails.exe.manifest: нерозкриті плейсхолдери {{...}}" }
    foreach ($m in [regex]::Matches($manifest, '<assemblyIdentity\s[^>]*?name="([^"]*)"[^>]*?version="([^"]*)"')) {
        if ($m.Groups[1].Value -notmatch '^[A-Za-z0-9]+([._-][A-Za-z0-9]+)*$') { throw "wails.exe.manifest: некоректне ім'я assembly '$($m.Groups[1].Value)'" }
        if ($m.Groups[2].Value -notmatch '^\d{1,5}\.\d{1,5}\.\d{1,5}\.\d{1,5}$') { throw "wails.exe.manifest: версія assembly '$($m.Groups[2].Value)' має бути a.b.c.d" }
    }
    [IO.File]::WriteAllText((Join-Path $renderDir "wails.exe.manifest"), $manifest, $utf8NoBom)
    $info = (Get-Content (Join-Path $winDir "info.json") -Raw -Encoding UTF8).
        Replace("{{.Info.ProductVersion}}", $Version).Replace("{{.Info.ProductName}}", $ProductName).
        Replace("{{.Info.CompanyName}}", $Company).Replace("{{.Info.Copyright}}", $Copyright).
        Replace("{{.Info.Comments}}", $Comments)
    # UTF-8 БЕЗ BOM: wails3 generate syso падає на BOM.
    [IO.File]::WriteAllText((Join-Path $renderDir "info.json"), $info, $utf8NoBom)
    $out = Join-Path $OutDir "wails_windows_amd64.syso"
    Invoke-Tool $shellDir $wails3 @("generate", "syso", "-arch", "amd64",
        "-manifest", (Join-Path $renderDir "wails.exe.manifest"), "-info", (Join-Path $renderDir "info.json"),
        "-icon", (Join-Path $winDir "icon.ico"), "-out", $out)
    return $out
}

function Build-GoExe([string]$ModuleDir, [string]$Tags, [string]$Ldflags, [string]$OutPath, [string]$ResVersion, [string]$ProductName) {
    $syso = New-Syso $ModuleDir $ResVersion $ProductName "SHAURMA TEAM" $copyright $ProductName
    try {
        Remove-Item $OutPath -Force -ErrorAction SilentlyContinue
        $a = @("build")
        if ($Tags) { $a += @("-tags", $Tags) }
        $a += @("-buildmode=pie", "-trimpath", "-buildvcs=false", "-ldflags", $Ldflags, "-o", $OutPath, ".")
        Invoke-Tool $ModuleDir "go" $a @{ CGO_ENABLED = "0" }
    } finally { Remove-Item $syso -Force -ErrorAction SilentlyContinue }
}

function Build-Dll([string]$ModuleDir, [string]$Tags, [string]$Ldflags, [string]$OutPath) {
    New-Item -ItemType Directory -Force -Path (Split-Path $OutPath) | Out-Null
    Remove-Item $OutPath -Force -ErrorAction SilentlyContinue
    $a = @("build")
    if ($Tags) { $a += @("-tags", $Tags) }
    $a += @("-buildmode=c-shared", "-trimpath", "-buildvcs=false", "-ldflags", $Ldflags, "-o", $OutPath, ".")
    Invoke-Tool $ModuleDir "go" $a @{ CGO_ENABLED = "1"; CC = $cc; PATH = ((Split-Path $cc) + ";" + $env:PATH) }
    Remove-Item ([IO.Path]::ChangeExtension($OutPath, ".h")) -Force -ErrorAction SilentlyContinue
}

function New-ComponentManifest([string]$AppDir, [object[]]$Specs) {
    $components = @()
    foreach ($s in $Specs) {
        $f = Join-Path $AppDir $s.path
        if (-not (Test-Path $f)) { throw "компонент відсутній у staging: $f" }
        $hash = (Get-FileHash $f -Algorithm SHA256).Hash.ToLower()
        $c = [ordered]@{ path = $s.path; sha256 = $hash; size = (Get-Item $f).Length; url = "/launcher/" + $s.path }
        if ($s.kind) { $c.kind = $s.kind; $c.extractTo = $s.extractTo }
        Write-Host "  $($s.path): sha256=$hash"
        $components += $c
    }
    $m = [ordered]@{ version = $Version; releaseNotes = $ReleaseNotes; components = $components }
    $json = $m | ConvertTo-Json -Depth 6
    [IO.File]::WriteAllText((Join-Path $AppDir "launcher-version.json"), $json, $utf8NoBom)  # без BOM
}

# ── 1. Очищення ─────────────────────────────────────────────────────────
Step "cleanup"
if (Test-Path $dist) { Remove-Item -Recurse -Force $dist }
New-Item -ItemType Directory -Force -Path $dist | Out-Null
foreach ($e in $edList) { New-Item -ItemType Directory -Force -Path (Join-Path $dist "app-$e") | Out-Null }
EndStep

# ── 2. Frontend ─────────────────────────────────────────────────────────
Step "frontend: npm install + build -> frontend.zip"
$feDir = Join-Path $root "frontend"
$npmArgs = @("install", "--no-audit", "--no-fund", "--progress=false", "--loglevel=error", "--fetch-retries=3")
try { Invoke-Tool $feDir "npm" $npmArgs } catch { Warn "npm install впав — повтор"; Invoke-Tool $feDir "npm" $npmArgs }
try { Invoke-Tool $feDir "npm" @("run", "build") } catch { Warn "npm run build впав — повтор"; Invoke-Tool $feDir "npm" @("run", "build") }
New-ZipFromDir (Join-Path $feDir "dist") (Join-Path $dist "frontend.zip")
EndStep

Step "updater-frontend -> updater-frontend.zip"
New-ZipFromDir (Join-Path $root "updater-frontend") (Join-Path $dist "updater-frontend.zip")
EndStep

# ── 3. Shell, bootstrap, backend для кожної редакції ───────────────────
foreach ($ed in $edList) {
    $appDir = Join-Path $dist "app-$ed"
    $token = if ($ed -eq "base") { $tokenBase } else { $tokenShaurma }
    $exe = $exeNames[$ed]

    Step "shell ($ed)"
    $shellOut = Join-Path $root "shell\build\bin\$exe"
    $ld = "-buildid= -H windowsgui -X main.editionChannel=$ed -X main.cdnToken=$token -X main.trustedKeys=$keys"
    Build-GoExe (Join-Path $root "shell") "production" $ld $shellOut "1.0.0" "Shaurma Launcher"
    Copy-Item $shellOut (Join-Path $appDir $exe) -Force
    Invoke-Sign (Join-Path $appDir $exe)
    EndStep

    Step "bootstrap ($ed)"
    $bsOut = Join-Path $appDir "bootstrap\$exe"
    New-Item -ItemType Directory -Force -Path (Split-Path $bsOut) | Out-Null
    $ld = "-buildid= -H windowsgui -X main.cdnToken=$token -X main.trustedKeys=$keys"
    # Ресурси (іконка, маніфест, version-info) беруться з шаблонів shell; syso кладемо в bootstrap\.
    Build-GoExe (Join-Path $root "bootstrap") "" $ld $bsOut "1.0.0" "Shaurma Launcher"
    Invoke-Sign $bsOut
    EndStep

    Step "backend.dll ($ed)"
    $tags = if ($ed -eq "shaurma") { "shaurma" } else { "" }
    $ld = "-X shaurma-launcher-backend/internal/update.token=$token"
    if ($ed -eq "shaurma") {
        $ld += " -X shaurma-launcher-backend/internal/api.tokenShaurma=$tokenShaurma"
        if ($gClient) {
            $ld += " -X main.shaurmaGoogleClientID=$gClient"
            if ($gSecret) { $ld += " -X main.shaurmaGoogleClientSecret=$gSecret" }
        } else { Warn "GOOGLE_CLIENT_ID не задано — shaurma-збірка без входу через Google" }
    }
    if ($cfKey)   { $ld += " -X shaurma-launcher-backend/internal/custompack.curseForgeAPIKey=$cfKey" }
    if ($groqKey) { $ld += " -X shaurma-launcher-backend/internal/ai.DefaultGroqAPIKey=$groqKey" }
    if ($gemKey)  { $ld += " -X shaurma-launcher-backend/internal/ai.DefaultAPIKey=$gemKey" }
    $dll = Join-Path $appDir "backend.dll"
    Build-Dll (Join-Path $root "backend") $tags $ld $dll
    Invoke-Sign $dll
    EndStep
}

# ── 4. updater.dll (спільний для редакцій) ──────────────────────────────
Step "updater.dll"
$ld = "-X shrm-updater-backend/internal/core.tokenShaurma=$tokenShaurma -X shrm-updater-backend/internal/core.tokenBase=$tokenBase -X shrm-updater-backend/internal/sigverify.TrustedKeys=$keys"
$updaterDll = Join-Path $dist "updater.dll"
Build-Dll (Join-Path $root "updater-backend") "" $ld $updaterDll
Invoke-Sign $updaterDll
EndStep

# ── 5. Staging + launcher-version.json + setup.exe ─────────────────────
foreach ($ed in $edList) {
    $appDir = Join-Path $dist "app-$ed"
    $exe = $exeNames[$ed]

    Step "staging ($ed)"
    Copy-Item $updaterDll (Join-Path $appDir "updater.dll") -Force
    Copy-Item (Join-Path $dist "frontend.zip") (Join-Path $appDir "frontend.zip") -Force
    Copy-Item (Join-Path $dist "updater-frontend.zip") (Join-Path $appDir "updater-frontend.zip") -Force
    New-Item -ItemType Directory -Force -Path (Join-Path $appDir "build\windows") | Out-Null
    Copy-Item (Join-Path $root "shell\build\windows\icon.ico") (Join-Path $appDir "build\windows\icon.ico") -Force
    New-ComponentManifest $appDir @(
        @{ path = $exe },
        @{ path = "backend.dll" },
        @{ path = "updater.dll" },
        @{ path = "frontend.zip"; kind = "archive"; extractTo = "frontend" },
        @{ path = "updater-frontend.zip"; kind = "archive"; extractTo = "updater-frontend" }
    )
    Expand-ZipToDir (Join-Path $appDir "frontend.zip") (Join-Path $appDir "frontend")
    Expand-ZipToDir (Join-Path $appDir "updater-frontend.zip") (Join-Path $appDir "updater-frontend")
    EndStep

    if (-not $SkipInstaller) {
        Step "setup.exe ($ed)"
        # Версія в installer.iss навмисно ФІКСОВАНА (1.0.0) — не передаємо її.
        $isArgs = @((Join-Path $root "installer\installer.iss"), "/DAppImageDir=$appDir", "/DMyEdition=$ed")
        $th = Get-Secret "SHAURMA_SIGNER_THUMBPRINTS"
        if ($th -and $th -match '^[0-9a-fA-F,]+$') { $isArgs += "/DOfficialSignerThumbprints=$th" }
        Invoke-Tool $root $iscc $isArgs
        $setup = Join-Path $dist $setupNames[$ed]
        if (-not (Test-Path $setup)) { throw "ISCC не створив $setup" }
        Invoke-Sign $setup
        EndStep
    }
}

Write-Host "Готово. Артефакти у $dist"
Get-ChildItem $dist -File | ForEach-Object { Write-Host ("  {0}  ({1:N1} MB)" -f $_.Name, ($_.Length / 1MB)) }
