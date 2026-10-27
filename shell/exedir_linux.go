//go:build linux

package main

import (
	"os"
	"path/filepath"
)

// linuxDataDirName — ім'я теки під XDG_DATA_HOME, куди на Linux пишуться
// докачані компоненти (updater.so/backend.so/frontend/index.html).
//
// РАНІШЕ це була константа "ShaurmLauncher" для ОБОХ редакцій — Base і
// звичайний Shaurma Launcher, встановлені одночасно на одній Linux-машині
// (два різні .deb/.rpm/AppImage, різні бінарники ShaurmLauncher /
// ShaurmLauncherBase), писали докачані .so/фронтенд в ОДНУ й ту саму
// ~/.local/share/ShaurmLauncher — редакції змішували дані одна одної
// (та й видалення одного пакета стирало б дані іншого). На Windows цього
// не було: там installDirName() (main.go) бере ім'я теки встановлення, і
// дві редакції фізично живуть у різних теках.
//
// Тепер визначаємо ім'я по базовому імені запущеного бінарника
// (ShaurmLauncher / ShaurmLauncherBase — див. linuxExeNames у
// pack-manager/internal/launcherbuild/paths.go, звідти й береться реальне
// ім'я ELF-файлу при пакуванні), аналогічно installDirName() у main.go.
func linuxDataDirName() string {
	exe, err := os.Executable()
	if err != nil {
		return "ShaurmLauncher"
	}
	base := filepath.Base(exe)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "ShaurmLauncher"
	}
	return base
}

// exeDir — записувана installDir на Linux.
//
// На Windows exeDir() = тека, де фізично лежить .exe: інсталятор кладе
// туди все і в неї ж є права на запис. На Linux типовий спосіб доставки —
// AppImage, який ОС монтує через FUSE як read-only образ у
// /tmp/.mount_XXXXXX/usr/bin/. Якщо installDir лишити рівним "текою
// бінарника" (як робив старий спільний exeDir()), Apply() з
// internal/install намагається створити backend.so.part саме там і
// падає з "read-only file system" — це і є причина помилки "Компонент
// updater.so не завантажився. Швидше за все, встановлення було
// перервано або файл пошкоджено": файл насправді ніколи не був
// пошкоджений, апдейтер просто фізично не міг його записати.
//
// Тому на Linux installDir свідомо ВІДВ'ЯЗАНА від того, звідки запущено
// бінарник, і завжди резолвиться в XDG data-теку користувача:
//
//	$XDG_DATA_HOME/ShaurmLauncher          (якщо XDG_DATA_HOME заданий)
//	~/.local/share/ShaurmLauncher          (типовий XDG-дефолт)
//	<тека поточного робочого каталогу>     (крайній fallback, якщо навіть
//	                                         os.UserHomeDir() недоступний —
//	                                         краще працювати з чимось
//	                                         записуваним, ніж падати)
//
// Це той самий підхід, яким користуються Steam/Discord AppImage-збірки:
// сам .AppImage — незмінний read-only артефакт, а дані й самооновлення
// живуть окремо в домашній теці користувача.
func exeDir() string {
	dir, err := writableDataDir()
	if err != nil {
		// Останній fallback: тека поточного процесу. Гірше за XDG (може
		// не мати прав або зникнути між запусками), але краще, ніж
		// повертати текою AppImage-монтування, яка гарантовано
		// read-only і зробить будь-яке оновлення неможливим.
		if wd, wdErr := os.Getwd(); wdErr == nil {
			return wd
		}
		return "."
	}
	return dir
}

// writableDataDir повертає (і за потреби створює) XDG data-теку
// лаунчера. Винесено окремою функцією, щоб #bootstrap і сам updater
// могли її перевірити незалежно від фолбеку в exeDir().
func writableDataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, linuxDataDirName())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// appImagePath повертає шлях до самого .AppImage-файлу (не до
// змонтованої read-only копії в /tmp), якщо процес запущено саме так.
// AppImage runtime виставляє змінну середовища APPIMAGE з абсолютним
// шляхом оригінального файлу ще до монтування — на відміну від
// os.Executable(), який після монтування вказує всередину
// /tmp/.mount_XXXXXX/. Поки що не використовується напряму в Apply(),
// але потрібна, якщо колись знадобиться self-update самого .AppImage
// (аналог applyShellSelfUpdate() на Windows) — на Windows self-update
// перейменовує сам .exe, на Linux еквівалент мусить замінювати файл за
// ЦИМ шляхом, а не всередині тимчасового монтування.
func appImagePath() string {
	return os.Getenv("APPIMAGE")
}
