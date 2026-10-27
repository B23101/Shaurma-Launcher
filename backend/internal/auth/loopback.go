package auth

import (
	"shaurma-launcher-backend/internal/crashguard"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Єдиний loopback-сервер для ВСІХ входів через браузер (Microsoft і Google).
//
// Раніше Microsoft слухав 29111-29115 (/auth), а Google — випадковий порт (/shaurma-oidc).
// Різні порти ламали Google: «Помилка 400: redirect_uri_mismatch» — Google приймає лише
// redirect_uri, що точно збігається із зареєстрованим. Тепер ОДИН порт і ОДИН шлях для обох:
//
//	http://localhost:29111/auth
//
// Саме цей URI (один раз) треба додати в «Authorized redirect URIs» клієнта Google
// (Google Cloud Console → Credentials). У Microsoft він уже зареєстрований.
//
// Сервер НЕ закривається одразу після отримання коду: сторінка, яку бачить користувач у
// браузері, опитує /auth/status і показує ПІДСУМОК входу — «вхід виконано» чи «реєстрацію
// завершено» (з переліком можливостей нового акаунта). Підсумок лаунчер повідомляє через
// Session.Finish; після цього сервер доживає ще кілька секунд (щоб сторінка встигла його
// забрати) і закривається.
const (
	CallbackPort = 29111
	CallbackPath = "/auth"
	statusPath   = "/auth/status"
)

// CallbackRedirectURI — єдиний redirect_uri для Microsoft і Google.
func CallbackRedirectURI() string {
	return fmt.Sprintf("http://localhost:%d%s", CallbackPort, CallbackPath)
}

// Скільки живе сесія на кожному етапі (таймер перезапускається при зміні етапу).
const (
	waitBrowserTTL = 5 * time.Minute  // чекаємо, поки користувач пройде вхід у браузері
	finishingTTL   = 6 * time.Minute  // код отримано, лаунчер завершує вхід
	needNickTTL    = 20 * time.Minute // чекаємо нік у лаунчері (як signInTTL)
	doneGrace      = 30 * time.Second // підсумок готовий — даємо сторінці його забрати
)

// CallbackResult — те, що прийшло на /auth: код авторизації або помилка.
type CallbackResult struct {
	Code string
	Err  error
}

// Outcome — підсумок входу для сторінки в браузері.
type Outcome struct {
	// Phase: "pending" (код отримано, завершуємо) | "need_nick" (треба нік у лаунчері) |
	// "done" | "error".
	Phase    string `json:"phase"`
	Provider string `json:"provider,omitempty"` // "google" | "microsoft"
	// Registered — створено НОВИЙ акаунт Шаурми (реєстрація). false — це просто вхід
	// в уже наявний акаунт.
	Registered bool   `json:"registered"`
	Licensed   bool   `json:"licensed"`
	Nickname   string `json:"nickname,omitempty"`
	// Warning — вхід вдався, але не все пройшло гладко (напр. акаунт Шаурми не створено).
	Warning string `json:"warning,omitempty"`
	Message string `json:"message,omitempty"` // phase=error
}

// Session — один вхід (одне відкриття браузера).
type Session struct {
	lb       *Loopback
	state    string
	provider string
	result   chan CallbackResult

	mu      sync.Mutex
	called  bool
	closed  bool
	outcome Outcome
	timer   *time.Timer
}

// Result — канал, у який потрапляє код (або помилка) з браузера. Буфер 1.
func (s *Session) Result() <-chan CallbackResult { return s.result }

// Cancel — скасування з лаунчера: будить того, хто чекає на Result, і закриває сервер.
func (s *Session) Cancel() {
	if s == nil {
		return
	}
	select {
	case s.result <- CallbackResult{Err: errors.New("авторизацію скасовано")}:
	default:
	}
	s.Close()
}

// Finish повідомляє підсумок входу сторінці в браузері. Безпечно викликати з nil-сесії та
// для вже закритої/заміненої сесії (тоді нічого не робить).
func (s *Session) Finish(o Outcome) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if o.Provider == "" {
		o.Provider = s.provider
	}
	s.outcome = o
	ttl := doneGrace
	if o.Phase == "need_nick" {
		ttl = needNickTTL
	}
	s.resetTimerLocked(ttl)
	s.mu.Unlock()
}

// Close закриває сесію і (якщо вона ще активна) сервер, звільняючи порт.
func (s *Session) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	already := s.closed
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()
	if !already {
		s.lb.closeSession(s)
	}
}

func (s *Session) resetTimerLocked(d time.Duration) {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(d, s.Close)
}

// Loopback — спільний локальний HTTP-сервер.
type Loopback struct {
	mu        sync.Mutex
	listeners []net.Listener
	srv       *http.Server
	cur       *Session
}

