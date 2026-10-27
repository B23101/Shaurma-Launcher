// store.go — локальна "книга адрес": зайняті P2P-адреси, які переживають
// перезапуск лаунчера.
//
// ── НАВІЩО ────────────────────────────────────────────────────────────
// Раніше адреса зберігалась ЛИШЕ у localStorage фронтенду і БУЛА ПРИВ'ЯЗАНА
// ДО LAN-ПОРТУ ("shaurma.p2p.address.<port>"). Але порт, який гра друкує в
// чаті при "Open to LAN", щоразу новий — тож кожен запуск лаунчера вимагав
// НОВОГО claim'у (нова випадкова адреса, новий суфікс), і друзям доводилось
// давати нову адресу. Це і був той самий подразник: «не зберігається
// занятий ip».
//
// Тепер книга живе на диску в .shaurma/p2p/addresses.json, а ключем є не
// ефемерний порт, а (акаунт, базова назва): claim повторно використовує вже
// зайняту адресу, а не створює щоразу нову.
//
// Файл навмисно окремий від settings.json: це не "налаштування гравця", а
// стан P2P-модуля (як owner-secret), і він не має бекапитись/експортуватись
// разом із налаштуваннями.
package p2p

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const addressBookFileName = "addresses.json"

// tempBookFileName — ОКРЕМЕ сховище тимчасових адрес (tmp0..tmp9, запрошення «у мій світ»).
// Постійні адреси (addresses.json) і тимчасові не змішуються ні на диску, ні в лімітах:
// закриття запрошення чи аварійний вихід чистять лише цей файл і не зачіпають збережені адреси.
const tempBookFileName = "temp-addresses.json"

// ClaimedAddress — одна зайнята адреса, яку лаунчер пам'ятає локально.
type ClaimedAddress struct {
	Subdomain string `json:"subdomain"` // "prlool-482917" (без суфікса .play...)
	BaseName  string `json:"baseName"`  // "prlool"
	// AccountID — акаунт, під яким адресу зайняли. Порожній = займали без
	// входу (легасі-записи до появи вимоги акаунта). Адреси показуються
	// лише власнику відповідного акаунта.
	AccountID string `json:"accountId,omitempty"`
	// Label — людяна мітка (напр. назва збірки/сервера), щоб у списку було
	// видно, яку саме адресу для чого займали.
	Label      string `json:"label,omitempty"`
	CreatedAt  int64  `json:"createdAt"`  // unix ms
	LastUsedAt int64  `json:"lastUsedAt"` // unix ms — останній StartP2PHost
}

// Address returns the full address to show/share.
func (a ClaimedAddress) Address() string { return a.Subdomain + "." + PlaySuffix }

// addressBook — вміст addresses.json.
type addressBook struct {
	Version   int              `json:"version"`
	Addresses []ClaimedAddress `json:"addresses"`
}

// readBookFile читає один файл книги. Відсутній файл — порожній список; пошкоджений JSON
// теж не валить P2P (сам файл НЕ видаляємо — може знадобитись для ручного відновлення).
func readBookFile(path string) ([]ClaimedAddress, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("p2p: читання %s: %w", path, err)
	}
	var book addressBook
	if err := json.Unmarshal(data, &book); err != nil {
		return nil, nil
	}
	return book.Addresses, nil
}

// loadAddressBook читає ОБИДВА сховища: addresses.json (постійні) і temp-addresses.json
// (тимчасові). У пам'яті це одна книга — викликачі (find/get/remove) працюють однаково, а на
// диску вони розділені (див. save). Тимчасові записи, що лишились у старому addresses.json
// (до появи окремого сховища), підхоплюються тут і при найближчому save переїдуть у temp-файл.
func loadAddressBook(stateDir string) (*addressBook, error) {
	perm, err := readBookFile(filepath.Join(stateDir, addressBookFileName))
	if err != nil {
		return nil, err
	}
	temp, err := readBookFile(filepath.Join(stateDir, tempBookFileName))
	if err != nil {
		return nil, err
	}
	book := &addressBook{Version: 1}
	seen := make(map[string]bool, len(perm)+len(temp))
	for _, a := range append(perm, temp...) {
		if seen[a.Subdomain] {
			continue
		}
		seen[a.Subdomain] = true
		book.Addresses = append(book.Addresses, a)
	}
	return book, nil
}

