//go:build !production

package main

// Unsigned fixtures are supported only in non-production builds.
const requireManifestSignature = false
