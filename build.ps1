# build.ps1 — збирає launcher.exe (ОБИХ версій) + shrm-updater.exe, готує
# staging dist\app-<edition>\ і компілює ОБИДВА setup.exe через Inno Setup.
#
# Двоверсійність:
#   shaurma — повна версія (build tag "shaurma"), зі Шаурма-збірками.
#   base    — без коду Шаурма-збірок (build tag "!shaurma"/порожньо).
# Вони збираються з того самого джерела, але різними `go build -tags`,
# і пакуються в РІЗНІ setup.exe (installer/installer.iss дивиться на
# /DMyEdition=...). shrm-updater.exe — ОДИН спільний файл для обох версій
# (в ньому вшиті обидва токени), просто копіюється в обидві staging теки.
#
# Структура репозиторію (monorepo):
#   launcher/    — сам лаунчер (Wails), модуль shaurma-launcher-wails
#   updater/     — оновлювач (Wails), модуль shrm-updater
#   installer/   — installer.iss (пакує обидва exe в один setup.exe)
#   build.ps1    — цей файл, в корені
#
# Використання (з кореня репозиторію):
#   .\build.ps1 -Version 1.0.0                    # обидві версії
#   .\build.ps1 -Version 1.0.0 -Edition base      # лише base
#
# Вимоги на машині:
#   - Go 1.25+
#   - Wails CLI v3 (go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.121)
#   - Inno Setup 6 (ISCC.exe в PATH, або -IsccPath "C:\...\ISCC.exe")
#   - Node.js + npm (для launcher/frontend — Svelte+Vite; updater/frontend
#     чистий статичний HTML, npm не потрібен)
#
# Як збирається Windows-exe (замість `wails build` з v2):
#   1. Рендеримо build\windows\wails.exe.manifest + info.json (підставляємо
#      версію/назви замість {{.Info.*}} — v3 `generate syso` читає файли як є,
#      шаблони підставляв ще wails v2 CLI).
#   2. `wails3 generate syso -arch amd64 ...` -> wails_windows_amd64.syso
#      (ВАЖЛИВО: без явного -arch CLI тихо пише ПОРОЖНІЙ .syso).
#   3. `go build -tags production[,shaurma] -trimpath -buildvcs=false
#      -ldflags="-w -s -H windowsgui ..."` — тег production обов'язковий,
#      інакше wails3 підхоплює dev-варіанти (application_dev.go, порожній
#      runtime_dev.go). `-H windowsgui` = GUI-підсистема (без консольного вікна).
#   4. Прибираємо згенерований .syso (як робить Taskfile wails3).

param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    # Яку версію збирати: shaurma | base | all (за замовчуванням).
    [ValidateSet("shaurma", "base", "all")]
    [string]$Edition = "all",

    [string]$IsccPath = "ISCC.exe",

    # Токени CDN, вшиваються прямо в shrm-updater.exe на етапі збірки
    # (internal/core/core.go: tokenShaurma/tokenBase). Якщо не передані
    # прапорцями — беруться з env SHRM_BUILD_TOKEN_SHAURMA /
    # SHRM_BUILD_TOKEN_BASE. Це ті самі значення, що прописані на
    # worker'і командами `wrangler secret put TOKEN_SHAURMA` /
    # `wrangler secret put TOKEN_BASE` (worker/README.md).
    [string]$TokenShaurma = $env:SHRM_BUILD_TOKEN_SHAURMA,
    [string]$TokenBase = $env:SHRM_BUILD_TOKEN_BASE,

    # Ключ CurseForge Core API, вшивається в launcher.exe на етапі збірки
    # (-X shaurma-launcher-wails/internal/custompack.curseForgeAPIKey=...).
    # Якщо не передано прапорцем — береться з env CF_API_KEY. Без ключа
    # підтримка CurseForge просто вимкнена (HasAPIKey() == false):
    # пошук/оновлення/залежності працюють лише через Modrinth.
    [string]$CFApiKey = $env:CF_API_KEY,

    # Пропустити компіляцію launcher/updater і одразу зібрати тільки
    # setup.exe з того, що вже лежить у dist\app-<edition> (корисно, якщо
    # ти щойно зібрав вручну і не хочеш чекати повний цикл ще раз).
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$launcherDir = Join-Path $root "launcher"
$updaterDir = Join-Path $root "updater"
$installerDir = Join-Path $root "installer"
$distDir = Join-Path $root "dist"
$appShaurmaDir = Join-Path $distDir "app-shaurma"
$appBaseDir = Join-Path $distDir "app-base"

