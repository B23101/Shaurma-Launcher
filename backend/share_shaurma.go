//go:build shaurma

// Поділитися збіркою / сервером (фаза J плану): повне копіювання друзям через P2P.
//
// Відправник: рахує маніфест → піднімає локальний HTTP-хост (internal/share) →
// відкриває його як ТИМЧАСОВИЙ P2P-доступ (та сама адреса tmpN, квиток від VPS і
// хост-ворота, що й у запрошень на світ) → шле кожному другові картку в зашифрований
// чат. Одержувач сам підключається й качає (pull), тож хосту нічого ініціювати.
//
// Картка — звичайне текстове повідомлення з останнім рядком
//
//	shaurma-share:v1:<base64url(json)>
//
// тож працює без змін на VPS і не ламає старі клієнти (вони бачать читабельний текст).
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/export"
	"shaurma-launcher-backend/internal/p2p"
	"shaurma-launcher-backend/internal/servers"
	"shaurma-launcher-backend/internal/share"
	"shaurma-launcher-backend/internal/social"
)

const (
	shareCardPrefix = "shaurma-share:v1:"
	shareMaxLife    = 12 * time.Hour
	// Вхід гостьового P2P-сервісу (p2p.guestExternalAddr) — туди пишемо хендшейк.
	shareGuestGateway = "127.0.0.2:25565"
)

