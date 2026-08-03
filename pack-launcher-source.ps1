<#
.SYNOPSIS
  Збирає усю кодову базу монопрепозиторію Shaurma в один архів (zip).

.DESCRIPTION
  Архівує вихідний код усіх компонентів:
    launcher      — Wails-лаунчер (Go + Svelte фронтенд)
    updater       — Wails-апдейтер (Go + статичний HTML-фронтенд)
    pack-manager  — JavaFX Pack Manager (Maven)
    installer     — скрипт Inno Setup (installer.iss)
    worker        — Cloudflare Worker (worker.js, wrangler.toml)
  Плюс кореневі файли: build.ps1, BUILD.md і сам цей скрипт.

  Кеші, згенеровані артефакти та сторонні бібліотеки НЕ включаються
  (і навіть не скануються):
    launcher/frontend/node_modules, frontend/dist, frontend/wailsjs,
    frontend/package.json.md5, launcher/build/bin, updater/build/bin,
    updater/frontend/wailsjs, pack-manager/dist, pack-manager/target,
    *.exe, *.zip, *.tar, *.7z, .git.

  Структура архіву = структура репозиторію: кожен компонент у своїй
  теці (launcher/..., updater/..., pack-manager/...), тобто архів можна
  одразу розпакувати як корінь проєкту і перезібрати все (`wails build`,
  `mvn package`, `build.ps1`).

.PARAMETER OutFile
  Шлях до створюваного zip. За замовчуванням
  <корінь репозиторію>\shaurma-monorepo-source.zip.

.EXAMPLE
  .\pack-launcher-source.ps1

.EXAMPLE
  .\pack-launcher-source.ps1 -OutFile C:\tmp\src.zip
#>
[CmdletBinding()]
param(
  [string]$OutFile,
  [string]$RepoRoot
)

$ErrorActionPreference = 'Stop'

if (-not $RepoRoot) { $RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path }
if (-not $OutFile)  { $OutFile  = Join-Path $RepoRoot 'shaurma-monorepo-source.zip' }

if (-not (Test-Path -LiteralPath (Join-Path $RepoRoot 'launcher\main.go'))) {
  throw "Це не корінь репозиторію: $RepoRoot (немає launcher\main.go)"
}

# ── Опис компонентів ────────────────────────────────────────────────────
# rootFiles   — файли в корені компонента (маска/ім'я).
# recursive   — відносні шляхи, які скануються рекурсивно (маски з *).
# explicit    — конкретні файли, які мають бути обов'язково.
$components = @(
  @{
    Name     = 'launcher'
    RootFiles = @('*.go', 'go.mod', 'go.sum', 'wails.json', 'CHANGES.md')
    Recursive = @(
      'internal\*.go',            # Go-пакети бекенду
      'frontend\src\*',           # вихідний код Svelte фронтенду
      'build\windows\*.ico',
      'build\windows\*.manifest'
    )
    Explicit = @(
      'frontend\index.html',
      'frontend\package.json',
      'frontend\package-lock.json',
      'frontend\svelte.config.js',
      'frontend\tsconfig.json',
      'frontend\tsconfig.node.json',
      'frontend\vite.config.ts',
      'build\appicon.png'         # embed: build/appicon.png (main.go)
    )
  },
  @{
    Name     = 'updater'
    RootFiles = @('*.go', 'go.mod', 'wails.json', 'README.md', '.gitignore')
    Recursive = @(
      'internal\*.go',
      'build\windows\*.ico',
      'frontend\fonts\*'          # статичний фронтенд: шрифти
    )
    Explicit = @(
      'frontend\index.html',
      'frontend\icon.png'
    )
  },
  @{
    Name     = 'pack-manager'
    RootFiles = @('pom.xml', 'buid.bat', 'rebuild-pack-manager.bat', 'rebuild-pm.ps1')
    Recursive = @(
      'src\main\*'                # Java-код + ресурси (css, logback.xml)
    )
    Explicit = @()
  },
  @{
    Name     = 'installer'
    RootFiles = @('*.iss')
    Recursive = @()
    Explicit = @()
  },
  @{
    Name     = 'worker'
    RootFiles = @('worker.js', 'wrangler.toml', 'README.md')
    Recursive = @()
    Explicit = @()
  }
)

