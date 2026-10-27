#!/usr/bin/env bash
# install.sh — встановлення Shaurma Launcher у Flatpak однією командою.
#
# Що робить (усе, що реально потрібно лаунчеру, і нічого зайвого):
#   1. перевіряє, що flatpak встановлений (якщо ні — каже точну команду
#      для твого дистрибутива, без самовільного sudo);
#   2. додає репозиторій flathub, якщо його ще немає;
#   3. ставить РІВНО той рантайм, під який зібраний shell —
#      org.gnome.Platform//50 (GTK4 + WebKitGTK 6.0). Це та сама причина,
#      чому на Debian/Ubuntu не треба ставити системні webkitgtk/gtk:
#      усе лежить у рантаймі, в одному місці, однакової версії;
#   4. ставить сам лаунчер — з локального .flatpak-файлу або з твого
#      репозиторію (.flatpakrepo);
#   5. перевіряє, що дозволи пісочниці справді застосовані: мережа,
#      GPU (--device=dri), звук гри (--socket=pulseaudio), Wayland/X11.
#
# ВИКОРИСТАННЯ
#   ./install.sh                          # файли .flatpak поруч зі скриптом
#   ./install.sh ./shaurma-launcher-shaurma.flatpak
#   ./install.sh https://твій-домен/shaurma.flatpakrepo --edition shaurma
#   ./install.sh https://твій-домен/com.shaurma.Launcher.flatpakref   # найпростіше
#   ./install.sh --edition both           # обидві редакції (у різні app-id)
#
# ПРАПОРЦІ
#   --edition shaurma|base|both   що ставити (типово: те, що є у наявності;
#                                 для .flatpakrepo — shaurma)
#   --skip-runtime                не ставити org.gnome.Platform (уже є)
#
# Скрипт неінтерактивний: нічого не питає, лише ставить те, що вибрано.
#
# Встановлення завжди --user: без sudo і без root, як і належить для
# десктопного застосунку. Дані лаунчера живуть у ~/.var/app/<app-id>.

set -euo pipefail

RUNTIME="org.gnome.Platform//50"
FLATHUB_REPO="https://flathub.org/repo/flathub.flatpakrepo"

APP_SHAURMA="com.shaurma.Launcher"
APP_BASE="com.shaurma.LauncherBase"

EDITION=""
SKIP_RUNTIME=0
SOURCE=""

die()  { echo "ПОМИЛКА: $*" >&2; exit 1; }
info() { echo "== $*"; }
warn() { echo "УВАГА: $*" >&2; }

usage() {
  # Друкує шапку файлу: усі рядки-коментарі після шебанга.
  awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --edition)      EDITION="${2:-}"; shift 2 ;;
    --edition=*)    EDITION="${1#*=}"; shift ;;
    --skip-runtime) SKIP_RUNTIME=1; shift ;;
    -h|--help)      usage; exit 0 ;;
    -*)             die "невідомий прапорець: $1 (див. --help)" ;;
    *)              [[ -z "$SOURCE" ]] || die "можна вказати лише одне джерело"; SOURCE="$1"; shift ;;
  esac
done

case "$EDITION" in
  ""|shaurma|base|both) ;;
  *) die "--edition має бути shaurma, base або both (отримано: $EDITION)" ;;
esac

# ── 1. flatpak ────────────────────────────────────────────────────────────
if ! command -v flatpak >/dev/null 2>&1; then
  cat >&2 <<'EOF'
ПОМИЛКА: flatpak не встановлений.

Постав його пакетом свого дистрибутива (потім запусти цей скрипт знову):
  Debian/Ubuntu/Mint:  sudo apt install flatpak
  Fedora/Nobara:       sudo dnf install flatpak
  Arch/Manjaro:        sudo pacman -S flatpak
  openSUSE:            sudo zypper install flatpak

Після перезаходу в сесію flatpak готовий; перевірка: flatpak --version
EOF
  exit 1
fi

if [[ -z "${XDG_RUNTIME_DIR:-}" && -z "${DBUS_SESSION_BUS_ADDRESS:-}" ]]; then
  warn "не схоже на графічну сесію (немає XDG_RUNTIME_DIR/DBUS)."
  warn "встановити можна й так, але запускати лаунчер треба з робочого столу."
fi

