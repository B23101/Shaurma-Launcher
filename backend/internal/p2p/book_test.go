package p2p

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Ці тести фіксують ГОЛОВНИЙ біль користувача: «не зберігається занятий ip і
// кожен раз при запуску лаунчера треба створювати новий». Причина була в
// тому, що адреса жила лише в localStorage фронтенду і була прив'язана до
// випадкового LAN-порту. Тепер вона лежить у .shaurma/p2p/addresses.json і
// прив'язана до (акаунт, базова назва).

func TestAddressBook_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	book := &addressBook{Version: 1}
	book.upsert(ClaimedAddress{Subdomain: "prlool-482917", BaseName: "prlool", AccountID: "acc1", CreatedAt: 100})
	if err := book.save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadAddressBook(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Addresses) != 1 {
		t.Fatalf("очікували 1 адресу, отримали %d", len(loaded.Addresses))
	}
	got := loaded.Addresses[0]
	if got.Subdomain != "prlool-482917" || got.BaseName != "prlool" || got.AccountID != "acc1" {
		t.Fatalf("адреса пошкоджена після round-trip: %+v", got)
	}
	if got.Address() != "prlool-482917."+PlaySuffix {
		t.Fatalf("Address() = %q", got.Address())
	}
}

// Відсутній файл — звичайний перший запуск, а не помилка.
func TestAddressBook_MissingFileIsEmptyBook(t *testing.T) {
	book, err := loadAddressBook(t.TempDir())
	if err != nil {
		t.Fatalf("load відсутнього файлу: %v", err)
	}
	if len(book.Addresses) != 0 {
		t.Fatalf("очікували порожню книгу, отримали %d адрес", len(book.Addresses))
	}
}

// Пошкоджений JSON не має валити P2P назавжди — краще порожня книга.
func TestAddressBook_CorruptFileDoesNotBreakP2P(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, addressBookFileName), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	book, err := loadAddressBook(dir)
	if err != nil {
		t.Fatalf("пошкоджений файл має давати порожню книгу, а не помилку: %v", err)
	}
	if len(book.Addresses) != 0 {
		t.Fatalf("очікували порожню книгу після пошкодження")
	}
}

func TestAddressBook_AccountIsolation(t *testing.T) {
	book := &addressBook{Version: 1}
	book.upsert(ClaimedAddress{Subdomain: "a-111111", BaseName: "a", AccountID: "acc1", CreatedAt: 1})
	book.upsert(ClaimedAddress{Subdomain: "b-222222", BaseName: "b", AccountID: "acc2", CreatedAt: 2})
	book.upsert(ClaimedAddress{Subdomain: "a-333333", BaseName: "a", AccountID: "", CreatedAt: 3})

	if got := book.list("acc1"); len(got) != 1 || got[0].Subdomain != "a-111111" {
		t.Fatalf("acc1 має бачити лише свою адресу: %+v", got)
	}
	if got := book.list("acc2"); len(got) != 1 || got[0].Subdomain != "b-222222" {
		t.Fatalf("acc2 має бачити лише свою адресу: %+v", got)
	}
	if got := book.list(""); len(got) != 1 || got[0].Subdomain != "a-333333" {
		t.Fatalf("без акаунта має бути лише безакаунтна адреса: %+v", got)
	}
	if got := book.list("nobody"); len(got) != 0 {
		t.Fatalf("чужий акаунт не має бачити адрес: %+v", got)
	}
}

func TestAddressBook_FindPicksSamePairOnly(t *testing.T) {
	book := &addressBook{Version: 1}
	book.upsert(ClaimedAddress{Subdomain: "a-111111", BaseName: "a", AccountID: "acc1", CreatedAt: 1})
	book.upsert(ClaimedAddress{Subdomain: "a-222222", BaseName: "a", AccountID: "acc1", CreatedAt: 5})
	book.upsert(ClaimedAddress{Subdomain: "a-333333", BaseName: "a", AccountID: "acc2", CreatedAt: 9})

	got, ok := book.find("a", "acc1")
	if !ok {
		t.Fatal("мали знайти адресу")
	}
	// Найновіша серед пари (acc1, a) — щоб повторний claim віддавав стабільну.
	if got.Subdomain != "a-222222" {
		t.Fatalf("find має віддавати найновішу адресу пари: %q", got.Subdomain)
	}
	if _, ok := book.find("a", "nobody"); ok {
		t.Fatal("не мали знайти адресу для чужого акаунта")
	}
}

