$ErrorActionPreference = "Stop"
$ProjectDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$TargetDir = Join-Path $ProjectDir "target"
$DistDir = Join-Path $ProjectDir "dist"
$M2 = Join-Path $env:USERPROFILE ".m2\repository"
$Version = "1.0.0"
$JfxVer = "21.0.2"
$AppName = "Shaurm Pack Manager"
$MainClass = "com.shaurma.packmanager.PackManagerApp"
$MainJar = "shaurma-pack-manager-$Version.jar"
$AppImageDir = Join-Path $DistDir $AppName
$BackupDir = Join-Path $env:TEMP "pm-backup"

# Backup user files before build
$userFiles = @("pack-manager-settings.json", "packs.json", "pack-manager.log", "local-paths.json")
New-Item -ItemType Directory -Path $BackupDir -Force | Out-Null
foreach ($f in $userFiles) {
    $src = Join-Path $AppImageDir $f
    if (Test-Path $src) {
        Copy-Item $src (Join-Path $BackupDir $f) -Force
        Write-Host "Backed up: $f"
    }
}

Write-Host "=== Shaurm Pack Manager - Build (PowerShell) ==="

# Find JDK 21
$Jdk = $null
$jdkPaths = @(
    "C:\Program Files\Java\jdk-21*",
    "C:\Program Files\Eclipse Adoptium\jdk-21*",
    "C:\Program Files\Microsoft\jdk-21*"
)
foreach ($pattern in $jdkPaths) {
    $dirs = Get-ChildItem $pattern -Directory -ErrorAction SilentlyContinue
    foreach ($d in $dirs) {
        if (Test-Path "$d\bin\java.exe") { $Jdk = $d.FullName; break }
    }
    if ($Jdk) { break }
}
if (-not $Jdk -and $env:JAVA_HOME -and (Test-Path "$env:JAVA_HOME\bin\java.exe")) { $Jdk = $env:JAVA_HOME }
if (-not $Jdk) { Write-Host "[ERROR] JDK 21 not found"; pause; exit 1 }
Write-Host "JDK: $Jdk"

# Find Maven
$Mvn = $null
$mvnPaths = @(
    "$env:USERPROFILE\maven\apache-maven-3.9.16\bin\mvn.cmd",
    (Get-Command "mvn" -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source),
    (Join-Path $ProjectDir "mvnw.cmd")
)
foreach ($p in $mvnPaths) { if ($p -and (Test-Path $p)) { $Mvn = $p; break } }
if (-not $Mvn) { Write-Host "[ERROR] Maven not found"; pause; exit 1 }
Write-Host "Maven: $Mvn"

Write-Host ""
Write-Host "[1/4] Maven build..."
$env:JAVA_HOME = $Jdk
Push-Location $ProjectDir
try {
    & $Mvn clean package -DskipTests | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "Maven build failed" }
} finally { Pop-Location }

# Find the JAR
$JarFile = Join-Path $TargetDir "$MainJar"
if (-not (Test-Path $JarFile)) {
    $shadedJar = Join-Path $TargetDir "shaurma-pack-manager-$Version-shaded.jar"
    if (Test-Path $shadedJar) { $JarFile = $shadedJar }
}
if (-not (Test-Path $JarFile)) {
    Write-Host "[ERROR] JAR not found in $TargetDir"
    Get-ChildItem $TargetDir -Filter "*.jar" | Select-Object Name
    pause; exit 1
}
Write-Host "JAR: $JarFile"

Write-Host ""
Write-Host "[2/4] jlink JRE..."
$RuntimeDir = Join-Path $DistDir "pm-runtime"
if (Test-Path $RuntimeDir) { Remove-Item -Recurse -Force $RuntimeDir }

# Get JavaFX jars
$JfxJars = @()
foreach ($m in @("javafx-base","javafx-graphics","javafx-controls","javafx-fxml")) {
    $jar = "$M2\org\openjfx\$m\$JfxVer\$m-$JfxVer-win.jar"
    if (Test-Path $jar) { $JfxJars += $jar } else { Write-Host "[WARN] $jar not found" }
}

if (-not $JfxJars) {
    Write-Host "[WARN] JavaFX jars not in cache, downloading..."
    $jfxTemp = Join-Path $env:TEMP "jfx-deps-pm"
    New-Item -ItemType Directory -Path $jfxTemp -Force | Out-Null
    Push-Location $ProjectDir
    try {
        & $Mvn dependency:copy-dependencies -DoutputDirectory="$jfxTemp" -Dclassifier=win -DincludeGroupIds=org.openjfx -q | Out-Null
    } finally { Pop-Location }
    foreach ($m in @("javafx-base","javafx-graphics","javafx-controls","javafx-fxml")) {
        $jars = Get-ChildItem "$jfxTemp\$m-*-win.jar" -ErrorAction SilentlyContinue
        foreach ($j in $jars) { $JfxJars += $j.FullName }
    }
    Remove-Item -Recurse -Force $jfxTemp -ErrorAction SilentlyContinue
}

if (-not $JfxJars) {
    Write-Host "[ERROR] JavaFX jars not found. Run mvn package first."
    pause; exit 1
}

$modPath = ($JfxJars -join ';') + ";" + (Join-Path $Jdk "jmods")
$modules = "java.se,java.desktop,jdk.unsupported,jdk.zipfs,jdk.crypto.ec,jdk.crypto.cryptoki,jdk.crypto.mscapi,jdk.naming.dns,javafx.base,javafx.graphics,javafx.controls,javafx.fxml"
$jlinkExe = Join-Path $Jdk "bin\jlink.exe"
& $jlinkExe "--module-path" $modPath "--add-modules" $modules "--output" $RuntimeDir "--strip-debug" "--no-man-pages" "--no-header-files"
if ($LASTEXITCODE -ne 0) { Write-Host "[ERROR] jlink failed!"; pause; exit 1 }

Write-Host ""
Write-Host "[3/4] jpackage app-image..."
$AppImage = Join-Path $DistDir $AppName
if (Test-Path $AppImage) { Remove-Item -Recurse -Force $AppImage }

$IconArg = @()
$iconPath = Join-Path $ProjectDir "..\icon.ico"
if (Test-Path $iconPath) { $IconArg = "--icon", $iconPath }

$jpackageExe = Join-Path $Jdk "bin\jpackage.exe"
& $jpackageExe "--type" "app-image" "--runtime-image" $RuntimeDir "--input" $TargetDir "--main-jar" $MainJar "--main-class" $MainClass "--name" $AppName "--dest" $DistDir "--vendor" "Shaurm" "--app-version" $Version $IconArg "--description" "Shaurm Pack Manager" "--java-options" "-Dfile.encoding=UTF-8" "--java-options" "--add-opens=javafx.graphics/com.sun.glass.ui=ALL-UNNAMED"
if ($LASTEXITCODE -ne 0) { Write-Host "[ERROR] jpackage failed!"; pause; exit 1 }

Write-Host ""
Write-Host "[4/4] Cleanup..."
if (Test-Path $RuntimeDir) { Remove-Item -Recurse -Force $RuntimeDir }

# Restore user files
Write-Host ""
Write-Host "[Restore] Restoring user files..."
foreach ($f in $userFiles) {
    $src = Join-Path $BackupDir $f
    $dst = Join-Path $AppImageDir $f
    if (Test-Path $src) {
        Copy-Item $src $dst -Force
        Write-Host "Restored: $f"
    }
}
Remove-Item -Recurse -Force $BackupDir -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "=== BUILD COMPLETE ==="
Write-Host "App: $AppImageDir\$AppName.exe"
pause
