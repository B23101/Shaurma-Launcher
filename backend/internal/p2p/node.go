// node.go — спільний підйом ефемерної tsnet-ноди для Host і Guest з
// повторною спробою на ЧИСТОМУ стані.
//
// Навіщо повторна спроба: найпоширеніша причина «нода не піднялась» — стара
// ідентичність ноди, якої Headscale вже не знає (node not found / invalid
// key / node expired). Перша спроба вже йде з унікальною текою, але якщо
// навіть вона впала саме з такої причини (напр. Headscale встиг прибрати
// ноду між реєстрацією й першим handshake), другу спробу робимо з НОВИМ
// auth key і НОВОЮ текою — цього достатньо, щоб реєстрація пройшла, і
// гравець не бачив «невідомий вузол» замість робочого тунелю.
package p2p

import (
	"context"
	"fmt"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/launchlog"

	"tailscale.com/tsnet"
)

// nodeUpTimeout — скільки чекаємо на реєстрацію ноди в Headscale. Мережа
// після свіжого auth key зазвичай укладається за 1-3 секунди; 30 с — це
// вже «щось не так із контрол-сервером», і краще повернути помилку в UI,
// ніж вішати лаунчер.
const nodeUpTimeout = 30 * time.Second

// nodeUpConfig — параметри однієї спроби підйому ноди.
type nodeUpConfig struct {
	// Role/Tag — лише для логів: "host"/"guest" і наскрізний ID сесії.
	Role        string
	Tag         string
	Prefix      string // префікс hostname (той самий і для теки стану)
	StateDir    string // тека стану (унікальна на сесію)
	AuthKey     string // одноразовий ephemeral ключ
	AuthKeyFunc func(ctx context.Context) (string, error)
	ControlURL  string
	// OnFreshState викликається ПЕРЕД повторною спробою: має підготувати
	// чисту теку стану (закрити/видалити попередню) і повернути її новий
	// шлях через замикання викликача.
	OnFreshState func() error
}

// upFreshNode піднімає ноду; при невдачі (є джерело свіжих ключів) робить
// рівно одну повторну спробу з нового auth key і чистої теки стану.
func upFreshNode(ctx context.Context, cfg nodeUpConfig) (*tsnet.Server, error) {
	role, tag := cfg.Role, cfg.Tag
	if role == "" {
		role = "node"
	}
	if tag == "" {
		tag = "-"
	}
	total := startWatch()
	authKey := cfg.AuthKey
	var lastErr error

	launchlog.Info(compNode, "[%s/%s] підйом ноди: prefix=%s control=%s timeout=%s ключ-на-повтор=%v",
		role, tag, cfg.Prefix, cfg.ControlURL, nodeUpTimeout, cfg.AuthKeyFunc != nil)

	for attempt := 0; attempt < 2; attempt++ {
		if authKey == "" {
			if lastErr == nil {
				lastErr = fmt.Errorf("відсутній auth key")
			}
			launchlog.Error(compNode, "[%s/%s] спроба %d: auth key порожній — нода не стартує", role, tag, attempt+1)
			break
		}

		hostname := cfg.Prefix + randomSuffix()
		logf := tsnetLogf(role + "/" + tag)
		ts := &tsnet.Server{
			Hostname:   hostname,
			Dir:        cfg.StateDir,
			AuthKey:    authKey,
			Ephemeral:  true, // нода зникає з мережі сама, щойно процес завершиться
			ControlURL: cfg.ControlURL,
			// Раніше тут була заглушка («tsnet дуже говіркий»), через що збій
			// реєстрації не лишав у лозі нічого. Тепер усе йде в p2p-tsnet
			// (debug), з редагуванням секретів і лімітом шуму — див. tsnetLogf.
			Logf:     logf,
			UserLogf: logf,
		}

		launchlog.Info(compNode, "[%s/%s] спроба %d/2: hostname=%s dir=%s authKey=%s",
			role, tag, attempt+1, hostname, cfg.StateDir, maskSecret(authKey))

		sw := startWatch()
		upCtx, cancel := context.WithTimeout(ctx, nodeUpTimeout)
		_, err := ts.Up(upCtx)
		cancel()
		if err == nil {
			launchlog.Info(compNode, "[%s/%s] нода ПІДНЯТА за %s (спроба %d, разом %s)", role, tag, sw, attempt+1, total)
			logNodeStatus(role, tag, ts, nil)
			return ts, nil
		}
		launchlog.Error(compNode, "[%s/%s] спроба %d НЕ вдалась за %s: %v", role, tag, attempt+1, sw, withHint(err, explainNodeErr(err)))
		if h := registrationHint(err); h != "" {
			launchlog.Warn(compNode, "[%s/%s] ПОЯСНЕННЯ:%s", role, tag, h)
		}
		_ = ts.Close()
		lastErr = err

		if attempt == 0 && cfg.AuthKeyFunc != nil && cfg.OnFreshState != nil {
			launchlog.Warn(compNode, "[%s/%s] повторна спроба: чиста тека стану + новий auth key", role, tag)
			if ferr := cfg.OnFreshState(); ferr != nil {
				launchlog.Error(compNode, "[%s/%s] не вдалося підготувати чисту теку: %v", role, tag, ferr)
				break
			}
			ksw := startWatch()
			key, kerr := cfg.AuthKeyFunc(ctx)
			if kerr != nil || key == "" {
				launchlog.Error(compNode, "[%s/%s] новий auth key НЕ отримано за %s: err=%v порожній=%v  ⇒ %s",
					role, tag, ksw, kerr, key == "", explainLobbyErr(kerr))
				break
			}
			launchlog.Info(compNode, "[%s/%s] новий auth key отримано за %s: %s", role, tag, ksw, maskSecret(key))
			authKey = key
			continue
		}
		break
	}

	// Нода не піднялась — окремо перевіряємо сам Headscale (DNS → TCP → HTTPS),
	// щоб у лозі було видно, ЧИ це блокування мережі, чи проблема глибше.
	preflightControl(ctx, cfg.ControlURL, role+"/"+tag)

	launchlog.Error(compNode, "[%s/%s] ПІДЙОМ НОДИ ПРОВАЛЕНО за %s", role, tag, total)
	return nil, fmt.Errorf("p2p: нода не піднялась у Headscale-мережі: %v%s", lastErr, registrationHint(lastErr))
}

// registrationHint додає до помилки підказку про причину, коли Headscale
// явно сказав, що не знає нашу ноду чи ключ. Текст помилки tsnet/Headscale
// показується в UI, і без підказки «node not found» нічого не говорить
// гравцю — а причина майже завжди одна (застаріла ідентичність ноди).
func registrationHint(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"node not found", "invalid key", "nodekey", "node key", "expired", "reusable"} {
		if strings.Contains(msg, needle) {
			return " (застаріла ідентичність ноди вже недійсна — спробуйте ще раз; якщо помилка повторюється, перевірте ephemeral_node_inactivity_timeout і HEADSCALE_API_KEY на сервері)"
		}
	}
	return ""
}