# Які версії замовлено — у порядку побудови (відповідає /DMyEdition в installer.iss).
$editions = @()
if ($Edition -eq "all") {
    # Кожна редакція збирається у СВІЙ файл
    # (build\bin\launcher-shaurma.exe / launcher-base.exe), тож порядок
    # не має значення — збірки не перезаписують одна одну.
    $editions = @("shaurma", "base")
} else {
    $editions = @($Edition)
}

# staging-тека для конкретної версії.
function Get-AppDir($ed) {
    if ($ed -eq "base") { return $appBaseDir }
    return $appShaurmaDir
}

# Кроків: cleanup + staging + setup.exe — по editions.Count; компіляції
# (launcher tidy+npm, launcher go build xN, updater tidy, updater build) —
# лише коли не SkipBuild.
$script:stepNum = 0
$buildSteps = 0
if ($SkipBuild) { $buildSteps = 0 } else { $buildSteps = 3 + $editions.Count }
$totalSteps = 1 + ($editions.Count * 2) + $buildSteps

function Step($text) {
    $script:stepNum++
    Write-Host "== $script:stepNum/$totalSteps`: $text ==" -ForegroundColor Cyan
}

# ── Чистка застарілих setup*.exe у dist\ ────────────────────────────────
# Кожна збірка лишає dist\ShaurmLauncher-setup-<ver>.exe / 
# dist\ShaurmLauncherBase-setup-<ver>.exe; без очищення тека заростає
# старими версіями. Тому на початку видаляємо всі попередні setup-файли
# тих редакцій, що збираються, щоб лишився лише свіжий результат.
Step "cleanup: видалення застарілих setup.exe у dist\"
foreach ($ed in $editions) {
    $pattern = if ($ed -eq "base") { "ShaurmLauncherBase-setup-*.exe" } else { "ShaurmLauncher-setup-*.exe" }
    Get-ChildItem -Path $distDir -Filter $pattern -File -ErrorAction SilentlyContinue |
        ForEach-Object {
            Write-Host "  видалено: $($_.Name)" -ForegroundColor DarkGray
            Remove-Item $_.FullName -Force
        }
}

# ── Рендер .syso-ресурсів (manifest + info.json) + go build ──────────
# Замінює {{.Info.*}} / {{.Name}} у build\windows\wails.exe.manifest та
# info.json, генерує wails_windows_amd64.syso через `wails3 generate syso`
# і збирає exe напряму через `go build`. Після збірки .syso видаляється
# (щоб не лишався в робочій теці — як у Taskfile wails3).
function New-WailsExe {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ModuleDir,     # launcher\ або updater\
        [Parameter(Mandatory = $true)]
        [string]$Version,
        [string]$ProductName,
        [string]$CompanyName,
        [string]$Tags,          # "production,shaurma" або "production"
        [string]$ExtraLdflags,  # додаткові -X прапорці (токени)
        [Parameter(Mandatory = $true)]
        [string]$OutPath        # build\bin\launcher-<ed>.exe тощо
    )

    $winDir = Join-Path $ModuleDir "build\windows"
    $renderDir = Join-Path $ModuleDir "build\windows\_render"
    New-Item -ItemType Directory -Path $renderDir -Force | Out-Null

    # Ім'я assembly у manifest (com.wails.<Name>) — з назви теки модуля.
    $assemblyName = (Split-Path $ModuleDir -Leaf)

    # 1. Рендер manifest
    $manifestPath = Join-Path $winDir "wails.exe.manifest"
    $manifest = Get-Content $manifestPath -Raw
    $manifest = $manifest.Replace("{{.Info.ProductVersion}}", $Version)
    $manifest = $manifest.Replace("{{.Name}}", $assemblyName)
    $manifestOut = Join-Path $renderDir "wails.exe.manifest"
    Set-Content -Path $manifestOut -Value $manifest -NoNewline -Encoding ASCII

    # 2. Рендер info.json
    $infoPath = Join-Path $winDir "info.json"
    $info = Get-Content $infoPath -Raw
    $info = $info.Replace("{{.Info.ProductVersion}}", $Version)
    $info = $info.Replace("{{.Info.ProductName}}", $ProductName)
    $info = $info.Replace("{{.Info.CompanyName}}", $CompanyName)
    $infoOut = Join-Path $renderDir "info.json"
    Set-Content -Path $infoOut -Value $info -NoNewline -Encoding ASCII

    # 3. generate syso (ОБОВ'ЯЗКОВО -arch, інакше порожній файл)
    $wails3 = Join-Path $env:USERPROFILE "go\bin\wails3.exe"
    if (-not (Test-Path $wails3)) {
        throw "Не знайдено wails3 CLI: $wails3`nВстанови: go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.121"
    }
    $sysoOut = Join-Path $ModuleDir "wails_windows_amd64.syso"
    & $wails3 generate syso -arch amd64 -manifest $manifestOut -info $infoOut -icon (Join-Path $winDir "icon.ico") -out $sysoOut
    if ($LASTEXITCODE -ne 0) {
        throw "wails3 generate syso failed ($ModuleDir)"
    }

    # 4. go build
    Push-Location $ModuleDir
    try {
        $ldflags = "-w -s -H windowsgui"
        if ($ExtraLdflags) { $ldflags += " $ExtraLdflags" }
        # CGO_ENABLED=0 — дефолт нативного Windows-білду wails3 (див.
        # Taskfile build:native). Без нього go build з .syso може спробувати
        # cgo-шлях і видати зайві повідомлення.
        $oldCgo = $env:CGO_ENABLED
        $env:CGO_ENABLED = "0"
        try {
            go build -tags $Tags -trimpath -buildvcs=false -ldflags $ldflags -o $OutPath .
        } finally {
            if ($null -eq $oldCgo) { Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue }
            else { $env:CGO_ENABLED = $oldCgo }
        }
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed ($ModuleDir)"
        }
    } finally {
        Pop-Location
    }

    # 5. Прибрати .syso
    Remove-Item $sysoOut -Force -ErrorAction SilentlyContinue
    Remove-Item $renderDir -Recurse -Force -ErrorAction SilentlyContinue
}

