// velocity_toml.go — точкове редагування й читання velocity.toml.
//
// Лаунчер керує лише своїми ключами (bind, режими, [servers], forced-hosts,
// частина [advanced]/[query]); усе інше в конфігу — власні ключі Velocity,
// які лаунчер не знає (query.map, accepts-transfers, resource-pack-…),
// коментарі, порядок — лишається як було. Раніше файл перезаписувався
// цілком, і ручні налаштування готової мережі губились.
package servers

import (
	"regexp"
	"strconv"
	"strings"
)

var tomlHeaderRe = regexp.MustCompile(`^\s*\[([^\[\]]+)\]\s*(#.*)?$`)

type tomlDoc struct{ lines []string }

func parseTOMLDoc(s string) *tomlDoc {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return &tomlDoc{lines: strings.Split(strings.TrimRight(s, "\n"), "\n")}
}

func (d *tomlDoc) String() string { return strings.Join(d.lines, "\n") + "\n" }

// sectionRange — [start, end) рядків секції без заголовка; "" — верхній
// рівень (до першого заголовка). ok=false — секції немає.
func (d *tomlDoc) sectionRange(section string) (start, end int, ok bool) {
	if section == "" {
		for i, l := range d.lines {
			if tomlHeaderRe.MatchString(l) {
				return 0, i, true
			}
		}
		return 0, len(d.lines), true
	}
	for i, l := range d.lines {
		m := tomlHeaderRe.FindStringSubmatch(l)
		if m == nil || strings.TrimSpace(m[1]) != section {
			continue
		}
		end = len(d.lines)
		for j := i + 1; j < len(d.lines); j++ {
			if tomlHeaderRe.MatchString(d.lines[j]) {
				end = j
				break
			}
		}
		return i + 1, end, true
	}
	return 0, 0, false
}

func tomlLineKey(l string) string {
	t := strings.TrimSpace(l)
	if t == "" || strings.HasPrefix(t, "#") {
		return ""
	}
	i := strings.Index(t, "=")
	if i <= 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(t[:i]), `"'`)
}

// keyIndex — рядок ключа в секції (-1 — нема).
func (d *tomlDoc) keyIndex(section, key string) int {
	s, e, ok := d.sectionRange(section)
	if !ok {
		return -1
	}
	for i := s; i < e; i++ {
		if tomlLineKey(d.lines[i]) == key {
			return i
		}
	}
	return -1
}

// indentOf — відступ ключів секції (Velocity пише таби).
func (d *tomlDoc) indentOf(section string) string {
	s, e, _ := d.sectionRange(section)
	for i := s; i < e; i++ {
		if tomlLineKey(d.lines[i]) != "" {
			l := d.lines[i]
			return l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		}
	}
	if section == "" {
		return ""
	}
	return "\t"
}

// Get — сире значення ключа (без коментаря в кінці рядка).
func (d *tomlDoc) Get(section, key string) (string, bool) {
	i := d.keyIndex(section, key)
	if i < 0 {
		return "", false
	}
	v := strings.TrimSpace(d.lines[i][strings.Index(d.lines[i], "=")+1:])
	return v, true
}

// GetString — рядкове значення без лапок.
func (d *tomlDoc) GetString(section, key string) (string, bool) {
	v, ok := d.Get(section, key)
	if !ok {
		return "", false
	}
	if u, err := strconv.Unquote(v); err == nil {
		return u, true
	}
	return strings.Trim(v, `"'`), true
}

// Set ставить ключ = сире значення: замінює рядок або додає в кінець
// секції (секцію створює, якщо нема).
func (d *tomlDoc) Set(section, key, raw string) {
	line := d.indentOf(section) + key + " = " + raw
	if i := d.keyIndex(section, key); i >= 0 {
		d.lines[i] = line
		return
	}
	s, e, ok := d.sectionRange(section)
	if !ok {
		d.lines = append(d.lines, "", "["+section+"]", "\t"+key+" = "+raw)
		return
	}
	// Після останнього непорожнього рядка секції.
	at := s
	for i := s; i < e; i++ {
		if strings.TrimSpace(d.lines[i]) != "" {
			at = i + 1
		}
	}
	d.lines = append(d.lines[:at], append([]string{line}, d.lines[at:]...)...)
}

// SetIfMissing — лише якщо ключа ще нема.
func (d *tomlDoc) SetIfMissing(section, key, raw string) {
	if d.keyIndex(section, key) < 0 {
		d.Set(section, key, raw)
	}
}

// ReplaceSectionBody замінює вміст секції повністю (створює, якщо нема).
func (d *tomlDoc) ReplaceSectionBody(section string, body []string) {
	ind := d.indentOf(section)
	out := make([]string, 0, len(body)+1)
	for _, b := range body {
		out = append(out, ind+b)
	}
	s, e, ok := d.sectionRange(section)
	if !ok {
		d.lines = append(d.lines, "", "["+section+"]")
		d.lines = append(d.lines, out...)
		return
	}
	out = append(out, "")
	d.lines = append(d.lines[:s], append(out, d.lines[e:]...)...)
}

// RemoveSection прибирає секцію разом із заголовком.
func (d *tomlDoc) RemoveSection(section string) {
	s, e, ok := d.sectionRange(section)
	if !ok || section == "" {
		return
	}
	d.lines = append(d.lines[:s-1], d.lines[e:]...)
}

// velocityServers — записи [servers] (ім'я → адреса) у порядку файлу і try.
func (d *tomlDoc) velocityServers() (entries [][2]string, try []string) {
	s, e, ok := d.sectionRange("servers")
	if !ok {
		return nil, nil
	}
	for i := s; i < e; i++ {
		k := tomlLineKey(d.lines[i])
		if k == "" {
			continue
		}
		v := strings.TrimSpace(d.lines[i][strings.Index(d.lines[i], "=")+1:])
		if k == "try" {
			// Масив може йти на кількох рядках (так писав старий генератор).
			for strings.HasPrefix(v, "[") && !strings.Contains(v, "]") && i+1 < e {
				i++
				v += " " + strings.TrimSpace(d.lines[i])
			}
			try = parseTOMLStringArray(v)
			continue
		}
		if addr, err := strconv.Unquote(v); err == nil {
			entries = append(entries, [2]string{k, addr})
		}
	}
	return entries, try
}

var tomlStrRe = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

func parseTOMLStringArray(v string) []string {
	var out []string
	for _, m := range tomlStrRe.FindAllStringSubmatch(v, -1) {
		if s, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
			out = append(out, s)
		}
	}
	return out
}
