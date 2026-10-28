//go:build !windows

package main

// processPrivateBytes — на інших ОС не міряємо (0 = невідомо): звіт покаже лише Go-частину.
func processPrivateBytes() uint64 { return 0 }
