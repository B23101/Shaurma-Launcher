@echo off
chcp 65001 >nul
setlocal enabledelayedexpansion

echo ============================================================
echo   Shaurm Pack Manager - Build Script
echo ============================================================
echo.

:: ─── Config ──────────────────────────────────────────────────────────
set PROJECT_DIR=%~dp0
set TARGET=%PROJECT_DIR%target
set DIST=%~dp0dist
set M2=%USERPROFILE%\.m2\repository
set VERSION=1.0.0
set APP_NAME=Shaurm Pack Manager
set MAIN_CLASS=com.shaurma.packmanager.PackManagerApp
set MAIN_JAR=shaurma-pack-manager-%VERSION%.jar

:: ─── Find JDK 21 ─────────────────────────────────────────────────────
:: Pack Manager використовує Java 21 (sealed interfaces, pattern matching)
set JAVA_JDK=
for /d %%d in ("C:\Program Files\Java\jdk-21*") do (
    if exist "%%d\bin\java.exe" set JAVA_JDK=%%d
)
if not defined JAVA_JDK (
    for /d %%d in ("C:\Program Files\Eclipse Adoptium\jdk-21*") do (
        if exist "%%d\bin\java.exe" set JAVA_JDK=%%d
    )
)
if not defined JAVA_JDK (
    for /d %%d in ("C:\Program Files\Microsoft\jdk-21*") do (
        if exist "%%d\bin\java.exe" set JAVA_JDK=%%d
    )
)
if not defined JAVA_JDK (
    for /d %%d in ("C:\Program Files\Java\jdk-17*") do (
        if exist "%%d\bin\java.exe" set JAVA_JDK=%%d
    )
)
if not defined JAVA_JDK (
    if defined JAVA_HOME if exist "%JAVA_HOME%\bin\java.exe" (
        set JAVA_JDK=%JAVA_HOME%
    )
)
if not defined JAVA_JDK (
    echo [ERROR] JDK 21 not found.
    echo   Download from https://adoptium.net  (Temurin 21 LTS)
    echo   або https://www.oracle.com/java/technologies/downloads/#java21
    pause & exit /b 1
)
echo [INFO] JDK: %JAVA_JDK%

:: ─── Find Maven ───────────────────────────────────────────────────────
set MVN_CMD=
if exist "%USERPROFILE%\maven\apache-maven-3.9.16\bin\mvn.cmd" (
    set MVN_CMD=%USERPROFILE%\maven\apache-maven-3.9.16\bin\mvn.cmd
)
if not defined MVN_CMD (
    where mvn >nul 2>&1 && set MVN_CMD=mvn
)
if not defined MVN_CMD (
    if exist "%~dp0mvnw.cmd" set MVN_CMD=%~dp0mvnw.cmd
)
if not defined MVN_CMD (
    if exist "%~dp0mvnw.cmd" set MVN_CMD=%~dp0mvnw.cmd
)
if not defined MVN_CMD (
    echo [ERROR] Maven not found!
    echo   Download https://maven.apache.org/download.cgi
    echo   Або покладіть mvn.cmd у PATH
    pause & exit /b 1
)
echo [INFO] Maven: %MVN_CMD%
echo.

:: ─── JavaFX jars для jlink ────────────────────────────────────────────
set JFX_BASE=%M2%\org\openjfx
set JFX_VER=21.0.2
set JFX=
for %%m in (javafx-base javafx-graphics javafx-controls javafx-fxml) do (
    if exist "%JFX_BASE%\%%m\%JFX_VER%\%%m-%JFX_VER%-win.jar" (
        set JFX=!JFX!%JFX_BASE%\%%m\%JFX_VER%\%%m-%JFX_VER%-win.jar;
    ) else (
        echo [WARN] JavaFX %%m %JFX_VER% not in Maven cache — буде завантажено при збірці
    )
)

:: ─── Крок 1: Maven build ─────────────────────────────────────────────
echo [1/4] Maven build...
set "JAVA_HOME=%JAVA_JDK%"
pushd "%PROJECT_DIR%"
"%MVN_CMD%" clean package -DskipTests
popd
if %errorlevel% neq 0 (
    echo [ERROR] Maven build failed!
    pause & exit /b 1
)

set SHADED_JAR=%TARGET%\shaurma-pack-manager-%VERSION%-shaded.jar
if not exist "%SHADED_JAR%" (
    :: Спробувати без -shaded суфіксу
    set SHADED_JAR=%TARGET%\%MAIN_JAR%
)
if not exist "%SHADED_JAR%" (
    echo [ERROR] JAR not found: %SHADED_JAR%
    echo   Перевірте target\ папку
    dir "%TARGET%\*.jar"
    pause & exit /b 1
)
echo [INFO] JAR: %SHADED_JAR%
echo OK

