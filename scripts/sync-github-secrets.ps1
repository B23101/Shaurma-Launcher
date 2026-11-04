<#
.SYNOPSIS
  Читає build-secrets.json Pack Manager і виставляє відповідні GitHub Actions secrets.
  Значення не друкуються й не пишуться в лог; порожні поля пропускаються.

.EXAMPLE
  .\scripts\sync-github-secrets.ps1 -DryRun     # лише показати, що буде виставлено
  .\scripts\sync-github-secrets.ps1             # виставити секрети
  .\scripts\sync-github-secrets.ps1 -SecretsFile D:\PM\build-secrets.json -Repository OWNER/REPO

.NOTES
  Потрібен GitHub CLI (gh). Якщо gh не залогінений, береться ghToken із build-secrets.json
  (потрібен доступ до секретів репозиторію: classic scope 'repo' або fine-grained 'Secrets: Read and write').
#>
[CmdletBinding()]
param(
    [string]$SecretsFile,
    [ValidatePattern('^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$')]
    [string]$Repository,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

# Поле у build-secrets.json -> ім'я GitHub secret (ті самі імена читає .github/workflows/build-windows.yml)
$map = [ordered]@{
    tokenShaurma       = 'TOKEN_SHAURMA'
    tokenBase          = 'TOKEN_BASE'
    cfApiKey           = 'CF_API_KEY'
    groqApiKey         = 'GROQ_API_KEY'
    geminiApiKey       = 'GEMINI_API_KEY'
    googleClientId     = 'GOOGLE_CLIENT_ID'
    googleClientSecret = 'GOOGLE_CLIENT_SECRET'
    manifestPubKeys    = 'MANIFEST_PUB_KEYS'
    signCmd            = 'SIGN_CMD'
}

# 1) Де лежить build-secrets.json: явний шлях, або поряд з Pack Manager (його робоча тека).
$root = Split-Path -Parent $PSScriptRoot
if (-not $SecretsFile) {
    $cands = @(
        (Join-Path $root 'pack-manager\config\build-secrets.json'),
        (Join-Path $root 'pack-manager\build-secrets.json'),
        (Join-Path $root 'build-secrets.json'),
        (Join-Path (Split-Path -Parent $root) 'build-secrets.json')
    )
    # DataDirectory з pack-manager-settings.json (якщо користувач переніс робочу теку)
    foreach ($sf in @((Join-Path $root 'pack-manager\config\pack-manager-settings.json'), (Join-Path $root 'pack-manager\pack-manager-settings.json'), (Join-Path $root 'pack-manager-settings.json'))) {
        if (Test-Path -LiteralPath $sf) {
            try {
                $dd = (Get-Content -LiteralPath $sf -Raw -Encoding UTF8 | ConvertFrom-Json).dataDirectory
                if ($dd) { $cands = @((Join-Path $dd 'build-secrets.json')) + $cands }
            } catch { }
        }
    }
    $SecretsFile = $cands | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
}
if (-not $SecretsFile -or -not (Test-Path -LiteralPath $SecretsFile)) {
    throw "build-secrets.json не знайдено. Вкажи шлях: -SecretsFile <шлях> (файл лежить у робочій теці Pack Manager)."
}
Write-Host "Читаю: $SecretsFile"
$cfg = Get-Content -LiteralPath $SecretsFile -Raw -Encoding UTF8 | ConvertFrom-Json

# 2) Репозиторій: параметр, інакше ghSourceRepo / ghPagesRepo з того ж файлу.
if (-not $Repository) {
    $url = if ($cfg.ghSourceRepo) { $cfg.ghSourceRepo } else { $cfg.ghPagesRepo }
    if ($url -match 'github\.com[/:]([^/\s]+)/([^/\s]+?)(?:\.git)?/?$') { $Repository = "$($Matches[1])/$($Matches[2])" }
}
if (-not $Repository) { throw "Не вдалось визначити репозиторій. Вкажи -Repository OWNER/REPO." }
Write-Host "Репозиторій: $Repository"

# 3) gh: лише ваш власний вхід (gh auth login). Токен із файлу не використовується:
#    усе, що може читати файли, бачило б його (план «Єдиний лаунчер», §17).
$gh = Get-Command gh -ErrorAction SilentlyContinue
if (-not $gh) { throw "GitHub CLI (gh) не знайдено. Встанови: https://cli.github.com/" }
$prevToken = $env:GH_TOKEN
if (-not $DryRun) {
    & gh auth status *> $null
    if ($LASTEXITCODE -ne 0) { throw "gh не залогінений. Виконай 'gh auth login' своїм акаунтом." }
}

$set = 0; $skipped = @(); $failed = @()
try {
    foreach ($field in $map.Keys) {
        $name = $map[$field]
        $value = [string]$cfg.$field
        if ([string]::IsNullOrWhiteSpace($value)) { $skipped += $name; continue }
        $value = $value.Trim()
        if ($DryRun) { Write-Host ("  [dry-run] {0}  ({1} символів)" -f $name, $value.Length); $set++; continue }

        # Значення — через stdin з тимчасового файлу БЕЗ символу нового рядка і BOM:
        # не світиться в аргументах/логу, і не псується перенос рядка.
        $tmp = [IO.Path]::GetTempFileName()
        try {
            [IO.File]::WriteAllText($tmp, $value, (New-Object Text.UTF8Encoding($false)))
            $p = Start-Process -FilePath $gh.Source -ArgumentList @('secret', 'set', $name, '--repo', $Repository) `
                -RedirectStandardInput $tmp -NoNewWindow -Wait -PassThru
            if ($p.ExitCode -eq 0) { Write-Host "  OK   $name"; $set++ } else { Write-Host "  FAIL $name (gh exit $($p.ExitCode))"; $failed += $name }
        } finally {
            if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Force }
        }
    }
} finally {
    $env:GH_TOKEN = $prevToken
}

Write-Host ""
Write-Host ("Виставлено: {0}" -f $set)
if ($skipped) { Write-Host ("Пропущено (порожні в build-secrets.json): {0}" -f ($skipped -join ', ')) }
if (@('TOKEN_SHAURMA', 'TOKEN_BASE') | Where-Object { $skipped -contains $_ }) {
    Write-Warning "TOKEN_SHAURMA і TOKEN_BASE обов'язкові — збірка на GitHub без них зупиниться."
}
if ($failed) { throw ("Не вдалось виставити: {0}" -f ($failed -join ', ')) }
if (-not $DryRun) { Write-Host "Перевірка: gh secret list --repo $Repository" }
