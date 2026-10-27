package social

// chatcrypto.go — E2E-менеджер чату: зв'язка ключів акаунта + шифрування/розшифрування.
//
// «Простий» режим: приватний ключ чату лежить на сервері у зашифрованому конверті й не залежить від
// жодного пристрою. Новий ПК, вихід з пристроїв, перевстановлення ОС історію не ламають.
//
// Інваріанти, заради яких цей файл написано так обережно (одна помилка тут = стерті чати):
//  1. Новий пристрій СПЕРШУ питає сервер про наявний ключ і лише на «ключа нема» створює свій.
//     Мережева/серверна помилка НІКОЛИ не привід генерувати новий ключ.
//  2. Спадковий ключ пристрою (identity.json) не видаляється й не перезаписується: він лишається
//     у зв'язці як «legacy» — ним ще розшифровуються старі повідомлення.
//  3. Кожна заміна файлу зв'язки спершу архівується.
//  4. Розшифрування пробує УСІ версії ключів (свої × ключі співрозмовника), а не лише поточні.
//  5. До отримання ключа чат не пише (ErrKeysPending) — «про всяк випадок» нічого не ротується.

import (
	"shaurma-launcher-backend/internal/crashguard"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"sync"
	"time"
)

var (
	// ErrKeysPending — ключів чату ще нема й дізнатись про них не вдалось (сервер/мережа). Спробуємо знову.
	ErrKeysPending = errors.New("social:keys_pending")
	// ErrKeysNeedRecovery — ключ цього акаунта лишився на іншому пристрої, якого тут нема. Нову пару
	// створюємо ЛИШЕ за явним рішенням користувача (ResetKeys): стара історія тоді нечитабельна.
	ErrKeysNeedRecovery = errors.New("social:keys_need_recovery")
	// ErrDeviceRevoked — цей пристрій вилучено з акаунта.
	ErrDeviceRevoked = errors.New("social:device_revoked")

	// Відповіді сервера ключів (KeyServer) — абстракція над VPS.
	ErrRemoteNoKey       = errors.New("social:remote_no_key")
	ErrRemoteDisabled    = errors.New("social:remote_disabled")
	ErrRemoteUnavailable = errors.New("social:remote_unavailable")
)

// RemoteKey — версія ключа з сервера. Обидва ключі — стандартний base64 (32 байти).
type RemoteKey struct {
	Version   int
	PublicB64 string
	SecretB64 string
	CreatedAt int64
}

// RemoteKeys — усі версії ключа чату акаунта.
type RemoteKeys struct {
	Keys    []RemoteKey
	Current int
}

// RemoteConflict — інший пристрій уже створив цю версію («перший виграє»): усиновіть Keys.
type RemoteConflict struct{ Keys *RemoteKeys }

func (e *RemoteConflict) Error() string { return "social:remote_conflict" }

// KeyServer — серверне сховище конвертів. Помилки: ErrRemoteNoKey (ключа нема), ErrRemoteDisabled
// (сервер не вміє/вимкнено), ErrRemoteUnavailable (тимчасово), *RemoteConflict, ErrDeviceRevoked.
type KeyServer interface {
	GetKeys(ctx context.Context) (*RemoteKeys, error)
	PutKey(ctx context.Context, version int, publicB64, secretB64 string) (*RemoteKeys, error)
}

// Стан ключів для UI.
const (
	KeysReady        = "ready"
	KeysPending      = "pending"
	KeysNeedRecovery = "need_recovery"
)

// KeyStatus — що показати користувачу про ключі чату.
type KeyStatus struct {
	State    string `json:"state"`    // ready | pending | need_recovery
	Managed  bool   `json:"managed"`  // ключ зберігає сервер
	Versions int    `json:"versions"` // скільки версій у зв'язці
	Offline  bool   `json:"offline"`  // працюємо з локального кешу, сервер недоступний
}