// Мітка (label) — це прив'язка адреси до конкретного сервера в лаунчері.
// Одна адреса = один сервер: bind має знімати мітку з інших адрес.
func TestAddressBook_BindMovesLabel(t *testing.T) {
	book := &addressBook{Version: 1}
	book.upsert(ClaimedAddress{Subdomain: "a-111111", BaseName: "a", AccountID: "acc1"})
	book.upsert(ClaimedAddress{Subdomain: "b-222222", BaseName: "b", AccountID: "acc1"})

	book.bind("a-111111", "server-1")
	if book.Addresses[0].Label != "server-1" {
		t.Fatalf("bind мав проставити мітку: %+v", book.Addresses[0])
	}
	// Переприв'язуємо той самий сервер на іншу адресу — стара мітка має зникнути.
	book.bind("b-222222", "server-1")
	if book.Addresses[0].Label != "" {
		t.Fatalf("стара мітка не знята: %q", book.Addresses[0].Label)
	}
	if book.Addresses[1].Label != "server-1" {
		t.Fatalf("нова мітка не проставлена: %q", book.Addresses[1].Label)
	}
	if book.bind("nope", "server-2") {
		t.Fatal("bind невідомої адреси мав повернути false")
	}
}

// AssignClaimedAddress — вибір збереженої адреси для сервера з UI.
func TestAssignClaimedAddress(t *testing.T) {
	srv, _ := claimServer(t)
	api := NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return srv.URL })

	first, err := api.ClaimP2PAddress("alpha", "acc1", "server-1")
	if err != nil {
		t.Fatalf("claim alpha: %v", err)
	}
	second, err := api.ClaimP2PAddress("beta", "acc1", "server-2")
	if err != nil {
		t.Fatalf("claim beta: %v", err)
	}

	// server-3 хоче ВЖЕ ЗАЙНЯТУ адресу першого сервера.
	if err := api.AssignClaimedAddress(first.Subdomain, "server-3", "acc1"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	list := api.ListClaimedAddresses("acc1")
	labels := map[string]string{}
	for _, a := range list {
		labels[a.Subdomain] = a.Label
	}
	if labels[first.Subdomain] != "server-3" {
		t.Fatalf("адреса не закріплена за server-3: %q", labels[first.Subdomain])
	}
	if labels[second.Subdomain] != "server-2" {
		t.Fatalf("мітка другого сервера зіпсована: %q", labels[second.Subdomain])
	}

	// Чужий акаунт не може призначити собі нашу адресу.
	if err := api.AssignClaimedAddress(first.Subdomain, "server-9", "other"); err == nil {
		t.Fatal("чужий акаунт не мав призначити нашу адресу")
	}
	// Неіснуюча адреса.
	if err := api.AssignClaimedAddress("nope-000000", "s", "acc1"); err == nil {
		t.Fatal("неіснуюча адреса мала дати помилку")
	}
}

func TestAddressBook_RemoveAndTouch(t *testing.T) {
	book := &addressBook{Version: 1}
	book.upsert(ClaimedAddress{Subdomain: "a-111111", BaseName: "a", AccountID: "acc1"})
	if !book.remove("a-111111") {
		t.Fatal("remove мав повернути true")
	}
	if book.remove("a-111111") {
		t.Fatal("повторний remove мав повернути false")
	}
	book.upsert(ClaimedAddress{Subdomain: "b-222222", BaseName: "b", AccountID: "acc1"})
	book.touch("b-222222", time.UnixMilli(1234))
	if book.Addresses[0].LastUsedAt != 1234 {
		t.Fatalf("touch не оновив LastUsedAt: %d", book.Addresses[0].LastUsedAt)
	}
}

// ── API-рівень: claim ПОВТОРНО використовує вже зайняту адресу ────────────

// claimServer піднімає фейковий lobby, який видає послідовні адреси й рахує
// кількість викликів /p2p/claim.
func claimServer(t *testing.T) (*httptest.Server, *int64) {
	t.Helper()
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/p2p/release" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		n := atomic.AddInt64(&calls, 1)
		_ = json.NewEncoder(w).Encode(ClaimResponse{Subdomain: "prlool-00000" + string(rune('0'+n))})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestClaimP2PAddress_ReusesExistingAddress(t *testing.T) {
	srv, calls := claimServer(t)
	api := NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return srv.URL })

	first, err := api.ClaimP2PAddress("prlool", "acc1", "pack-1")
	if err != nil {
		t.Fatalf("перший claim: %v", err)
	}
	if first.Reused {
		t.Error("перший claim не може бути reused")
	}
	if first.Subdomain != "prlool-000001" {
		t.Fatalf("subdomain = %q", first.Subdomain)
	}
	if first.Address != "prlool-000001."+PlaySuffix {
		t.Fatalf("address = %q", first.Address)
	}

	// Друге застовблення тієї ж бази тим самим акаунтом — БЕЗ нового запису.
	again, err := api.ClaimP2PAddress("prlool", "acc1", "pack-1")
	if err != nil {
		t.Fatalf("повторний claim: %v", err)
	}
	if !again.Reused {
		t.Error("повторний claim мав бути reused (це і є виправлення бага)")
	}
	if again.Subdomain != first.Subdomain {
		t.Fatalf("адреса змінилась при повторному claim: %q → %q", first.Subdomain, again.Subdomain)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("мав бути лише 1 мережевий claim, а було %d", got)
	}

	// Інший акаунт з тією ж базою — нова адреса (ізоляція акаунтів).
	other, err := api.ClaimP2PAddress("prlool", "acc2", "pack-1")
	if err != nil {
		t.Fatalf("claim іншого акаунта: %v", err)
	}
	if other.Reused || other.Subdomain == first.Subdomain {
		t.Fatalf("інший акаунт мав отримати власну адресу: %+v", other)
	}
	if got := atomic.LoadInt64(calls); got != 2 {
		t.Fatalf("очікували 2 мережеві claim'и, отримали %d", got)
	}
}

