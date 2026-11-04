//go:build production

package sigverify

// Production-збірка ЗАВЖДИ вимагає підпис маніфесту (план «Єдиний лаунчер», §0 п.1):
// без вшитих ключів непідписаний маніфест відхиляється, а не приймається мовчки.
// Узгоджено з launcherbuild.RequireSignedManifests=true у pack-manager.
const requireManifestSignature = true