const (
	histTTL        = 10 * time.Minute
	peerTTL        = 15 * time.Second // як довго вірити кешу публічного ключа співрозмовника
	peerRefreshGap = 5 * time.Second  // не частіше за раз на 5 с перечитувати ключі одного друга після невдачі
	resyncMinGap   = 30 * time.Second
	remoteTimeout  = 20 * time.Second
)

type peerHist struct {
	keys    []PublicKey
	fetched time.Time
}

// ChatCrypto зв'язує зв'язку ключів із реєстром публічних ключів Worker. Приватні ключі не віддаються
// назовні; спільні секрети кешуються (по одному на співрозмовника).
type ChatCrypto struct {
	ks     *KeyStore
	ring   *Keyring
	client *Client
	remote KeyServer
	ownID  func() string

	initMu sync.Mutex // серіалізує Init/sync/Rotate (мережа під ним, але не під mu)

	mu         sync.Mutex
	loaded     bool
	keys       []ringKey // [0] — поточний
	state      string
	managed    bool
	offline    bool
	needsSync  bool
	lastResync time.Time
	peers      map[string]SharedKey // співрозмовник → shared(поточний мій, поточний його)
	peerPub    map[string]PublicKey // його поточний публічний ключ
	peerAt     map[string]time.Time // коли peers/peerPub для співрозмовника отримано
	peerRetry  map[string]time.Time // коли востаннє примусово перечитували його ключі
	localMode  bool                 // сервер ключів вимкнено → ключ лише локальний
	pairs      map[string]SharedKey // "myPub|peerPub" → shared (повільний шлях)
	hist       map[string]peerHist
}

func NewChatCrypto(ks *KeyStore, client *Client) *ChatCrypto {
	c := &ChatCrypto{ks: ks, client: client, peers: map[string]SharedKey{}, peerPub: map[string]PublicKey{},
		peerAt: map[string]time.Time{}, peerRetry: map[string]time.Time{}, pairs: map[string]SharedKey{}, hist: map[string]peerHist{}}
	if ks != nil {
		c.ring = NewKeyring(ks.dir)
	}
	return c
}

// WithKeyServer вмикає серверні конверти. ownID — мій accountId (для звірки з опублікованим ключем).
func (c *ChatCrypto) WithKeyServer(r KeyServer, ownID func() string) *ChatCrypto {
	c.remote, c.ownID = r, ownID
	return c
}

// Status — стан ключів (для UI).
func (c *ChatCrypto) Status() KeyStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.state
	if st == "" {
		st = KeysPending
	}
	return KeyStatus{State: st, Managed: c.managed, Versions: len(c.keys), Offline: c.offline}
}

// ── ініціалізація ────────────────────────────────────────────────────────

// Init завантажує зв'язку ключів (за потреби питає сервер). Ідемпотентно.
// nil + Status()!=ready — ключі недоступні, але розшифровувати наявним можна (need_recovery).
func (c *ChatCrypto) Init() error {
	c.mu.Lock()
	ok := c.loaded
	c.mu.Unlock()
	if ok {
		return nil
	}
	c.initMu.Lock()
	defer c.initMu.Unlock()
	c.mu.Lock()
	ok = c.loaded
	c.mu.Unlock()
	if ok {
		return nil
	}
	if c.remote == nil {
		return c.initLocalOnly()
	}
	return c.syncLocked()
}

// Resync перечитує ключі з сервера (не частіше за resyncMinGap). Викликається після невдалого
// розшифрування (інший пристрій міг ротувати ключ) і для повернення з offline-режиму.
func (c *ChatCrypto) Resync() {
	if c.remote == nil {
		return
	}
	c.mu.Lock()
	if time.Since(c.lastResync) < resyncMinGap {
		c.mu.Unlock()
		return
	}
	c.lastResync = time.Now()
	c.mu.Unlock()
	c.initMu.Lock()
	defer c.initMu.Unlock()
	_ = c.syncLocked()
}

