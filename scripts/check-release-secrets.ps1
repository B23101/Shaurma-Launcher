[CmdletBinding()]
param(
    [Parameter(Mandatory)] [string]$Directory,
    [Parameter(Mandatory)] [ValidateSet('base', 'shaurma')] [string]$Edition,
    [switch]$AllowBackendAIKeys
)
$ErrorActionPreference = 'Stop'
# Only names and relative paths are printed; never print matches or credential values.
$forbiddenNames = @('GROQ_API_KEY', 'GEMINI_API_KEY')
if ($Edition -eq 'base') { $forbiddenNames += @('SHRM_BUILD_TOKEN_SHAURMA') }
$forbidden = @($forbiddenNames | ForEach-Object {
    $value = [Environment]::GetEnvironmentVariable($_)
    if ($value -and $value.Length -ge 8) { $value }
})
$files = @(Get-ChildItem -LiteralPath $Directory -Recurse -File | Where-Object { $_.Extension -in @('.exe', '.dll', '.so', '.js', '.html', '.json') -or $_.Name -in @('ShaurmLauncher', 'ShaurmLauncherBase') })
if ($files.Count -eq 0) { throw 'No release components found to scan' }
foreach ($file in $files) {
    $text = [Text.Encoding]::GetEncoding(28591).GetString([IO.File]::ReadAllBytes($file.FullName))
    # Only explicitly supplied provider values are allowed, only in the full backend.
    if ($AllowBackendAIKeys -and $Edition -eq 'shaurma' -and $file.Name -in @('backend.dll', 'backend.so')) {
        foreach ($name in @('GEMINI_API_KEY', 'GROQ_API_KEY')) {
            $allowed = [Environment]::GetEnvironmentVariable($name)
            if ($allowed -and $allowed.Length -ge 8) { $text = $text.Replace($allowed, '') }
        }
    }
    foreach ($value in $forbidden) {
        if ($text.Contains($value)) { throw "Forbidden credential found in artifact: $($file.Name)" }
    }
    if ($text -match 'gsk_[A-Za-z0-9]{20,}|AIza[A-Za-z0-9_-]{30,}') {
        throw "Provider credential pattern found in artifact: $($file.Name)"
    }
}
Write-Output "PASS: scanned $($files.Count) release components (credential values redacted)"
