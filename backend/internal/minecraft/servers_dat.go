package minecraft

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// ── Читання/запис servers.dat (список серверів гри) ─────────────────────
// servers.dat — це NBT-файл: кореневий compound "servers", всередині —
// список "servers" з compound-ів {name, ip, icon, acceptTextures, hidden}.
// ВАЖЛИВО: залежно від версії гри файл може бути ЯК gzip-стиснений, ТАК і
// "plain" (без стиснення) — сучасні версії Minecraft пишуть його БЕЗ
// gzip. Читання автоматично визначає формат за magic-байтами (gzip
// починається з 0x1F 0x8B), запис завжди пише plain NBT (той самий
// формат, що зараз пише сама гра — гра однаково коректно читає обидва
// варіанти, тож зворотної несумісності немає). НБТ-парсер мінімальний
// (потрібні лише string/list/compound/byte), без зовнішніх залежностей —
// стандартна бібліотека.
//
// Це ЄДИНЕ джерело правди для розділу "Сервери" на сторінці збірки:
// лаунчер читає/пише той самий файл, який редагує сама гра (внутрішньоігровий
// екран Multiplayer), тому додавання/видалення/переставляння серверів у
// лаунчері миттєво відображається в грі і навпаки.

// ServerEntry — один сервер з servers.dat збірки.
type ServerEntry struct {
	Name string
	IP   string
	// Icon — base64 PNG (64x64) іконка сервера, як зберігає сама гра
	// (без префіксу "data:image/png;base64,"). Порожній рядок — іконки
	// немає (стандартна сіра трава-заглушка в грі).
	Icon string
	// AcceptTextures: 0 = запитати про ресурс-пак сервера при заході (як у
	// грі за замовчуванням), 1 = приймати автоматично, 2 = відхиляти
	// автоматично. Немає збереженого значення (гра ще не питала) —
	// AcceptTexturesSet=false.
	AcceptTextures    byte
	AcceptTexturesSet bool
	// Hidden — сервер прихований зі списку LAN/скановних серверів гри.
	// Зберігаємо і повертаємо назад незміненим, щоб лаунчер не губив цей
	// прапорець гри при перезаписі файлу.
	Hidden    bool
	HiddenSet bool
	// RequiredPackSkipped — гравець відхилив ОБОВ'ЯЗКОВИЙ ресурс-пак
	// сервера (resource pack "required"); гра запам'ятовує це per-сервер.
	// Зберігаємо і повертаємо незміненим — те саме, що й Hidden.
	RequiredPackSkipped    bool
	RequiredPackSkippedSet bool
}

// ReadServersDat читає servers.dat і повертає список серверів
// (порядок збереження у файлі). Помилка (немає файлу/некоректний NBT) —
// порожній результат з помилкою; викликач вирішує, як показувати.
func ReadServersDat(path string) ([]ServerEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data, err := maybeUngzip(raw)
	if err != nil {
		return nil, err
	}
	return parseServersNBT(data)
}

// WriteServersDat перезаписує servers.dat повним списком серверів
// (атомарно: спершу у тимчасовий файл, потім rename — щоб гра ніколи не
// побачила напівзаписаний файл, якщо саме зараз читає його при старті).
// Пишемо у форматі PLAIN NBT (без gzip) — це формат, який зараз реально
// пише сама гра; гра коректно читає його незалежно від того, чи файл був
// раніше стиснений.
func WriteServersDat(path string, entries []ServerEntry) error {
	data := encodeServersNBT(entries)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EnsureOfficialServer гарантує, що servers.dat за шляхом path містить
// офіційний сервер збірки (serverIP, serverName) — але ЛИШЕ коли файлу
// ще немає або список серверів у ньому порожній. Якщо у файлі вже є хоч
// один сервер — файл належить гравцю, і ми його не чіпаємо (навіть коли
// офіційної адреси серед них немає): те саме правило on_missing, що й для
// options.txt — наявний список = вибір гравця, не нав'язуємо сервер
// повторно. Порожній serverIP — no-op. Повертає true, якщо сервер додано.
func EnsureOfficialServer(path, serverName, serverIP string) (bool, error) {
	serverIP = strings.TrimSpace(serverIP)
	if serverIP == "" {
		return false, nil
	}
	entries, err := ReadServersDat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			// Файл пошкоджений/некоректний — не перезаписуємо чуже.
			return false, nil
		}
		entries = nil
	}
	if len(entries) > 0 {
		// У списку вже щось є — це файл гравця.
		return false, nil
	}
	name := strings.TrimSpace(serverName)
	if name == "" {
		name = serverIP
	}
	if err := WriteServersDat(path, []ServerEntry{{Name: name, IP: serverIP}}); err != nil {
		return false, err
	}
	return true, nil
}

