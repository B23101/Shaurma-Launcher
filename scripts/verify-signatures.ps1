[CmdletBinding()]
param(
    [Parameter(Mandatory)] [string]$Directory,
    [Parameter(Mandatory)] [ValidateSet('base', 'shaurma')] [string]$Edition,
    [Parameter(Mandatory)] [ValidateSet('components', 'installer')] [string]$Stage
)
$ErrorActionPreference = 'Stop'
$shell = if ($Edition -eq 'base') { 'ShaurmLauncherBase.exe' } else { 'ShaurmLauncher.exe' }
$setup = if ($Edition -eq 'base') { 'ShaurmLauncherBase-setup.exe' } else { 'ShaurmLauncher-setup.exe' }
$expected = if ($Stage -eq 'components') {
    @("$Edition/$shell", "$Edition/bootstrap/$shell", "$Edition/backend.dll", "$Edition/updater.dll")
} else { @($setup) }
foreach ($relative in $expected) {
    $path = Join-Path $Directory $relative
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing signed artifact: $relative" }
    $signature = Get-AuthenticodeSignature -LiteralPath $path
    if ($signature.Status -ne 'Valid') { throw "Invalid Authenticode signature: $relative ($($signature.Status))" }
    if ($signature.SignerCertificate.Subject -notmatch '(^|,\s*)CN=SignPath Foundation(,|$)') {
        throw "Unexpected publisher: $relative"
    }
    $metadata = (Get-Item -LiteralPath $path).VersionInfo
    if ($metadata.ProductName -ne 'Shaurma Launcher' -or $metadata.ProductVersion -ne '1.0.0') {
        throw "Unexpected product metadata: $relative"
    }
    Write-Output "Verified: $relative"
}
