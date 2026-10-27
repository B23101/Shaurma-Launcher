// pool.go — пул ізольованих локальних loopback-адрес для нової P2P-схеми
// "розумного маршрутизатора": замість одного спільного 127.0.0.1:45678
// кожна ОДНОЧАСНА кімната (хостингова чи гостьова) отримує власну
// 127.0.0.X, на якій ми піднімаємо СПРАВЖНІй Minecraft-порт 25565.
// Пакети при цьому ніяк не підмінюються — ми лише читаємо домен із
// першого пакета і вирішуємо, ЯКУ 127.0.0.X дати цьому з'єднанню.
//
// 127.0.0.1 свідомо НЕ займаємо — цю адресу лишаємо вільною під локальні
// потреби гравця (свій "Open to LAN" сервер, інші застосунки), щоб P2P
// ніколи не конфліктував з чимось локальним. 127.0.0.2 теж пропускаємо —
// це guestExternalAddr (guest.go), єдиний зовнішній listener гостя, і
// видача її ж під внутрішню кімнату призводить до bind-конфлікту з самим
// собою на першій-таки кімнаті. Пул стартує з 127.0.0.3.
package p2p

import (
	"fmt"
	"sync"
)

// MaxConcurrentRooms — жорсткий хард-ліміт: один гравець може одночасно
// хостити/приєднуватись не більше ніж до стількох P2P-кімнат. Ресурси
// ПК (tsnet-ноди, WireGuard-тунелі, TCP-listener'и) не безмежні — це
// свідоме архітектурне обмеження, а не тимчасовий недолік.
const MaxConcurrentRooms = 3

// ipPoolBase — з цієї адреси починається видача (127.0.0.1 — локальні
// потреби гравця, 127.0.0.2 — guestExternalAddr, обидві пропускаємо).
var ipPoolBase = [4]byte{127, 0, 0, 3}

// ErrPoolExhausted повертається, коли всі MaxConcurrentRooms адрес вже
// зайняті іншими кімнатами і вільної немає.
var ErrPoolExhausted = fmt.Errorf("p2p: досягнуто ліміту %d одночасних P2P-серверів", MaxConcurrentRooms)

// localIPPool — потокобезпечний розподільник 127.0.0.3..127.0.0.(2+N).
// Один екземпляр обслуговує ОДНУ роль (гостьову або хостову) в межах
// одного лаунчера: у гостя і хоста свої окремі пули, тож, наприклад,
// одночасний хостинг 2 серверів і приєднання до 1 стороннього — це
// 2 адреси в one пулі + 1 в іншому, а не 3 в спільному.
type localIPPool struct {
	mu   sync.Mutex
	used map[string]string // 127.0.0.X -> ключ кімнати (домен/subdomain), яка її тримає
}

func newLocalIPPool() *localIPPool {
	return &localIPPool{used: make(map[string]string, MaxConcurrentRooms)}
}

// acquire повертає вже виділену адресу для цього ключа кімнати (ідемпотентно
// для повторних підключень до тієї самої кімнати), або видає нову вільну
// адресу з пулу. ErrPoolExhausted — якщо всі MaxConcurrentRooms зайняті
// ІНШИМИ кімнатами.
func (p *localIPPool) acquire(roomKey string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for ip, owner := range p.used {
		if owner == roomKey {
			return ip, nil // та сама кімната — повертаємо вже виділену адресу
		}
	}
	if len(p.used) >= MaxConcurrentRooms {
		return "", ErrPoolExhausted
	}
	for i := 0; i < MaxConcurrentRooms; i++ {
		ip := nthLoopbackIP(i)
		if _, taken := p.used[ip]; !taken {
			p.used[ip] = roomKey
			return ip, nil
		}
	}
	// Не мало статись за тих самих умов (len < Max, але вільної не
	// знайшлося) — захист від помилки в майбутньому рефакторингу.
	return "", ErrPoolExhausted
}

// release звільняє адресу, закріплену за roomKey (закриття кімнати/сесії).
// No-op, якщо roomKey зараз нічого не тримає.
func (p *localIPPool) release(roomKey string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for ip, owner := range p.used {
		if owner == roomKey {
			delete(p.used, ip)
			return
		}
	}
}

// count — скільки кімнат зараз активні (для показу "2/3" у статусі UI).
func (p *localIPPool) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.used)
}

// nthLoopbackIP — i=0 -> "127.0.0.3", i=1 -> "127.0.0.4", ...
func nthLoopbackIP(i int) string {
	b := ipPoolBase
	b[3] += byte(i)
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}