if (-not $SkipBuild) {
    if ([string]::IsNullOrWhiteSpace($TokenShaurma) -or [string]::IsNullOrWhiteSpace($TokenBase)) {
        throw "Токени не задані. Передай -TokenShaurma/-TokenBase, або встанови env SHRM_BUILD_TOKEN_SHAURMA / SHRM_BUILD_TOKEN_BASE.`nЦе ті самі значення, що ти прописав на worker'і через 'wrangler secret put TOKEN_SHAURMA'/'TOKEN_BASE'."
    }

    Step "launcher: go mod tidy + npm install + npm run build (frontend)"
    Push-Location $launcherDir
    try {
        go mod tidy
        if ($LASTEXITCODE -ne 0) { throw "launcher: go mod tidy failed" }

        if (Test-Path "frontend\package.json") {
            Push-Location frontend
            try {
                npm install
                if ($LASTEXITCODE -ne 0) { throw "launcher/frontend: npm install failed" }

                # Фронтенд-білд ОБОВ'ЯЗКОВИЙ: launcher/main.go має
                # `//go:embed all:frontend/dist` — без свіжого dist у exe
                # потрапить стара (або порожня) версія UI.
                npm run build
                if ($LASTEXITCODE -ne 0) { throw "launcher/frontend: npm run build failed" }
            } finally { Pop-Location }
        }
    } finally { Pop-Location }

    # Прибираємо застарілий бінарник зі старою назвою (якщо лишився від
    # попередніх збірок) — щоб у build\bin були тільки launcher-shaurma.exe
    # та launcher-base.exe і не було плутанини, який запускати.
    $oldBin = Join-Path $launcherDir "build\bin\shaurma-launcher-wails.exe"
    if (Test-Path $oldBin) {
        Remove-Item $oldBin -Force -ErrorAction SilentlyContinue
        Write-Host "  прибрано застарілий $oldBin" -ForegroundColor DarkGray
    }

    foreach ($ed in $editions) {
        Step "launcher ($ed): go build"
        # Кожна редакція збирається у СВІЙ вихідний файл через -o
        # (build\bin\launcher-shaurma.exe / launcher-base.exe), тож
        # збірки ніколи не затирають одна одну.
        $outName = "launcher-$ed.exe"

        # Ключ CurseForge вшивається в ОБИДВІ редакції — internal/custompack
        # (і, відповідно, підтримка CurseForge у Мод-менеджері) спільний код
        # для shaurma і base. Без ключа CurseForgeClient.HasAPIKey() == false,
        # і пошук/оновлення модів іде лише через Modrinth.
        $cfLdflags = ""
        if ($CFApiKey) {
            $cfLdflags = "-X shaurma-launcher-wails/internal/custompack.curseForgeAPIKey=$CFApiKey"
        } else {
            Write-Host "  [!] CFApiKey не передано — збірка $ed БЕЗ підтримки CurseForge (лише Modrinth)" -ForegroundColor Yellow
        }

        if ($ed -eq "shaurma") {
            # Вшиваємо токен CDN у бінарник — воркер без X-Shaurma-Token
            # віддає 401, тож без цього список Шаурма-збірок не завантажиться.
            $shaurmaLdflags = "-X shaurma-launcher-wails/internal/api.tokenShaurma=$TokenShaurma"
            if ($cfLdflags) { $shaurmaLdflags += " $cfLdflags" }
            New-WailsExe -ModuleDir $launcherDir -Version $Version `
                -ProductName "Shaurma Launcher" -CompanyName "B23101" `
                -Tags "production,shaurma" -ExtraLdflags $shaurmaLdflags `
                -OutPath (Join-Path $launcherDir "build\bin\$outName")
        } else {
            # base = без build tag "shaurma" => збирається !shaurma-варіант
            # (app_shaurma_stub.go), фізично без коду Шаурма-збірок.
            #
            # ВАЖЛИВО: НЕ додаємо тег "shaurma" (у wails.json він був
            # дефолтом для v2-CLI; тут теги передаються явно, тож base-збірка
            # гарантовано без коду Шаурма-збірок).
            New-WailsExe -ModuleDir $launcherDir -Version $Version `
                -ProductName "Shaurma Launcher" -CompanyName "B23101" `
                -Tags "production" -ExtraLdflags $cfLdflags `
                -OutPath (Join-Path $launcherDir "build\bin\$outName")
        }

        $launcherExe = Join-Path $launcherDir "build\bin\$outName"
        if (-not (Test-Path $launcherExe)) {
            throw "Не знайдено launcher.exe після збірки ($ed): $launcherExe`nПеревір -o у go build — має бути build\bin\launcher-$ed.exe."
        }

        # Автоперевірка build-tag: рядок "shaurma-proxy" є ТІЛЬКИ у shaurma-варіанті
        # коду (internal/api/shaurma_packs_full.go). Якщо тег мовчки не застосувався
        # б — обидві збірки були б base (дві однакові версії); падаємо тут зі
        # зрозумілою помилкою замість тихого дубля.
        $exeBytes = [System.IO.File]::ReadAllBytes($launcherExe)
        $exeText = [System.Text.Encoding]::GetEncoding(28591).GetString($exeBytes)
        $hasShaurma = $exeText.Contains("shaurma-proxy")
        $wantShaurma = ($ed -eq "shaurma")
        if ($hasShaurma -ne $wantShaurma) {
            throw "launcher ($ed): build tag не застосувався! Маркер shaurma-proxy знайдено=$hasShaurma, очікувалось=$wantShaurma. Перевір go build -tags."
        }
        Write-Host "  маркер shaurma-proxy: $hasShaurma (очікувано для ${ed}: $wantShaurma) — OK" -ForegroundColor DarkGray

        Copy-Item $launcherExe (Join-Path (Get-AppDir $ed) "launcher.exe") -Force
    }

    Step "updater: go mod tidy"
    Push-Location $updaterDir
    try {
        go mod tidy
        if ($LASTEXITCODE -ne 0) { throw "updater: go mod tidy failed" }
    } finally { Pop-Location }

    Step "updater: go build (з вшитими токенами)"
    $updaterLdflags = "-X shrm-updater/internal/core.tokenShaurma=$TokenShaurma -X shrm-updater/internal/core.tokenBase=$TokenBase"
    $updaterOut = Join-Path $updaterDir "build\bin\shrm-updater.exe"
    New-WailsExe -ModuleDir $updaterDir -Version $Version `
        -ProductName "Shaurma Updater" -CompanyName "SHAURMA TEAM" `
        -Tags "production" -ExtraLdflags $updaterLdflags `
        -OutPath $updaterOut

    if (-not (Test-Path $updaterOut)) {
        throw "Не знайдено shrm-updater.exe після збірки: $updaterOut"
    }
    foreach ($ed in $editions) {
        Copy-Item $updaterOut (Join-Path (Get-AppDir $ed) "shrm-updater.exe") -Force
    }
} else {
    Write-Host "-SkipBuild: пропускаю компіляцію, використовую те, що вже в dist\app-<edition>" -ForegroundColor Yellow
    foreach ($ed in $editions) {
        $appDir = Get-AppDir $ed
        if (-not (Test-Path (Join-Path $appDir "launcher.exe")) -or -not (Test-Path (Join-Path $appDir "shrm-updater.exe"))) {
            throw "SkipBuild вимагає вже наявних launcher.exe і shrm-updater.exe у $appDir"
        }
    }
}

foreach ($ed in $editions) {
    $appDir = Get-AppDir $ed
    Step "staging ($ed): dist\app-<edition> + launcher-version.json"
    New-Item -ItemType Directory -Path (Join-Path $appDir "build\windows") -Force | Out-Null

    # Іконка інсталятора = іконка ЛАУНЧЕРА (launcher/build/windows/icon.ico),
    # а не updater-а — по ТЗ "іконка становлювача це іконка лаунчера".
    Copy-Item (Join-Path $launcherDir "build\windows\icon.ico") (Join-Path $appDir "build\windows\icon.ico") -Force -ErrorAction SilentlyContinue

    $launcherBytes = [System.IO.File]::ReadAllBytes((Join-Path $appDir "launcher.exe"))
    $sha256 = [System.BitConverter]::ToString([System.Security.Cryptography.SHA256]::Create().ComputeHash($launcherBytes)).Replace("-", "").ToLower()
    $size = $launcherBytes.Length

    $manifest = @{
        version      = $Version
        releaseNotes = ""
        components   = @(
            @{ path = "launcher.exe"; sha256 = $sha256; size = $size; url = "/launcher/launcher.exe" }
        )
    } | ConvertTo-Json -Depth 5
    Set-Content -Path (Join-Path $appDir "launcher-version.json") -Value $manifest -Encoding UTF8

    Write-Host "  launcher.exe ($ed): sha256=$sha256 size=$size"
}

Write-Host ""
Write-Host "  !! Ті самі launcher.exe + launcher-version.json (з тими самими SHA256)" -ForegroundColor Yellow
Write-Host "  !! треба залити на CDN worker: shaurma-версію у launchers/shaurma/," -ForegroundColor Yellow
Write-Host "  !! base-версію у launchers/base/ — інакше shrm-updater вважатиме" -ForegroundColor Yellow
Write-Host "  !! щойно встановлену версію 'застарілою'." -ForegroundColor Yellow
Write-Host ""

$iscc = Get-Command $IsccPath -ErrorAction SilentlyContinue
if (-not $iscc) {
    # Автопошук ISCC.exe у типових теках встановлення Inno Setup 6 —
    # програма не додає себе в PATH, тож явний шлях надійніший.
    $isccDirs = @(
        "${env:ProgramFiles(x86)}\Inno Setup 6",
        "$env:ProgramFiles\Inno Setup 6",
        "$env:LOCALAPPDATA\Programs\Inno Setup 6"
    )
    foreach ($dir in $isccDirs) {
        $cand = Join-Path $dir "ISCC.exe"
        if (Test-Path $cand) {
            Write-Host "Знайдено ISCC.exe: $cand" -ForegroundColor Cyan
            $iscc = Get-Command $cand -ErrorAction SilentlyContinue
            break
        }
    }
}
if (-not $iscc) {
    Write-Host "ISCC.exe не знайдено в PATH і в типових теках встановлення." -ForegroundColor Yellow
    Write-Host "Staging готовий у: $appShaurmaDir (і $appBaseDir для base)" -ForegroundColor Yellow
    Write-Host "Встанови Inno Setup 6, або передай -IsccPath \"C:\...\ISCC.exe\"" -ForegroundColor Yellow
    Write-Host "або прожени вручну:" -ForegroundColor Yellow
    $viVersion = "0.0.0"
    if ($Version -match "^\d+\.\d+\.\d+") { $viVersion = $Matches[0] }
    foreach ($ed in $editions) {
        $appDir = Get-AppDir $ed
        Write-Host "  ISCC.exe installer\installer.iss /DMyAppVersion=$Version /DMyVersionInfoVersion=$viVersion /DAppImageDir=$appDir /DMyEdition=$ed" -ForegroundColor Yellow
    }
    exit 0
}

foreach ($ed in $editions) {
    $appDir = Get-AppDir $ed
    Step "setup.exe ($ed) (Inno Setup)"
    # VersionInfoVersion не приймає нечислові суфікси (див. installer.iss,
    # MyVersionInfoVersion) — вирізаємо з -Version перші три числові частини.
    $viVersion = "0.0.0"
    if ($Version -match "^\d+\.\d+\.\d+") {
        $viVersion = $Matches[0]
    }
    & $iscc.Source (Join-Path $installerDir "installer.iss") "/DMyAppVersion=$Version" "/DMyVersionInfoVersion=$viVersion" "/DAppImageDir=$appDir" "/DMyEdition=$ed"
    if ($LASTEXITCODE -ne 0) { throw "ISCC compilation failed ($ed)" }
}

Write-Host ""
Write-Host "Готово:" -ForegroundColor Green
foreach ($ed in $editions) {
    if ($ed -eq "base") {
        Write-Host "  dist\ShaurmLauncherBase-setup-$Version.exe" -ForegroundColor Green
    } else {
        Write-Host "  dist\ShaurmLauncher-setup-$Version.exe" -ForegroundColor Green
    }
}