// Адреси переживають перезапуск лаунчера: новий API на тій самій теці
// (тобто новий процес) бачить ту саму книгу й не йде в мережу заново.
func TestClaimP2PAddress_SurvivesRestart(t *testing.T) {
	srv, calls := claimServer(t)
	dir := t.TempDir()
	baseURL := func() string { return srv.URL }

	api1 := NewAPI(dir, func() string { return "token-1234567890abcdef" }, baseURL)
	first, err := api1.ClaimP2PAddress("prlool", "acc1", "pack-1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// «Перезапуск»: новий API-об'єкт, той самий каталог.
	api2 := NewAPI(dir, func() string { return "token-1234567890abcdef" }, baseURL)
	after, err := api2.ClaimP2PAddress("prlool", "acc1", "pack-1")
	if err != nil {
		t.Fatalf("claim після перезапуску: %v", err)
	}
	if after.Subdomain != first.Subdomain {
		t.Fatalf("після перезапуску адреса змінилась: %q → %q", first.Subdomain, after.Subdomain)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("після перезапуску не має бути нового мережевого claim (було %d)", got)
	}

	list := api2.ListClaimedAddresses("acc1")
	if len(list) != 1 || list[0].Subdomain != first.Subdomain {
		t.Fatalf("книга після перезапуску: %+v", list)
	}
}

func TestReleaseP2PAddress_ForgetsLocally(t *testing.T) {
	srv, _ := claimServer(t)
	api := NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return srv.URL })

	claimed, err := api.ClaimP2PAddress("prlool", "acc1", "")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Фейковий lobby віддає 200 на release (Req.URL.Path = /p2p/release).
	if err := api.ReleaseP2PAddress(claimed.Subdomain); err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := api.ListClaimedAddresses("acc1"); len(got) != 0 {
		t.Fatalf("після release адреса мала зникнути з книги: %+v", got)
	}
}

func TestBaseNameOf(t *testing.T) {
	cases := map[string]string{
		"prlool-482917": "prlool",
		"prlool":        "prlool",
		"a-b-c-999999":  "a-b-c",
	}
	for in, want := range cases {
		if got := baseNameOf(in); got != want {
			t.Fatalf("baseNameOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// Усі акаунти одного лаунчера бачать ті самі постійні адреси (тимчасові — ні).
func TestListClaimedAddresses_SharedAcrossAccounts(t *testing.T) {
	api := NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return "" })
	api.mu.Lock()
	book, err := api.bookLocked()
	if err != nil {
		api.mu.Unlock()
		t.Fatal(err)
	}
	book.upsert(ClaimedAddress{Subdomain: "a-111111", BaseName: "a", AccountID: "acc1", CreatedAt: 1})
	book.upsert(ClaimedAddress{Subdomain: "b-222222", BaseName: "b", AccountID: "acc2", CreatedAt: 2})
	book.upsert(ClaimedAddress{Subdomain: "tmp1-333333", BaseName: "tmp1", AccountID: "acc1", CreatedAt: 3})
	api.mu.Unlock()
	for _, acc := range []string{"acc1", "acc2", "acc3"} {
		got := api.ListClaimedAddresses(acc)
		if len(got) != 2 {
			t.Fatalf("%s: очікували 2 постійні адреси, отримали %+v", acc, got)
		}
	}
}

// Тимчасові й постійні адреси лежать у РІЗНИХ файлах.
func TestAddressBook_TempStoredSeparately(t *testing.T) {
	dir := t.TempDir()
	book := &addressBook{Version: 1}
	book.upsert(ClaimedAddress{Subdomain: "a-111111", BaseName: "a", AccountID: "acc1", CreatedAt: 1})
	book.upsert(ClaimedAddress{Subdomain: "tmp2-444444", BaseName: "tmp2", AccountID: "acc1", CreatedAt: 2})
	if err := book.save(dir); err != nil {
		t.Fatal(err)
	}
	perm, _ := readBookFile(dir + "/" + addressBookFileName)
	temp, _ := readBookFile(dir + "/" + tempBookFileName)
	if len(perm) != 1 || perm[0].Subdomain != "a-111111" {
		t.Fatalf("постійні: %+v", perm)
	}
	if len(temp) != 1 || temp[0].Subdomain != "tmp2-444444" {
		t.Fatalf("тимчасові: %+v", temp)
	}
	loaded, err := loadAddressBook(dir)
	if err != nil || len(loaded.Addresses) != 2 {
		t.Fatalf("load: %v %+v", err, loaded)
	}
}