# Кореневі файли репозиторію (не в компонентах).
$repoRootFiles = @('build.ps1', 'BUILD.md', 'pack-launcher-source.ps1')

# ── Збірка списку файлів ────────────────────────────────────────────────
$included = [System.Collections.Generic.List[string]]::new()

function Add-Entries([string]$componentName, [string]$compRoot, $spec) {
  # Файли в корені компонента.
  foreach ($pat in $spec.RootFiles) {
    foreach ($f in Get-ChildItem -LiteralPath $compRoot -File -Filter $pat -ErrorAction SilentlyContinue) {
      $included.Add("$componentName\$($f.Name)")
    }
  }
  # Рекурсивні маски. PS 5.1 ламає Get-ChildItem -Recurse з маскою в кінці
  # шляху (-Path 'src\main\*' -Recurse повертає нічого), тому шлях ділимо
  # на теку + -Filter: так рекурсія працює передбачувано на будь-якій версії.
  foreach ($rel in $spec.Recursive) {
    if ($rel -match '^(.*)\\([^\\]+)$') {
      $dirPart  = $matches[1]
      $filter   = $matches[2]
    } else {
      $dirPart  = $rel
      $filter   = '*'
    }
    $fullDir = Join-Path $compRoot $dirPart
    foreach ($f in Get-ChildItem -LiteralPath $fullDir -Recurse -File -Force -Filter $filter -ErrorAction SilentlyContinue) {
      $relPath = $f.FullName.Substring($compRoot.Length).TrimStart('\', '/')
      $included.Add("$componentName\$relPath")
    }
  }
  # Явні файли.
  foreach ($rel in $spec.Explicit) {
    $f = Join-Path $compRoot $rel
    if (Test-Path -LiteralPath $f) {
      $included.Add("$componentName\$rel")
    }
  }
}

foreach ($comp in $components) {
  $compRoot = Join-Path $RepoRoot $comp.Name
  if (-not (Test-Path -LiteralPath $compRoot)) {
    Write-Warning "Компонент не знайдено, пропускаю: $compRoot"
    continue
  }
  Add-Entries $comp.Name $compRoot $comp
}

# Кореневі файли.
foreach ($rel in $repoRootFiles) {
  $f = Join-Path $RepoRoot $rel
  if (Test-Path -LiteralPath $f) { $included.Add($rel) }
}

# Дедуплікація + сортування.
$included = $included | Sort-Object -Unique

if ($included.Count -eq 0) {
  throw 'Жодного файлу не включено в архів — перевірте правила включення.'
}

# ── Створення zip ───────────────────────────────────────────────────────
if (Test-Path -LiteralPath $OutFile) { Remove-Item -LiteralPath $OutFile -Force }

Add-Type -AssemblyName System.IO.Compression.FileSystem
$zip = [System.IO.Compression.ZipFile]::Open($OutFile, 'Create')
try {
  foreach ($rel in $included) {
    $src = Join-Path $RepoRoot $rel
    # .NET ZipFile використовує '/' усередині архіву — замінюємо зворотні слеші.
    $entryName = $rel.Replace('\', '/')
    [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $src, $entryName, 'Optimal') | Out-Null
  }
} finally {
  $zip.Dispose()
}

# ── Підсумок ────────────────────────────────────────────────────────────
$size = (Get-Item -LiteralPath $OutFile).Length
Write-Host ''
Write-Host "Архів створено: $OutFile" -ForegroundColor Green
Write-Host ('Файлів включено: {0}   Розмір архіву: {1:N0} КБ' -f $included.Count, ($size / 1KB))

# Підрахунок по компонентах.
Write-Host ''
Write-Host 'Розбивка по компонентах:' -ForegroundColor Cyan
foreach ($comp in $components) {
  $count = ($included | Where-Object { $_ -like "$($comp.Name)\*" }).Count
  if ($count -gt 0) { Write-Host ("  {0,-15} {1,5} файлів" -f $comp.Name, $count) }
}
$rootCount = ($included | Where-Object { $_ -notmatch '\\' }).Count
if ($rootCount -gt 0) { Write-Host ("  {0,-15} {1,5} файлів" -f 'root', $rootCount) }