// SyncOfficialServer — режим «офіційний сервер ЗАВЖДИ в списку» (опція
// збірки OfficialServerSync у Pack Manager). На відміну від
// EnsureOfficialServer, працює і з НЕпорожнім списком гравця:
//
//   - запис з IP == serverIP вже є → нічого не робимо (ім'я/іконку, які
//     гравець міг змінити, не чіпаємо);
//   - запису з актуальним IP нема, але є наш запис зі СТАРИМ IP (prevIP —
//     те, що лаунчер записав минулого разу; або запис з тим самим іменем
//     збірки) → оновлюємо в ньому лише адресу (назва, іконка, прапорці гри
//     лишаються), дублікатів не плодимо;
//   - нічого не знайшли (видалили / ще не було) → додаємо НА ПОЧАТОК списку.
//
// Решту серверів гравця не чіпаємо. Повертає (true, oldIP), якщо файл
// змінено; oldIP — адреса, яку замінили (порожня, якщо запис додано).
// Пошкоджений файл (не NotExist) не перезаписуємо.
func SyncOfficialServer(path, serverName, serverIP, prevIP string) (changed bool, oldIP string, err error) {
	serverIP = strings.TrimSpace(serverIP)
	if serverIP == "" {
		return false, "", nil
	}
	prevIP = strings.TrimSpace(prevIP)
	name := strings.TrimSpace(serverName)

	entries, rerr := ReadServersDat(path)
	if rerr != nil {
		if !os.IsNotExist(rerr) {
			return false, "", nil // файл гравця пошкоджений — не чіпаємо
		}
		entries = nil
	}

	for _, e := range entries {
		if strings.EqualFold(strings.TrimSpace(e.IP), serverIP) {
			return false, "", nil // вже актуально
		}
	}

	idx := -1
	if prevIP != "" {
		for i, e := range entries {
			if strings.EqualFold(strings.TrimSpace(e.IP), prevIP) {
				idx = i
				break
			}
		}
	}
	if idx < 0 && name != "" {
		for i, e := range entries {
			if strings.EqualFold(strings.TrimSpace(e.Name), name) {
				idx = i
				break
			}
		}
	}

	if idx >= 0 {
		oldIP = entries[idx].IP
		entries[idx].IP = serverIP
	} else {
		if name == "" {
			name = serverIP
		}
		entries = append([]ServerEntry{{Name: name, IP: serverIP}}, entries...)
	}
	if err := WriteServersDat(path, entries); err != nil {
		return false, "", err
	}
	return true, oldIP, nil
}

// maybeUngzip розпаковує дані, ЯКЩО вони gzip (перевірка за magic-байтами
// 0x1F 0x8B), інакше повертає їх як є (сучасний "plain" формат servers.dat).
func maybeUngzip(data []byte) ([]byte, error) {
	if len(data) >= 2 && data[0] == 0x1F && data[1] == 0x8B {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		return io.ReadAll(gz)
	}
	return data, nil
}

// ── Мінімальний NBT-читач ───────────────────────────────────────────────

type nbtReader struct {
	data []byte
	off  int
}

func (r *nbtReader) readByte() byte {
	b := r.data[r.off]
	r.off++
	return b
}

func (r *nbtReader) readInt32() int32 {
	v := binary.BigEndian.Uint32(r.data[r.off:])
	r.off += 4
	return int32(v)
}

func (r *nbtReader) readString() string {
	n := int(binary.BigEndian.Uint16(r.data[r.off:]))
	r.off += 2
	s := string(r.data[r.off : r.off+n])
	r.off += n
	return s
}

// readValue пропускає значення заданого NBT-типу.
func (r *nbtReader) readValue(t byte) {
	switch t {
	case 0x01: // byte
		r.off++
	case 0x02: // short
		r.off += 2
	case 0x03: // int
		r.off += 4
	case 0x04: // long
		r.off += 8
	case 0x05: // float
		r.off += 4
	case 0x06: // double
		r.off += 8
	case 0x07: // byte array
		n := int(r.readInt32())
		r.off += n
	case 0x08: // string
		r.readString()
	case 0x09: // list
		et := r.readByte()
		n := int(r.readInt32())
		for i := 0; i < n; i++ {
			r.readValue(et)
		}
	case 0x0A: // compound
		r.readCompound(nil)
	case 0x0B: // int array
		n := int(r.readInt32())
		r.off += n * 4
	case 0x0C: // long array
		n := int(r.readInt32())
		r.off += n * 8
	}
}

