//go:build !linux

package main

// app_identity_other.go — ім'я застосунку для application.New на
// Windows/macOS: там іконку/групування визначає сам exe (ресурс + маніфест,
// AppUserModelID з шляху інсталяції), тому поведінка навмисно лишається
// такою, як була — одна назва на обидві редакції. Редакційна назва потрібна
// ЛИШЕ на Linux, де ім'я застосунку стає application-id (див.
// app_identity.go/app_identity_linux.go).
func appOptionsName() string {
	return launcherDisplayNameShaurma
}
