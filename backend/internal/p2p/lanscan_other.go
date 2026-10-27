//go:build !windows

package p2p

// lanscan_other.go — на Linux/macOS власника сокета без важкого розбору /proc/<pid>/fd не знаємо,
// тому беремо усі локальні LISTEN-порти ≥1024 з /proc/net/tcp (Server List Ping далі відсіє
// не-Minecraft). На macOS файлу немає — повертаємо нічого, лишаються LAN-розсилка й типові порти.

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
)

func listeningJavaPorts() []int {
	seen := map[int]bool{}
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Scan() // заголовок
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 4 || fields[3] != "0A" { // 0A = TCP_LISTEN
				continue
			}
			addr := fields[1]
			i := strings.LastIndexByte(addr, ':')
			if i < 0 {
				continue
			}
			p, err := strconv.ParseInt(addr[i+1:], 16, 32)
			if err != nil || p < 1024 || p > 65535 {
				continue
			}
			seen[int(p)] = true
		}
		fh.Close()
	}
	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Ints(out)
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}
