$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
# Build the same files that git add -A would publish. Ignored local tools and
# generated Go packages must not participate in the release check.
$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$stage = Join-Path $tempRoot ("shaurma-release-" + [guid]::NewGuid())
function Check($module, $program, [string[]]$arguments) {
    Push-Location (Join-Path $stage $module)
    try {
        Write-Host "[$module] $program $arguments"
        & $program @arguments
        if ($LASTEXITCODE -ne 0) { throw "$module / $program $arguments failed ($LASTEXITCODE)" }
    } finally { Pop-Location }
}
New-Item -ItemType Directory -Path $stage | Out-Null
try {
    $files = git -C $root -c core.quotepath=false ls-files --cached --others --exclude-standard
    if ($LASTEXITCODE -ne 0) { throw 'Could not list release sources' }
    foreach ($file in $files) {
        $source = Join-Path $root $file
        if (!(Test-Path -LiteralPath $source -PathType Leaf)) { continue }
        $target = Join-Path $stage $file
        [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($target)) | Out-Null
        [IO.File]::Copy($source, $target, $true)
    }
    Check frontend npm.cmd @('ci','--no-audit','--no-fund')
    Check frontend npm.cmd @('run','check','--','--threshold','error')
    Check frontend npm.cmd @('run','build','--','--logLevel','error')
    foreach ($module in @('backend','shell','bootstrap','updater-backend')) {
        $shared = $module -in @('backend','updater-backend')
        $mode = if ($shared) { 'c-shared' } else { 'pie' }
        $artifact = Join-Path $stage $(if ($shared) { 'verify.dll' } else { 'verify.exe' })
        $env:CGO_ENABLED = '1'
        Check $module go @('test','./...','-timeout','10m')
        if (!$shared) { $env:CGO_ENABLED = '0' }
        Check $module go @('build','-tags','production',"-buildmode=$mode",'-o',$artifact,'.')
        if ($module -eq 'backend') {
            Check $module go @('test','-tags','shaurma','./...','-timeout','10m')
            Check $module go @('build','-tags','production,shaurma',"-buildmode=$mode",'-o',$artifact,'.')
        }
    }
} finally {
    $resolved = (Resolve-Path -LiteralPath $stage).Path
    if ($resolved -eq [IO.Path]::GetFullPath($stage) -and (Split-Path $resolved -Parent) -eq $tempRoot.TrimEnd('\')) {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
exit 0
