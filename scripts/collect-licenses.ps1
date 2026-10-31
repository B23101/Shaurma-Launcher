[CmdletBinding()]
param([Parameter(Mandatory)] [string]$Destination)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$Destination = [IO.Path]::GetFullPath($Destination)
New-Item -ItemType Directory -Force -Path $Destination | Out-Null
$inventory = @()
foreach ($module in @('backend', 'shell', 'bootstrap', 'updater-backend')) {
    Push-Location (Join-Path $root $module)
    try {
        $packageJson = @(& go list -mod=readonly -deps -json -tags production .)
        if ($LASTEXITCODE -ne 0) { throw "Cannot enumerate dependencies: $module" }
        if ($module -eq 'backend') {
            $packageJson += @(& go list -mod=readonly -deps -json -tags production,shaurma .)
            if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate full-edition dependencies' }
        }
        $json = $packageJson -join "`n"
        if ($LASTEXITCODE -ne 0) { throw "Cannot enumerate dependencies: $module" }
        $dependencies = ConvertFrom-Json ('[' + ($json -replace '(?m)^}\r?\n\{', "},`n{") + ']')
        $dependencies = @($dependencies | Where-Object { $_.Module } | ForEach-Object { $_.Module } | Sort-Object Path, Version -Unique)
        foreach ($dependency in $dependencies) {
            if ($dependency.Main) { continue }
            if ($dependency.Replace) { throw "Unreviewed module replacement: $($dependency.Path)" }
            $id = ($dependency.Path + '@' + $dependency.Version) -replace '[^A-Za-z0-9._@-]', '_'
            $target = Join-Path $Destination $id
            New-Item -ItemType Directory -Force -Path $target | Out-Null
            if (-not $dependency.Dir) { throw "Dependency source unavailable: $($dependency.Path)" }
            $notices = @(Get-ChildItem -LiteralPath $dependency.Dir -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE|AUTHORS)(\.|$)' })
            if (-not ($notices | Where-Object { $_.Name -match '^(LICENSE|COPYING)(\.|$)' })) {
                throw "Review missing dependency license before release: $($dependency.Path)"
            }
            foreach ($notice in $notices) { Copy-Item -LiteralPath $notice.FullName -Destination $target -Force }
            $inventory += [pscustomobject]@{ Module = $module; Dependency = $dependency.Path; Version = $dependency.Version; Notices = @($notices.Name) }
        }
    } finally { Pop-Location }
}
$bundleInventory = Join-Path $root 'frontend/dist/licenses/bundled-packages.json'
if (-not (Test-Path -LiteralPath $bundleInventory)) { throw 'Build the frontend before collecting bundled dependency licenses' }
$bundledPackages = @(Get-Content -LiteralPath $bundleInventory -Raw | ConvertFrom-Json)
foreach ($package in $bundledPackages) {
    if ($package -notmatch '^(@[A-Za-z0-9._-]+/)?[A-Za-z0-9._-]+$') { throw 'Invalid bundled dependency name' }
    $source = Join-Path (Join-Path $root 'frontend/node_modules') $package
    $metadata = Get-Content -LiteralPath (Join-Path $source 'package.json') -Raw | ConvertFrom-Json
    $notices = @(Get-ChildItem -LiteralPath $source -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)(\.|$)' })
    if (-not $notices) { throw "Review missing npm license: $package" }
    $target = Join-Path $Destination ($package -replace '[^A-Za-z0-9._-]', '_')
    New-Item -ItemType Directory -Force -Path $target | Out-Null
    foreach ($notice in $notices) { Copy-Item -LiteralPath $notice.FullName -Destination $target -Force }
    $inventory += [pscustomobject]@{ Module = 'frontend'; Dependency = $package; Version = $metadata.version; Notices = @($notices.Name) }
}
$goRoot = (& go env GOROOT).Trim()
Copy-Item -LiteralPath (Join-Path $goRoot 'LICENSE') -Destination (Join-Path $Destination 'Go-LICENSE.txt') -Force
$inventory | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $Destination 'inventory.json') -Encoding utf8
Write-Output "Collected dependency licenses: $($inventory.Count) module entries"
