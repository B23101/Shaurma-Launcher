package core

// flatpak_update.go — оновлення лаунчера у Flatpak цілим пакетом.
//
// У Flatpak shell, backend.so, updater.so і фронтенд запечені в образ
// (/app). Тому для Linux реліз містить один файл — ShaurmaLauncher.flatpak
// (роль "other", платформа linux-amd64), а його sha256 підписано в маніфесті
// так само, як файли Windows. Updater качає пакет (download перевіряє
// sha256 і розмір), просить flatpak на хості перевстановити застосунок і
// перезапускає лаунчер. Дані користувача (~/.var/app/<id>) лишаються: flatpak
// --reinstall їх не чіпає. Нова версія при першому старті сама розкладає свій
// запечений набір (shell/flatpak_seed_linux.go).
//
// Для доступу до flatpak на хості пакет має дозвіл
// --talk-name=org.freedesktop.Flatpak (flatpak/com.shaurma.Launcher.yml).
// Поза Flatpak (AppImage тощо) пакет ніколи не качається.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"shrm-updater-backend/internal/manifest"
)

// isFlatpakBundle — компонент є пакетом Flatpak (увесь лаунчер для Linux).
func isFlatpakBundle(c manifest.Component) bool {
	return strings.HasSuffix(strings.ToLower(c.Path), ".flatpak")
}

// filterFlatpakBundle — у Flatpak качаємо ЛИШЕ пакет (решта всередині нього),
// поза Flatpak — усе, крім пакета.
func filterFlatpakBundle(list []manifest.Component, inFlatpak bool) []manifest.Component {
	out := make([]manifest.Component, 0, len(list))
	for _, c := range list {
		if isFlatpakBundle(c) == inFlatpak {
			out = append(out, c)
		}
	}
	return out
}

var flatpakAppIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_-]+){2,}$`)

// flatpakInfo — що з /.flatpak-info потрібно оновлювачу.
type flatpakInfo struct {
	AppID  string
	System bool // застосунок стоїть у системній інсталяції (/var/lib/flatpak)
}

// parseFlatpakInfo читає ключі name= ([Application]) і app-path= ([Instance]).
func parseFlatpakInfo(text string) (flatpakInfo, error) {
	var info flatpakInfo
	section := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch {
		case section == "[Application]" && k == "name":
			info.AppID = v
		case section == "[Instance]" && k == "app-path":
			info.System = !strings.Contains(v, "/.local/share/flatpak/")
		}
	}
	if !flatpakAppIDRe.MatchString(info.AppID) {
		return info, fmt.Errorf("невідомий app-id Flatpak %q", info.AppID)
	}
	return info, nil
}

// flatpakInstallArgs — аргументи flatpak-spawn для перевстановлення пакета.
func flatpakInstallArgs(info flatpakInfo, bundle string) []string {
	scope := "--user"
	if info.System {
		scope = "--system" // flatpak сам попросить пароль через polkit
	}
	return []string{"--host", "flatpak", "install", scope, "--noninteractive", "-y", "--reinstall", "--bundle", bundle}
}

// installFlatpakBundle — перевстановлює застосунок із завантаженого пакета
// (sha256 уже звірено з підписаним маніфестом).
func installFlatpakBundle(ctx context.Context, bundle string) (flatpakInfo, error) {
	raw, err := os.ReadFile(flatpakInfoPath)
	if err != nil {
		return flatpakInfo{}, err
	}
	info, err := parseFlatpakInfo(string(raw))
	if err != nil {
		return info, err
	}
	if _, err := exec.LookPath("flatpak-spawn"); err != nil {
		return info, errors.New("flatpak-spawn недоступний")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "flatpak-spawn", flatpakInstallArgs(info, bundle)...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return info, fmt.Errorf("flatpak install: %v: %s", err, msg)
	}
	return info, nil
}

// relaunchFlatpak — запускає нову версію через 2 с (поточний процес тим часом
// завершується, single-instance не заважає).
func relaunchFlatpak(info flatpakInfo) error {
	cmd := exec.Command("flatpak-spawn", "--host", "sh", "-c", "sleep 2; exec flatpak run "+info.AppID)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
