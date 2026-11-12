#!/bin/sh
# build-in-sdk.sh — виконується flatpak-builder УСЕРЕДИНІ org.gnome.Sdk//50
# (модуль shaurma-launcher у com.shaurma.Launcher.yml). Збирає shell,
# backend.so, updater.so і ставить їх у /app разом із запеченим набором.
# Ключі й версія приходять з build.env, який пише scripts/build-linux-flatpak.sh.
set -eu

. ./build.env

export PATH="$PWD/goroot/bin:$PATH"
export GOPATH="$PWD/.gopath" GOCACHE="$PWD/.gocache"
go version

NEWUPD=''
if [ -n "${ROOT_KEYS:-}" ]; then
  NEWUPD="-X shrm-updater-backend/internal/newupdate.RootKeys=$ROOT_KEYS -X shrm-updater-backend/internal/newupdate.EmbeddedKeyset=$KEYSET_B64 -X shrm-updater-backend/internal/newupdate.EmbeddedKeysetSig=$KEYSET_SIG"
fi
mkdir -p out

echo '== updater.so'
UPD_LD="-buildid= $NEWUPD"
[ -n "${MANIFEST_KEYS:-}" ] && UPD_LD="$UPD_LD -X shrm-updater-backend/internal/sigverify.TrustedKeys=$MANIFEST_KEYS"
(cd updater-backend && go build -tags production -buildmode=c-shared -trimpath -buildvcs=false -ldflags "$UPD_LD" -o ../out/updater.so .)

echo '== backend.so'
BE_LD='-buildid='
if [ -n "${ROOT_KEYS:-}" ]; then
  BE_LD="$BE_LD -X shaurma-launcher-backend/internal/access.RootKeys=$ROOT_KEYS -X shaurma-launcher-backend/internal/access.EmbeddedKeyset=$KEYSET_B64 -X shaurma-launcher-backend/internal/access.EmbeddedKeysetSig=$KEYSET_SIG"
fi
if [ -n "${GOOGLE_ID:-}" ]; then
  BE_LD="$BE_LD -X main.shaurmaGoogleClientID=$GOOGLE_ID"
  [ -n "${GOOGLE_SECRET:-}" ] && BE_LD="$BE_LD -X main.shaurmaGoogleClientSecret=$GOOGLE_SECRET"
fi
[ -n "${CF_KEY:-}" ] && BE_LD="$BE_LD -X shaurma-launcher-backend/internal/custompack.curseForgeAPIKey=$CF_KEY"
[ -n "${GEMINI_KEY:-}" ] && BE_LD="$BE_LD -X shaurma-launcher-backend/internal/ai.DefaultAPIKey=$GEMINI_KEY"
[ -n "${GROQ_KEY:-}" ] && BE_LD="$BE_LD -X shaurma-launcher-backend/internal/ai.DefaultGroqAPIKey=$GROQ_KEY"
(cd backend && go build -tags production,shaurma -buildmode=c-shared -trimpath -buildvcs=false -ldflags "$BE_LD" -o ../out/backend.so .)

echo '== shell'
SH_LD='-buildid= -X main.editionChannel=shaurma'
[ -n "${MANIFEST_KEYS:-}" ] && SH_LD="$SH_LD -X main.trustedKeys=$MANIFEST_KEYS"
[ -n "${ROOT_KEYS:-}" ] && SH_LD="$SH_LD -X main.rootKeys=$ROOT_KEYS -X main.embeddedKeyset=$KEYSET_B64 -X main.embeddedKeysetSig=$KEYSET_SIG"
(cd shell && go build -tags production -trimpath -buildvcs=false -ldflags "$SH_LD" -o ../out/ShaurmLauncher .)
rm -f out/*.h

# Усі бібліотеки shell мають бути в рантаймі (інакше застосунок не стартує).
missing=$(ldd out/ShaurmLauncher | grep 'not found' || true)
if [ -n "$missing" ]; then
  echo "ПОМИЛКА: бракує бібліотек у org.gnome.Platform//50:"; echo "$missing"; exit 1
fi

echo '== install'
install -Dm755 out/ShaurmLauncher /app/bin/ShaurmLauncher
SEED=/app/shaurma-seed
install -Dm644 out/backend.so "$SEED/backend.so"
install -Dm644 out/updater.so "$SEED/updater.so"
mkdir -p "$SEED/frontend" "$SEED/updater-frontend"
# unzip повертає 1 на попередження (zip з Windows може мати «\» у шляхах — unzip сам їх виправляє).
unz() { unzip -q -o "$1" -d "$2" || [ $? -eq 1 ]; }
unz frontend.zip "$SEED/frontend"
unz updater-frontend.zip "$SEED/updater-frontend"
[ -f "$SEED/frontend/index.html" ] || { echo 'ПОМИЛКА: у frontend.zip немає index.html'; exit 1; }
[ -f "$SEED/updater-frontend/index.html" ] || { echo 'ПОМИЛКА: у updater-frontend.zip немає index.html'; exit 1; }

# Локальний маніфест запеченої версії: updater бачить, що версія вже стоїть,
# а shell після оновлення пакета розкладає новий набір (flatpak_seed_linux.go).
be_sum=$(sha256sum out/backend.so | cut -d' ' -f1); be_size=$(stat -c %s out/backend.so)
up_sum=$(sha256sum out/updater.so | cut -d' ' -f1); up_size=$(stat -c %s out/updater.so)
cat > "$SEED/launcher-version.json" <<EOF
{
  "version": "$VERSION",
  "releaseNotes": "",
  "components": [
    {"path": "backend.so", "sha256": "$be_sum", "size": $be_size, "url": "/backend.so"},
    {"path": "updater.so", "sha256": "$up_sum", "size": $up_size, "url": "/updater.so"}
  ]
}
EOF

install -Dm644 com.shaurma.Launcher.desktop /app/share/applications/com.shaurma.Launcher.desktop
install -Dm644 shaurma-icon.png /app/share/icons/hicolor/256x256/apps/com.shaurma.Launcher.png
install -Dm644 com.shaurma.Launcher.metainfo.xml /app/share/metainfo/com.shaurma.Launcher.metainfo.xml
sed -i "s/@VERSION@/$VERSION/g; s/@DATE@/$(date -u +%Y-%m-%d)/g" /app/share/metainfo/com.shaurma.Launcher.metainfo.xml
