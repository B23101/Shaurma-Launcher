//go:build !windows

package servers

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// getProcessRAM повертає RSS пам'яті процесу в МБ.
func getProcessRAM(pid int) int64 {
	return getProcessRAMLinux(pid)
}

// getProcessCPU повертає сукупний CPU time процесу.
func getProcessCPU(pid int) time.Duration {
	return getProcessCPUUnix(pid)
}

// getProcessRAMLinux — RSS з /proc/<pid>/statm (в pages, *4096 = bytes).
func getProcessRAMLinux(pid int) int64 {
	f, err := os.Open(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			// fields[1] = resident (pages)
			if pages, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				return (pages * 4096) / (1024 * 1024) // bytes → MB
			}
		}
	}
	return 0
}

// getProcessCPUUnix — CPU time з /proc/<pid>/stat.
func getProcessCPUUnix(pid int) time.Duration {
	f, err := os.Open(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		text := scanner.Text()
		// Format: pid (comm) state ppid ... utime(14) stime(15)
		// Знайти закриту дужку після назви процесу.
		if idx := strings.LastIndex(text, ")"); idx > 0 {
			fields := strings.Fields(text[idx+2:])
			if len(fields) >= 15 {
				utime, _ := strconv.ParseInt(fields[13], 10, 64)
				stime, _ := strconv.ParseInt(fields[14], 10, 64)
				ticks := utime + stime
				// clk_tck зазвичай 100 на Linux.
				return time.Duration(ticks) * (time.Second / 100)
			}
		}
	}
	return 0
}
