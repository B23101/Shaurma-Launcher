package main

// components.go — платформозалежні імена файлів-компонентів, які shell
// шукає/якає поруч із собою (installDir): backend і updater.
//
// НАВІЩО ОКРЕМИЙ ФАЙЛ: до цього shell буквально шукав рядки "backend.dll"
// і "updater.dll" у restart/bootstrap/updater_phase — на Windows це
// правильно, але на Linux ці ж компоненти збираються як backend.so/
// updater.so (-buildmode=c-shared природно дає розширення .so поза
// Windows, той самий підхід, що вже застосований у
// updater-backend/internal/manifest.UpdaterLibraryName). Через буквальний
// хардкод shell на Linux ніколи не знаходив жодного з двох модулів: сам
// dlopen у bridge_host_linux.go працює коректно і приймає БУДЬ-ЯКЕ ім'я
// файлу (розширення для нього не має значення), але виклики
// filepath.Join(exeDir(), "backend.dll") вище рівнем підставляли ім'я з
// хвостом .dll навіть на Linux, тому dlopen отримував шлях до файлу, якого
// на диску просто нема (там лежить backend.so).
//
// Використовуються всюди в shell (updater_phase.go, bootstrap.go), де
// раніше стояли рядкові літерали "backend.dll"/"updater.dll".
import "runtime"

// backendLibraryName — ім'я файлу backend-модуля для поточної ОС:
// "backend.dll" на Windows, "backend.so" на Linux. Дзеркалить
// updater-backend/internal/core.backendLibraryName — той самий контракт,
// той самий файл фізично на диску (апдейтер якає компонент під цим
// ім'ям, shell лише відкриває вже завантажений файл через dlopen).
func backendLibraryName() string {
	if runtime.GOOS == "windows" {
		return "backend.dll"
	}
	return "backend.so"
}

// updaterLibraryName — ім'я файлу updater-модуля для поточної ОС:
// "updater.dll" на Windows, "updater.so" на Linux. Дзеркалить
// updater-backend/internal/manifest.UpdaterLibraryName.
func updaterLibraryName() string {
	if runtime.GOOS == "windows" {
		return "updater.dll"
	}
	return "updater.so"
}

// installerName — ім'я встановлювача лаунчера для поточної ОС, для тексту
// сторінки фатальної помилки ("Перевстановіть лаунчер заново (...)").
// На Windows це підписаний NSIS-інсталятор (setup.exe); на Linux
// офіційного одного файлу немає — просимо повторити той спосіб
// встановлення, яким лаунчер поставили (AppImage/пакет/install.sh),
// тому формулювання нейтральне, а не конкретний неіснуючий бінарник.
func installerName() string {
	if runtime.GOOS == "windows" {
		return "setup.exe"
	}
	return "встановлювач лаунчера для Linux"
}

// platformSegment — сегмент шляху URL для запитів до CDN, що залежить від
// ОС: "" (порожньо, зворотна сумісність) на Windows, "linux/" на Linux.
// ТОЧНА КОПІЯ updater-backend/internal/manifest.PlatformSegment — не можна
// імпортувати той пакет напряму (internal-пакет чужого Go-модуля,
// shell — окремий module shaurma-shell), тож контракт продубльовано тут.
// Обидві копії МАЮТЬ лишатись синхронними: зміниш одну — онови другу,
// інакше bootstrap (цей файл) і апдейтер запитуватимуть різні шляхи
// маніфесту на CDN.
func platformSegment() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return runtime.GOOS + "/"
}

// logsDirHint — текстова підказка шляху до логів лаунчера для повідомлень
// про фатальну помилку (showFatalError). Дзеркалить
// backend/internal/config.baseDir: %APPDATA%\.shaurm\logs на Windows,
// ~/.shaurm/logs на Linux (і на macOS, для якого окремого рядка тут
// немає — не наш білд-таргет, але той самий ~/.shaurm-фолбек не зашкодить).
func logsDirHint() string {
	if runtime.GOOS == "windows" {
		return `%APPDATA%\.shaurm\logs`
	}
	return "~/.shaurm/logs"
}
