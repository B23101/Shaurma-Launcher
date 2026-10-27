//go:build !linux && !windows

package procmetrics

import (
	"os/exec"
	"strconv"
	"strings"
)

func listProcs() []procInfo {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=,cputime=").Output()
	if err != nil {
		return nil
	}
	var res []procInfo
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		rss, e3 := strconv.ParseUint(f[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		res = append(res, procInfo{pid: pid, ppid: ppid, rss: rss * 1024, cpuNs: parseCPUTime(f[3])})
	}
	return res
}

// parseCPUTime розбирає [[дд-]гг:]хх:сс[.сс] у наносекунди.
func parseCPUTime(s string) uint64 {
	days := 0.0
	if i := strings.IndexByte(s, '-'); i > 0 {
		d, _ := strconv.Atoi(s[:i])
		days = float64(d)
		s = s[i+1:]
	}
	total := 0.0
	for _, part := range strings.Split(s, ":") {
		v, _ := strconv.ParseFloat(part, 64)
		total = total*60 + v
	}
	return uint64((total + days*86400) * 1e9)
}
