//go:build production

package main

// Release binaries never accept unsigned update manifests, even without keys.
const requireManifestSignature = true
