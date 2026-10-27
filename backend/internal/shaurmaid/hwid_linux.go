//go:build linux

package shaurmaid

import (
	"os"
	"strings"
)

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func collectHWIDParts() map[string]string {
	m := map[string]string{
		"machine": firstNonEmpty(readTrim("/etc/machine-id"), readTrim("/var/lib/dbus/machine-id")),
		"board":   readTrim("/sys/class/dmi/id/board_name"),
		"vendor":  readTrim("/sys/class/dmi/id/board_vendor"),
		"product": readTrim("/sys/class/dmi/id/product_uuid"), // зазвичай лише root — тоді порожньо, і це нормально
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "model name") {
				if i := strings.IndexByte(ln, ':'); i > 0 {
					m["cpu"] = strings.TrimSpace(ln[i+1:])
				}
				break
			}
		}
	}
	return m
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
