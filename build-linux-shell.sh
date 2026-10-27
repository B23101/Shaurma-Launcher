#!/usr/bin/env bash
# build-linux-shell.sh — збирає ТІЛЬКИ Linux-версію shell (GUI, GTK4/
# WebKitGTK6.0) всередині WSL2 і кладе результат туди, де його підбирає
# build.ps1 (-LinuxShellDir, дефолт dist\linux-shell-prebuilt\).
#
# ЧОМУ ЛИШЕ SHELL: build.ps1 з -IncludeLinux -UseWSLForLinux уже сам
# збирає backend.so/updater.so всередині WSL2 (Build-CShared-Linux —
# викликає wsl.exe bash напряму, без цього файлу). Єдине, чого build.ps1
# НЕ вміє — крос-компілювати сам shell: Wails v3 генерує syso/manifest
# лише для Windows-цілі, а cgo на GTK4/WebKitGTK6.0 вимагає повного
# Linux-sysroot, якого на Windows-хості нема. Тому саме shell компілюється
# в реальному WSL2-середовищі (де GTK4-dev пакети справді стоять), а не
# крос-компіляцією "ззовні".
#
# ІМЕНА ВИХІДНИХ ФАЙЛІВ — рядок в рядок як $shellNamesLinux у build.ps1:
#   shaurma -> ShaurmLauncher       (БЕЗ літери "a" після Shaurm — так у
#   base    -> ShaurmLauncherBase    build.ps1, це не одруківка тут)
#
# ВИКОРИСТАННЯ (можна руками, але зазвичай викликається з build.bat):
#   chmod +x build-linux-shell.sh
#   ./build-linux-shell.sh                # обидві редакції
#   ./build-linux-shell.sh shaurma        # лише одна
#   ./build-linux-shell.sh base
#
# Токен/редакція вшиваються в shell так само, як компілює його
# build.ps1 (перевір main.go, якщо реальні -ldflags-змінні звуться інакше
# — тут узято ті самі імена, що вже використовувались у попередній версії
# цього скрипта; якщо збірка впаде на -X, просто прибери -ldflags і onNext
# скажи мені точні імена змінних з shell/main.go).

set -euo pipefail

if ! grep -qi microsoft /proc/version 2>/dev/null; then
    echo "[!] Схоже, це не WSL2 (не знайдено 'microsoft' у /proc/version)."
    echo "    Продовжую все одно — може працювати і на звичайному Linux."
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

if [ ! -d "shell" ]; then
    echo "[x] Не бачу shell/ поруч зі скриптом."
    echo "    Поклади build-linux-shell.sh у КОРІНЬ репозиторію (там, де build.ps1) і запусти знову."
    exit 1
fi