# ── 2. flathub ────────────────────────────────────────────────────────────
# Перевіряємо САМЕ user-інсталяцію: нижче все ставиться з --user, а
# `flatpak install --user flathub …` не бачить flathub, доданий лише на
# рівні системи (типово на Fedora/Ubuntu) — і падало б «remote not found».
# --if-not-exists робить додавання безпечним, навіть якщо flathub уже є.
if ! flatpak remotes --user --columns=name 2>/dev/null | grep -qx flathub; then
  info "додаю репозиторій flathub (потрібен для рантайму GTK4/WebKitGTK)"
  flatpak remote-add --user --if-not-exists flathub "$FLATHUB_REPO" ||
    die "не вдалося додати flathub — перевір інтернет і повтори"
fi

# ── 3. рантайм ────────────────────────────────────────────────────────────
if [[ "$SKIP_RUNTIME" -eq 0 ]]; then
  if flatpak info "$RUNTIME" >/dev/null 2>&1; then
    info "рантайм $RUNTIME уже встановлений"
  else
    info "ставлю рантайм $RUNTIME (GTK4 + WebKitGTK 6.0 — без системних пакетів)"
    flatpak install -y --user flathub "$RUNTIME" ||
      die "не вдалося встановити $RUNTIME з flathub"
  fi
else
  info "перевірку рантайму пропущено (--skip-runtime)"
fi

# ── 4. сам лаунчер ────────────────────────────────────────────────────────
script_dir="$(cd "$(dirname "$0")" && pwd)"

install_bundle() {
  local file="$1"
  [[ -f "$file" ]] || die "файл не знайдено: $file"
  info "ставлю $(basename "$file")"
  flatpak install -y --user --noninteractive "$file" ||
    die "flatpak install не вдався для $file"
}

install_from_repo() {
  local url="$1"
  local repo_name="shaurma"
  info "підключаю репозиторій $url"

  # Репозиторій із GPGKey у .flatpakrepo підключається як є. Якщо підпису
  # немає (наш публічний пайплайн поки не підписує), flatpak на етапі
  # install відмовить «GPG verification enabled, but no signatures
  # found» — тоді перепідключаємо з --no-gpg-verify і повторюємо. Обидва
  # варіанти — штатна поведінка flatpak; різниця лише в наявності підпису,
  # і ми не вгадуємо, а покладаємось на фактичну відповідь.
  flatpak remote-add --user --if-not-exists "$repo_name" "$url" ||
    die "не вдалося підключити репозиторій $url"

  local apps=()
  case "${EDITION:-shaurma}" in
    shaurma) apps=("$APP_SHAURMA") ;;
    base)    apps=("$APP_BASE") ;;
    both)    apps=("$APP_SHAURMA" "$APP_BASE") ;;
  esac

  local app
  for app in "${apps[@]}"; do
    # Для base не тягнемо повну редакцію і навпаки — ставимо лише вибране.
    info "ставлю $app"
    if flatpak install -y --user --noninteractive "$repo_name" "$app"; then
      continue
    fi
    warn "перша спроба не вдалася (ймовірно, репозиторій без підпису) — повторюю з --no-gpg-verify"
    flatpak remote-delete --user --force "$repo_name" >/dev/null 2>&1 || true
    flatpak remote-add --user --no-gpg-verify "$repo_name" "$url" ||
      die "не вдалося підключити репозиторій $url з --no-gpg-verify"
    flatpak install -y --user --noninteractive "$repo_name" "$app" ||
      die "flatpak install не вдався для $app"
  done
}

install_ref() {
  # .flatpakref вже містить усе: адресу репозиторію, app-id і RuntimeRepo
  # (звідки докачати org.gnome.Platform//50). Один виклик — і готово.
  local ref="$1"
  info "ставлю за $(basename "$ref")"
  flatpak install -y --user --noninteractive "$ref" ||
    die "flatpak install не вдався для $ref"
}

if [[ -n "$SOURCE" ]]; then
  case "$SOURCE" in
    *.flatpakref)                     install_ref "$SOURCE" ;;
    *.flatpakrepo|http://*|https://*) install_from_repo "$SOURCE" ;;
    *.flatpak)                        install_bundle "$SOURCE" ;;
    *) die "не розумію, що ставити: $SOURCE (очікую *.flatpak, *.flatpakref або *.flatpakrepo/URL)" ;;
  esac
