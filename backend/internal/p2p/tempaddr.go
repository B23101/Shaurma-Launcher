// tempaddr.go — тимчасові адреси (запрошення «у мій світ») НЕ є збереженими.
//
// ── ЧОМУ ЦЕ ОКРЕМО ──────────────────────────────────────────────────────
// Тимчасове запрошення бере одну з трьох адрес tmp1..tmp3 через ту саму книгу, що й адреси серверів.
// Через це «tmp1-684140…» лишалась у addresses.json після закриття світу, потрапляла у вікно
// «Вибрати зі збережених» і навіть закріплювалась за звичайним сервером. Тимчасова адреса живе рівно
// стільки, скільки живе запрошення, тому:
//   - у списки для UI вона не віддається взагалі (ListClaimedAddresses фільтрує через IsTempAddress);
//   - закріпити її за сервером не можна (AssignClaimedAddress);
//   - після закриття запрошення / лаунчера адресу звільняємо у lobby і забуваємо з книги
//     (ReleaseTempAddress, PurgeTempAddresses).
package p2p

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"
)

// TempLabel — мітка, з якою тимчасове запрошення бере адресу з книги.
const TempLabel = "temp-invite"

// tmpBaseRe — базові назви tmp1..tmp3, зарезервовані під тимчасові запрошення.
var tmpBaseRe = regexp.MustCompile(`^tmp[0-9]$`)

// IsTempBase — чи зарезервована ця базова назва під тимчасові запрошення.
func IsTempBase(base string) bool {
	return tmpBaseRe.MatchString(strings.ToLower(strings.TrimSpace(base)))
}

// IsTempAddress — чи це адреса тимчасового запрошення (за міткою або за зарезервованою базою).
func IsTempAddress(a ClaimedAddress) bool {
	return a.Label == TempLabel || IsTempBase(a.BaseName)
}

// ReleaseTempAddress звільняє тимчасову адресу: зупиняє хост-сесію, віддає адресу в lobby і прибирає
// з книги. Помилка lobby не страшна — з книги запис зникає в будь-якому разі (див. ReleaseP2PAddress).
func (a *API) ReleaseTempAddress(subdomain string) error {
	return a.ReleaseP2PAddress(subdomain)
}

// PurgeTempAddresses звільняє УСІ тимчасові адреси з книги. Викликається на старті лаунчера (живих
// запрошень тоді немає, лишилось лише те, що не встигло прибратись після аварійного виходу) і при
// виході. Книгу чистимо одразу, а запити в lobby йдуть паралельно й обмежені за часом, щоб вихід
// із лаунчера не висів на мережі.
func (a *API) PurgeTempAddresses(budget time.Duration) int {
	a.mu.Lock()
	book, err := a.bookLocked()
	if err != nil {
		a.mu.Unlock()
		return 0
	}
	var subs []string
	for _, c := range book.Addresses {
		if IsTempAddress(c) {
			subs = append(subs, c.Subdomain)
		}
	}
	a.mu.Unlock()
	if len(subs) == 0 {
		return 0
	}

	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Add(1)
		go func(sub string) {
			defer wg.Done()
			_ = a.ReleaseTempAddress(sub)
		}(sub)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	select {
	case <-done:
	case <-ctx.Done():
		// Мережа не відповіла вчасно: з книги все одно прибираємо (Release робить це навіть при
		// помилці lobby, але тут могли не дійти до цього рядка). Запис у lobby згодом протухне сам.
		for _, sub := range subs {
			a.forgetClaimed(sub)
		}
	}
	return len(subs)
}
