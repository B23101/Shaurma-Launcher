<#
.SYNOPSIS
  READ-ONLY audit of the GitHub repository settings (plan "single launcher", §16).

.DESCRIPTION
  Uses your own `gh auth login`. Prints names and flags only, never secret values.
  Changes nothing. Items it cannot see (Settings -> Integrations, PATs, 2FA, SignPath MFA)
  are listed at the end for a manual check.

.EXAMPLE
  .\scripts\audit-github-settings.ps1
#>
[CmdletBinding()]
param([string]$Repository = 'B23101/Shaurma-Launcher')
$ErrorActionPreference = 'Continue'
& gh auth status *> $null
if ($LASTEXITCODE -ne 0) { throw "gh is not logged in. Run 'gh auth login'." }

function Api([string]$path) {
    $out = & gh api -H 'Accept: application/vnd.github+json' $path 2>$null
    if ($LASTEXITCODE -ne 0) { return $null }
    # ConvertFrom-Json у PowerShell 5 віддає масив одним об'єктом — розгортаємо.
    return ($out | ConvertFrom-Json) | ForEach-Object { $_ }
}
function Show([string]$name, $ok, [string]$detail = '') {
    $mark = if ($ok -eq $true) { 'OK  ' } elseif ($ok -eq $false) { 'FIX ' } else { '??  ' }
    Write-Host ("[{0}] {1} {2}" -f $mark, $name, $detail)
}

$repo = Api "repos/$Repository"
Show 'repository visibility public' ($repo.visibility -eq 'public') $repo.visibility
Show 'default branch main' ($repo.default_branch -eq 'main') $repo.default_branch
$sa = $repo.security_and_analysis
Show 'secret scanning' ($sa.secret_scanning.status -eq 'enabled') $sa.secret_scanning.status
Show 'push protection' ($sa.secret_scanning_push_protection.status -eq 'enabled') $sa.secret_scanning_push_protection.status
Show 'private vulnerability reporting' ((Api "repos/$Repository/private-vulnerability-reporting").enabled -eq $true)

$perm = Api "repos/$Repository/actions/permissions/workflow"
Show 'default GITHUB_TOKEN = read' ($perm.default_workflow_permissions -eq 'read') $perm.default_workflow_permissions
Show 'workflows cannot approve PRs' ($perm.can_approve_pull_request_reviews -eq $false)

$rulesets = Api "repos/$Repository/rulesets"
Show 'rulesets present' (@($rulesets).Count -gt 0) ((@($rulesets) | ForEach-Object { "$($_.name) [$($_.target)/$($_.enforcement)]" }) -join '; ')
$bp = Api "repos/$Repository/branches/main/protection"
if ($bp) {
    Show 'main: force-push forbidden' (-not $bp.allow_force_pushes.enabled)
    Show 'main: deletion forbidden' (-not $bp.allow_deletions.enabled)
} else { Show 'main: classic branch protection' $null '(none — check rulesets above)' }
Show 'v* tags protected (ruleset target tag)' (@(@($rulesets) | Where-Object { $_.target -eq 'tag' }).Count -gt 0)

$envs = Api "repos/$Repository/environments"
foreach ($e in @($envs.environments)) {
    $rules = ($e.protection_rules | ForEach-Object { $_.type }) -join ','
    Show "environment $($e.name)" $null "protection: $rules"
}
Show 'environment "release" exists' (@(@($envs.environments) | Where-Object { $_.name -eq 'release' }).Count -gt 0)

$secrets = Api "repos/$Repository/actions/secrets"
$names = @($secrets.secrets | ForEach-Object { $_.name })
Write-Host "`nRepository secret NAMES: $($names -join ', ')"
$legacy = $names | Where-Object { $_ -match '^(TOKEN_|SHRM_BUILD_TOKEN_|SIGN_CMD)' }
Show 'no legacy channel tokens on GitHub' (@($legacy).Count -eq 0) (($legacy) -join ', ')
$vars = Api "repos/$Repository/actions/variables"
Write-Host "Repository variable NAMES: $((@($vars.variables | ForEach-Object { $_.name })) -join ', ')"
foreach ($v in 'ORDER_PUBKEYS', 'SHAURMA_ROOT_PUBKEYS', 'SHAURMA_KEYSET_B64', 'SHAURMA_KEYSET_SIG') {
    Show "variable $v" (@(@($vars.variables) | Where-Object { $_.name -eq $v }).Count -gt 0)
}

Write-Host "`nDeploy keys: $((@(Api "repos/$Repository/keys") | ForEach-Object { $_.title }) -join ', ')"
Write-Host "Webhooks:    $((@(Api "repos/$Repository/hooks") | ForEach-Object { $_.config.url -replace '\?.*$','' }) -join ', ')"
Write-Host "Collaborators: $((@(Api "repos/$Repository/collaborators") | ForEach-Object { "$($_.login)($($_.role_name))" }) -join ', ')"
$wf = Api "repos/$Repository/actions/workflows"
Write-Host "Workflows: $((@($wf.workflows) | ForEach-Object { "$($_.path)[$($_.state)]" }) -join ', ')"

Write-Host @"

Check manually (not visible through the API):
  - Settings -> Integrations -> GitHub Apps: remove apps you do not use (e.g. AI assistants).
  - Your account -> Developer settings -> PATs: expiry, scope only this repository.
  - 2FA / passkey on GitHub, MFA on SignPath.
"@
