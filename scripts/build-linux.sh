#!/usr/bin/env bash
# Shaurma Launcher - Linux build (no secrets in this file).
# Builds, per edition: shell (GTK4/WebKitGTK 6.0), backend.so, updater.so + frontend zips,
# then packs dist/linux-release/shaurma-launcher-linux-<edition>.tar.gz and SHA256SUMS-linux.txt.
#
# Needs: Go (version from backend/go.mod), Node 20+, build-essential, pkg-config,
#        libgtk-4-dev, libwebkitgtk-6.0-dev   (sudo apt install ...)
# Secrets come ONLY from environment variables, same names as scripts/build-windows.ps1:
#   SHRM_BUILD_TOKEN_SHAURMA SHRM_BUILD_TOKEN_BASE CF_API_KEY
#   SHAURMA_GOOGLE_CLIENT_ID SHAURMA_GOOGLE_CLIENT_SECRET SHAURMA_MANIFEST_PUBKEYS
# Usage: bash scripts/build-linux.sh [shaurma|base ...]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist/linux"
REL="$ROOT/dist/linux-release"
EDITIONS=("$@"); [ ${#EDITIONS[@]} -eq 0 ] && EDITIONS=(base)

TOKEN_SHAURMA="${SHRM_BUILD_TOKEN_SHAURMA:-}"
TOKEN_BASE="${SHRM_BUILD_TOKEN_BASE:-}"
PUBKEYS="${SHAURMA_MANIFEST_PUBKEYS:-}"
if [[ -n "${SHAURMA_GOOGLE_CLIENT_SECRET:-}" && "${SHAURMA_GOOGLE_CLIENT_KIND:-}" != desktop ]]; then
  echo 'Only public Desktop OAuth client credentials may be embedded; verify SHAURMA_GOOGLE_CLIENT_KIND' >&2; exit 1
fi
if [[ -n "$PUBKEYS" && ! "$PUBKEYS" =~ ^[A-Za-z0-9+/=,]+$ ]]; then
  echo "SHAURMA_MANIFEST_PUBKEYS: only base64 keys separated by commas" >&2; exit 1
fi
[[ -n "$PUBKEYS" ]] || { echo 'SHAURMA_MANIFEST_PUBKEYS is required' >&2; exit 1; }
IFS=',' read -ra manifest_keys <<< "$PUBKEYS"
for key in "${manifest_keys[@]}"; do
  decoded_size=$(printf '%s' "$key" | base64 --decode | wc -c)
  [[ "$decoded_size" -eq 32 ]] || { echo 'Invalid Ed25519 public key' >&2; exit 1; }
done
[[ "$PUBKEYS" != *, ]] || { echo 'Empty manifest public key' >&2; exit 1; }
for ed in "${EDITIONS[@]}"; do
  [[ "$ed" = base || "$ed" = shaurma ]] || { echo 'Unknown edition' >&2; exit 1; }
  if [[ "$ed" = base ]]; then required_token="$TOKEN_BASE"; else required_token="$TOKEN_SHAURMA"; fi
  [[ -n "$required_token" ]] || { echo "Missing CDN token for $ed" >&2; exit 1; }
done

token_for() { [ "$1" = base ] && echo "$TOKEN_BASE" || echo "$TOKEN_SHAURMA"; }
declare -A SHELL_NAME=([shaurma]=ShaurmLauncher [base]=ShaurmLauncherBase)

rm -rf "$DIST" "$REL"; mkdir -p "$DIST" "$REL"

echo "== frontend"
npm_flags=(--no-audit --no-fund --progress=false --loglevel=error)
( cd "$ROOT/frontend"
  npm ci "${npm_flags[@]}"
  npm run build )
pwsh -NoProfile -File "$ROOT/scripts/collect-licenses.ps1" -Destination "$ROOT/frontend/dist/licenses/dependencies"
( cd "$ROOT/frontend/dist" && zip -q -r "$DIST/frontend.zip" . )
( cd "$ROOT/updater-frontend" && zip -q -r "$DIST/updater-frontend.zip" . )

for ed in "${EDITIONS[@]}"; do
  out="$DIST/$ed"; mkdir -p "$out"
  tok="$(token_for "$ed")"
  token_field=tokenBase; [[ "$ed" = shaurma ]] && token_field=tokenShaurma
  upd_ld="-X shrm-updater-backend/internal/core.$token_field=$tok -X shrm-updater-backend/internal/sigverify.TrustedKeys=$PUBKEYS"
  echo "== updater.so ($ed)"
  ( cd "$ROOT/updater-backend" && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -tags production -buildmode=c-shared -trimpath -buildvcs=false -ldflags "$upd_ld" -o "$out/updater.so" . )
  rm -f "$out/updater.h"

  echo "== shell ($ed)"
  ( cd "$ROOT/shell" && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -tags production -trimpath -buildvcs=false \
      -ldflags "-X main.editionChannel=$ed -X main.cdnToken=$tok -X main.trustedKeys=$PUBKEYS" \
      -o "$out/${SHELL_NAME[$ed]}" . )

  echo "== backend.so ($ed)"
  ld="-X shaurma-launcher-backend/internal/update.token=$tok"
  tags=(-tags production)
  if [ "$ed" = shaurma ]; then
    tags=(-tags production,shaurma)
    ld+=" -X shaurma-launcher-backend/internal/api.tokenShaurma=$TOKEN_SHAURMA"

  fi
if [ -n "${SHAURMA_GOOGLE_CLIENT_ID:-}" ]; then
      ld+=" -X main.shaurmaGoogleClientID=$SHAURMA_GOOGLE_CLIENT_ID"
      [ -n "${SHAURMA_GOOGLE_CLIENT_SECRET:-}" ] && ld+=" -X main.shaurmaGoogleClientSecret=$SHAURMA_GOOGLE_CLIENT_SECRET"
    fi
  [ -n "${CF_API_KEY:-}" ]     && ld+=" -X shaurma-launcher-backend/internal/custompack.curseForgeAPIKey=$CF_API_KEY"
  ( cd "$ROOT/backend" && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build "${tags[@]}" -buildmode=c-shared -trimpath -buildvcs=false -ldflags "$ld" -o "$out/backend.so" . )
  rm -f "$out/backend.h"

  cp "$DIST/frontend.zip" "$DIST/updater-frontend.zip" "$out/"
  chmod +x "$out/${SHELL_NAME[$ed]}"
  tar -C "$out" -czf "$REL/shaurma-launcher-linux-$ed.tar.gz" .
done

( cd "$REL" && sha256sum ./*.tar.gz | sed 's#\./##' > SHA256SUMS-linux.txt )
echo; echo "Done: $REL"; ls -la "$REL"