// NeedsSync — працюємо з кешу, і варто повторити спробу синхронізації.
func (c *ChatCrypto) NeedsSync() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.needsSync || !c.loaded
}

func (c *ChatCrypto) loadLegacy() (*ringKey, error) {
	pub, sec, err := c.ks.Load()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err // пошкоджений файл — НЕ генеруємо новий ключ
	}
	return &ringKey{version: 0, kind: kindLegacy, pub: pub, sec: sec, created: time.Now().UnixMilli()}, nil
}

func (c *ChatCrypto) loadRing() (*keyringFile, error) {
	f, err := c.ring.Load()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return f, err
}

// initLocalOnly — режим без сервера ключів (тести, збірка без акаунтів): один локальний ключ.
func (c *ChatCrypto) initLocalOnly() error {
	pub, sec, err := c.ks.LoadOrCreate()
	if err != nil {
		return err
	}
	c.setKeys([]ringKey{{version: 0, kind: kindLegacy, pub: pub, sec: sec}}, KeysReady, false, false)
	return nil
}

// initLocalGuarded — сервер ключів вимкнено, а пристрій працює з локальним ключем. НЕ створюємо новий
// ключ мовчки, якщо друзям уже опубліковано ІНШИЙ: інакше новий пристрій затре чужий (старого
// пристрою) ключ, і співрозмовники шифруватимуть у нікуди.
func (c *ChatCrypto) initLocalGuarded(legacy *ringKey) error {
	c.mu.Lock()
	c.localMode = true
	c.mu.Unlock()
	if legacy != nil {
		return c.initLocalOnly() // свій ключ уже є — працюємо як раніше
	}
	published, found, perr := c.publishedKey()
	if perr != nil {
		return ErrKeysPending // не знаємо, що опубліковано — нічого не створюємо
	}
	if found && published != (PublicKey{}) {
		c.setKeys(nil, KeysNeedRecovery, false, false)
		return nil
	}
	return c.initLocalOnly()
}

func (c *ChatCrypto) setKeys(keys []ringKey, state string, managed, offline bool) {
	c.mu.Lock()
	c.keys, c.state, c.managed, c.offline = keys, state, managed, offline
	c.needsSync = offline
	c.loaded = true
	c.peers = map[string]SharedKey{}
	c.peerPub = map[string]PublicKey{}
	c.peerAt = map[string]time.Time{}
	c.pairs = map[string]SharedKey{}
	c.mu.Unlock()
}

// syncLocked — головна логіка. Викликати під initMu.
func (c *ChatCrypto) syncLocked() error {
	legacy, err := c.loadLegacy()
	if err != nil {
		return err
	}
	ring, err := c.loadRing()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
	defer cancel()
	rk, err := c.remote.GetKeys(ctx)
	switch {
	case err == nil:
		return c.adopt(rk, legacy, ring)
	case errors.Is(err, ErrRemoteNoKey):
		return c.noServerKey(ctx, legacy, ring)
	case errors.Is(err, ErrRemoteDisabled):
		return c.initLocalGuarded(legacy) // сервер не керує ключами — як до оновлення, але без затирання
	case errors.Is(err, ErrDeviceRevoked):
		return err
	default:
		return c.offlineFallback(legacy, ring)
	}
}

func decodeRemote(rk *RemoteKeys) ([]ringKey, error) {
	if rk == nil || len(rk.Keys) == 0 {
		return nil, ErrRemoteUnavailable
	}
	out := make([]ringKey, 0, len(rk.Keys))
	for _, k := range rk.Keys {
		pub, err := DecodePublicKey(k.PublicB64)
		if err != nil {
			return nil, ErrRemoteUnavailable
		}
		raw, err := b64StdDecodeExact(k.SecretB64, SecretKeyBytes)
		if err != nil {
			return nil, ErrRemoteUnavailable
		}
		var sec SecretKey
		copy(sec[:], raw)
		if d, ok := publicFromSecret(sec); !ok || d != pub {
			return nil, ErrRemoteUnavailable // сервер віддав суперечливу пару — не вірити
		}
		out = append(out, ringKey{version: k.Version, kind: kindServer, pub: pub, sec: sec, created: k.CreatedAt})
	}
	return out, nil
}