// writeBookFile атомарно пише один файл (temp + rename).
func writeBookFile(path string, list []ClaimedAddress) error {
	data, err := json.MarshalIndent(&addressBook{Version: 1, Addresses: list}, "", "  ")
	if err != nil {
		return fmt.Errorf("p2p: серіалізація книги адрес: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("p2p: запис %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("p2p: заміна %s: %w", path, err)
	}
	return nil
}

// save пише книгу атомарно у ДВА окремі файли: постійні → addresses.json, тимчасові →
// temp-addresses.json. Обрив процесу на півдорозі не лишає пошкодженого JSON.
func (b *addressBook) save(stateDir string) error {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("p2p: не вдалося створити %s: %w", stateDir, err)
	}
	var perm, temp []ClaimedAddress
	for _, a := range b.Addresses {
		if IsTempAddress(a) {
			temp = append(temp, a)
		} else {
			perm = append(perm, a)
		}
	}
	if err := writeBookFile(filepath.Join(stateDir, addressBookFileName), perm); err != nil {
		return err
	}
	tempPath := filepath.Join(stateDir, tempBookFileName)
	if len(temp) == 0 {
		// Порожнє сховище тимчасових — файл не тримаємо.
		if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("p2p: видалення %s: %w", tempPath, err)
		}
		return nil
	}
	return writeBookFile(tempPath, temp)
}

// find шукає вже зайняту адресу для цієї пари (базова назва, акаунт).
// Повертає найновішу, якщо записів кілька (напр. колись займали двічі) —
// щоб повторний claim завжди віддавав ту саму адресу.
func (b *addressBook) find(baseName, accountID string) (ClaimedAddress, bool) {
	var best ClaimedAddress
	found := false
	for _, a := range b.Addresses {
		if a.BaseName != baseName || a.AccountID != accountID {
			continue
		}
		if !found || a.CreatedAt > best.CreatedAt {
			best = a
			found = true
		}
	}
	return best, found
}

// upsert додає нову адресу або оновлює наявну з тим самим subdomain.
func (b *addressBook) upsert(a ClaimedAddress) {
	for i := range b.Addresses {
		if b.Addresses[i].Subdomain == a.Subdomain {
			b.Addresses[i] = a
			return
		}
	}
	b.Addresses = append(b.Addresses, a)
}

// remove прибирає адресу (явний Release у lobby).
func (b *addressBook) remove(subdomain string) bool {
	for i := range b.Addresses {
		if strings.EqualFold(b.Addresses[i].Subdomain, subdomain) {
			b.Addresses = append(b.Addresses[:i], b.Addresses[i+1:]...)
			return true
		}
	}
	return false
}

// touch оновлює LastUsedAt наявної адреси.
func (b *addressBook) touch(subdomain string, at time.Time) {
	for i := range b.Addresses {
		if b.Addresses[i].Subdomain == subdomain {
			b.Addresses[i].LastUsedAt = at.UnixMilli()
			return
		}
	}
}

// bind закріплює адресу за конкретним сервером (label) і ЗНІМАЄ ту саму
// мітку з будь-якої іншої адреси — одна адреса належить одному серверу.
// Інакше два сервери вказували б на ту саму адресу, а застовбити з них
// можна лише одну (той самий subdomain не можна хостити двічі).
// Повертає false, якщо адреси немає в книзі.
func (b *addressBook) bind(subdomain, label string) bool {
	found := false
	for i := range b.Addresses {
		if b.Addresses[i].Subdomain == subdomain {
			b.Addresses[i].Label = label
			found = true
			continue
		}
		if label != "" && b.Addresses[i].Label == label {
			b.Addresses[i].Label = ""
		}
	}
	return found
}

// get повертає адресу за subdomain (копію), якщо вона є в книзі.
func (b *addressBook) get(subdomain string) (ClaimedAddress, bool) {
	for _, a := range b.Addresses {
		if a.Subdomain == subdomain {
			return a, true
		}
	}
	return ClaimedAddress{}, false
}

// listAll — усі ПОСТІЙНІ адреси лаунчера незалежно від акаунта (найновіші спершу). Секрет
// власника зберігається в лаунчері, а не в акаунті, тож перемикання акаунта не має
// «губити» адреси: сервер перевіряє власність за секретом. Тимчасові (tmp0..tmp9) пропускаємо.
func (b *addressBook) listAll() []ClaimedAddress {
	out := make([]ClaimedAddress, 0, len(b.Addresses))
	for _, a := range b.Addresses {
		if !IsTempAddress(a) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].Subdomain < out[j].Subdomain
	})
	return out
}

// list повертає адреси цього акаунта (найновіші спершу). Порожній accountID
// означає "без акаунта" — так само точний збіг, без витоку чужих адрес.
func (b *addressBook) list(accountID string) []ClaimedAddress {
	out := make([]ClaimedAddress, 0, len(b.Addresses))
	for _, a := range b.Addresses {
		if a.AccountID == accountID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].Subdomain < out[j].Subdomain
	})
	return out
}
