//go:build !windows

package servers

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func getSystemRAM() (totalMB int, availMB int) {
	// Читаємо /proc/meminfo (Linux).
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		// macOS або інше — повертаємо дефолт.
		return 8192, 4096
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			totalMB = parseMemInfoKB(line) / 1024
		} else if strings.HasPrefix(line, "MemAvailable:") {
			availMB = parseMemInfoKB(line) / 1024
		}
	}
	if totalMB == 0 {
		totalMB = 8192
	}
	if availMB == 0 {
		availMB = totalMB / 2
	}
	return
}

func parseMemInfoKB(line string) int {
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		v, _ := strconv.Atoi(fields[1])
		return v
	}
	return 0
}
