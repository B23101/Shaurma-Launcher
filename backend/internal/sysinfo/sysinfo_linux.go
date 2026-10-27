//go:build linux

package sysinfo

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"shaurma-launcher-backend/internal/model"
)

// archName перетворює runtime.GOARCH у зрозумілу архітектуру UI
// (той самий формат, що й у Windows-версії — "x64" / "x86" / "arm64").
func archName(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	default:
		return goarch
	}
}

// osName читає /etc/os-release (PRETTY_NAME) для «людської» назви
// дистрибутива (напр. "Ubuntu 24.04.1 LTS", "SteamOS 3.6"). Це той самий
// файл, за яким Частина 5 плану пропонує детектити Steam Deck
// (product_name містить "Jupiter"), але тут беремо лише вивід для UI —
// на відміну від Windows тут не треба збирати major/minor/build вручну,
// дистрибутиви самі публікують готовий рядок.
func osName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		v := strings.TrimPrefix(line, "PRETTY_NAME=")
		v = strings.Trim(v, `"`)
		if v != "" {
			return v
		}
	}
	return "Linux"
}

// memInfoMB читає /proc/meminfo і повертає (total, available) у МБ —
// стандартний спосіб дізнатись обсяг RAM на Linux без cgo/syscall
// сторонніх бібліотек. Аналог GlobalMemoryStatusEx на Windows.
//
// available — MemAvailable (кБ), а НЕ MemFree: MemFree показує лише
// цілком порожню пам'ять і на практиці майже завжди мале число, бо ядро
// охоче віддає вільну RAM під page cache/buffers — за MemFree довелось би
// вважати систему "критично забитою" навіть з купою кешу, який ядро
// миттю звільнить під потреби програми. MemAvailable (з ядра 3.14+) уже
// враховує це і оцінює, скільки пам'яті реально можна виділити новому
// процесу без свопу — саме те, що потрібно для перевірки "чи вистачить
// вільної RAM під гру". MemFree лишається фолбеком для дуже старих ядер,
// де MemAvailable відсутній.
func memInfoMB() (total, available int64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var memFreeKB int64
	haveMemAvailable := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			if kb, ok := parseMeminfoKB(line); ok {
				total = kb / 1024
			}
		case strings.HasPrefix(line, "MemAvailable:"):
			if kb, ok := parseMeminfoKB(line); ok {
				available = kb / 1024
				haveMemAvailable = true
			}
		case strings.HasPrefix(line, "MemFree:"):
			if kb, ok := parseMeminfoKB(line); ok {
				memFreeKB = kb
			}
		}
	}
	if !haveMemAvailable {
		available = memFreeKB / 1024
	}
	return total, available
}

// parseMeminfoKB витягує число кБ з рядка формату "Label:   12345 kB".
func parseMeminfoKB(line string) (int64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, false
	}
	kb, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return kb, true
}

// freeDiskMB рахує вільне місце для dir через syscall.Statfs — POSIX-
// еквівалент GetDiskFreeSpaceExW. Bavail (доступно НЕ-root процесу)
// навмисно, а не Bfree (доступно взагалі), щоб не показати місце, яке
// лаунчер від імені звичайного користувача однаково не зможе записати
// (резерв для root на деяких файлових системах, напр. ext4).
func freeDiskMB(dir string) int64 {
	if dir == "" {
		return 0
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0
	}
	// #nosec G115 — Bavail/Bsize на linux/amd64 вже uint64/int64 залежно
	// від архітектури; явне приведення тримає код портабельним між ними.
	return int64(uint64(st.Bavail) * uint64(st.Bsize) / 1024 / 1024)
}

func collect(dir string) model.SystemInfo {
	info := model.SystemInfo{CPUCount: runtime.NumCPU()}
	info.Platform = runtime.GOOS
	info.Arch = archName(runtime.GOARCH)
	info.OS = osName()
	info.TotalRAMMB, info.FreeRAMMB = memInfoMB()
	info.FreeDiskMB = freeDiskMB(dir)
	return info
}