// Callback — єдиний екземпляр на весь лаунчер.
var Callback = &Loopback{}

// Begin закриває попередній вхід (якщо він ще висить), відкриває порт і повертає нову сесію.
func (l *Loopback) Begin(state, provider string) (*Session, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closeLocked()

	lns, err := bindLocalServer()
	if err != nil {
		return nil, err
	}
	s := &Session{
		lb:       l,
		state:    state,
		provider: provider,
		result:   make(chan CallbackResult, 1),
		outcome:  Outcome{Phase: "pending", Provider: provider},
	}
	s.mu.Lock()
	s.resetTimerLocked(waitBrowserTTL)
	s.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, l.handleCallback)
	mux.HandleFunc(statusPath, l.handleStatus)
	// Будь-який інший шлях (браузер тягне /favicon.ico) — миттєва порожня відповідь.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := &http.Server{
		Handler: mux,
		// Без цього напіввідкрите з'єднання тримало б горутину сервера вічно.
		ReadHeaderTimeout: 10 * time.Second,
	}
	l.listeners, l.srv, l.cur = lns, srv, s
	for _, ln := range lns {
		ln := ln
		// Локальна копія srv: closeLocked може обнулити l.srv до старту горутини.
		go func() { _ = srv.Serve(ln) }()
	}
	return s, nil
}

func (l *Loopback) closeSession(s *Session) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cur == s {
		l.closeLocked()
	}
}

func (l *Loopback) closeLocked() {
	old := l.cur
	l.cur = nil
	if l.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = l.srv.Shutdown(ctx)
		cancel()
		l.srv = nil
	}
	for _, ln := range l.listeners {
		_ = ln.Close()
	}
	l.listeners = nil
	if old != nil {
		// Заміна сесії: позначаємо стару закритою (її Finish/Close далі — no-op) і гасимо таймер.
		go func(s *Session) {
			defer crashguard.Recover("loopback.go:227")
			s.mu.Lock()
			s.closed = true
			if s.timer != nil {
				s.timer.Stop()
				s.timer = nil
			}
			s.mu.Unlock()
		}(old)
	}
}

func (l *Loopback) session(state string) *Session {
	l.mu.Lock()
	s := l.cur
	l.mu.Unlock()
	if s == nil || state == "" || state != s.state {
		return nil
	}
	return s
}