EDITIONS=("$@")
if [ ${#EDITIONS[@]} -eq 0 ]; then
    EDITIONS=("shaurma" "base")
fi

TOKEN_SHAURMA="${TOKEN_SHAURMA:-dev-token-shaurma}"
TOKEN_BASE="${TOKEN_BASE:-dev-token-base}"

# Пароль unix-користувача WSL2 для неінтерактивного sudo (apt install,
# копіювання Go в /usr/local). Дефолт "1234" — той самий пароль, що
# заведений при першому запуску `wsl --install -d Ubuntu-24.04`
# (Розділ 4 LINUX-BUILD-GUIDE.md). Постав свій через
# WSL_SUDO_PASSWORD=... якщо на цій машині пароль інший — build.bat теж
# прокидає цю змінну (WSL_SUDO_PASSWORD=1234 усередині build.bat).
WSL_SUDO_PASSWORD="${WSL_SUDO_PASSWORD:-1234}"

# sudo_run — обгортка над sudo, що читає пароль зі stdin (-S), а не з
# інтерактивного терміналу. Без цього build.bat (запущений через
# wsl.exe без прив'язаного TTY для введення пароля) просто зависав би
# на першому "[sudo] password for user:" без жодного повідомлення про
# помилку — саме це сталось у попередньому запуску.
sudo_run() {
    echo "$WSL_SUDO_PASSWORD" | sudo -S "$@" 2>/dev/null
}

echo "== Редакції для збірки shell: ${EDITIONS[*]}"

# ── 1. Системні залежності ───────────────────────────────────────────────
NEED_APT_PKGS=()
command -v pkg-config >/dev/null 2>&1 || NEED_APT_PKGS+=("pkg-config")
command -v gcc >/dev/null 2>&1 || NEED_APT_PKGS+=("build-essential")
pkg-config --exists gtk4 2>/dev/null || NEED_APT_PKGS+=("libgtk-4-dev")
pkg-config --exists webkitgtk-6.0 2>/dev/null || NEED_APT_PKGS+=("libwebkitgtk-6.0-dev")
command -v curl >/dev/null 2>&1 || NEED_APT_PKGS+=("curl")

if [ ${#NEED_APT_PKGS[@]} -gt 0 ]; then
    echo "== [1/3] Ставлю apt-пакети: ${NEED_APT_PKGS[*]}"
    sudo_run apt-get update -qq
    sudo_run apt-get install -y "${NEED_APT_PKGS[@]}"
else
    echo "== [1/3] Усі apt-пакети (GTK4/WebKitGTK6.0/build-essential) вже стоять."
fi

# ── 2. Go 1.25+ ───────────────────────────────────────────────────────────
echo "== [2/3] Перевірка Go..."
GO_MIN_VERSION="1.25.0"
GO_OK=0

# ВАЖЛИВО (фікс "качає Go заново щоразу, хоча вже стоїть"): якщо цей
# скрипт запущено як `wsl.exe bash build-linux-shell.sh` (або будь-яким
# іншим НЕ-login способом) з Windows/build.bat, ~/.bashrc НЕ читається —
# тому export PATH, дописаний туди попереднім запуском, у НОВОМУ процесі
# просто відсутній, і нижченаведений `command -v go` не бачить Go, навіть
# якщо /usr/local/go фізично стоїть на диску з минулого разу. Тому ТУТ,
# ще до першої перевірки, підмішуємо стандартний шлях встановлення
# напряму в PATH цього процесу — незалежно від того, як скрипт викликаний
# (interactive/login чи ні) і чи є .bashrc-рядок.
if [ -x "/usr/local/go/bin/go" ]; then
    export PATH="/usr/local/go/bin:$PATH"
fi

if command -v go >/dev/null 2>&1; then
    CUR_GO="$(go version | grep -oP 'go\K[0-9]+\.[0-9]+(\.[0-9]+)?' || echo "0.0.0")"
    if printf '%s\n%s\n' "$GO_MIN_VERSION" "$CUR_GO" | sort -V -C; then
        GO_OK=1
        echo "   Go $CUR_GO — годиться."
    fi
fi
if [ "$GO_OK" -ne 1 ]; then
    GO_TARBALL="go1.25.0.linux-amd64.tar.gz"
    echo "   Качаю $GO_TARBALL..."
    curl -fSL "https://go.dev/dl/${GO_TARBALL}" -o "/tmp/${GO_TARBALL}"
    sudo_run rm -rf /usr/local/go
    sudo_run tar -C /usr/local -xzf "/tmp/${GO_TARBALL}"
    rm -f "/tmp/${GO_TARBALL}"
    export PATH="/usr/local/go/bin:$PATH"
    if ! grep -q '/usr/local/go/bin' "$HOME/.bashrc" 2>/dev/null; then
        echo 'export PATH="/usr/local/go/bin:$PATH"' >> "$HOME/.bashrc"
    fi
fi
export PATH="/usr/local/go/bin:$PATH"

# ── 3. Збірка shell для кожної редакції ─────────────────────────────────
# ТОЧНО ті самі імена й тека, які build.ps1 очікує в -LinuxShellDir
# (дефолт dist\linux-shell-prebuilt\) — $shellNamesLinux у build.ps1.
OUT_DIR="$ROOT_DIR/dist/linux-shell-prebuilt"
mkdir -p "$OUT_DIR"

declare -A SHELL_NAMES=( ["shaurma"]="ShaurmLauncher" ["base"]="ShaurmLauncherBase" )

for ED in "${EDITIONS[@]}"; do
    if [[ "$ED" != "shaurma" && "$ED" != "base" ]]; then
        echo "[!] Невідома редакція '$ED' — пропускаю."
        continue
    fi
    SHELL_NAME="${SHELL_NAMES[$ED]}"
    TOKEN_FOR_ED="$TOKEN_SHAURMA"
    [ "$ED" = "base" ] && TOKEN_FOR_ED="$TOKEN_BASE"

    echo "== [3/3] shell ($ED) -> $OUT_DIR/$SHELL_NAME"
    (
        cd "$ROOT_DIR/shell"
        CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
            -tags production \
            -trimpath -buildvcs=false \
            -ldflags "-X main.editionChannel=$ED -X main.cdnToken=$TOKEN_FOR_ED" \
            -o "$OUT_DIR/$SHELL_NAME" .
    )
    chmod +x "$OUT_DIR/$SHELL_NAME"
done

echo ""
echo "Готово: $OUT_DIR/"
ls -la "$OUT_DIR"
echo ""
echo "Тепер build.ps1 -IncludeLinux -UseWSLForLinux -BuildAppImage підбере ці"
echo "файли автоматично (дефолтний -LinuxShellDir саме сюди й показує)."
