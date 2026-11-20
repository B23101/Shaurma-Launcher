package java

// Inspect — перевірка Java, яку користувач обрав сам (свій java/javaw.exe):
// чи це справді Java (файл названий java/javaw і JVM відповідає на
// -XshowSettings:properties), яка в неї версія, вендор і розрядність, і чи
// підходить вона для версії Minecraft збірки.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Info — що вдалося дізнатися про Java.
type Info struct {
	Path    string `json:"path"`
	Major   int    `json:"major"`
	Version string `json:"version"`
	Vendor  string `json:"vendor"`
	Arch    string `json:"arch"`
	Bits    int    `json:"bits"`
}

// Сумісність Java з версією гри (див. Compat).
const (
	CompatOK     = "ok"      // рекомендована версія
	CompatNewer  = "newer"   // новіша за потрібну — зазвичай працює, Forge може впасти
	CompatOld    = "too_old" // старіша — гра не запуститься
	CompatLegacy = "legacy"  // гра на Java 8, а обрана новіша — старі версії й Forge ламаються
)

// ErrNotJava — файл не є Java.
var ErrNotJava = errors.New("це не Java")

func javaNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"javaw.exe", "java.exe"}
	}
	return []string{"java"}
}

// Inspect запускає обрану Java з таймаутом і читає її властивості.
func Inspect(exe string) (Info, error) {
	exe = filepath.Clean(exe)
	base := strings.ToLower(filepath.Base(exe))
	ok := false
	for _, n := range javaNames() {
		if base == n {
			ok = true
		}
	}
	if !ok {
		return Info{}, fmt.Errorf("%w: оберіть файл %s", ErrNotJava, strings.Join(javaNames(), " або "))
	}
	st, err := os.Stat(exe)
	if err != nil || st.IsDir() {
		return Info{}, fmt.Errorf("файл не знайдено: %s", exe)
	}
	// На Windows опитуємо javaw.exe (без консольного вікна), якщо він поруч.
	probe := exe
	if runtime.GOOS == "windows" && base == "java.exe" {
		if w := filepath.Join(filepath.Dir(exe), "javaw.exe"); fileExists(w) {
			probe = w
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, probe, "-XshowSettings:properties", "-version")
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return Info{}, fmt.Errorf("%w: програма не відповіла за 10 с", ErrNotJava)
	}
	props := parseProps(string(out))
	info := Info{
		Path:    exe,
		Version: props["java.version"],
		Vendor:  firstNonEmpty(props["java.vendor"], props["java.vm.vendor"]),
		Arch:    props["os.arch"],
	}
	info.Bits, _ = strconv.Atoi(props["sun.arch.data.model"])
	info.Major = majorFromVersion(info.Version)
	if info.Major == 0 {
		// Стара JVM без -XshowSettings: беремо рядок `version "…"`.
		if m := versionRe.FindStringSubmatch(string(out)); m != nil {
			info.Version = strings.TrimPrefix(m[0], `version "`)
			info.Major = majorFromVersion(info.Version)
		}
	}
	if info.Major == 0 {
		if err != nil {
			return Info{}, fmt.Errorf("%w: %v", ErrNotJava, err)
		}
		return Info{}, ErrNotJava
	}
	return info, nil
}

// Compat — чи підходить Java мажора have для гри, якій потрібна need.
func Compat(have, need int) string {
	switch {
	case need <= 0 || have == need:
		return CompatOK
	case have < need:
		return CompatOld
	case need <= 8:
		return CompatLegacy
	default:
		return CompatNewer
	}
}

func parseProps(out string) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, seen := m[k]; !seen {
			m[k] = strings.TrimSpace(v)
		}
	}
	return m
}

// majorFromVersion: "1.8.0_402" → 8, "17.0.10" → 17, "21" → 21, "25-ea" → 25.
func majorFromVersion(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	parts := strings.FieldsFunc(v, func(r rune) bool { return r < '0' || r > '9' })
	if len(parts) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(parts[0])
	if n == 1 && len(parts) > 1 {
		n, _ = strconv.Atoi(parts[1])
	}
	if n < 1 || n > 99 {
		return 0
	}
	return n
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
