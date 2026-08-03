package minecraft

import (
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// ── Читання servers.dat (список серверів гри) ───────────────────────────
// servers.dat — це gzip-стиснений NBT-файл: кореневий compound "servers",
// всередині — список "servers" з compound-ів {name, ip, icon, ...}.
// НБТ-парсер мінімальний (потрібні лише string/list/compound), без
// зовнішніх залежностей — стандартна бібліотека.

// ServerEntry — один сервер з servers.dat збірки.
type ServerEntry struct {
	Name string
	IP   string
}

// ReadServersDat читає servers.dat і повертає список серверів
// (порядок збереження у файлі). Помилка (немає файлу/некоректний NBT) —
// порожній результат з помилкою; викликач вирішує, як показувати.
func ReadServersDat(path string) ([]ServerEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}
	return parseServersNBT(data)
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
			var sName, sIP string
			r.readCompound(func(k string, kt byte) {
				switch k {
				case "name":
					sName = r.readString()
				case "ip":
					sIP = r.readString()
				default:
					r.readValue(kt)
				}
			})
			if sIP != "" {
				out = append(out, ServerEntry{Name: sName, IP: sIP})
			}
		}
	})
	return out, nil
}
