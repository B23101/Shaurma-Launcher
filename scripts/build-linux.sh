#!/usr/bin/env bash
# Shaurma Launcher - Linux build (no secrets in this file).
# Builds, per edition: shell (GTK4/WebKitGTK 6.0), backend.so, updater.so + frontend zips,
# then packs dist/linux-release/shaurma-launcher-linux-<edition>.tar.gz and SHA256SUMS-linux.txt.
#
# Needs: Go (version from backend/go.mod), Node 20+, build-essential, pkg-config,
#        libgtk-4-dev, libwebkitgtk-6.0-dev   (sudo apt install ...)
# Secrets come ONLY from environment variables, same names as scripts/build-windows.ps1:
#   SHRM_BUILD_TOKEN_SHAURMA SHRM_BUILD_TOKEN_BASE CF_API_KEY GROQ_API_KEY GEMINI_API_KEY
#   SHAURMA_GOOGLE_CLIENT_ID SHAURMA_GOOGLE_CLIENT_SECRET SHAURMA_MANIFEST_PUBKEYS
# Usage: bash scripts/build-linux.sh [shaurma|base ...]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist/linux"
REL="$ROOT/dist/linux-release"
EDITIONS=("$@"); [ ${#EDITIONS[@]} -eq 0 ] && EDITIONS=(shaurma base)

TOKEN_SHAURMA="${SHRM_BUILD_TOKEN_SHAURMA:-}"
TOKEN_BASE="${SHRM_BUILD_TOKEN_BASE:-}"
PUBKEYS="${SHAURMA_MANIFEST_PUBKEYS:-}"
if [[ -n "$PUBKEYS" && ! "$PUBKEYS" =~ ^[A-Za-z0-9+/=,]+$ ]]; then
  echo "SHAURMA_MANIFEST_PUBKEYS: only base64 keys separated by commas" >&2; exit 1
fi
for v in SHRM_BUILD_TOKEN_SHAURMA SHRM_BUILD_TOKEN_BASE CF_API_KEY GROQ_API_KEY GEMINI_API_KEY SHAURMA_GOOGLE_CLIENT_ID; do
  [ -z "${!v:-}" ] && echo "warning: $v is not set - the matching feature is disabled in this build" >&2
done

token_for() { [ "$1" = base ] && echo "$TOKEN_BASE" || echo "$TOKEN_SHAURMA"; }
declare -A SHELL_NAME=([shaurma]=ShaurmLauncher [base]=ShaurmLauncherBase)

rm -rf "$DIST" "$REL"; mkdir -p "$DIST" "$REL"

echo "== frontend"
npm_flags=(--no-audit --no-fund --progress=false --loglevel=error)
( cd "$ROOT/frontend"
  npm ci "${npm_flags[@]}" || { echo "warning: npm ci failed (lock file out of sync) - using npm install" >&2; npm install "${npm_flags[@]}"; }
  npm run build )
( cd "$ROOT/frontend/dist" && zip -q -r "$DIST/frontend.zip" . )
( cd "$ROOT/updater-frontend" && zip -q -r "$DIST/updater-frontend.zip" . )

# updater.so is shared by both editions
UPD_LD="-X shrm-updater-backend/internal/core.tokenShaurma=$TOKEN_SHAURMA -X shrm-updater-backend/internal/core.tokenBase=$TOKEN_BASE -X shrm-updater-backend/internal/sigverify.TrustedKeys=$PUBKEYS"
echo "== updater.so"
( cd "$ROOT/updater-backend" && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -trimpath -buildvcs=false -ldflags "$UPD_LD" -o "$DIST/updater.so" . )
rm -f "$DIST/updater.h"

for ed in "${EDITIONS[@]}"; do
  out="$DIST/$ed"; mkdir -p "$out"
  tok="$(token_for "$ed")"

  echo "== shell ($ed)"
  ( cd "$ROOT/shell" && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -tags production -trimpath -buildvcs=false \
      -ldflags "-X main.editionChannel=$ed -X main.cdnToken=$tok -X main.trustedKeys=$PUBKEYS" \
      -o "$out/${SHELL_NAME[$ed]}" . )

  echo "== backend.so ($ed)"
  ld="-X shaurma-launcher-backend/internal/update.token=$tok"
  tags=()
  if [ "$ed" = shaurma ]; then
    tags=(-tags shaurma)
    ld+=" -X shaurma-launcher-backend/internal/api.tokenShaurma=$TOKEN_SHAURMA"
    if [ -n "${SHAURMA_GOOGLE_CLIENT_ID:-}" ]; then
      ld+=" -X main.shaurmaGoogleClientID=$SHAURMA_GOOGLE_CLIENT_ID"
      [ -n "${SHAURMA_GOOGLE_CLIENT_SECRET:-}" ] && ld+=" -X main.shaurmaGoogleClientSecret=$SHAURMA_GOOGLE_CLIENT_SECRET"
    fi
  fi
  [ -n "${CF_API_KEY:-}" ]     && ld+=" -X shaurma-launcher-backend/internal/custompack.curseForgeAPIKey=$CF_API_KEY"
  [ -n "${GROQ_API_KEY:-}" ]   && ld+=" -X shaurma-launcher-backend/internal/ai.DefaultGroqAPIKey=$GROQ_API_KEY"
  [ -n "${GEMINI_API_KEY:-}" ] && ld+=" -X shaurma-launcher-backend/internal/ai.DefaultAPIKey=$GEMINI_API_KEY"
  ( cd "$ROOT/backend" && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build "${tags[@]}" -buildmode=c-shared -trimpath -buildvcs=false -ldflags "$ld" -o "$out/backend.so" . )
  rm -f "$out/backend.h"

  cp "$DIST/updater.so" "$DIST/frontend.zip" "$DIST/updater-frontend.zip" "$out/"
  chmod +x "$out/${SHELL_NAME[$ed]}"
  tar -C "$out" -czf "$REL/shaurma-launcher-linux-$ed.tar.gz" .
done

( cd "$REL" && sha256sum ./*.tar.gz | sed 's#\./##' > SHA256SUMS-linux.txt )
echo; echo "Done: $REL"; ls -la "$REL"
