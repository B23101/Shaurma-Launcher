//go:build !windows

// Package authenticode — поза Windows підпису PE немає: цілісність файлів
// тримає SHA-256 з підписаного маніфесту.
package authenticode

import "shaurma-trust"

func Verify(path string, signers []trust.AuthenticodeSigner) error { return nil }
