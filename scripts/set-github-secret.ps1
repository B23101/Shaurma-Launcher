<#
.SYNOPSIS
  Sets ONE GitHub Actions secret with hidden input (plan "single launcher", §17).

.DESCRIPTION
  The value is typed by you in this terminal (Read-Host -AsSecureString), is never
  echoed, never written to a file in the repository and never passed as a command-line
  argument: it goes to `gh secret set` through stdin. Uses YOUR own `gh auth login`.

.EXAMPLE
  .\scripts\set-github-secret.ps1 -Name SIGNPATH_API_TOKEN -Environment release
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)] [ValidatePattern('^[A-Z][A-Z0-9_]{1,99}$')] [string]$Name,
    [string]$Repository = 'B23101/Shaurma-Launcher',
    [string]$Environment = ''
)
$ErrorActionPreference = 'Stop'
$gh = Get-Command gh -ErrorAction SilentlyContinue
if (-not $gh) { throw 'GitHub CLI (gh) not found: https://cli.github.com/' }
& gh auth status *> $null
if ($LASTEXITCODE -ne 0) { throw "gh is not logged in. Run 'gh auth login' with your own account." }

$secure = Read-Host -AsSecureString "Value for $Name (hidden)"
$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
try {
    $plain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
    if ([string]::IsNullOrWhiteSpace($plain)) { throw 'Empty value, nothing set.' }
    $ghArgs = @('secret', 'set', $Name, '--repo', $Repository)
    if ($Environment) { $ghArgs += @('--env', $Environment) }
    $psi = New-Object Diagnostics.ProcessStartInfo $gh.Source
    $psi.Arguments = ($ghArgs | ForEach-Object { '"' + $_ + '"' }) -join ' '
    $psi.RedirectStandardInput = $true
    $psi.UseShellExecute = $false
    $p = [Diagnostics.Process]::Start($psi)
    $p.StandardInput.Write($plain.Trim())
    $p.StandardInput.Close()
    $p.WaitForExit()
    if ($p.ExitCode -ne 0) { throw "gh secret set failed ($($p.ExitCode))" }
    Write-Host "$Name set$(if ($Environment) { " (environment $Environment)" })." -ForegroundColor Green
}
finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
    $plain = $null
}
