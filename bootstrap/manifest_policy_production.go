//go:build production

package main

// Перевірка підпису маніфесту вмикається ЛИШЕ коли вшито ключі (TrustedKeys). Без ключів — не перевіряється
// (узгоджено з launcherbuild.RequireSignedManifests=false у pack-manager).
const requireManifestSignature = false
