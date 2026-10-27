//go:build linux

package main

// app_identity_linux.go — ім'я застосунку для application.New на Linux.
//
// На Linux від Options.Name напряму залежить GTK application-id
// (`org.wails.<sanitizeAppName(Name)>`), а від нього — ім'я .desktop-файлу,
// за яким GNOME/KDE знаходять іконку для вікна (деталі — в app_identity.go).
// Options.Name сам НІКОЛИ не показується користувачу як текст (це лише
// вхід для sanitizeAppName); видиму назву користувач бачить із Name= у
// самому .desktop-файлі (пакується окремо, pack-manager/.../paths.go) та з
// Options.Description (задається окремо в main.go). Тому тут повертається
// ТЕХНІЧНИЙ токен редакції (linuxAppIDToken*, див. app_identity.go), а не
// людяна назва — так дві редакції мають різний application-id (потрібно
// для співіснування двох .desktop-файлів на диску), не показуючи
// користувачу жодного тексту "Base"/"повна" ніде в UI.
func appOptionsName() string {
	if currentChannel() == "base" {
		return linuxAppIDTokenBase
	}
	return linuxAppIDTokenShaurma
}
