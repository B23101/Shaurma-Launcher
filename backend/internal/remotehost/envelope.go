// Package remotehost — віддалене керування серверами друзів: сторона ВЛАСНИКА (Host виконує)
// і сторона ДРУГА (Client надсилає). Ніякого мережевого коду тут нема: усе йде через інтерфейси
// Net/Crypto/Servers/Friends, тож логіку безпеки можна перевірити тестами без VPS і без гри.
//
// Рубежі захисту власника (кожен — окремо, без жодного довіри «на слово»):
//  1. мандат VPS: підпис Ed25519 закріпленого ключа, прив'язка до відправника, reqId, sid
//     і хеша конверта (VPS підтвердив дружбу й право САМЕ на цю команду, щойно);
//  2. E2E-конверт: розшифровується ключем САМЕ цього друга — VPS не може вкласти туди чужу дію;
//     дія/сервер/сесія в конверті мусять збігатися з мандатом;
//  3. свіжість і заборона повторів (час + кеш побачених id);
//  4. локальні права власника (remotectl): дружба, право, пауза, ліміти, політики;
//  5. команди консолі — очищення, заборона привілейованих і перекриття «stop» правом Stop.
package remotehost

import (
	"bytes"
	"encoding/json"
	"time"
)

const (
	envVersion = 1
	// MaxPlain — межа відкритого тексту конверта (як social.MaxPlaintextBytes).
	MaxPlain = 1200

	tsWindow  = 5 * time.Minute
	replayTTL = 15 * time.Minute
)

// Line — рядок консолі в потоці.
type Line struct {
	ID uint64 `json:"i"`
	T  string `json:"t"` // HH:MM:SS
	L  string `json:"l"` // info | warn | error | system
	X  string `json:"x"` // текст
}

// envelope — вміст E2E-конверта (і команди, і відповіді, і шматка потоку).
type envelope struct {
	V     int    `json:"v"`
	K     string `json:"k"` // "cmd" | "res" | "out"
	ID    string `json:"id,omitempty"`
	Op    string `json:"op,omitempty"`
	Srv   string `json:"srv,omitempty"`
	SID   string `json:"sid,omitempty"`
	TS    int64  `json:"ts"`
	N     string `json:"n,omitempty"` // випадковий nonce проти повторів
	Cmd   string `json:"cmd,omitempty"`
	OK    bool   `json:"ok,omitempty"`
	Code  string `json:"code,omitempty"`
	State string `json:"state,omitempty"`
	Final bool   `json:"final,omitempty"`
	Lines []Line `json:"lines,omitempty"`
	End   bool   `json:"end,omitempty"` // у "out": сесію завершено власником
}

func (e envelope) marshal() (string, bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // «<», «>», «&» не роздувають розмір у 6 разів
	if enc.Encode(e) != nil {
		return "", false
	}
	b := bytes.TrimRight(buf.Bytes(), "\n")
	if len(b) > MaxPlain {
		return "", false
	}
	return string(b), true
}

func parseEnvelope(plain, kind string) (envelope, bool) {
	var e envelope
	if len(plain) > MaxPlain || json.Unmarshal([]byte(plain), &e) != nil || e.V != envVersion || e.K != kind {
		return e, false
	}
	return e, true
}

func fresh(ts int64, now time.Time) bool {
	d := now.Sub(time.UnixMilli(ts))
	if d < 0 {
		d = -d
	}
	return d <= tsWindow
}
