// lanscan.go — пошук справжніх Minecraft-серверів на ЦЬОМУ ПК для тимчасових запрошень:
//   - LAN-світи: слухаємо офіційну LAN-розсилку Minecraft (224.0.2.60:4445,
//     «[MOTD]…[/MOTD][AD]порт[/AD]») — так гра сама оголошує «Відкрити для мережі»;
//   - локальні сервери: Server List Ping на типових портах 127.0.0.1;
//   - для кожного знайденого — реальні ознаки зі статусу сервера: версія, онлайн/макс,
//     MOTD, чи є мод-лоадер (forgeData/modinfo). Нічого не вигадуємо: що не відповіло — не показуємо.
package p2p

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type LocalServer struct {
	Port    int    `json:"port"`
	Kind    string `json:"kind"` // lan | server | modded
	Motd    string `json:"motd"`
	Version string `json:"version"`
	Players int    `json:"players"`
	Max     int    `json:"max"`
	LAN     bool   `json:"lan"` // знайдено через LAN-розсилку гри
}

var reLAN = regexp.MustCompile(`\[MOTD\](.{0,200}?)\[/MOTD\]\[AD\](\d{1,5})\[/AD\]`)

// ScanLocalServers — ~4 с. Три незалежні джерела, результат — їх об'єднання:
//  1. LAN-розсилка гри (224.0.2.60:4445) — слухаємо на КОЖНОМУ мережевому інтерфейсі;
//  2. SLP типових портів 25565–25575;
//  3. усі TCP-порти, які слухає java на цьому ПК (listeningJavaPorts) — саме так знаходиться
//     світ «Відкрити для мережі» з випадковим портом, навіть якщо розсилка не дійшла.
//
// ЧОМУ РАНІШЕ «не знаходило, а з точним портом знаходило»: LAN-світ слухає ВИПАДКОВИЙ порт
// (50000+), SLP-скан бачив лише 25565–25575, а multicast слухався на одному «типовому»
// інтерфейсі — на ПК з WSL/VPN/кількома адаптерами це не той, куди гра шле розсилку.
// Ручне введення порту йшло прямо в ProbeLocalServer, тож усе знаходилось.
func ScanLocalServers(ctx context.Context) []LocalServer {
	found := map[int]LocalServer{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for port, motd := range listenLAN(ctx, 4*time.Second) {
			mu.Lock()
			found[port] = LocalServer{Port: port, Kind: "lan", Motd: motd, LAN: true}
			mu.Unlock()
		}
	}()

	// Кандидати для SLP: типові порти + порти, які слухає java.
	cand := map[int]bool{}
	for p := 25565; p <= 25575; p++ {
		cand[p] = true
	}
	for _, p := range listeningJavaPorts() {
		cand[p] = true
	}
	sem := make(chan struct{}, 24)
	for p := range cand {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			if s, err := ProbeLocalServer(p); err == nil {
				mu.Lock()
				if _, ok := found[p]; !ok {
					found[p] = s
				}
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()

	var out []LocalServer
	for p, s := range found {
		if s.LAN { // доповнюємо LAN-світ реальними даними статусу
			if st, err := ProbeLocalServer(p); err == nil {
				st.Kind, st.LAN = "lan", true
				if st.Motd == "" {
					st.Motd = s.Motd
				}
				s = st
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// lanMulticastGroup — адреса офіційної LAN-розсилки Minecraft.
var lanMulticastGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 2, 60), Port: 4445}

// listenLAN слухає LAN-розсилку на КОЖНОМУ придатному інтерфейсі (up, multicast, має IPv4,
// не loopback) паралельно, плюс один сокет на «типовому» інтерфейсі як страховка. Повертає
// порт → MOTD для світів ЦЬОГО ПК.
func listenLAN(ctx context.Context, d time.Duration) map[int]string {
	out := map[int]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	deadline := time.Now().Add(d)

	collect := func(c *net.UDPConn) {
		defer wg.Done()
		defer c.Close()
		_ = c.SetReadDeadline(deadline)
		buf := make([]byte, 1024)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			n, src, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if src == nil || !isLocalIP(src.IP) {
				continue // лише світи цього ПК: запросити можна тільки свій світ
			}
			if m := reLAN.FindSubmatch(buf[:n]); m != nil {
				if p, err := strconv.Atoi(string(m[2])); err == nil && p > 0 && p < 65536 {
					mu.Lock()
					out[p] = cleanText(string(m[1]), 120)
					mu.Unlock()
				}
			}
		}
	}

	ifaces, _ := net.Interfaces()
	for i := range ifaces {
		ifc := ifaces[i]
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagMulticast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if !ifaceHasIPv4(&ifc) {
			continue
		}
		c, err := net.ListenMulticastUDP("udp4", &ifc, lanMulticastGroup)
		if err != nil {
			continue
		}
		wg.Add(1)
		go collect(c)
	}
	if c, err := net.ListenMulticastUDP("udp4", nil, lanMulticastGroup); err == nil {
		wg.Add(1)
		go collect(c)
	}
	wg.Wait()
	return out
}

func ifaceHasIPv4(ifc *net.Interface) bool {
	addrs, err := ifc.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return true
		}
	}
	return false
}

func isLocalIP(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// ProbeLocalServer — Server List Ping на 127.0.0.1:port. Помилка = там не Minecraft.
func ProbeLocalServer(port int) (LocalServer, error) {
	if port < 1 || port > 65535 {
		return LocalServer{}, errors.New("порт 1–65535")
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 800*time.Millisecond)
	if err != nil {
		return LocalServer{}, fmt.Errorf("на порту %d нічого не слухає", port)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	var hs bytes.Buffer
	hs.WriteByte(0x00)
	writeVarint(&hs, 767)
	writeVarint(&hs, len("127.0.0.1"))
	hs.WriteString("127.0.0.1")
	_ = binary.Write(&hs, binary.BigEndian, uint16(port))
	writeVarint(&hs, 1)
	var pkt bytes.Buffer
	writeVarint(&pkt, hs.Len())
	pkt.Write(hs.Bytes())
	pkt.Write([]byte{0x01, 0x00}) // status request
	if _, err := c.Write(pkt.Bytes()); err != nil {
		return LocalServer{}, err
	}
	r := bufio.NewReader(c)
	if _, err := readVarint(r); err != nil {
		return LocalServer{}, errors.New("це не Minecraft-сервер")
	}
	if id, err := readVarint(r); err != nil || id != 0 {
		return LocalServer{}, errors.New("це не Minecraft-сервер")
	}
	n, err := readVarint(r)
	if err != nil || n <= 0 || n > 512<<10 {
		return LocalServer{}, errors.New("це не Minecraft-сервер")
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(r, raw); err != nil {
		return LocalServer{}, errors.New("неповна відповідь сервера")
	}
	var st struct {
		Version struct {
			Name string `json:"name"`
		} `json:"version"`
		Players struct {
			Online int `json:"online"`
			Max    int `json:"max"`
		} `json:"players"`
		Description json.RawMessage `json:"description"`
		ForgeData   json.RawMessage `json:"forgeData"`
		ModInfo     json.RawMessage `json:"modinfo"`
	}
	if json.Unmarshal(raw, &st) != nil {
		return LocalServer{}, errors.New("це не Minecraft-сервер")
	}
	s := LocalServer{Port: port, Kind: "server", Version: cleanText(st.Version.Name, 40),
		Players: st.Players.Online, Max: st.Players.Max, Motd: cleanText(motdText(st.Description), 120)}
	if len(st.ForgeData) > 0 || len(st.ModInfo) > 0 {
		s.Kind = "modded"
	}
	return s, nil
}

func writeVarint(b *bytes.Buffer, v int) {
	u := uint32(v)
	for {
		if u&^0x7f == 0 {
			b.WriteByte(byte(u))
			return
		}
		b.WriteByte(byte(u&0x7f | 0x80))
		u >>= 7
	}
}

// motdText — текст з рядка або chat-компонента ({"text":…,"extra":[…]}).
func motdText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var c struct {
		Text  string            `json:"text"`
		Extra []json.RawMessage `json:"extra"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	out := c.Text
	for i, e := range c.Extra {
		if i > 20 {
			break
		}
		out += motdText(e)
	}
	return out
}

var reColor = regexp.MustCompile(`§.`)

func cleanText(s string, max int) string {
	s = reColor.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		s = s[:max]
	}
	return s
}
