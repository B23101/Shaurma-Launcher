package minecraft

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// buildServersDat збирає GZIP-стиснутий NBT файл servers.dat зі списком
// серверів (старіший формат гри, використовується у
// TestReadServersDatGzipStillWorks для перевірки зворотної сумісності;
// сучасний плаский формат тестує WriteServersDat/TestReadServersDatPlainNoGzip).
func buildServersDat(t *testing.T, servers [][2]string) []byte {
	t.Helper()
	entries := make([]ServerEntry, len(servers))
	for i, s := range servers {
		entries[i] = ServerEntry{Name: s[0], IP: s[1]}
	}
	buf := encodeServersNBT(entries)

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(buf); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return gz.Bytes()
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

func TestWriteServersDatRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/servers.dat"
	entries := []ServerEntry{
		{Name: "Shaurma SMP", IP: "smp.shaurma.gg:25565", Icon: "aGVsbG8=", AcceptTextures: 1, AcceptTexturesSet: true},
		{Name: "Local", IP: "127.0.0.1:25566"},
	}
	if err := WriteServersDat(path, entries); err != nil {
		t.Fatalf("WriteServersDat: %v", err)
	}
	got, err := ReadServersDat(path)
	if err != nil {
		t.Fatalf("ReadServersDat: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Name != "Shaurma SMP" || got[0].IP != "smp.shaurma.gg:25565" || got[0].Icon != "aGVsbG8=" {
		t.Errorf("entry 0 mismatch: %+v", got[0])
	}
	if !got[0].AcceptTexturesSet || got[0].AcceptTextures != 1 {
		t.Errorf("entry 0 acceptTextures mismatch: %+v", got[0])
	}
	if got[1].Name != "Local" || got[1].IP != "127.0.0.1:25566" || got[1].Icon != "" {
		t.Errorf("entry 1 mismatch: %+v", got[1])
	}
	if got[1].AcceptTexturesSet {
		t.Errorf("entry 1 should not have acceptTextures set: %+v", got[1])
	}
}

func TestWriteServersDatEmpty(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/servers.dat"
	if err := WriteServersDat(path, nil); err != nil {
		t.Fatalf("WriteServersDat empty: %v", err)
	}
	got, err := ReadServersDat(path)
	if err != nil {
		t.Fatalf("ReadServersDat empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 entries, got %d", len(got))
	}
}

func TestReadServersDatMissing(t *testing.T) {
	if _, err := ReadServersDat("/nonexistent/servers.dat"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestReadServersDatPlainNoGzip перевіряє реальний формат, який пишуть
// сучасні версії гри: NBT БЕЗ gzip-стиснення (звичайний баг раніше — код
// вимагав gzip і мовчки повертав порожній список для таких файлів).
// Поля у справжньому файлі йдуть у порядку hidden → ip → name → icon →
// requiredPackSkipped (НЕ name → ip, як у старих прикладах) — парсер має
// читати за іменами тегів, а не за позицією.
func TestReadServersDatPlainNoGzip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/servers.dat"

	var b bytes.Buffer
	b.WriteByte(0x0A) // root compound
	writeNBTString(&b, "")
	b.WriteByte(0x09) // servers list
	writeNBTString(&b, "servers")
	b.WriteByte(0x0A) // element type: compound
	writeNBTInt32(&b, 1)

	// один сервер, поля у "реальному" порядку: hidden, ip, name,
	// requiredPackSkipped (без icon — теж валідний випадок)
	b.WriteByte(0x01)
	writeNBTString(&b, "hidden")
	b.WriteByte(0)
	b.WriteByte(0x08)
	writeNBTString(&b, "ip")
	writeNBTString(&b, "play.mccisland.net")
	b.WriteByte(0x08)
	writeNBTString(&b, "name")
	writeNBTString(&b, "Сервер Minecraft")
	b.WriteByte(0x01)
	writeNBTString(&b, "requiredPackSkipped")
	b.WriteByte(0)
	b.WriteByte(0x00) // end server compound

	b.WriteByte(0x00) // end root compound

	if err := os.WriteFile(path, b.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	// Перевіряємо, що файл дійсно НЕ gzip (magic-байти) — тест сам себе
	// не обманює фолбеком на стиснутий формат.
	if b.Bytes()[0] == 0x1F && b.Bytes()[1] == 0x8B {
		t.Fatal("test fixture is accidentally gzip-compressed")
	}

	entries, err := ReadServersDat(path)
	if err != nil {
		t.Fatalf("ReadServersDat (plain, no gzip): %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Name != "Сервер Minecraft" || e.IP != "play.mccisland.net" {
		t.Errorf("entry mismatch: %+v", e)
	}
	if !e.HiddenSet || e.Hidden {
		t.Errorf("hidden mismatch: %+v", e)
	}
	if !e.RequiredPackSkippedSet || e.RequiredPackSkipped {
		t.Errorf("requiredPackSkipped mismatch: %+v", e)
	}
}

// TestReadServersDatGzipStillWorks — старіші версії гри пишуть servers.dat
// gzip-стиснутим; auto-detect не має цього зламати.
func TestReadServersDatGzipStillWorks(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/servers.dat"
	if err := writeFile(path, buildServersDat(t, [][2]string{{"Gzip Server", "gzip.example.com"}})); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadServersDat(path)
	if err != nil {
		t.Fatalf("ReadServersDat (gzip): %v", err)
	}
	if len(entries) != 1 || entries[0].IP != "gzip.example.com" {
		t.Errorf("unexpected: %+v", entries)
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

// EnsureOfficialServer: файла немає — офіційний сервер додається.
func TestEnsureOfficialServerCreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/servers.dat"
	added, err := EnsureOfficialServer(path, "Shaurma", "play.example.com")
	if err != nil {
		t.Fatalf("EnsureOfficialServer: %v", err)
	}
	if !added {
		t.Fatal("очікувалося додавання при відсутньому файлі")
	}
	entries, err := ReadServersDat(path)
	if err != nil {
		t.Fatalf("ReadServersDat: %v", err)
	}
	if len(entries) != 1 || entries[0].IP != "play.example.com" || entries[0].Name != "Shaurma" {
		t.Fatalf("некоректний вміст після додавання: %+v", entries)
	}
	// Повторний виклик — сервер уже є, нічого не міняється.
	added, err = EnsureOfficialServer(path, "Shaurma", "play.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Fatal("сервер не мав додаватись удруге")
	}
}

// EnsureOfficialServer: «порожній» servers.dat (валідний NBT без серверів)
// — офіційний сервер додається (саме кейс з репорту «пустий servers.dat»).
func TestEnsureOfficialServerAddsToEmptyList(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/servers.dat"
	if err := WriteServersDat(path, nil); err != nil {
		t.Fatalf("WriteServersDat empty: %v", err)
	}
	added, err := EnsureOfficialServer(path, "Shaurma", "play.example.com")
	if err != nil {
		t.Fatalf("EnsureOfficialServer: %v", err)
	}
	if !added {
		t.Fatal("офіційний сервер мав додатись у порожній список")
	}
	entries, _ := ReadServersDat(path)
	if len(entries) != 1 || entries[0].IP != "play.example.com" {
		t.Fatalf("некоректний вміст: %+v", entries)
	}
}

// EnsureOfficialServer: список гравця не порожній — файл не чіпаємо.
func TestEnsureOfficialServerKeepsPlayerList(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/servers.dat"
	if err := WriteServersDat(path, []ServerEntry{{Name: "My SMP", IP: "smp.example.net"}}); err != nil {
		t.Fatalf("WriteServersDat: %v", err)
	}
	added, err := EnsureOfficialServer(path, "Shaurma", "play.example.com")
	if err != nil {
		t.Fatalf("EnsureOfficialServer: %v", err)
	}
	if added {
		t.Fatal("список гравця не мав змінюватись")
	}
	entries, _ := ReadServersDat(path)
	if len(entries) != 1 || entries[0].IP != "smp.example.net" {
		t.Fatalf("файл гравця змінений: %+v", entries)
	}
}

// EnsureOfficialServer: порожній serverIP — no-op, файл не створюється.
func TestEnsureOfficialServerEmptyIPNoop(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/servers.dat"
	added, err := EnsureOfficialServer(path, "Shaurma", "   ")
	if err != nil {
		t.Fatalf("EnsureOfficialServer: %v", err)
	}
	if added {
		t.Fatal("порожній serverIP не мав нічого додавати")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("файл не мав створюватись при порожньому serverIP")
	}
}

func TestSyncOfficialServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.dat")

	// 1) файлу нема → створюється з офіційним сервером
	if ch, _, err := SyncOfficialServer(path, "Збірка", "old.example:25565", ""); err != nil || !ch {
		t.Fatalf("створення: changed=%v err=%v", ch, err)
	}
	// 2) гравець додав власний сервер
	es, _ := ReadServersDat(path)
	es = append(es, ServerEntry{Name: "Мій", IP: "mine.example"})
	if err := WriteServersDat(path, es); err != nil {
		t.Fatal(err)
	}
	// 3) IP на CDN змінився → оновлюється ТОЙ САМИЙ запис (за prevIP), без дубліката
	ch, old, err := SyncOfficialServer(path, "Збірка", "new.example:25565", "old.example:25565")
	if err != nil || !ch || old != "old.example:25565" {
		t.Fatalf("оновлення IP: changed=%v old=%q err=%v", ch, old, err)
	}
	es, _ = ReadServersDat(path)
	if len(es) != 2 || es[0].IP != "new.example:25565" || es[0].Name != "Збірка" || es[1].IP != "mine.example" {
		t.Fatalf("неочікуваний список: %+v", es)
	}
	// 4) повторний виклик — no-op
	if ch, _, _ := SyncOfficialServer(path, "Збірка", "new.example:25565", "new.example:25565"); ch {
		t.Fatal("актуальний запис не має переписуватись")
	}
	// 5) гравець видалив офіційний запис → повертається НА ПОЧАТОК, власний лишається
	if err := WriteServersDat(path, []ServerEntry{{Name: "Мій", IP: "mine.example"}}); err != nil {
		t.Fatal(err)
	}
	if ch, _, _ := SyncOfficialServer(path, "Збірка", "new.example:25565", "new.example:25565"); !ch {
		t.Fatal("видалений офіційний сервер має повернутись")
	}
	es, _ = ReadServersDat(path)
	if len(es) != 2 || es[0].IP != "new.example:25565" || es[1].IP != "mine.example" {
		t.Fatalf("після повернення: %+v", es)
	}
	// 6) запис за іменем збірки (prevIP невідомий — перше ввімкнення опції)
	if err := WriteServersDat(path, []ServerEntry{{Name: "Збірка", IP: "ancient.example"}}); err != nil {
		t.Fatal(err)
	}
	if ch, old, _ := SyncOfficialServer(path, "Збірка", "new.example:25565", ""); !ch || old != "ancient.example" {
		t.Fatalf("за іменем: changed=%v old=%q", ch, old)
	}
	es, _ = ReadServersDat(path)
	if len(es) != 1 || es[0].IP != "new.example:25565" {
		t.Fatalf("дублікат за іменем: %+v", es)
	}
}
