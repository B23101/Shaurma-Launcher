<#
.SYNOPSIS
  Резервна копія ключів, підписів, сертифіката й токенів (на флешку).

.DESCRIPTION
  Збирає в одну теку все, без чого не можна випустити оновлення чи
  відновити сервери. Значення НЕ виводяться на екран і НЕ змінюються
  на ПК: файли лише копіюються.

  Приклад (флешка E:):
    powershell -ExecutionPolicy Bypass -File scripts\backup-secrets.ps1 -Destination E:\
  Зашифрований архів .7z (якщо встановлено 7-Zip, пароль спитає 7-Zip):
    powershell -ExecutionPolicy Bypass -File scripts\backup-secrets.ps1 -Destination E:\ -Encrypt
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Destination,
    [string]$RootKeyDir = (Join-Path $env:USERPROFILE 'shaurma-root'),
    [switch]$Encrypt
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$cfg = Join-Path $repo 'pack-manager\config'
$stamp = Get-Date -Format 'yyyyMMdd-HHmm'
$name = "shaurma-keys-$stamp"

if (-not (Test-Path $Destination)) { throw "Немає теки призначення: $Destination" }
$stage = if ($Encrypt) { Join-Path ([IO.Path]::GetTempPath()) $name } else { Join-Path $Destination $name }
if (Test-Path $stage) { throw "Вже існує: $stage" }
New-Item -ItemType Directory -Force $stage | Out-Null

$copied = New-Object System.Collections.Generic.List[string]
$missing = New-Object System.Collections.Generic.List[string]
function Put([string]$sub, [string[]]$paths) {
    $dst = Join-Path $stage $sub
    New-Item -ItemType Directory -Force $dst | Out-Null
    foreach ($p in $paths) {
        $items = @(Get-Item -Path $p -Force -ErrorAction SilentlyContinue)
        if ($items.Count -eq 0) { $missing.Add($p); continue }
        foreach ($i in $items) {
            if ($i.PSIsContainer) { Copy-Item $i.FullName (Join-Path $dst $i.Name) -Recurse -Force }
            else { Copy-Item $i.FullName $dst -Force }
            $copied.Add("$sub\$($i.Name)")
        }
    }
}

Put '1-root-OFFLINE'         @("$RootKeyDir\*")
Put '2-trust-private'        @("$cfg\trust-private\*")
Put '3-trust-public'         @("$cfg\trust\*")
Put '4-manifest-signing-R1'  @("$cfg\installer-signing.key", "$cfg\installer-signing.pub", "$cfg\installer-signing.backup.json")
Put '5-vps'                  @("$cfg\vps-state.json", "$cfg\shaurma_launcher", "$cfg\shaurma_launcher.pub")
Put '6-build-and-settings'   @("$cfg\build-secrets*.json", "$cfg\pack-manager-settings.json", "$cfg\build-options.json")
Put '7-worker-local'         @("$repo\worker\.dev.vars", "$repo\access-worker\.dev.vars")

$readme = @"
РЕЗЕРВНА КОПІЯ КЛЮЧІВ SHAURMA LAUNCHER — $stamp
Тримати офлайн. Не завантажувати в хмару, месенджери чи GitHub.
Флешку краще зашифрувати (BitLocker To Go: ПКМ по диску → «Увімкнути BitLocker»).

1-root-OFFLINE\root.key
    Кореневий ключ (root). Підписує лише keyset.json і master.cert.
    Без нього не можна змінити ключі release/server і видати новий сертифікат.
    Після копіювання на флешку його варто ВИДАЛИТИ з ПК (C:\Users\User\shaurma-root).

2-trust-private\
    release.key   — підпис маніфестів v2, замовлень збірки, сигналу й звітів (Pack Manager).
    server.key, server.seed.txt — ключ нового worker (секрет SERVER_SIGNING_KEY).
    access-admin-key.txt — ADMIN_KEY нового worker (адмінка /v1/admin).
    internal-key.txt     — INTERNAL_KEY нового worker = PACKS_INTERNAL_KEY старого.
    Відновлення: скопіювати теку в pack-manager\config\trust-private;
    секрети worker — access-worker\set-secrets.ps1.

3-trust-public\
    root.pub, keyset.json(.sig) — вшиваються в збірки (змінні GitHub SHAURMA_*).
    master.cert — сертифікат повної збірки (вшивається в updater моста R1).
    Відновлення: pack-manager\config\trust.

4-manifest-signing-R1\installer-signing.*
    Ключ підпису launcher-version.json старого CDN (міст R1, 2.7.x).
    Потрібен, щоб старі лаунчери й надалі мігрували на нову систему.
    Відновлення: pack-manager\config.

5-vps\
    vps-state.json — секрети VPS (ADMIN_KEY, SESSION_SIGNING_KEY, JWT_SECRET, бази, R2 скінів...).
    shaurma_launcher(.pub) — SSH-ключ доступу до VPS.

6-build-and-settings\
    build-secrets*.json — токени каналів, CurseForge, Google OAuth тощо.
    pack-manager-settings.json — налаштування Pack Manager (R2-доступи).

7-worker-local\  — локальні змінні wrangler (якщо були).

Чого тут НЕМАЄ (живе лише в сервісах, відновлюється заново):
    секрети GitHub Actions, секрети Cloudflare Workers (ставляться з цих файлів),
    API-токени Cloudflare/R2 з дашборда, вхід gh / wrangler.

SHA256SUMS.txt — контрольні суми всіх файлів, щоб перевірити копію.
"@
[IO.File]::WriteAllText((Join-Path $stage 'README.txt'), $readme, (New-Object Text.UTF8Encoding($true)))

$sums = Get-ChildItem $stage -Recurse -File | Where-Object { $_.Name -ne 'SHA256SUMS.txt' } | Sort-Object FullName | ForEach-Object {
    '{0}  {1}' -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.FullName.Substring($stage.Length + 1)
}
[IO.File]::WriteAllLines((Join-Path $stage 'SHA256SUMS.txt'), [string[]]$sums, (New-Object Text.UTF8Encoding($false)))

Write-Host "Скопійовано файлів/тек: $($copied.Count)"
$copied | ForEach-Object { Write-Host "  + $_" }
if ($missing.Count) {
    Write-Warning 'Не знайдено (пропущено):'
    $missing | ForEach-Object { Write-Host "  - $_" }
}
if (-not ($copied | Where-Object { $_ -like '1-root-OFFLINE\*' })) { Write-Warning "root.key НЕ знайдено в $RootKeyDir — вкажіть -RootKeyDir" }
if (-not ($copied | Where-Object { $_ -like '2-trust-private\*' })) { Write-Warning 'trust-private НЕ знайдено' }

if ($Encrypt) {
    $7z = @("$env:ProgramFiles\7-Zip\7z.exe", "${env:ProgramFiles(x86)}\7-Zip\7z.exe") | Where-Object { Test-Path $_ } | Select-Object -First 1
    if (-not $7z) {
        Remove-Item $stage -Recurse -Force
        throw '7-Zip не знайдено: запустіть без -Encrypt і зашифруйте флешку BitLocker To Go'
    }
    $archive = Join-Path $Destination "$name.7z"
    try {
        # -p без значення: 7-Zip сам спитає пароль (не потрапляє в історію команд).
        & $7z a -t7z -mhe=on -p $archive "$stage\*" | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "7-Zip завершився з кодом $LASTEXITCODE" }
    }
    finally { Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue }
    Write-Host "Готово: $archive (AES-256, імена файлів теж зашифровано)" -ForegroundColor Green
}
else {
    Write-Host "Готово: $stage" -ForegroundColor Green
}
