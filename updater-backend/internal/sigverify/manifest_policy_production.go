//go:build production

package sigverify

// Перевірка підпису маніфесту вмикається ЛИШЕ коли вшито ключі (TrustedKeys). Без ключів — не перевіряється
// (узгоджено з launcherbuild.RequireSignedManifests=false у pack-manager).
const requireManifestSignature = false
