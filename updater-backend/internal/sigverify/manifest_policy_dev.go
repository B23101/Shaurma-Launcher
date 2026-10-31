//go:build !production

package sigverify

// Unsigned fixtures are supported only in non-production builds.
const requireManifestSignature = false