// adopt усиновлює ключі сервера, зберігаючи локальні (спадкові) для розшифрування старого.
func (c *ChatCrypto) adopt(rk *RemoteKeys, legacy *ringKey, ring *keyringFile, extra ...ringKey) error {
	server, err := decodeRemote(rk)
	if err != nil {
		return c.offlineFallback(legacy, ring)
	}
	have := map[PublicKey]bool{}
	for _, k := range server {
		have[k.pub] = true
	}
	all := append([]ringKey(nil), server...)
	addLegacy := func(k ringKey) {
		if !have[k.pub] {
			have[k.pub] = true
			k.version, k.kind = 0, kindLegacy
			all = append(all, k)
		}
	}
	if legacy != nil {
		addLegacy(*legacy)
	}
	if ring != nil {
		for _, e := range ring.Entries {
			if k, ok := e.key(); ok {
				addLegacy(k) // старі server-версії, яких сервер уже не віддає, теж не губимо
			}
		}
	}
	for _, k := range extra {
		addLegacy(k)
	}
	cur := rk.Current
	ordered := orderKeys(all, cur)
	f := &keyringFile{Current: cur}
	for _, k := range ordered {
		f.Entries = append(f.Entries, k.entry())
	}
	// Кеш на диск — best-effort: чат працює й без нього (ключі є на сервері).
	_ = c.ring.Save(f)
	c.setKeys(ordered, KeysReady, true, false)
	return nil
}

// noServerKey — сервер каже «ключа нема». Тут І ТІЛЬКИ ТУТ можна вирішувати, чи створювати ключ.
func (c *ChatCrypto) noServerKey(ctx context.Context, legacy *ringKey, ring *keyringFile) error {
	// (1) Сервер втратив дані, а локально є server-версії — повертаємо їх по порядку.
	if ring != nil {
		var srv []keyringEntry
		for _, e := range ring.Entries {
			if e.Kind == kindServer && e.Version >= 1 {
				srv = append(srv, e)
			}
		}
		if len(srv) > 0 {
			return c.reupload(ctx, srv, legacy, ring)
		}
	}
	// (2) Який ключ зараз опубліковано друзям?
	published, found, perr := c.publishedKey()
	if perr != nil {
		return c.offlineFallback(legacy, ring) // не знаємо — нічого не створюємо
	}
	switch {
	case !found:
		// Ніхто ще нічого не публікував: безпечно взяти спадковий ключ або створити перший.
		if legacy != nil {
			return c.uploadFirst(ctx, *legacy, legacy, ring)
		}
		pub, sec, err := GenerateIdentity()
		if err != nil {
			return err
		}
		return c.uploadFirst(ctx, ringKey{kind: kindServer, pub: pub, sec: sec, created: time.Now().UnixMilli()}, nil, ring)
	case legacy != nil && legacy.pub == published:
		// Міграція: цей пристрій тримає саме той ключ, яким користуються друзі.
		return c.uploadFirst(ctx, *legacy, legacy, ring)
	default:
		// Друзі шифрують на ключ, якого тут нема (він на іншому пристрої). Новий ключ створимо лише за
		// явним рішенням користувача; доти чат не пише, а наявні (спадкові) ключі лише читають.
		var keys []ringKey
		if legacy != nil {
			keys = append(keys, *legacy)
		}
		c.setKeys(keys, KeysNeedRecovery, false, false)
		return nil
	}
}