func (l *Loopback) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s := l.session(q.Get("state"))
	if s == nil { // чужий/повторний/застарілий запит — ігноруємо, вхід не чіпаємо
		http.Error(w, "bad state", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	first := !s.called
	s.called = true
	s.mu.Unlock()

	if first {
		var res CallbackResult
		switch {
		case q.Get("error") != "":
			res.Err = fmt.Errorf("OAuth помилка: %s %s", q.Get("error"), q.Get("error_description"))
		case q.Get("code") == "":
			res.Err = errors.New("немає коду авторизації в callback")
		default:
			res.Code = q.Get("code")
		}
		if res.Err != nil {
			msg := "Вхід не вдався."
			if e := q.Get("error"); e == "access_denied" {
				msg = "Вхід скасовано."
			}
			s.Finish(Outcome{Phase: "error", Message: msg})
		} else {
			s.mu.Lock()
			s.resetTimerLocked(finishingTTL)
			s.mu.Unlock()
		}
		select { // non-blocking: Cancel міг уже покласти «скасовано» в буфер
		case s.result <- res:
		default:
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = io.WriteString(w, strings.Replace(resultPage, "__STATE__", html.EscapeString(s.state), 1))
}

func (l *Loopback) handleStatus(w http.ResponseWriter, r *http.Request) {
	s := l.session(r.URL.Query().Get("state"))
	if s == nil {
		http.Error(w, "gone", http.StatusGone)
		return
	}
	s.mu.Lock()
	o := s.outcome
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(o)
}

// bindLocalServer відкриває порт CallbackPort ОДРАЗУ НА ДВОХ loopback-адресах: 127.0.0.1 та
// [::1].
//
// "localhost" у redirect_uri на сучасних системах резолвиться і в ::1, і в 127.0.0.1, причому
// браузери часто пробують ::1 ПЕРШИМ. Якби сервер слухав лише 127.0.0.1, перше з'єднання
// впиралось би в «connection refused» (у гіршому разі — таймаут). IPv6-listener НЕ обов'язковий
// (у системах без IPv6 bind на [::1] просто впаде); обов'язковий лише 127.0.0.1.
//
// Порт один, тож коротко повторюємо спробу: попередній listener міг бути закритий долі секунди
// тому, і ОС ще не віддала порт.
func bindLocalServer() ([]net.Listener, error) {
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		ln4, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", CallbackPort))
		if err == nil {
			lns := []net.Listener{ln4}
			if ln6, err6 := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", CallbackPort)); err6 == nil {
				lns = append(lns, ln6)
			}
			return lns, nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("порт %d для входу зайнятий іншою програмою — закрийте її і повторіть вхід (%v)", CallbackPort, lastErr)
}

// resultPage — сторінка, яку бачить користувач у браузері після входу. Спершу «Завершуємо вхід…»,
// далі опитує /auth/status і малює підсумок: вхід / реєстрація (з можливостями акаунта) / помилка.
// Увесь динамічний текст вставляється через textContent — жодного innerHTML з даних.
const resultPage = `<!DOCTYPE html>
<html lang="uk">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Shaurma Launcher</title>
<style>
  *{margin:0;padding:0;box-sizing:border-box}
  body{
    background:
      radial-gradient(900px 500px at 12%  -5%,rgba(255,138,0,.07),transparent 60%),
      radial-gradient(900px 600px at 100% 110%,rgba(255,94,0,.08),transparent 55%),
      #0f0f0f;
    color:#f5f5f5;
    font-family:'Inter','Segoe UI',system-ui,sans-serif;
    min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px;
  }
  .card{
    width:min(94vw,440px);
    background:#161616;border:1px solid rgba(255,255,255,.09);
    border-radius:16px;padding:38px 34px;text-align:center;
    box-shadow:0 24px 60px rgba(0,0,0,.5);
  }
  .brand{display:flex;align-items:center;justify-content:center;gap:10px}
  .brand .logo{
    width:34px;height:34px;border-radius:10px;
    display:flex;align-items:center;justify-content:center;
    background:linear-gradient(135deg,#ff8a00,#ff5e00);
    box-shadow:0 8px 18px rgba(255,138,0,.3);
  }
  .brand .logo svg{width:19px;height:19px;color:#fff}
  .brand .txt{text-align:left}
  .brand .name{font-size:15px;font-weight:800;letter-spacing:.3px}
  .brand .sub{font-size:8.5px;font-weight:700;color:#a0a0a0;letter-spacing:1.8px}
  .badge{
    width:78px;height:78px;border-radius:50%;margin:28px auto 20px;
    display:flex;align-items:center;justify-content:center;
    background:rgba(255,255,255,.03);border:1px solid rgba(255,255,255,.08);
    color:#ff8a00;
  }
  .badge svg{width:36px;height:36px;display:none}
  .badge.pending .spin{display:block;animation:spin 1s linear infinite}
  .badge.ok{color:#00c851}.badge.ok .i-ok{display:block}
  .badge.wait{color:#ff8a00}.badge.wait .i-wait{display:block}
  .badge.err{color:#ff4757}.badge.err .i-err{display:block}
  @keyframes spin{to{transform:rotate(360deg)}}
  .msg{font-size:17px;font-weight:800}
  .hint{font-size:13px;color:#a0a0a0;margin-top:8px;line-height:1.5}
  .warn{margin-top:14px;font-size:12.5px;line-height:1.5;color:#ffb74d;background:rgba(255,183,77,.08);
    border:1px solid rgba(255,183,77,.25);border-radius:10px;padding:10px 12px}
  .perks{margin-top:22px;text-align:left;display:none}
  .perks h4{font-size:11px;font-weight:700;letter-spacing:1.4px;text-transform:uppercase;color:#a0a0a0;margin-bottom:10px;text-align:center}
  .perk{display:flex;gap:12px;align-items:flex-start;padding:11px 12px;margin-top:8px;border-radius:12px;
    background:rgba(255,255,255,.03);border:1px solid rgba(255,255,255,.07)}
  .perk .ico{flex:none;width:30px;height:30px;border-radius:9px;display:flex;align-items:center;justify-content:center;
    background:linear-gradient(135deg,rgba(255,138,0,.22),rgba(255,94,0,.14));color:#ff8a00}
  .perk .ico svg{width:17px;height:17px}
  .perk b{display:block;font-size:13.5px}
  .perk span{display:block;font-size:12px;color:#a0a0a0;margin-top:2px;line-height:1.45}
  .foot{margin-top:28px;padding-top:14px;border-top:1px solid rgba(255,255,255,.06);font-size:10.5px;color:#5c6570;letter-spacing:.4px;text-transform:uppercase}
</style>
</head>
<body>
  <div class="card" id="card" data-state="__STATE__">
    <div class="brand">
      <div class="logo"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8.5 14.5A2.5 2.5 0 0 0 11 12c0-1.38-.5-2-1-3-1.072-2.143-.224-4.054 2-6 .5 2.5 2 4.9 4 6.5 2 1.6 3 3.5 3 5.5a7 7 0 1 1-14 0c0-1.153.433-2.294 1-3a2.5 2.5 0 0 0 2.5 2.5z"/></svg></div>
      <div class="txt"><div class="name">Shaurma</div><div class="sub">LAUNCHER</div></div>
    </div>

    <div class="badge pending" id="badge">
      <svg class="spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3a9 9 0 1 0 9 9"/></svg>
      <svg class="i-ok" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12l5 5l10 -10"/></svg>
      <svg class="i-wait" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>
      <svg class="i-err" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6l-12 12M6 6l12 12"/></svg>
    </div>

    <div class="msg" id="msg">Завершуємо вхід…</div>
    <div class="hint" id="hint">Не закривайте цю вкладку кілька секунд.</div>
    <div class="warn" id="warn" style="display:none"></div>

    <div class="perks" id="perks">
      <h4>Що тепер доступно</h4>
      <div class="perk">
        <div class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 7a4 4 0 1 0 8 0a4 4 0 0 0-8 0"/><path d="M3 21v-2a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v2"/><path d="M16 3.1a4 4 0 0 1 0 7.8"/><path d="M21 21v-2a4 4 0 0 0-3-3.85"/></svg></div>
        <div><b>Друзі</b><span>Додавайте друзів, бачте, хто онлайн, і діліться з ними збірками та модами.</span></div>
      </div>
      <div class="perk">
        <div class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg></div>
        <div><b>P2P-чат з друзями</b><span>Спілкуйтеся напряму з друзями просто в лаунчері.</span></div>
      </div>
      <div class="perk" id="perk-skins">
        <div class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="3" width="16" height="18" rx="2"/><circle cx="12" cy="9" r="2.5"/><path d="M7.5 18c.8-2.2 2.4-3.3 4.5-3.3s3.7 1.1 4.5 3.3"/></svg></div>
        <div><b>Власна система скінів</b><span>Для неліцензійного акаунта — свій скін у гардеробі Шаурми, який бачать інші гравці.</span></div>
      </div>
    </div>

    <div class="foot" id="foot">OAuth</div>
  </div>
<noscript><p style="position:fixed;bottom:16px;left:0;right:0;text-align:center;color:#a0a0a0;font-size:13px">Підсумок входу дивіться у вікні лаунчера.</p></noscript>
<script>
(function () {
  var card = document.getElementById('card');
  var state = card.getAttribute('data-state');
  var $ = function (id) { return document.getElementById(id); };
  var names = { google: 'Google', microsoft: 'Microsoft' };
  var lastKey = '', fails = 0, stopped = false;

  function set(cls, msg, hint) {
    $('badge').className = 'badge ' + cls;
    $('msg').textContent = msg;
    $('hint').textContent = hint || '';
  }
  function render(o) {
    var key = JSON.stringify(o);
    if (key === lastKey) return;
    lastKey = key;
    $('foot').textContent = (names[o.provider] || 'OAuth') + ' · вхід';
    var nick = o.nickname ? ', ' + o.nickname : '';
    $('warn').style.display = o.warning ? 'block' : 'none';
    if (o.warning) $('warn').textContent = o.warning;
    $('perks').style.display = 'none';

    if (o.phase === 'error') {
      stopped = true;
      set('err', 'Не вдалося увійти', (o.message || 'Помилка входу.') + ' Поверніться в лаунчер і спробуйте ще раз.');
    } else if (o.phase === 'need_nick') {
      set('wait', 'Майже готово', 'Особу підтверджено. Поверніться в лаунчер і оберіть нік — це завершить реєстрацію.');
    } else if (o.phase === 'done' && o.registered) {
      stopped = true;
      set('ok', 'Реєстрацію завершено', 'Ласкаво просимо' + nick + '! Можна закрити цю вкладку й повернутися в лаунчер.');
      $('perks').style.display = 'block';
      $('perk-skins').style.display = o.licensed ? 'none' : 'flex';
    } else if (o.phase === 'done') {
      stopped = true;
      set('ok', 'Вхід виконано', 'З поверненням' + nick + '! Можна закрити цю вкладку й повернутися в лаунчер.');
    } else {
      set('pending', 'Завершуємо вхід…', 'Не закривайте цю вкладку кілька секунд.');
    }
  }
  function tick() {
    if (stopped) return;
    fetch('/auth/status?state=' + encodeURIComponent(state), { cache: 'no-store' })
      .then(function (r) { if (!r.ok) throw new Error('gone'); return r.json(); })
      .then(function (o) { fails = 0; render(o); })
      .catch(function () {
        // Лаунчер закрив сервер (вже готово/скасовано) або зв'язок втрачено.
        if (++fails >= 4 && !stopped) {
          stopped = true;
          set('wait', 'Поверніться в лаунчер', 'Підсумок входу — у вікні лаунчера. Цю вкладку можна закрити.');
        }
      })
      .then(function () { if (!stopped) setTimeout(tick, 700); });
  }
  tick();
})();
</script>
</body>
</html>`
