//go:build production

package sigverify

// Release binaries never accept unsigned update manifests, even without keys.
const requireManifestSignature = true
