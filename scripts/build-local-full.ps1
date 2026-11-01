<#
.SYNOPSIS
  Локальна ПОВНА (shaurma) збірка Shaurma Launcher + setup.exe без CDN.

.DESCRIPTION
  Збирає лише повну редакцію: shell.exe, backend.dll, updater.dll,
  frontend.zip/updater-frontend.zip — і пакує їх в Inno Setup інсталятор,
  який ставить лаунчер із ЛОКАЛЬНИХ файлів. Bootstrap-качки і звернень до
  worker'а немає: local-only.flag вимикає апдейтер.

  Токен Shaurma вшивається ТІЛЬКИ в backend.dll (лаунчер качає збірки
  Шаурми). У shell.exe та updater.dll токена немає — тому оновлення з
  сервера неможливі навіть теоретично. Базова редакція не збирається.

.PARAMETER Version  Версія локальної збірки (у launcher-version.json), напр. 1.0.0
.PARAMETER Token    Токен Shaurma. Якщо не задано — $env:TOKEN_SHAURMA,
                    $env:SHRM_BUILD_TOKEN_SHAURMA або прихований ввід.
.PARAMETER NoRun    Не пропонувати запуск інсталятора після збірки.

.EXAMPLE
  .\scripts\build-local-full.ps1
  .\scripts\build-local-full.ps1 -Version 1.2.3 -NoRun
#>
[CmdletBinding()]
param(
    [string]$Version = "1.0.0",
    [string]$Token = "",
    [switch]$NoRun
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

function Info($m) { Write-Host "  $m" -ForegroundColor Gray }
function Ok($m)   { Write-Host "[OK]   $m" -ForegroundColor Green }
function Warn($m) { Write-Host "[УВАГА] $m" -ForegroundColor Yellow }
function Fail($m) { Write-Host "[ПОМИЛКА] $m" -ForegroundColor Red; exit 1 }
function Ask($q)  { $a = Read-Host "$q (Y/n)"; return ($a -eq "" -or $a -match '^[YyТтДд]') }

function Read-Secret($prompt) {
    $sec  = Read-Host $prompt -AsSecureString
    $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec)
    try { return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr) }
}

Write-Host ""
Write-Host "=== Shaurma Launcher: локальна ПОВНА збірка (без CDN) ===" -ForegroundColor Cyan

if (-not (Test-Path (Join-Path $root "scripts\build-windows.ps1"))) {
    Fail "Не знайдено scripts\build-windows.ps1 поруч із батьківською текою скрипта — поклади репозиторій цілком і запусти знову."
}

# ── 1. Інструменти ───────────────────────────────────────────────────────────
Write-Host "`n[1/4] Перевірка інструментів" -ForegroundColor Cyan

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Fail "Не знайдено Go. Встанови Go 1.25+: winget install GoLang.Go (потім відкрий новий термінал)."
}
$goVer = (& go version) -replace '.*go(\d+\.\d+(\.\d+)?).*', '$1'
if ([version]$goVer -lt [version]"1.25.0") { Fail "Потрібен Go 1.25+, зараз $goVer" }
Ok "Go $goVer"

if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
    Fail "Не знайдено Node.js. Встанови: winget install OpenJS.NodeJS.LTS (потім відкрий новий термінал)."
}
Ok "Node $((& node --version))"

$cc = $env:CC
if (-not $cc) {
    $cc = @(
        "C:\msys64\clang64\bin\clang.exe",
        "C:\msys64\ucrt64\bin\gcc.exe",
        "C:\msys64\mingw64\bin\gcc.exe",
        "C:\mingw64\bin\gcc.exe",
        "C:\ProgramData\mingw64\mingw64\bin\gcc.exe",
        "C:\Program Files\LLVM\bin\clang.exe"
    ) | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $cc -and (Get-Command gcc -ErrorAction SilentlyContinue)) { $cc = (Get-Command gcc).Source }
if (-not $cc) {
    Fail "Не знайдено C-компілятор (clang/gcc) для backend.dll і updater.dll. Постав MSYS2: winget install MSYS2.MSYS2 — і запусти скрипт ще раз."
}
Ok "C-компілятор: $cc"

$iscc = @(
    "C:\Program Files (x86)\Inno Setup 6\ISCC.exe",
    "C:\Program Files\Inno Setup 6\ISCC.exe",
    (Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 6\ISCC.exe")
) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
if (-not $iscc -and (Get-Command ISCC.exe -ErrorAction SilentlyContinue)) { $iscc = (Get-Command ISCC.exe).Source }
if (-not $iscc) {
    Fail "Не знайдено Inno Setup 6. Встанови: winget install JRSoftware.InnoSetup (потім відкрий новий термінал)."
}
Ok "Inno Setup: $iscc"

# ── 2. Токен ─────────────────────────────────────────────────────────────────
Write-Host "`n[2/4] Токен Shaurma" -ForegroundColor Cyan

if (-not $Token) { $Token = $env:TOKEN_SHAURMA }
if (-not $Token) { $Token = $env:SHRM_BUILD_TOKEN_SHAURMA }
if (-not $Token) {
    Info "Токен потрібен ЛИШЕ для backend.dll: без нього лаунчер не качатиме збірки Шаурми."
    $Token = Read-Secret "Встав токен Shaurma (ввід прихований)"
}
$Token = $Token.Trim()
if ($Token.Length -lt 16) {
    Fail "Токен Shaurma закороткий (менше 16 символів) — перевір, чи вставив повністю."
}
Ok "токен задано (у файли не записується)"

# ── 3. Збірка компонентів + setup.exe ────────────────────────────────────────
Write-Host "`n[3/4] Збірка (кілька хвилин)" -ForegroundColor Cyan

$env:SHRM_BUILD_TOKEN_SHAURMA = $Token
try {
    & (Join-Path $root "scripts\build-windows.ps1") -Step all -Edition shaurma -LocalInstall -Version $Version
}
catch {
    Fail "Збірка впала: $($_.Exception.Message)"
}
finally {
    Remove-Item Env:SHRM_BUILD_TOKEN_SHAURMA -ErrorAction SilentlyContinue
}

# ── 4. Результат ─────────────────────────────────────────────────────────────
Write-Host "`n[4/4] Готово" -ForegroundColor Cyan

$setup = Join-Path $root "dist\local\ShaurmLauncher-setup.exe"
$portable = Join-Path $root "dist\app-shaurma-local"
if (-not (Test-Path $setup)) {
    Fail "Не знайдено $setup. Помилка ISCC — у виводі вище."
}
Ok "інсталятор: $setup"
Ok "портативна тека: $portable (можна запускати ShaurmLauncher.exe напряму)"
Info "Оновлення з сервера вимкнено (local-only.flag). Збірки Шаурми — за токеном у backend.dll."

if (-not $NoRun -and (Ask "Запустити інсталятор зараз?")) {
    Start-Process $setup
}
