//go:build linux

package procmetrics

import (
	"os"
	"strconv"
	"strings"
)

const clkTck = 100 // USER_HZ на всіх сучасних ядрах Linux

func listProcs() []procInfo {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	page := uint64(os.Getpagesize())
	var out []procInfo
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		p, ok := parseStat(string(data))
		if !ok {
			continue
		}
		p.pid = pid
		if sm, err := os.ReadFile("/proc/" + e.Name() + "/statm"); err == nil {
			if f := strings.Fields(string(sm)); len(f) >= 2 {
				if pages, err := strconv.ParseUint(f[1], 10, 64); err == nil {
					p.rss = pages * page
				}
			}
		}
		out = append(out, p)
	}
	return out
}

// parseStat читає ppid, utime, stime з /proc/<pid>/stat (назва в дужках може
// містити пробіли й дужки, тому розбір іде після останньої ')').
func parseStat(s string) (procInfo, bool) {
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return procInfo{}, false
	}
	f := strings.Fields(s[i+1:])
	// f[0]=state f[1]=ppid ... f[11]=utime f[12]=stime
	if len(f) < 13 {
		return procInfo{}, false
	}
	ppid, e1 := strconv.Atoi(f[1])
	ut, e2 := strconv.ParseUint(f[11], 10, 64)
	st, e3 := strconv.ParseUint(f[12], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil {
		return procInfo{}, false
	}
	return procInfo{ppid: ppid, cpuNs: (ut + st) * (1_000_000_000 / clkTck)}, true
}