func (c *ChatCrypto) publishedKey() (PublicKey, bool, error) {
	if c.ownID == nil || c.client == nil {
		return PublicKey{}, false, ErrKeysPending
	}
	id := c.ownID()
	if id == "" {
		return PublicKey{}, false, ErrKeysPending
	}
	pub, err := c.client.FetchPeerPublicKey(id)
	if errors.Is(err, ErrKeyNotFound) {
		return PublicKey{}, false, nil
	}
	if err != nil {
		return PublicKey{}, false, err
	}
	return pub, true, nil
}

func (c *ChatCrypto) uploadFirst(ctx context.Context, k ringKey, legacy *ringKey, ring *keyringFile) error {
	rk, err := c.remote.PutKey(ctx, 1, EncodePublicKey(k.pub), b64StdEncode(k.sec[:]))
	var cf *RemoteConflict
	switch {
	case err == nil:
		return c.adopt(rk, legacy, ring, k)
	case errors.As(err, &cf):
		return c.adopt(cf.Keys, legacy, ring, k) // інший пристрій випередив — беремо його, свій лишаємо для читання
	case errors.Is(err, ErrDeviceRevoked):
		return err
	default:
		return c.offlineFallback(legacy, ring)
	}
}

func (c *ChatCrypto) reupload(ctx context.Context, entries []keyringEntry, legacy *ringKey, ring *keyringFile) error {
	var last *RemoteKeys
	for _, e := range entries {
		rk, err := c.remote.PutKey(ctx, e.Version, e.PublicKey, e.SecretKey)
		var cf *RemoteConflict
		switch {
		case err == nil:
			last = rk
		case errors.As(err, &cf):
			return c.adopt(cf.Keys, legacy, ring)
		case errors.Is(err, ErrDeviceRevoked):
			return err
		default:
			return c.offlineFallback(legacy, ring)
		}
	}
	return c.adopt(last, legacy, ring)
}

// offlineFallback — сервер недоступний. Є локальні ключі → працюємо з них і пробуємо пізніше.
// Нічого нема → ErrKeysPending: НЕ генеруємо новий ключ «на всяк випадок».
func (c *ChatCrypto) offlineFallback(legacy *ringKey, ring *keyringFile) error {
	var keys []ringKey
	cur := 0
	if ring != nil {
		cur = ring.Current
		for _, e := range ring.Entries {
			if k, ok := e.key(); ok {
				keys = append(keys, k)
			}
		}
	}
	if legacy != nil {
		dup := false
		for _, k := range keys {
			if k.pub == legacy.pub {
				dup = true
			}
		}
		if !dup {
			keys = append(keys, *legacy)
		}
	}
	if len(keys) == 0 {
		return ErrKeysPending
	}
	c.setKeys(orderKeys(keys, cur), KeysReady, ring != nil, true)
	return nil
}

// ── ротація / скидання ───────────────────────────────────────────────────

// Rotate створює нову версію ключа чату (після «Це не я» / екстреної ротації). version=0 → наступна.
// Інший пристрій встиг першим — усиновлюємо його версію.
func (c *ChatCrypto) Rotate(version int) error {
	if c.remote == nil {
		return ErrNotSupported
	}
	if err := c.Init(); err != nil {
		return err
	}
	c.initMu.Lock()
	defer c.initMu.Unlock()
	c.mu.Lock()
	if c.state != KeysReady || !c.managed {
		c.mu.Unlock()
		return ErrKeysPending
	}
	if version <= 0 {
		for _, k := range c.keys {
			if k.kind == kindServer && k.version >= version {
				version = k.version + 1
			}
		}
	}
	c.mu.Unlock()
	pub, sec, err := GenerateIdentity()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
	defer cancel()
	rk, err := c.remote.PutKey(ctx, version, EncodePublicKey(pub), b64StdEncode(sec[:]))
	var cf *RemoteConflict
	switch {
	case err == nil:
		return c.adoptFromRotation(rk)
	case errors.As(err, &cf):
		return c.adoptFromRotation(cf.Keys)
	default:
		return err
	}
}

