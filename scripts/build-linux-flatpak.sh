#!/usr/bin/env bash
# build-linux-flatpak.sh — збирає ShaurmaLauncher.flatpak (Linux, x86_64).
#
# Той самий скрипт працює на GitHub (job linux-flatpak у build-launcher.yml) і
# локально в WSL/Linux. Код компілюється всередині org.gnome.Sdk//50
# (flatpak/com.shaurma.Launcher.yml, flatpak/build-in-sdk.sh).
#
#   scripts/build-linux-flatpak.sh <версія> <тека з frontend.zip і updater-frontend.zip> <тека результату>
#
# Оточення (публічні значення, як у scripts/build-windows.ps1):
#   SHAURMA_ROOT_PUBKEYS, SHAURMA_KEYSET_B64, SHAURMA_KEYSET_SIG  — нова система оновлень
#   SHAURMA_MANIFEST_PUBKEYS, CF_API_KEY, SHAURMA_GOOGLE_CLIENT_ID, SHAURMA_GOOGLE_CLIENT_SECRET — необов'язкові
#
# Результат: <тека>/ShaurmaLauncher.flatpak і <тека>/install-linux.sh.
set -euo pipefail

VERSION="${1:?версія X.Y.Z}"
ASSETS="$(cd "${2:?тека з frontend.zip}" && pwd)"
OUT="${3:?тека результату}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_ID=com.shaurma.Launcher
RUNTIME_REPO=https://dl.flathub.org/repo/flathub.flatpakrepo

[[ "$VERSION" =~ ^[0-9]{1,6}(\.[0-9]{1,6}){2,3}$ ]] || { echo "некоректна версія: $VERSION" >&2; exit 1; }
for f in frontend.zip updater-frontend.zip; do
  [[ -f "$ASSETS/$f" ]] || { echo "немає $ASSETS/$f" >&2; exit 1; }
done
b64() { [[ -z "$1" || "$1" =~ ^[A-Za-z0-9+/=,]+$ ]]; }
b64 "${SHAURMA_ROOT_PUBKEYS:-}" && b64 "${SHAURMA_KEYSET_B64:-}" && b64 "${SHAURMA_KEYSET_SIG:-}" && b64 "${SHAURMA_MANIFEST_PUBKEYS:-}" \
  || { echo 'ключі мають бути base64' >&2; exit 1; }
if [[ -n "${SHAURMA_ROOT_PUBKEYS:-}${SHAURMA_KEYSET_B64:-}${SHAURMA_KEYSET_SIG:-}" ]] && \
   [[ -z "${SHAURMA_ROOT_PUBKEYS:-}" || -z "${SHAURMA_KEYSET_B64:-}" || -z "${SHAURMA_KEYSET_SIG:-}" ]]; then
  echo 'SHAURMA_ROOT_PUBKEYS, SHAURMA_KEYSET_B64 і SHAURMA_KEYSET_SIG задаються разом' >&2; exit 1
fi

for tool in flatpak flatpak-builder unzip; do
  command -v "$tool" >/dev/null || { echo "потрібен $tool (Ubuntu/Debian: sudo apt install flatpak flatpak-builder unzip)" >&2; exit 1; }
done
flatpak remote-add --user --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo

# ── тека збірки: лише потрібний код (без node_modules, dist, бінарників) ──────
# FLATPAK_WORK_DIR — тека для проміжних файлів (у WSL краще на диску Linux, не /mnt/c).
WORK="${FLATPAK_WORK_DIR:-$ROOT/dist}"
STAGE="$WORK/flatpak-stage"
rm -rf "$STAGE"
mkdir -p "$STAGE/src" "$OUT"
OUT="$(cd "$OUT" && pwd)"  # абсолютний шлях: нижче робимо cd "$STAGE"
(cd "$ROOT" && git ls-files -co --exclude-standard -- shell backend updater-backend \
   | grep -v -E '(^|/)(node_modules|dist|build/bin)/' \
   | while IFS= read -r f; do [[ -f "$f" ]] && printf '%s\0' "$f"; done \
   | tar --null -T - -cf -) | tar -C "$STAGE/src" -xf -
cp "$ASSETS/frontend.zip" "$ASSETS/updater-frontend.zip" "$STAGE/src/"
cp "$ROOT/flatpak/build-in-sdk.sh" "$ROOT/flatpak/com.shaurma.Launcher.desktop" \
   "$ROOT/flatpak/com.shaurma.Launcher.metainfo.xml" "$ROOT/flatpak/shaurma-icon.png" "$STAGE/src/"
cp "$ROOT/flatpak/com.shaurma.Launcher.yml" "$STAGE/"
{
  printf 'VERSION=%q\n' "$VERSION"
  printf 'ROOT_KEYS=%q\nKEYSET_B64=%q\nKEYSET_SIG=%q\n' "${SHAURMA_ROOT_PUBKEYS:-}" "${SHAURMA_KEYSET_B64:-}" "${SHAURMA_KEYSET_SIG:-}"
  printf 'MANIFEST_KEYS=%q\nCF_KEY=%q\n' "${SHAURMA_MANIFEST_PUBKEYS:-}" "${CF_API_KEY:-}"
  printf 'GOOGLE_ID=%q\nGOOGLE_SECRET=%q\n' "${SHAURMA_GOOGLE_CLIENT_ID:-}" "${SHAURMA_GOOGLE_CLIENT_SECRET:-}"
  printf 'GEMINI_KEY=%q\nGROQ_KEY=%q\n' "${GEMINI_API_KEY:-}" "${GROQ_API_KEY:-}"
} > "$STAGE/src/build.env"

# ── збірка ─────────────────────────────────────────────────────────────────────
cd "$STAGE"
flatpak-builder --user --install-deps-from=flathub --force-clean --disable-rofiles-fuse --state-dir="$WORK/flatpak-state" \
  --default-branch=stable --repo=repo build-dir com.shaurma.Launcher.yml
flatpak build-bundle --runtime-repo="$RUNTIME_REPO" repo "$OUT/ShaurmaLauncher.flatpak" "$APP_ID" stable
cp "$ROOT/flatpak/install.sh" "$OUT/install-linux.sh"
chmod +x "$OUT/install-linux.sh"
rm -f "$STAGE/src/build.env"
ls -la "$OUT"
