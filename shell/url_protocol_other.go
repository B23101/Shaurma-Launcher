//go:build !windows

package main

// На Linux схему shaurma:// реєструє .desktop-файл пакета (MimeType=x-scheme-handler/shaurma).
func ensureURLProtocol() bool { return false }
