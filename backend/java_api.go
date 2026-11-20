package main

import (
	"errors"
	"runtime"

	"shaurma-launcher-backend/internal/java"
)

// JavaCheck — результат перевірки Java, яку користувач обрав сам.
// Status: ok | newer | too_old | legacy (див. java.Compat) або not_java.
type JavaCheck struct {
	OK       bool   `json:"ok"`
	Path     string `json:"path"`
	Major    int    `json:"major"`
	Version  string `json:"version"`
	Vendor   string `json:"vendor"`
	Arch     string `json:"arch"`
	Bits     int    `json:"bits"`
	Required int    `json:"required"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// InspectJava перевіряє, чи шлях (java/javaw.exe або тека Java) — справжня
// Java, і чи підходить вона для mcVersion (порожній — без перевірки версії).
func (a *App) InspectJava(path, mcVersion string) JavaCheck {
	exe := a.cfg.ResolveJavaExe(path)
	info, err := java.Inspect(exe)
	if err != nil {
		st := "error"
		if errors.Is(err, java.ErrNotJava) {
			st = "not_java"
		}
		return JavaCheck{Path: exe, Status: st, Error: err.Error()}
	}
	res := JavaCheck{
		OK: true, Path: info.Path, Major: info.Major, Version: info.Version,
		Vendor: info.Vendor, Arch: info.Arch, Bits: info.Bits, Status: java.CompatOK,
	}
	if mcVersion != "" {
		res.Required = java.RecommendedMajor(mcVersion)
		res.Status = java.Compat(info.Major, res.Required)
	}
	// 32-бітна Java не дасть гри більше ~1.5 ГБ пам'яті — гра падає з OOM.
	if info.Bits == 32 && res.Status == java.CompatOK {
		res.Status = "32bit"
	}
	return res
}

// BrowseJavaExe — діалог вибору виконуваного файлу Java.
func (a *App) BrowseJavaExe() (string, error) {
	filter := map[string][]string{"Java (java.exe, javaw.exe)": {"exe"}}
	if runtime.GOOS != "windows" {
		filter = map[string][]string{"Java": {"*"}}
	}
	return bridgeDialogOpenFile("Виберіть java.exe або javaw.exe", false, filter), nil
}
