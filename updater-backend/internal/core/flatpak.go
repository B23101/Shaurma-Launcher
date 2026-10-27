package core

// flatpak.go — Flatpak-специфіка самоновлення лаунчера.
//
// У Flatpak сам shell (ShaurmLauncher) лежить у read-only /app/bin і
// оновлюється ТІЛЬКИ через `flatpak update` — разом із runtime і
// бібліотеками. Тому апдейтер не повинен його чіпати, і ось чому це
// важливо:
//
//   - компонент shell є в launcher-version.json (він же self-update для
//     Windows/AppImage/DEB/RPM), тож Diff() включає його при кожному
//     релізі shell — це зайві ~15 МБ з CDN;
//   - завантажена копія кладеться в теку даних, але НІКОЛИ не
//     запускається: Flatpak завжди стартує /app/bin/ShaurmLauncher;
//   - install.Apply() для заблокованого файлу пише <name>.new, а shell
//     при наступному старті намагається перейменувати /app/bin/... —
//     в образі, змонтованому read-only, це гарантовано fail.
//
// Поза Flatpak (Windows/AppImage/DEB/RPM) поведінка не змінюється:
// /.flatpak-info там не існує, і фільтр нічого не прибирає.

import (
	"os"
	"path/filepath"
	"strings"

	"shrm-updater-backend/internal/manifest"
)

// flatpakInfoPath — маркер, який створює сам flatpak у контейнері (не
// застосунок). Той самий детект, що shell/flatpak_seed_linux.go:
// runningInsideFlatpak і internal/autostart/autostart_linux.go.
const flatpakInfoPath = "/.flatpak-info"

// runningInFlatpak — чи процес виконується всередині Flatpak-пісочниці.
func runningInFlatpak() bool {
	_, err := os.Stat(flatpakInfoPath)
	return err == nil
}

// dropFlatpakShellComponent прибирає зі списку компонентів сам shell, якщо
// апдейтер працює у Flatpak. Решту (backend.so, updater.so, frontend,
// updater-frontend) оновлюємо як звичайно — вони лежать у writable-теці
// даних усередині пісочниці.
func dropFlatpakShellComponent(list []manifest.Component, ch Channel) []manifest.Component {
	if !runningInFlatpak() {
		return list
	}
	return dropComponentByBaseName(list, launcherExeName(ch))
}

// dropComponentByBaseName — чиста частина фільтра (без звернень до
// файлової системи), щоб її можна було покрити тестом.
func dropComponentByBaseName(list []manifest.Component, base string) []manifest.Component {
	if len(list) == 0 || base == "" {
		return list
	}
	want := strings.ToLower(base)
	out := make([]manifest.Component, 0, len(list))
	for _, c := range list {
		if strings.ToLower(filepath.Base(filepath.FromSlash(c.Path))) == want {
			continue
		}
		out = append(out, c)
	}
	return out
}