:: ─── Крок 2: jlink runtime ───────────────────────────────────────────
echo.
echo [2/4] jlink — мінімальний JRE...
set RUNTIME=%DIST%\pm-runtime
if exist "%RUNTIME%" rmdir /s /q "%RUNTIME%"

if "%JFX%"=="" (
    echo [WARN] JavaFX jars не знайдено в Maven cache.
    echo   Запускаємо mvn dependency:copy-dependencies...
    set JFX_TEMP=%TEMP%\jfx-deps
    mkdir "%JFX_TEMP%" 2>nul
    pushd "%PROJECT_DIR%"
    "%MVN_CMD%" dependency:copy-dependencies -DoutputDirectory="%JFX_TEMP%" -Dclassifier=win -DincludeGroupIds=org.openjfx -q
    popd
    for %%f in ("%JFX_TEMP%\javafx-base-*-win.jar" "%JFX_TEMP%\javafx-graphics-*-win.jar" "%JFX_TEMP%\javafx-controls-*-win.jar" "%JFX_TEMP%\javafx-fxml-*-win.jar") do (
        if exist "%%f" set JFX=!JFX!%%f;
    )
)

if "%JFX%"=="" (
    echo [ERROR] JavaFX jars не знайдено. Запустіть mvn package спочатку.
    pause & exit /b 1
)

"%JAVA_JDK%\bin\jlink.exe" ^
    --module-path "%JFX%;%JAVA_JDK%\jmods" ^
    --add-modules java.se,java.desktop,jdk.unsupported,jdk.zipfs,jdk.crypto.ec,jdk.crypto.cryptoki,jdk.crypto.mscapi,jdk.naming.dns,javafx.base,javafx.graphics,javafx.controls,javafx.fxml ^
    --output "%RUNTIME%" ^
    --strip-debug --no-man-pages --no-header-files

if %errorlevel% neq 0 (
    echo [ERROR] jlink failed!
    pause & exit /b 1
)
echo OK

:: ─── Крок 3: jpackage app-image ──────────────────────────────────────
echo.
echo [3/4] jpackage — app-image...
set APP_IMAGE=%DIST%\%APP_NAME%
if exist "%APP_IMAGE%" rmdir /s /q "%APP_IMAGE%"

set ICON_PATH=
if exist "%~dp0..\icon.ico" set ICON_PATH=--icon "%~dp0..\icon.ico"

"%JAVA_JDK%\bin\jpackage.exe" ^
    --type app-image ^
    --runtime-image "%RUNTIME%" ^
    --input "%TARGET%" ^
    --main-jar %MAIN_JAR% ^
    --main-class %MAIN_CLASS% ^
    --name "%APP_NAME%" ^
    --dest "%DIST%" ^
    --vendor "Shaurm" ^
    --app-version "%VERSION%" ^
    %ICON_PATH% ^
    --description "Shaurm Pack Manager" ^
    --java-options "-Dfile.encoding=UTF-8" ^
    --java-options "--add-opens javafx.graphics/com.sun.glass.ui=ALL-UNNAMED"

if %errorlevel% neq 0 (
    echo [ERROR] jpackage failed!
    pause & exit /b 1
)
echo OK

:: ─── Крок 4: Cleanup ─────────────────────────────────────────────────
echo.
echo [4/4] Cleanup...
if exist "%RUNTIME%" rmdir /s /q "%RUNTIME%"
echo OK

echo.
echo ============================================================
echo   BUILD COMPLETE
echo ============================================================
echo.
echo   Папка з програмою:
echo   %APP_IMAGE%\
echo.
echo   Запускач:
echo   "%APP_IMAGE%\%APP_NAME%.exe"
echo.
echo   ──────────────────────────────────────────────────────────
echo   ЩО ПОКЛАСТИ В ПАПКУ СЕРВЕРА:
echo   (Вказується в Налаштуваннях -> "Папка з даними")
echo.
echo   server-data\               <- твоя папка даних
echo   ├── pack-manager-settings.json  <- автоматично
echo   ├── pack-manager.log            <- автоматично
echo   ├── packs.json                  <- список збірок
echo   ├── {packId}\                   <- папка збірки
echo   │   ├── {packId}.mrpack
echo   │   ├── mods\                   <- bundled-моди
echo   │   ├── tacz\                   <- папка для архівації
echo   │   ├── config\
echo   │   └── options.txt
echo   └── ...
echo.
echo   Pack Manager.exe можна запускати з БУДЬ-ЯКОЇ папки —
echo   шлях до даних вказується в Налаштуваннях GUI.
echo ============================================================
echo.
pause
