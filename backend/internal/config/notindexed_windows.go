//go:build windows

package config

import (
	"golang.org/x/sys/windows"
)

// MarkNotIndexed ставить на теці атрибут FILE_ATTRIBUTE_NOT_CONTENT_INDEXED
// (0x2000): Windows Search перестає індексувати вміст теки. Це фікс
// «провідник лагає під час встановлення збірки»: при розпаковці/синку
// лаунчер створює тисячі дрібних файлів (моди, асети), і Search-індексатор
// намагається обробити кожен — Explorer від цього підвисає на десятки
// секунд. Атрибут на теці даних лаунчера (збірки, java, кеш) не потрібен
// для пошуку користувачу — це робочі файли, не документи.
func MarkNotIndexed(dir string) {
	if dir == "" {
		return
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return
	}
	// GetFileAttributes + SetFileAttributes: читаємо поточні атрибути,
	// додаємо біт і записуємо назад (SetFileAttributes перезаписує ВСЕ).
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return
	}
	if attrs&windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED != 0 {
		return // уже стоїть — не пишемо зайвий раз
	}
	_ = windows.SetFileAttributes(p, attrs|windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED)
}