func (c *ChatCrypto) adoptFromRotation(rk *RemoteKeys) error {
	legacy, err := c.loadLegacy()
	if err != nil {
		return err
	}
	ring, err := c.loadRing()
	if err != nil {
		return err
	}
	// Поточні в пам'яті (раніше усиновлені) теж не губимо.
	c.mu.Lock()
	extra := append([]ringKey(nil), c.keys...)
	c.mu.Unlock()
	return c.adopt(rk, legacy, ring, extra...)
}

// ResetKeys — користувач свідомо погодився почати чат «з чистого аркуша» (стан need_recovery):
// створюється версія 1. Стара історія, зашифрована втраченим ключем, лишиться нечитабельною.
func (c *ChatCrypto) ResetKeys() error {
	if c.remote == nil {
		return ErrNotSupported
	}
	c.initMu.Lock()
	defer c.initMu.Unlock()
	c.mu.Lock()
	st, local := c.state, c.localMode
	c.mu.Unlock()
	if st != KeysNeedRecovery {
		return ErrBadInput
	}
	if local { // сервер ключів вимкнено: нова локальна пара (стара історія нечитабельна)
		if err := c.initLocalOnly(); err != nil {
			return err
		}
		c.mu.Lock()
		c.localMode = true
		c.mu.Unlock()
		return nil
	}
	legacy, err := c.loadLegacy()
	if err != nil {
		return err
	}
	ring, err := c.loadRing()
	if err != nil {
		return err
	}
	pub, sec, err := GenerateIdentity()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
	defer cancel()
	return c.uploadFirst(ctx, ringKey{kind: kindServer, pub: pub, sec: sec, created: time.Now().UnixMilli()}, legacy, ring)
}

// ── публікація, шифрування, розшифрування ────────────────────────────────

// PublicKeyB64 — поточний публічний ключ (base64). Порожньо, якщо ключів немає.
func (c *ChatCrypto) PublicKeyB64() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded || len(c.keys) == 0 {
		return ""
	}
	return EncodePublicKey(c.keys[0].pub)
}

// PublishOwnKey: якщо ключ керує сервер — він сам публікує публічну частину разом із конвертом;
// інакше (старий сервер) публікуємо як раніше. Відмова key_managed — не помилка.
func (c *ChatCrypto) PublishOwnKey() error {
	if err := c.Init(); err != nil {
		return err
	}
	c.mu.Lock()
	managed, st := c.managed, c.state
	c.mu.Unlock()
	if managed || st != KeysReady {
		return nil
	}
	err := c.client.RegisterOwnPublicKey(c.PublicKeyB64())
	var ae *APIError
	if errors.As(err, &ae) && ae.Code == "key_managed" {
		return nil
	}
	return err
}

func (c *ChatCrypto) ready() ([]ringKey, error) {
	if err := c.Init(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.keys) == 0 {
		if c.state == KeysNeedRecovery {
			return nil, ErrKeysNeedRecovery
		}
		return nil, ErrKeysPending
	}
	return append([]ringKey(nil), c.keys...), nil
}

// sharedWith — спільний секрет «мій поточний × його поточний» (з кеша; ключ друга береться один раз).
func (c *ChatCrypto) sharedWith(accountID string) (SharedKey, error) {
	keys, err := c.ready()
	if err != nil {
		return SharedKey{}, err
	}
	if !ValidAccountID(accountID) {
		return SharedKey{}, ErrBadInput
	}
	c.mu.Lock()
	sk, ok := c.peers[accountID]
	fresh := ok && time.Since(c.peerAt[accountID]) < peerTTL
	c.mu.Unlock()
	if fresh {
		return sk, nil
	}

	// Кеш прострочений: ключ друга міг змінитись (новий пристрій, ротація) — без перечитування ми б
	// шифрували на старий ключ, і друг бачив би «зашифровано старим ключем».
	pub, err := c.client.FetchPeerPublicKey(accountID)
	if err != nil {
		if ok {
			return sk, nil // мережа лягла — лишаємось на кеші, краще за відмову
		}
		return SharedKey{}, err
	}
	sk = Precompute(pub, keys[0].sec)
	c.mu.Lock()
	c.peers[accountID] = sk
	c.peerPub[accountID] = pub
	c.peerAt[accountID] = time.Now()
	c.mu.Unlock()
	return sk, nil
}