else
  # Локальні файли поруч зі скриптом (типова роздача «як AppImage»).
  shopt -s nullglob
  bundles=("$script_dir"/shaurma-launcher-*.flatpak)
  shopt -u nullglob
  [[ ${#bundles[@]} -gt 0 ]] || die "поруч зі скриптом немає shaurma-launcher-*.flatpak — вкажи файл або URL .flatpakrepo (див. --help)"

  installed_any=0
  for b in "${bundles[@]}"; do
    case "$(basename "$b")" in
      *-base.flatpak)
        [[ -z "$EDITION" || "$EDITION" == "base" || "$EDITION" == "both" ]] || continue
        install_bundle "$b"; installed_any=1 ;;
      *-shaurma.flatpak)
        [[ -z "$EDITION" || "$EDITION" == "shaurma" || "$EDITION" == "both" ]] || continue
        install_bundle "$b"; installed_any=1 ;;
    esac
  done
  [[ "$installed_any" -eq 1 ]] || die "жоден .flatpak не відповідає --edition $EDITION"
fi

# ── 5. перевірка дозволів (звук гри, GPU, мережа) ─────────────────────────
info "перевіряю дозволи пісочниці"
missing_perms=0
check_perm() {
  local perms="$1" needle="$2" label="$3"
  if grep -q -- "$needle" <<<"$perms"; then
    echo "      OK   $label"
  else
    echo "      НЕМА $label"
    missing_perms=1
  fi
}
for app in "$APP_SHAURMA" "$APP_BASE"; do
  flatpak info "$app" >/dev/null 2>&1 || continue
  perms="$(flatpak info --show-permissions "$app" 2>/dev/null || true)"
  echo "   $app:"
  check_perm "$perms" 'pulseaudio' 'звук (--socket=pulseaudio)'
  check_perm "$perms" 'dri'        'GPU (--device=dri)'
  check_perm "$perms" 'network'    'мережа (--share=network)'
  # Саме x11, а НЕ fallback-x11: на Wayland-сесії fallback-x11 сокет не
  # віддає, і гра (GLFW/X11) не мала б дисплея. grep -E, щоб "fallback-x11"
  # не зараховувався як "x11".
  if grep -Eq '(^|[=;])x11(;|$)' <<<"$perms"; then
    echo "      OK   вікно гри (--socket=x11)"
  else
    echo "      НЕМА вікно гри (--socket=x11)"
    missing_perms=1
  fi
done

# Живий тест звуку: чи справді PulseAudio-сокет видно ВСЕРЕДИНІ пісочниці.
# Дозвіл у маніфесті ще не гарантує звук: якщо на хості не запущений
# PipeWire-pulse/PulseAudio, сокета просто нема, і гра мовчатиме.
for app in "$APP_SHAURMA" "$APP_BASE"; do
  flatpak info "$app" >/dev/null 2>&1 || continue
  if flatpak run --command=sh "$app" -c 'test -S "${XDG_RUNTIME_DIR}/pulse/native"' >/dev/null 2>&1; then
    echo "   $app: звуковий сервер у пісочниці доступний"
  else
    warn "$app: у пісочниці немає PulseAudio-сокета — на хості не запущений звуковий сервер."
    warn "   PipeWire: systemctl --user enable --now pipewire-pulse.service"
    warn "   PulseAudio: pulseaudio --start"
  fi
done

if [[ "$missing_perms" -ne 0 ]]; then
  warn "у встановленого застосунку бракує частини дозволів — можливо, файл зібрано зі старого маніфесту."
  warn "перезбери Flatpak (flatpak/build-and-publish.sh) або повідом розробнику app-id вище."
fi

# ── 6. що далі ────────────────────────────────────────────────────────────
cat <<EOF

Готово. Запуск — з меню застосунків або:
  flatpak run $APP_SHAURMA       # повна редакція
  flatpak run $APP_BASE          # базова редакція

Оновлення: Flatpak — лише встановлювач. Backend, frontend і updater
лаунчер оновлює сам через CDN. Сам shell (вікно GTK/WebKit) змінюється
рідко — його оновлює новий .flatpak-файл (поставити поверх тією ж
командою).

Якщо звуку в грі немає — спершу перевір, що в системі працює звук
(pw-play / aplay), а тоді переконайся, що в дозволах вище є
«звук (--socket=pulseaudio)». Flatpak передає звук лише через
PulseAudio/PipeWire-pulse; прямого доступу до /dev/snd у пісочниці немає
і не потрібно.

Дані (налаштування, збірки, Java) лежать у:
  ~/.var/app/$APP_SHAURMA/data/ShaurmLauncher
  ~/.var/app/$APP_BASE/data/ShaurmLauncherBase
Повне видалення разом із даними:
  flatpak uninstall --user --delete-data $APP_SHAURMA
EOF
