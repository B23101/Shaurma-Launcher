package main

import "shaurma-launcher-backend/internal/jvmargs"

// ValidateJvmArgs — для редактора JVM-аргументів: повертає аргументи, які
// запуск відхилить (з причиною). Та сама логіка, що й перед стартом гри
// (jvmargs.SanitizeUser), тож підсвітка в UI не розходиться з реальністю.
func (a *App) ValidateJvmArgs(raw string) []jvmargs.Rejection {
	_, rejected := jvmargs.SanitizeUser(raw)
	if rejected == nil {
		return []jvmargs.Rejection{}
	}
	return rejected
}