// plainNonce — позначка «простого» режиму в полі nonce: поле ciphertext тоді містить base64(UTF-8 текст).
// Це рівно 24 символи std-base64 — проходить перевірки Worker (24..40) і VPS без жодних змін на сервері.
// Повідомлення без цієї позначки — старі E2E (NaCl box), їх лише читаємо.
var plainNonce = base64.StdEncoding.EncodeToString([]byte("shaurma-plain-v1"))

// EncryptFor готує текст до відправки. «Простий» режим: жодних ключів пристроїв — ні новий пристрій,
// ні втрата старого, ні перевстановлення ОС не можуть зробити чат нечитабельним. Захист у дорозі —
// TLS (WSS) до VPS; на самих пристроях нічого не шифрується й не губиться.
func (c *ChatCrypto) EncryptFor(accountID, text string) (ciphertextB64, nonceB64 string, err error) {
	if !ValidAccountID(accountID) {
		return "", "", ErrBadInput
	}
	clean := CleanMessage(text, MaxPlaintextBytes)
	if clean == "" {
		return "", "", ErrBadInput
	}
	if len([]byte(clean)) > MaxPlaintextBytes {
		return "", "", ErrCryptoTooLarge
	}
	return base64.StdEncoding.EncodeToString([]byte(clean)), plainNonce, nil
}

// encryptE2E — СТАРИЙ режим (NaCl box, ключі пристроїв). Нові повідомлення так не шифруються;
// лишено для сумісності та тестів читання історії.
func (c *ChatCrypto) encryptE2E(accountID, text string) (ciphertextB64, nonceB64 string, err error) {
	clean := CleanMessage(text, MaxPlaintextBytes) // прибирає керуючі/bidi символи
	if clean == "" {
		return "", "", ErrBadInput
	}
	if len([]byte(clean)) > MaxPlaintextBytes {
		return "", "", ErrCryptoTooLarge
	}
	c.mu.Lock()
	notReady := c.loaded && c.state == KeysNeedRecovery
	c.mu.Unlock()
	if notReady {
		return "", "", ErrKeysNeedRecovery // не пишемо, поки ключ чату не відновлено
	}
	sk, err := c.sharedWith(accountID)
	if err != nil {
		return "", "", err
	}
	return Encrypt(sk, []byte(clean))
}