var (
	shareKeyRe  = regexp.MustCompile(`^[a-f0-9]{32}$`)
	shareAddrRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{2,120}$`)
)

// ── Типи для UI ──────────────────────────────────────────────────────────

// ShareCard — те, що їде в чаті (і що фронтенд розбирає з повідомлення).
type ShareCard struct {
	V     int               `json:"v"`
	Kind  string            `json:"k"`
	Name  string            `json:"n"`
	Addr  string            `json:"a"`
	Key   string            `json:"key"`
	MID   string            `json:"mid"`
	SID   string            `json:"sid"`
	Files int               `json:"f"`
	Bytes int64             `json:"b"`
	Meta  map[string]string `json:"m,omitempty"`
	// Dev — анонімна мітка ПРИСТРОЮ відправника (див. shareDeviceTag). Одержувач на тому
	// самому пристрої (інший акаунт у тому самому лаунчері) не може прийняти власну
	// передачу: приймання б зациклилось на підключенні до самого себе.
	Dev string `json:"d,omitempty"`
}

// ShareOutView — стан передачі для відправника.
type ShareOutView struct {
	ID         string                 `json:"id"`
	Kind       string                 `json:"kind"`
	RefID      string                 `json:"refId"` // packId або serverId
	Name       string                 `json:"name"`
	Phase      string                 `json:"phase"` // preparing | ready | closed | error
	Hashed     int                    `json:"hashed"`
	HashTotal  int                    `json:"hashTotal"`
	Files      int                    `json:"files"`
	TotalBytes int64                  `json:"totalBytes"`
	Skipped    []string               `json:"skipped,omitempty"`
	Recipients []share.RecipientState `json:"recipients"`
	Error      string                 `json:"error,omitempty"`
}

// ShareInView — стан приймання для одержувача.
type ShareInView struct {
	ID       string         `json:"id"` // sid картки
	MID      string         `json:"mid"`
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	From     string         `json:"from"`
	Phase    string         `json:"phase"` // connecting | downloading | paused | installing | done | error | declined | cancelled
	Progress share.Progress `json:"progress"`
	Error    string         `json:"error,omitempty"`
	// ResultID — id створеної збірки/сервера після завершення.
	ResultID string `json:"resultId,omitempty"`
}

// ShareStartRequest — запит «Поділитися».
type ShareStartRequest struct {
	Kind      string   `json:"kind"` // build | server
	RefID     string   `json:"refId"`
	FriendIDs []string `json:"friendIds"`
	Included  []string `json:"included"`
	Excluded  []string `json:"excluded"`
}

// ── Внутрішній стан ──────────────────────────────────────────────────────

type shareOut struct {
	mu     sync.Mutex
	view   ShareOutView
	host   *share.Host
	cancel context.CancelFunc
	addr   string
	sub    string
	invID  string
	keys   map[string]string // accountID → ключ
	cardFn func(key string) ShareCard
}

type shareIn struct {
	mu      sync.Mutex
	view    ShareInView
	dl      *share.Downloader
	cancel  context.CancelFunc
	card    ShareCard
	stage   string
	fromAcc string
	done    chan struct{} // закривається, коли горутина прийому завершилась
}

var (
	shareMu   sync.Mutex
	shareOuts = map[string]*shareOut{}
	shareIns  = map[string]*shareIn{}
)

func (a *App) shareRoot() string { return filepath.Join(a.cfg.Dir(), "share") }

func (a *App) shareStageDir(mid string) string { return filepath.Join(a.shareRoot(), "in", mid) }

// shareDeviceTag — коротка анонімна мітка цього пристрою (sha256 від DeviceID, не сам ID).
// Порожня, якщо DeviceID недоступний (тоді захист «від себе» просто не спрацює).
func (a *App) shareDeviceTag() string {
	id, err := a.cfg.DeviceID()
	if err != nil || id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("shaurma-share-dev:" + id))
	return hex.EncodeToString(sum[:8])
}

// ShareDeviceTag — для UI: чи картка надіслана з цього самого пристрою.
func (a *App) ShareDeviceTag() string { return a.shareDeviceTag() }

func errShareSelf() error {
	return errors.New("це передача з цього самого лаунчера (інший акаунт на цьому ПК) — прийняти її тут не можна; увійдіть на іншому пристрої")
}

func newShareID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Блокування (J.5) ─────────────────────────────────────────────────────

// shareLockErr — помилка, якщо збірка/сервер зараз віддається друзям: файли мають
// лишатись незмінними (інакше контрольні суми одержувача не зійдуться).
func (a *App) shareLockErr(kind, refID string) error {
	shareMu.Lock()
	defer shareMu.Unlock()
	return shareLockErrLocked(kind, refID)
}

// shareLockErrLocked — те саме, але shareMu уже взято викликачем (щоб перевірка й реєстрація
// нової передачі були атомарними: подвійний клік не відкриє дві передачі однієї збірки).
func shareLockErrLocked(kind, refID string) error {
	for _, o := range shareOuts {
		o.mu.Lock()
		hit := o.view.Kind == kind && o.view.RefID == refID && o.view.Phase != "closed" && o.view.Phase != "error"
		o.mu.Unlock()
		if hit {
			return errors.New("збірку зараз передають друзям — дочекайтесь завершення або закрийте передачу")
		}
	}
	return nil
}

// ShareBusy — чи є активні передачі (для попередження при закритті лаунчера).
func (a *App) ShareBusy() bool {
	shareMu.Lock()
	defer shareMu.Unlock()
	for _, o := range shareOuts {
		o.mu.Lock()
		p := o.view.Phase
		o.mu.Unlock()
		if p == "preparing" || p == "ready" {
			return true
		}
	}
	for _, i := range shareIns {
		i.mu.Lock()
		p := i.view.Phase
		i.mu.Unlock()
		if p == "connecting" || p == "downloading" || p == "paused" || p == "installing" {
			return true
		}
	}
	return false
}

// shareShutdown — при виході: зупиняємо передачі (журнал одержувача вже на диску).
func (a *App) shareShutdown() {
	shareMu.Lock()
	outs := make([]*shareOut, 0, len(shareOuts))
	for _, o := range shareOuts {
		outs = append(outs, o)
	}
	ins := make([]*shareIn, 0, len(shareIns))
	for _, i := range shareIns {
		ins = append(ins, i)
	}
	shareMu.Unlock()
	for _, o := range outs {
		o.cancel()
	}
	for _, i := range ins {
		i.cancel()
	}
}

// ── Відправник ───────────────────────────────────────────────────────────

func (a *App) emitShareOut(o *shareOut) {
	o.mu.Lock()
	v := o.view
	v.Recipients = append([]share.RecipientState{}, o.view.Recipients...)
	o.mu.Unlock()
	a.emit("share:out", v)
}

// shareSkipDirs — службове, що не передаємо (логи, крашдампи, конфіг лаунчера).
func shareInclude(kind string) func(rel string, isDir bool) bool {
	return func(rel string, isDir bool) bool {
		top := rel
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			top = rel[:i]
		}
		switch strings.ToLower(top) {
		case "logs", "crash-reports", ".shaurma":
			return false
		}
		return true
	}
}

// ShareStart — «Поділитися»: готує маніфест і відкриває доступ друзям. Повертає одразу
// (фаза "preparing"); хід подій — подія "share:out".
func (a *App) ShareStart(req ShareStartRequest) (ShareOutView, error) {
	if a.p2pDisabled() {
		return ShareOutView{}, errP2PDisabled()
	}
	if len(req.FriendIDs) == 0 || len(req.FriendIDs) > share.MaxRecipients {
		return ShareOutView{}, fmt.Errorf("оберіть від 1 до %d друзів", share.MaxRecipients)
	}
	if err := a.shareLockErr(req.Kind, req.RefID); err != nil {
		return ShareOutView{}, err
	}
	accountID, _, err := a.p2pAccountCheck()
	if err != nil {
		return ShareOutView{}, err
	}
	svc := a.socialService()
	if svc == nil {
		return ShareOutView{}, social.ErrNotLoggedIn
	}

	var root, name string
	var meta map[string]string
	var filter func(string, bool) bool
	switch req.Kind {
	case share.KindBuild:
		b, berr := a.resolveLaunchBuild(req.RefID)
		if berr != nil {
			return ShareOutView{}, berr
		}
		if b.IsShaurma {
			return ShareOutView{}, errors.New("офіційні збірки Shaurma передавати не можна — друг завантажить їх із каталогу")
		}
		root = a.packRootDir(req.RefID)
		if root == "" {
			return ShareOutView{}, errors.New("теку збірки не знайдено")
		}
		name = b.Name
		meta = map[string]string{"mc": b.MCVersion, "loader": b.Loader, "loaderVersion": b.LoaderVersion}
		if b.UseCustomRAM {
			meta["minRam"], meta["maxRam"] = strconv.Itoa(b.MinRAMMB), strconv.Itoa(b.MaxRAMMB)
		}
		inc := shareInclude(req.Kind)
		pf := newSharePathFilter(req.Included, req.Excluded)
		filter = func(rel string, isDir bool) bool { return inc(rel, isDir) && (isDir || pf(rel)) }
	case share.KindServer:
		sv, serr := a.srvAPI.GetServer(req.RefID)
		if serr != nil {
			return ShareOutView{}, serr
		}
		switch a.srvAPI.ServerStatus(req.RefID).Status {
		case "", "stopped", "crashed", "error":
		default:
			return ShareOutView{}, errors.New("зупиніть сервер перед передачею — під час роботи його файли змінюються")
		}
		root, name = sv.Dir, sv.Name
		eula, _ := os.ReadFile(filepath.Join(root, "eula.txt"))
		if !strings.Contains(string(eula), "eula=true") {
			return ShareOutView{}, errors.New("у сервері не прийнято EULA (eula.txt) — друг не зможе його додати")
		}
		meta = map[string]string{"core": sv.Core, "mc": sv.MCVersion, "ram": strconv.Itoa(sv.RAMMB)}
		filter = shareInclude(req.Kind)
	default:
		return ShareOutView{}, errors.New("невідомий тип передачі")
	}

	ctx, cancel := context.WithCancel(context.Background())
	o := &shareOut{cancel: cancel, keys: map[string]string{}}
	o.view = ShareOutView{ID: newShareID(), Kind: req.Kind, RefID: req.RefID, Name: name, Phase: "preparing"}
	shareMu.Lock()
	if err := shareLockErrLocked(req.Kind, req.RefID); err != nil {
		shareMu.Unlock()
		cancel()
		return ShareOutView{}, err
	}
	shareOuts[o.view.ID] = o
	shareMu.Unlock()

	go func() {
		defer crashguard.Recover("shareStart")
		defer func() {
			// Паніка в підготовці не повинна лишати «привидну» передачу, що блокує збірку.
			o.mu.Lock()
			stuck := o.view.Phase == "preparing"
			o.mu.Unlock()
			if stuck && ctx.Err() == nil {
				o.mu.Lock()
				o.view.Phase, o.view.Error = "error", "внутрішня помилка підготовки передачі"
				o.mu.Unlock()
				a.emitShareOut(o)
				a.finishShareOut(o)
			}
		}()
		if err := a.runShareOut(ctx, o, svc, accountID, root, meta, filter, req.FriendIDs); err != nil {
			o.mu.Lock()
			if errors.Is(err, context.Canceled) {
				// Відправник сам закрив передачу під час підготовки — це не помилка.
				o.view.Phase = "closed"
			} else {
				o.view.Phase, o.view.Error = "error", err.Error()
			}
			o.mu.Unlock()
			a.emitShareOut(o)
			a.finishShareOut(o)
		}
	}()
	o.mu.Lock()
	v := o.view
	o.mu.Unlock()
	return v, nil
}

// newSharePathFilter — той самий вибір «що копіювати», що в дублюванні збірки:
// порожній included = усе, крім excluded.
func newSharePathFilter(included, excluded []string) func(rel string) bool {
	if len(included) > 0 {
		pf := export.NewPathFilter(included, excluded)
		return pf.Includes
	}
	return func(rel string) bool {
		for _, ex := range excluded {
			ex = strings.Trim(strings.ReplaceAll(ex, "\\", "/"), "/")
			if ex != "" && (rel == ex || strings.HasPrefix(rel, ex+"/")) {
				return false
			}
		}
		return true
	}
}

func (a *App) runShareOut(ctx context.Context, o *shareOut, svc *social.Service, accountID, root string, meta map[string]string, filter func(string, bool) bool, friends []string) error {
	var lastEmit time.Time
	m, skipped, err := share.BuildManifest(ctx, root, o.view.Kind, o.view.Name, meta, share.BuildOptions{
		Include: filter,
		OnProgress: func(done, total int) {
			o.mu.Lock()
			o.view.Hashed, o.view.HashTotal = done, total
			o.mu.Unlock()
			if time.Since(lastEmit) > 200*time.Millisecond {
				lastEmit = time.Now()
				a.emitShareOut(o)
			}
		},
	})
	if err != nil {
		return err
	}
	if len(m.Files) == 0 {
		return errors.New("у вибраному немає жодного файлу")
	}
	host, err := share.NewHost(root, m, func(rs share.RecipientState) {
		o.mu.Lock()
		replaced := false
		for i := range o.view.Recipients {
			if o.view.Recipients[i].ID == rs.ID {
				o.view.Recipients[i], replaced = rs, true
			}
		}
		if !replaced {
			o.view.Recipients = append(o.view.Recipients, rs)
		}
		o.mu.Unlock()
		a.emitShareOut(o)
	})
	if err != nil {
		return err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := l.Addr().(*net.TCPAddr).Port
	go func() {
		defer crashguard.Recover("shareServe")
		_ = host.Serve(l, true)
	}()

	sub, addr, err := a.freeTempAddress(accountID)
	if err != nil {
		host.Close()
		return err
	}
	st, herr := a.StartP2PHost(strconv.Itoa(port), sub, 0) // UDP не потрібен
	if herr != nil || !st.Active {
		_ = a.p2pAPI.ReleaseTempAddress(sub)
		host.Close()
		return fmt.Errorf("не вдалося відкрити P2P-доступ: %v %s", herr, st.Error)
	}
	if st.Subdomain != "" && st.Subdomain != sub {
		sub = st.Subdomain
		if st.Address != "" {
			addr = st.Address
		}
	}
	invID, err := svc.TempInviteCreate(social.TempInviteMeta{Address: addr, Port: port, Kind: "", Motd: o.view.Name})
	if err != nil {
		_ = a.p2pAPI.StopP2PHost(sub)
		_ = a.p2pAPI.ReleaseTempAddress(sub)
		host.Close()
		var ae *social.APIError
		if errors.As(err, &ae) && ae.Status == 429 {
			return errors.New("уже відкрито 3 тимчасові доступи (світи/передачі) — закрийте один")
		}
		return err
	}
	a.p2pAPI.SetHostGate(sub, &p2p.HostGate{InviteID: invID, Admit: svc.TempInviteAdmit})

	o.mu.Lock()
	o.host, o.addr, o.sub, o.invID = host, addr, sub, invID
	o.view.Files, o.view.TotalBytes, o.view.Skipped = len(m.Files), m.TotalBytes, skipped
	o.view.Phase = "ready"
	kind, name, sid := o.view.Kind, o.view.Name, o.view.ID
	o.mu.Unlock()
	o.mu.Lock()
	o.cardFn = func(key string) ShareCard {
		return ShareCard{V: 1, Kind: kind, Name: name, Addr: addr, Key: key, MID: m.ID(), SID: sid,
			Files: len(m.Files), Bytes: m.TotalBytes, Meta: shareCardMeta(meta), Dev: a.shareDeviceTag()}
	}
	o.mu.Unlock()
	a.emitShareOut(o)

	if err := a.shareInvite(o, svc, friends); err != nil {
		// Частину карток не вдалося надіслати — доступ усе одно відкритий, показуємо в UI.
		o.mu.Lock()
		o.view.Error = "не всі повідомлення надіслано: " + err.Error()
		o.mu.Unlock()
		a.emitShareOut(o)
	}
	go func() {
		defer crashguard.Recover("shareWatch")
		a.watchShareOut(ctx, o, svc)
	}()
	return nil
}

func shareCardMeta(m map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"mc", "loader", "core"} {
		if v := m[k]; v != "" && len(v) <= 32 {
			out[k] = v
		}
	}
	return out
}

// shareInvite видає ключі й шле картки. Викликається і при старті, і для «додати ще друга».
func (a *App) shareInvite(o *shareOut, svc *social.Service, friends []string) error {
	o.mu.Lock()
	host, invID, cardFn, name := o.host, o.invID, o.cardFn, o.view.Name
	o.mu.Unlock()
	if host == nil || cardFn == nil {
		return errors.New("передача ще готується")
	}
	if err := svc.TempInviteAddGuests(invID, friends); err != nil {
		return err
	}
	var sendErr error
	for _, f := range friends {
		key, err := host.AddRecipient(f)
		if err != nil {
			return err
		}
		o.mu.Lock()
		o.keys[f] = key
		o.mu.Unlock()
		raw, _ := json.Marshal(cardFn(key))
		text := fmt.Sprintf("📦 «%s» — збірка/сервер для вас. Відкрийте цей чат у лаунчері Shaurma (оновленому), щоб прийняти.\n%s%s",
			name, shareCardPrefix, base64.RawURLEncoding.EncodeToString(raw))
		if _, err := svc.SendChatMessage(f, text); err != nil {
			sendErr = err
		} else {
			a.emit("tinvite:invited", f)
		}
	}
	return sendErr
}

// ShareInviteMore — додати ще друзів у вже відкриту передачу (до 3 разом).
func (a *App) ShareInviteMore(id string, friendIDs []string) error {
	shareMu.Lock()
	o := shareOuts[id]
	shareMu.Unlock()
	if o == nil {
		return errors.New("передачу вже закрито")
	}
	svc := a.socialService()
	if svc == nil {
		return social.ErrNotLoggedIn
	}
	return a.shareInvite(o, svc, friendIDs)
}

func (a *App) watchShareOut(ctx context.Context, o *shareOut, svc *social.Service) {
	defer a.finishShareOut(o)
	deadline := time.After(shareMaxLife)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	n := 0
	settledAt := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-tick.C:
		}
		o.mu.Lock()
		host, invID := o.host, o.invID
		o.mu.Unlock()
		if n++; n%3 == 0 {
			if alive, err := svc.TempInviteBeat(invID, 0); err == nil && !alive {
				return
			}
		}
		// Усі дали фінальну відповідь → через кілька секунд закриваємо.
		if host.AllSettled() {
			if settledAt.IsZero() {
				settledAt = time.Now()
			} else if time.Since(settledAt) > 4*time.Second {
				return
			}
		} else {
			settledAt = time.Time{}
		}
	}
}

func (a *App) finishShareOut(o *shareOut) {
	o.mu.Lock()
	host, sub, invID := o.host, o.sub, o.invID
	if o.view.Phase != "error" {
		o.view.Phase = "closed"
	}
	o.mu.Unlock()
	if host != nil {
		host.Close()
	}
	if sub != "" {
		if err := a.p2pAPI.ReleaseTempAddress(sub); err != nil {
			_ = a.p2pAPI.StopP2PHost(sub)
		}
	}
	if svc := a.socialService(); svc != nil && invID != "" {
		_ = svc.TempInviteClose(invID)
	}
	a.emitShareOut(o)
	shareMu.Lock()
	delete(shareOuts, o.view.ID)
	shareMu.Unlock()
	a.emit("share:out-closed", o.view.ID)
}

// ShareStop — відправник закриває передачу (усім одержувачам). Прийняте ними лишається.
func (a *App) ShareStop(id string) error {
	shareMu.Lock()
	o := shareOuts[id]
	shareMu.Unlock()
	if o == nil {
		return nil
	}
	o.cancel()
	return nil
}

// ShareSenderPause — пауза/відновлення з боку відправника для одного одержувача.
func (a *App) ShareSenderPause(id, accountID string, paused bool) error {
	shareMu.Lock()
	o := shareOuts[id]
	shareMu.Unlock()
	if o == nil {
		return errors.New("передачу не знайдено")
	}
	o.mu.Lock()
	host := o.host
	o.mu.Unlock()
	if host == nil {
		return errors.New("передачу не знайдено")
	}
	host.SetPaused(accountID, paused)
	return nil
}

// ShareRevokeRecipient — скасувати передачу одному одержувачу.
func (a *App) ShareRevokeRecipient(id, accountID string) error {
	shareMu.Lock()
	o := shareOuts[id]
	shareMu.Unlock()
	if o == nil {
		return errors.New("передачу не знайдено")
	}
	o.mu.Lock()
	host := o.host
	o.mu.Unlock()
	if host == nil {
		return errors.New("передачу не знайдено")
	}
	host.Revoke(accountID)
	return nil
}

// ListShareOut — активні передачі відправника.
func (a *App) ListShareOut() []ShareOutView {
	shareMu.Lock()
	defer shareMu.Unlock()
	out := make([]ShareOutView, 0, len(shareOuts))
	for _, o := range shareOuts {
		o.mu.Lock()
		v := o.view
		v.Recipients = append([]share.RecipientState{}, o.view.Recipients...)
		o.mu.Unlock()
		out = append(out, v)
	}
	return out
}

// ── Одержувач ────────────────────────────────────────────────────────────

func parseShareCard(encoded string) (ShareCard, error) {
	encoded = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(encoded), shareCardPrefix))
	if len(encoded) == 0 || len(encoded) > 4096 {
		return ShareCard{}, errors.New("некоректна картка передачі")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ShareCard{}, errors.New("некоректна картка передачі")
	}
	var c ShareCard
	if json.Unmarshal(raw, &c) != nil || c.V != 1 {
		return ShareCard{}, errors.New("картку створено новішою версією лаунчера — оновіть лаунчер")
	}
	c.Addr = strings.ToLower(strings.TrimSpace(c.Addr))
	if (c.Kind != share.KindBuild && c.Kind != share.KindServer) || !shareKeyRe.MatchString(c.Key) ||
		!shareKeyRe.MatchString(c.MID) || !shareKeyRe.MatchString(c.SID) || !shareAddrRe.MatchString(c.Addr) ||
		len(c.Name) == 0 || len(c.Name) > 200 {
		return ShareCard{}, errors.New("некоректна картка передачі")
	}
	return c, nil
}

func shareDomain(addr string) string {
	if strings.HasSuffix(addr, "."+p2p.PlaySuffix) {
		return addr
	}
	return addr + "." + p2p.PlaySuffix
}

func (a *App) emitShareIn(i *shareIn) {
	i.mu.Lock()
	v := i.view
	i.mu.Unlock()
	a.emit("share:in", v)
}

// ShareAccept — прийняти передачу з картки (encoded — рядок після маркера або повний
// останній рядок повідомлення). fromName — ім'я відправника для позначки походження.
func (a *App) ShareAccept(encoded, fromAccountID, fromName string) (ShareInView, error) {
	card, err := parseShareCard(encoded)
	if err != nil {
		return ShareInView{}, err
	}
	if a.p2pDisabled() {
		return ShareInView{}, errP2PDisabled()
	}
	if tag := a.shareDeviceTag(); tag != "" && card.Dev == tag {
		return ShareInView{}, errShareSelf()
	}
	shareMu.Lock()
	if ex, ok := shareIns[card.SID]; ok {
		ex.mu.Lock()
		v := ex.view
		active := v.Phase != "error" && v.Phase != "cancelled" && v.Phase != "declined"
		ex.mu.Unlock()
		if active {
			shareMu.Unlock()
			return v, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	in := &shareIn{cancel: cancel, card: card, stage: a.shareStageDir(card.MID), fromAcc: fromAccountID, done: make(chan struct{})}
	in.view = ShareInView{ID: card.SID, MID: card.MID, Kind: card.Kind, Name: card.Name, From: fromName, Phase: "connecting"}
	shareIns[card.SID] = in
	shareMu.Unlock()

	_ = os.MkdirAll(in.stage, 0o755)
	if raw, e := json.Marshal(map[string]any{"card": encoded, "from": fromName, "fromAccount": fromAccountID}); e == nil {
		_ = os.WriteFile(filepath.Join(in.stage, "card.json"), raw, 0o644)
	}
	go func() {
		defer close(in.done)
		defer crashguard.Recover("shareAccept")
		defer func() {
			// Після паніки прийом не має лишатись у «connecting/downloading» назавжди.
			in.mu.Lock()
			p := in.view.Phase
			in.mu.Unlock()
			if p == "connecting" || p == "downloading" || p == "paused" || p == "installing" {
				in.mu.Lock()
				in.view.Phase, in.view.Error = "error", "внутрішня помилка приймання"
				in.mu.Unlock()
				a.emitShareIn(in)
			}
		}()
		a.runShareIn(ctx, in)
	}()
	return in.view, nil
}

// shareConnectTimeout — скільки максимум чекаємо підключення до відправника (приєднання до
// тимчасового інвайту + маніфест). Без ліміту картка висіла на «Підключення…» вічно, коли
// відправник закрив доступ чи недосяжний.
const shareConnectTimeout = 45 * time.Second

// shareDialer готує підключення. JoinTempInvite/EnsureP2PGuestService не знають про
// context, тому виконуємо їх у горутині й чекаємо з ctx та лімітом часу — інакше «Зупинити»
// не діяло, поки ці виклики не повернуться.
func (a *App) shareDialer(ctx context.Context, card ShareCard) (func(ctx context.Context) (net.Conn, error), error) {
	res := make(chan error, 1)
	go func() {
		defer crashguard.Recover("shareDialerPrep")
		if err := a.JoinTempInvite(card.Addr); err != nil {
			res <- errors.New("відправник уже закрив доступ або недоступний")
			return
		}
		res <- a.p2pAPI.EnsureP2PGuestService()
	}()
	select {
	case err := <-res:
		if err != nil {
			return nil, err
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(shareConnectTimeout):
		return nil, errors.New("відправник не відповідає — можливо, він закрив доступ")
	}
	domain := shareDomain(card.Addr)
	return func(ctx context.Context) (net.Conn, error) {
		return share.DialMC(ctx, shareGuestGateway, domain, 1)
	}, nil
}

func (a *App) runShareIn(ctx context.Context, in *shareIn) {
	setPhase := func(phase, errText string) {
		in.mu.Lock()
		if in.view.Phase == "declined" { // користувач відмовився — пізніші події не воскрешають прийом
			in.mu.Unlock()
			return
		}
		in.view.Phase, in.view.Error = phase, errText
		in.mu.Unlock()
		a.emitShareIn(in)
	}
	// Завершення без зв'язку: усе вже прийняте до краху.
	if info, ok := share.ReadStage(in.stage); ok && info.Complete && info.ManifestID == in.card.MID {
		if m, err := share.LoadStageManifest(in.stage); err == nil && m.ID() == in.card.MID {
			a.finalizeShareIn(in, m, setPhase)
			return
		}
	}
	dial, err := a.shareDialer(ctx, in.card)
	if err != nil {
		if ctx.Err() != nil {
			setPhase("cancelled", "")
		} else {
			setPhase("error", err.Error())
		}
		return
	}
	var lastEmit time.Time
	dl := share.NewDownloader(share.Options{
		Dial: dial, Key: in.card.Key, StageDir: in.stage, ExpectManifestID: in.card.MID,
		OnProgress: func(p share.Progress) {
			in.mu.Lock()
			if in.view.Phase == "declined" || in.view.Phase == "cancelled" {
				in.mu.Unlock()
				return
			}
			in.view.Progress = p
			switch p.State {
			case share.ProgDownloading:
				in.view.Phase = "downloading"
			case share.ProgPaused:
				in.view.Phase = "paused"
			case share.ProgConnecting:
				in.view.Phase = "connecting"
			}
			in.mu.Unlock()
			if time.Since(lastEmit) > 250*time.Millisecond || p.State == share.ProgDone {
				lastEmit = time.Now()
				a.emitShareIn(in)
			}
		},
	})
	in.mu.Lock()
	in.dl = dl
	in.mu.Unlock()

	// Маніфест — з обмеженням часу (Manifest кешується в dl, тож Run його не качає вдруге).
	mctx, mcancel := context.WithTimeout(ctx, shareConnectTimeout)
	_, merr := dl.Manifest(mctx)
	mcancel()
	if merr != nil {
		switch {
		case ctx.Err() != nil:
			setPhase("cancelled", "")
		case errors.Is(merr, context.DeadlineExceeded):
			setPhase("error", "відправник не відповідає — можливо, він закрив доступ")
		default:
			setPhase("error", merr.Error())
		}
		return
	}

	if err := dl.Run(ctx); err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			setPhase("cancelled", "")
		default:
			setPhase("error", err.Error())
		}
		return
	}
	m, err := dl.Manifest(ctx)
	if err != nil {
		setPhase("error", err.Error())
		return
	}
	a.finalizeShareIn(in, m, setPhase)
}

// finalizeShareIn переносить прийняті файли на місце й реєструє збірку/сервер.
func (a *App) finalizeShareIn(in *shareIn, m *share.Manifest, setPhase func(phase, errText string)) {
	setPhase("installing", "")
	files := share.FilesDir(in.stage)
	var resultID string
	var err error
	switch m.Kind {
	case share.KindBuild:
		resultID, err = a.installSharedBuild(in, m, files)
	case share.KindServer:
		resultID, err = a.installSharedServer(in, m, files)
	default:
		err = errors.New("невідомий тип передачі")
	}
	if err != nil {
		setPhase("error", err.Error())
		return
	}
	_ = os.RemoveAll(in.stage)
	in.mu.Lock()
	in.view.ResultID = resultID
	in.mu.Unlock()
	setPhase("done", "")
}

func (a *App) installSharedBuild(in *shareIn, m *share.Manifest, files string) (string, error) {
	newID, newName := a.customPacks.UniqueIDAndName(m.Name)
	for i := 2; a.packIDExists(newID); i++ {
		newID = fmt.Sprintf("%s-%d", newID, i)
	}
	dst := filepath.Join(a.packDir(newID, ""), ".minecraft")
	if err := share.MoveTree(files, dst); err != nil {
		return "", fmt.Errorf("не вдалося встановити збірку: %w", err)
	}
	pack := custompack.Pack{
		ID: newID, Name: newName,
		MCVersion: m.Meta["mc"], Loader: m.Meta["loader"], LoaderVersion: m.Meta["loaderVersion"],
		Source: "manual", SharedFrom: in.view.From,
	}
	if mx, e := strconv.Atoi(m.Meta["maxRam"]); e == nil && mx > 0 {
		mn, _ := strconv.Atoi(m.Meta["minRam"])
		pack.UseCustomRAM, pack.MinRAMMB, pack.MaxRAMMB = true, mn, mx
	}
	if err := a.customPacks.Save(pack); err != nil {
		_ = share.MoveTree(dst, files) // файли лишаємо в stage — можна повторити
		return "", fmt.Errorf("не вдалося зберегти збірку: %w", err)
	}
	a.buildsReg.SetInstalled(newID, builds.KindCustom, "")
	a.emit("builds:changed", newID)
	return newID, nil
}

func (a *App) installSharedServer(in *shareIn, m *share.Manifest, files string) (string, error) {
	folder := servers.SanitizeServerFolder(m.Name)
	if folder == "" {
		folder = "server"
	}
	base := folder
	for i := 2; !a.srvAPI.CheckServerFolder(folder).Free || dirExists(filepath.Join(a.cfg.Dir(), "servers", folder)); i++ {
		folder = fmt.Sprintf("%s-%d", base, i)
	}
	dst := filepath.Join(a.cfg.Dir(), "servers", folder)
	if err := share.MoveTree(files, dst); err != nil {
		return "", fmt.Errorf("не вдалося встановити сервер: %w", err)
	}
	ram, _ := strconv.Atoi(m.Meta["ram"])
	view, err := a.srvAPI.AddServerFromFolder(dst, m.Name, ram, false)
	if err != nil {
		_ = share.MoveTree(dst, files)
		return "", fmt.Errorf("не вдалося додати сервер: %w", err)
	}
	a.emit("servers:changed", view.ID)
	return view.ID, nil
}

func (a *App) getShareIn(sid string) *shareIn {
	shareMu.Lock()
	defer shareMu.Unlock()
	return shareIns[sid]
}

// SharePause / ShareResume — пауза одержувача.
func (a *App) shareDownloader(sid string) *share.Downloader {
	in := a.getShareIn(sid)
	if in == nil {
		return nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.dl
}

func (a *App) SharePause(sid string) error {
	dl := a.shareDownloader(sid)
	if dl == nil {
		return errors.New("передачу не знайдено")
	}
	dl.Pause()
	return nil
}

func (a *App) ShareResume(sid string) error {
	dl := a.shareDownloader(sid)
	if dl == nil {
		return errors.New("передачу не знайдено")
	}
	dl.Resume()
	return nil
}

// ShareCancelIn — зупинити прийом; прийняте лишається на диску (можна продовжити).
func (a *App) ShareCancelIn(sid string) error {
	if in := a.getShareIn(sid); in != nil {
		in.cancel()
		// Одразу показуємо «зупинено» — не чекаємо, поки горутина прийому (можливо, застрягла
		// в мережевому виклику) помітить скасування.
		in.mu.Lock()
		p := in.view.Phase
		stop := p == "connecting" || p == "downloading" || p == "paused"
		if stop {
			in.view.Phase, in.view.Error = "cancelled", ""
		}
		in.mu.Unlock()
		if stop {
			a.emitShareIn(in)
		}
	}
	return nil
}

// ShareDiscard — відмовитись остаточно й стерти прийняте.
func (a *App) ShareDiscard(sid, encoded string) error {
	card, err := parseShareCard(encoded)
	if err != nil {
		return err
	}
	prev := a.getShareIn(card.SID)
	if prev != nil {
		prev.cancel()
	}
	// Повідомляємо відправника (best-effort, не блокуємо UI).
	go func() {
		defer crashguard.Recover("shareDecline")
		dctx, dcancel := context.WithTimeout(context.Background(), shareConnectTimeout)
		defer dcancel()
		dial, derr := a.shareDialer(dctx, card)
		if derr != nil {
			return
		}
		dl := share.NewDownloader(share.Options{Dial: dial, Key: card.Key, StageDir: a.shareStageDir(card.MID)})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = dl.Decline(ctx)
	}()
	shareMu.Lock()
	if in, ok := shareIns[card.SID]; ok {
		in.mu.Lock()
		in.view.Phase = "declined"
		in.mu.Unlock()
		shareMu.Unlock()
		a.emitShareIn(in)
	} else {
		shareMu.Unlock()
	}
	// Дочекатись, поки горутина прийому справді зупиниться: інакше вона ще дописує файли в stage,
	// а RemoveAll лишає сміття. Очікування обмежене — UI не повинен зависати.
	if prev != nil && prev.done != nil {
		select {
		case <-prev.done:
		case <-time.After(3 * time.Second):
		}
	}
	return os.RemoveAll(a.shareStageDir(card.MID))
}

// ListShareIn — активні/недавні прийоми цього сеансу.
func (a *App) ListShareIn() []ShareInView {
	shareMu.Lock()
	defer shareMu.Unlock()
	out := make([]ShareInView, 0, len(shareIns))
	for _, i := range shareIns {
		i.mu.Lock()
		out = append(out, i.view)
		i.mu.Unlock()
	}
	return out
}

// ShareRecoverable — недокачані прийоми з попереднього сеансу (після краху/закриття):
// лаунчер пропонує «Продовжити». Прийняті шматки лишились на диску.
type ShareRecoverable struct {
	MID       string `json:"mid"`
	Name      string `json:"name"`
	From      string `json:"from"`
	FilesDone int    `json:"filesDone"`
	Card      string `json:"card"`
}

func (a *App) ListShareRecoverable() []ShareRecoverable {
	entries, err := os.ReadDir(filepath.Join(a.shareRoot(), "in"))
	if err != nil {
		return nil
	}
	var out []ShareRecoverable
	for _, e := range entries {
		if !e.IsDir() || !shareKeyRe.MatchString(e.Name()) {
			continue
		}
		stage := filepath.Join(a.shareRoot(), "in", e.Name())
		raw, err := os.ReadFile(filepath.Join(stage, "card.json"))
		if err != nil {
			continue
		}
		var cj struct {
			Card string `json:"card"`
			From string `json:"from"`
		}
		if json.Unmarshal(raw, &cj) != nil {
			continue
		}
		card, err := parseShareCard(cj.Card)
		if err != nil || a.getShareIn(card.SID) != nil {
			continue
		}
		info, _ := share.ReadStage(stage)
		out = append(out, ShareRecoverable{MID: card.MID, Name: card.Name, From: cj.From, FilesDone: info.FilesDone, Card: cj.Card})
	}
	return out
}

// ShareResumeRecovered — продовжити недокачане (з картки, збереженої при старті прийому).
func (a *App) ShareResumeRecovered(mid string) (ShareInView, error) {
	if !shareKeyRe.MatchString(mid) {
		return ShareInView{}, errors.New("некоректний ідентифікатор")
	}
	raw, err := os.ReadFile(filepath.Join(a.shareStageDir(mid), "card.json"))
	if err != nil {
		return ShareInView{}, errors.New("дані передачі не знайдено")
	}
	var cj struct {
		Card        string `json:"card"`
		From        string `json:"from"`
		FromAccount string `json:"fromAccount"`
	}
	if err := json.Unmarshal(raw, &cj); err != nil {
		return ShareInView{}, err
	}
	return a.ShareAccept(cj.Card, cj.FromAccount, cj.From)
}

// ShareDiscardRecovered — стерти недокачане з попереднього сеансу.
func (a *App) ShareDiscardRecovered(mid string) error {
	if !shareKeyRe.MatchString(mid) {
		return errors.New("некоректний ідентифікатор")
	}
	return os.RemoveAll(a.shareStageDir(mid))
}
