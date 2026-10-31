[CmdletBinding(SupportsShouldProcess)]
param(
    [Parameter(Mandatory)]
    [ValidatePattern('^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$')]
    [string]$Repository
)

$ErrorActionPreference = 'Stop'
$sourceRepository = 'B23101/Shaurma-Launcher'
if ($Repository -eq $sourceRepository) { throw 'The build destination must be a separate private repository' }

# Read metadata only; credentials remain in GitHub CLI's credential store.
$metadata = & gh api "repos/$Repository" --jq '{private: .private, default_branch: .default_branch}'
if ($LASTEXITCODE -ne 0) { throw 'Cannot verify the destination repository' }
$destination = $metadata | ConvertFrom-Json
if ($destination.private -ne $true) { throw 'The build destination is not private' }
if ($destination.default_branch -ne 'main') { throw 'The build destination must use main' }

if ($PSCmdlet.ShouldProcess($Repository, 'Fast-forward main from the published public source; do not copy tags')) {
    Push-Location (Split-Path -Parent $PSScriptRoot)
    try {
        & git fetch --no-tags "https://github.com/$sourceRepository.git" main
        if ($LASTEXITCODE -ne 0) { throw 'Cannot fetch the published source' }
        $sourceCommit = & git rev-parse --verify FETCH_HEAD
        if ($LASTEXITCODE -ne 0 -or $sourceCommit -notmatch '^[0-9a-f]{40}$') { throw 'Cannot resolve the published source commit' }
        & git push "https://github.com/$Repository.git" "${sourceCommit}:refs/heads/main"
        if ($LASTEXITCODE -ne 0) { throw 'Fast-forward sync failed; inspect divergence without force pushing' }
        Write-Output "Synced published source commit $sourceCommit; local uncommitted changes were not copied"
    }
    finally { Pop-Location }
}