// DecryptFrom розшифровує отримане від/для співрозмовника. Спершу швидкий шлях (поточні ключі), далі
// перебір усіх версій: своїх (поточна, старі, спадкова) × його (поточна + історія).
// Нічого не підійшло → ErrCryptoDecryptFail (UI покаже «зашифровано старим ключем»).
func (c *ChatCrypto) DecryptFrom(accountID, ciphertextB64, nonceB64 string) (string, error) {
	if nonceB64 == plainNonce { // «простий» режим: ключів не треба
		if len(ciphertextB64) > maxCiphertextB64Len {
			return "", ErrCryptoDecryptFail
		}
		raw, derr := base64.StdEncoding.DecodeString(ciphertextB64)
		if derr != nil || len(raw) == 0 || len(raw) > MaxPlaintextBytes {
			return "", ErrCryptoDecryptFail
		}
		clean := CleanMessage(string(raw), MaxPlaintextBytes)
		if clean == "" {
			return "", ErrCryptoDecryptFail
		}
		return clean, nil
	}
	keys, err := c.ready()
	if err != nil {
		return "", err
	}
	if !ValidAccountID(accountID) {
		return "", ErrBadInput
	}
	// Швидкий шлях тільки якщо поточний ключ придатний до відправки/читання.
	if sk, err := c.sharedWith(accountID); err == nil {
		if pt, e := Decrypt(sk, ciphertextB64, nonceB64); e == nil {
			return string(pt), nil
		}
	} else if errors.Is(err, ErrBadInput) || errors.Is(err, ErrDeviceRevoked) {
		return "", err
	}
	if pt, ok := c.decryptSlow(accountID, ciphertextB64, nonceB64, keys); ok {
		return pt, nil
	}
	// Можливо, ключ співрозмовника змінився, а наш кеш застарів: перечитуємо його ключі (з обмеженням
	// частоти) і пробуємо ще раз, перш ніж здатись.
	if c.refreshPeer(accountID) {
		if sk, err := c.sharedWith(accountID); err == nil {
			if pt, e := Decrypt(sk, ciphertextB64, nonceB64); e == nil {
				return string(pt), nil
			}
		}
		if pt, ok := c.decryptSlow(accountID, ciphertextB64, nonceB64, keys); ok {
			return pt, nil
		}
	}
	{ // можливо, інший пристрій ротував ключ — підтягнемо нову версію
		_f := c.Resync
		go func() {
			defer crashguard.Recover("c.Resync")
			_f()
		}()
	}
	return "", ErrCryptoDecryptFail
}

// refreshPeer скидає кеш ключів співрозмовника. false — вже перечитували щойно (не довбемо сервер
// на кожне нечитабельне повідомлення зі списку).
func (c *ChatCrypto) refreshPeer(accountID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.peerRetry[accountID]) < peerRefreshGap {
		return false
	}
	c.peerRetry[accountID] = time.Now()
	delete(c.peers, accountID)
	delete(c.peerPub, accountID)
	delete(c.peerAt, accountID)
	delete(c.hist, accountID)
	return true
}

func (c *ChatCrypto) peerKeys(accountID string) []PublicKey {
	c.mu.Lock()
	h, ok := c.hist[accountID]
	cur, haveCur := c.peerPub[accountID]
	c.mu.Unlock()
	if !ok || time.Since(h.fetched) > histTTL {
		keys, err := c.client.FetchPeerKeyHistory(accountID)
		if err != nil {
			keys = nil // історії нема (старий сервер/мережа) — лишається поточний
		}
		h = peerHist{keys: keys, fetched: time.Now()}
		c.mu.Lock()
		c.hist[accountID] = h
		c.mu.Unlock()
	}
	var out []PublicKey
	seen := map[PublicKey]bool{}
	if haveCur {
		out = append(out, cur)
		seen[cur] = true
	}
	for _, k := range h.keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func (c *ChatCrypto) decryptSlow(accountID, ct, nonce string, keys []ringKey) (string, bool) {
	for _, peer := range c.peerKeys(accountID) {
		for _, mine := range keys {
			ck := EncodePublicKey(mine.pub) + "|" + EncodePublicKey(peer)
			c.mu.Lock()
			sk, ok := c.pairs[ck]
			c.mu.Unlock()
			if !ok {
				sk = Precompute(peer, mine.sec)
				c.mu.Lock()
				c.pairs[ck] = sk
				c.mu.Unlock()
			}
			if pt, err := Decrypt(sk, ct, nonce); err == nil {
				return string(pt), true
			}
		}
	}
	return "", false
}

// Forget стерти спільні секрети співрозмовника (напр. після видалення друга).
func (c *ChatCrypto) Forget(accountID string) {
	c.mu.Lock()
	delete(c.peers, accountID)
	delete(c.peerPub, accountID)
	delete(c.peerAt, accountID)
	delete(c.hist, accountID)
	c.mu.Unlock()
}