// readCompound читає вміст compound: пари (тип, ім'я, значення) до End (0).
// Якщо fn != nil — для кожного тега викликається fn(name, type), і КОЛБЕК
// сам читає значення; інакше значення пропускається readValue.
func (r *nbtReader) readCompound(fn func(name string, t byte)) {
	for {
		t := r.readByte()
		if t == 0 {
			return
		}
		name := r.readString()
		if fn != nil {
			fn(name, t)
		} else {
			r.readValue(t)
		}
	}
}

func parseServersNBT(data []byte) ([]ServerEntry, error) {
	if len(data) < 3 || data[0] != 0x0A {
		return nil, fmt.Errorf("не NBT compound")
	}
	r := &nbtReader{data: data}
	r.readByte()   // root tag type (compound)
	r.readString() // root name

	var out []ServerEntry
	r.readCompound(func(name string, t byte) {
		if name != "servers" || t != 0x09 {
			r.readValue(t)
			return
		}
		elemType := r.readByte()
		count := int(r.readInt32())
		for i := 0; i < count; i++ {
			if elemType != 0x0A {
				r.readValue(elemType)
				continue
			}
			e := ServerEntry{}
			r.readCompound(func(k string, kt byte) {
				switch k {
				case "name":
					e.Name = r.readString()
				case "ip":
					e.IP = r.readString()
				case "icon":
					e.Icon = r.readString()
				case "acceptTextures":
					e.AcceptTextures = r.readByte()
					e.AcceptTexturesSet = true
				case "hidden":
					e.Hidden = r.readByte() != 0
					e.HiddenSet = true
				case "requiredPackSkipped":
					e.RequiredPackSkipped = r.readByte() != 0
					e.RequiredPackSkippedSet = true
				default:
					r.readValue(kt)
				}
			})
			if e.IP != "" {
				out = append(out, e)
			}
		}
	})
	return out, nil
}

// ── Мінімальний NBT-письменник ──────────────────────────────────────────

func writeNBTString(b *bytes.Buffer, s string) {
	n := len(s)
	if n > 0xFFFF {
		n = 0xFFFF
		s = s[:n]
	}
	b.WriteByte(byte(n >> 8))
	b.WriteByte(byte(n))
	b.WriteString(s)
}

func writeNBTInt32(b *bytes.Buffer, v int32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(v))
	b.Write(tmp[:])
}

// encodeServersNBT будує повний NBT-документ (без gzip) зі списком
// серверів, у тому самому форматі, який очікує гра: кореневий compound
// (порожнє ім'я) → список compound-ів "servers".
func encodeServersNBT(entries []ServerEntry) []byte {
	var b bytes.Buffer
	b.WriteByte(0x0A) // root compound
	writeNBTString(&b, "")

	b.WriteByte(0x09) // list tag
	writeNBTString(&b, "servers")
	b.WriteByte(0x0A) // element type: compound
	writeNBTInt32(&b, int32(len(entries)))

	for _, e := range entries {
		b.WriteByte(0x08) // string
		writeNBTString(&b, "name")
		writeNBTString(&b, e.Name)

		b.WriteByte(0x08) // string
		writeNBTString(&b, "ip")
		writeNBTString(&b, e.IP)

		if e.Icon != "" {
			b.WriteByte(0x08) // string
			writeNBTString(&b, "icon")
			writeNBTString(&b, e.Icon)
		}
		if e.AcceptTexturesSet {
			b.WriteByte(0x01) // byte
			writeNBTString(&b, "acceptTextures")
			b.WriteByte(e.AcceptTextures)
		}
		if e.HiddenSet {
			b.WriteByte(0x01) // byte
			writeNBTString(&b, "hidden")
			if e.Hidden {
				b.WriteByte(1)
			} else {
				b.WriteByte(0)
			}
		}
		if e.RequiredPackSkippedSet {
			b.WriteByte(0x01) // byte
			writeNBTString(&b, "requiredPackSkipped")
			if e.RequiredPackSkipped {
				b.WriteByte(1)
			} else {
				b.WriteByte(0)
			}
		}
		b.WriteByte(0x00) // end compound
	}
	b.WriteByte(0x00) // end root compound
	return b.Bytes()
}
