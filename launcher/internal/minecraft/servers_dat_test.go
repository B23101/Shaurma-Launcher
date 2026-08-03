package minecraft

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"os"
	"testing"
)

// buildServersDat збирає gzip-NBT файл servers.dat зі списком серверів.
func buildServersDat(t *testing.T, servers [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	// кореневий compound
	buf.WriteByte(0x0A)
	writeNBTString(&buf, "") // root name
	// "servers" list
	buf.WriteByte(0x09)
	writeNBTString(&buf, "servers")
	buf.WriteByte(0x0A) // element type: compound
	writeInt32(&buf, int32(len(servers)))
	for _, s := range servers {
		buf.WriteByte(0x08)
		writeNBTString(&buf, "name")
		writeNBTString(&buf, s[0])
		buf.WriteByte(0x08)
		writeNBTString(&buf, "ip")
		writeNBTString(&buf, s[1])
		buf.WriteByte(0x00) // end compound
	}
	buf.WriteByte(0x00) // end root

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return gz.Bytes()
}

func writeNBTString(b *bytes.Buffer, s string) {
	b.WriteByte(byte(len(s) >> 8))
	b.WriteByte(byte(len(s)))
	b.WriteString(s)
}

func writeInt32(b *bytes.Buffer, v int32) {
	b.WriteByte(byte(v >> 24))
	b.WriteByte(byte(v >> 16))
	b.WriteByte(byte(v >> 8))
	b.WriteByte(byte(v))
}

func TestParseServersNBT(t *testing.T) {
	gz := buildServersDat(t, [][2]string{
		{"Test Server", "play.example.com"},
		{"Local", "127.0.0.1:25566"},
	})
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(zr); err != nil {
		t.Fatal(err)
	}
	zr.Close()
	entries, err := parseServersNBT(raw.Bytes())
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "Test Server" || entries[0].IP != "play.example.com" {
		t.Errorf("entry 0 mismatch: %+v", entries[0])
	}
	if entries[1].Name != "Local" || entries[1].IP != "127.0.0.1:25566" {
		t.Errorf("entry 1 mismatch: %+v", entries[1])
	}
}

func TestReadServersDat(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/servers.dat"
	if err := writeFile(path, buildServersDat(t, [][2]string{{"A", "a.b"}})); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadServersDat(path)
	if err != nil {
		t.Fatalf("ReadServersDat: %v", err)
	}
	if len(entries) != 1 || entries[0].IP != "a.b" {
		t.Errorf("unexpected: %+v", entries)
	}
}

func TestReadServersDatMissing(t *testing.T) {
	if _, err := ReadServersDat("/nonexistent/servers.dat"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestVarintRoundtrip(t *testing.T) {
	values := []int64{0, 1, 127, 128, 255, 300, 65535, 2147483647, -1}
	for _, v := range values {
		enc := appendVarint(nil, v)
		dec, err := readVarint(bufio.NewReader(bytes.NewReader(enc)))
		if err != nil {
			t.Fatalf("decode %d: %v", v, err)
		}
		if dec != v {
			t.Errorf("roundtrip %d -> %d", v, dec)
		}
	}
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
