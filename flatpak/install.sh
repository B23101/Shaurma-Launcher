#!/usr/bin/env bash
# Shaurma Launcher для Linux — встановлення однією командою:
#
#   curl -fsSL https://github.com/B23101/Shaurma-Launcher/releases/latest/download/install-linux.sh | bash
#
# 1. Якщо flatpak немає — ставить його пакетним менеджером дистрибутива
#    (apt, dnf, zypper, pacman, apk, xbps, eopkg, emerge; попросить пароль sudo).
# 2. Качає ShaurmaLauncher.flatpak з останнього релізу GitHub і звіряє SHA-256
#    з SHA256SUMS того самого релізу.
# 3. Ставить лаунчер для поточного користувача. Рантайм (GTK4, WebKitGTK, GL)
#    flatpak сам бере з flathub — системні бібліотеки не потрібні.
#
# Далі лаунчер оновлюється сам. Видалення: flatpak uninstall --user com.shaurma.Launcher
set -euo pipefail

REPO=B23101/Shaurma-Launcher
APP_ID=com.shaurma.Launcher
BASE="${SHAURMA_RELEASE_BASE:-https://github.com/$REPO/releases/latest/download}"
FILE=ShaurmaLauncher.flatpak

say()  { printf '\033[1;33m==>\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mПомилка:\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(uname -m)" = x86_64 ] || die "поки що є лише збірка для x86_64 (у вас $(uname -m))"
[ "$(id -u)" -ne 0 ] || die "запускайте без sudo: лаунчер ставиться для вашого користувача"

need_sudo() { command -v sudo >/dev/null || die "немає sudo — встановіть flatpak вручну і запустіть скрипт знову"; sudo "$@"; }

install_flatpak() {
  say "Ставлю flatpak (потрібен один раз)…"
  if command -v apt-get >/dev/null; then need_sudo apt-get update -qq && need_sudo apt-get install -y flatpak
  elif command -v dnf >/dev/null; then need_sudo dnf install -y flatpak
  elif command -v zypper >/dev/null; then need_sudo zypper --non-interactive install flatpak
  elif command -v pacman >/dev/null; then need_sudo pacman -S --needed --noconfirm flatpak
  elif command -v apk >/dev/null; then need_sudo apk add flatpak
  elif command -v xbps-install >/dev/null; then need_sudo xbps-install -Sy flatpak
  elif command -v eopkg >/dev/null; then need_sudo eopkg install -y flatpak
  elif command -v emerge >/dev/null; then need_sudo emerge --ask=n sys-apps/flatpak
  else die "не знаю пакетного менеджера цього дистрибутива — встановіть flatpak (https://flatpak.org/setup/) і запустіть скрипт знову"
  fi
  NEW_FLATPAK=1
}

NEW_FLATPAK=0
command -v flatpak >/dev/null || install_flatpak
command -v curl >/dev/null || die "потрібен curl"

flatpak remote-add --user --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
say "Завантажую $FILE…"
curl -fL --progress-bar -o "$TMP/$FILE" "$BASE/$FILE"
curl -fsSL -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"
want="$(awk -v f="$FILE" '$2 == f { print $1 }' "$TMP/SHA256SUMS")"
[ -n "$want" ] || die "у SHA256SUMS немає $FILE"
got="$(sha256sum "$TMP/$FILE" | cut -d' ' -f1)"
[ "$want" = "$got" ] || die "SHA-256 не збігається — файл пошкоджено, спробуйте ще раз"

say "Встановлюю Shaurma Launcher (рантайм flatpak докачає сам)…"
flatpak install --user --noninteractive -y --reinstall --bundle "$TMP/$FILE"

say "Готово. Shaurma Launcher є в меню застосунків. Запуск з терміналу: flatpak run $APP_ID"
if [ "$NEW_FLATPAK" = 1 ]; then
  say "flatpak щойно встановлено: якщо значка ще немає в меню, вийдіть із системи й увійдіть знову."
fi
