package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// appVersion — версія лаунчера (фолбек, коли launcher-version.json поруч
// з exe недоступний — напр. dev-збірка в build\bin). Основний шлях —
// читати версію з launcher-version.json при старті, щоб версіонована
// тека WebView2-профілю не вимагала ручної синхронізації з -Version у
// build.ps1. (Перенесено з launcher/main.go — backend.dll теж потребує
// версію для update.Checker та GetVersion.)
const appVersion = "2.0.0"

// readLauncherVersion читає version з launcher-version.json у installDir
// — РЕАЛЬНІЙ теці встановлення, яку передає shell через backend_init
// (backendInitConfig.InstallDir), а НЕ через os.Executable().
//
// ВАЖЛИВО (баг, який ця сигнатура назавжди усуває): на Windows
// filepath.Dir(os.Executable()) і installDir — одна й та сама тека, тому
// стара реалізація "os.Executable() + launcher-version.json поруч"
// випадково працювала. На Linux (AppImage) це не так: AppImage
// монтується як READ-ONLY образ через FUSE в /tmp/.mount_XXXXXX/, а
// installDir свідомо відв'язана від цього шляху й резолвиться в XDG
// data-теку користувача (див. shell/exedir_linux.go, writableDataDir) —
// саме туди updater пише актуальний launcher-version.json. Читання файлу
// через os.Executable() на Linux завжди провалювалось (файлу там нема),
// readLauncherVersion() мовчки поверталась до жорстко вшитого appVersion
// = "2.0.0", і саме це бачив користувач у вкладці «Про лаунчер» навіть
// після встановлення 2.5.0 — не тому, що версія не оновилась, а тому, що
// UI дивився не в ту теку. Приймаючи installDir параметром (той самий,
// що вже правильно обчислений у shell і переданий через backend_init),
// ця функція завжди дивиться в те саме місце, що й апдейтер — на будь-
// якій платформі, без платформо-залежних гілок тут.
func readLauncherVersion(installDir string) string {
	if installDir == "" {
		return appVersion
	}
	data, err := os.ReadFile(filepath.Join(installDir, "launcher-version.json"))
	if err != nil {
		return appVersion
	}
	// build.ps1/installer пишуть файл з UTF-8 BOM (U+FEFF), а Go
	// json.Unmarshal на BOM падає — прибираємо префікс до парсингу.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &m) != nil || m.Version == "" {
		return appVersion
	}
	return m.Version
}
