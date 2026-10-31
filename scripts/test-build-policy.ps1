$ErrorActionPreference = 'Stop'
$source = Join-Path $PSScriptRoot 'build-windows.ps1'
$ast = [Management.Automation.Language.Parser]::ParseFile($source, [ref]$null, [ref]$null)
foreach ($name in @('Get-Token', 'Check-Secrets', 'Build-Updater', 'Build-Backend')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
    if (-not $definition) { throw "Missing build function: $name" }
    Invoke-Expression $definition.Extent.Text
}
function Write-Step($message) {}
function Fail($message) { throw $message }
function Build-Dll($module, $tags, $ldflags, $out, $original, $description) {
    $script:captured = @{ Tags = $tags; Flags = $ldflags; Out = $out }
}
$Root = Split-Path -Parent $PSScriptRoot
$UnsignedDir = Join-Path $Root 'dist/unsigned'
$ProductName = 'Shaurma Launcher'
$TokenBase = [guid]::NewGuid().ToString('N')
$TokenShaurma = [guid]::NewGuid().ToString('N')
$PubKeys = [Convert]::ToBase64String((New-Object byte[] 32))
$RequireSecrets = $true
$GoogleId = [guid]::NewGuid().ToString('N')
$GoogleSecret = ''
$CfKey = ''
foreach ($ed in @('base', 'shaurma')) {
    $Editions = @($ed)
    Check-Secrets
    Build-Updater $ed
    $expected = if ($ed -eq 'base') { $TokenBase } else { $TokenShaurma }
    $forbidden = if ($ed -eq 'base') { $TokenShaurma } else { $TokenBase }
    if (-not $captured.Flags.Contains($expected) -or $captured.Flags.Contains($forbidden)) { throw 'Updater edition isolation failed' }
    if ($captured.Tags -ne 'production' -or -not $captured.Out.EndsWith("$ed\updater.dll")) { throw 'Updater output/tag isolation failed' }
    Build-Backend $ed
    if ($ed -eq 'base' -and ($captured.Tags -ne 'production' -or $captured.Flags.Contains($TokenShaurma))) { throw 'Base skin build leaked private edition support' }
}
foreach ($invalid in @('', 'not-base64', [Convert]::ToBase64String((New-Object byte[] 31)), ($PubKeys + ','))) {
    $PubKeys = $invalid
    $rejected = $false
    try { Check-Secrets } catch { $rejected = $true }
    if (-not $rejected) { throw 'Invalid release manifest key was accepted' }
}
Write-Output 'PASS: updater edition isolation and release public-key validation'
