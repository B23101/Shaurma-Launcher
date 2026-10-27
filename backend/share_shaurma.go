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
//
// Фінальні події, що мусять дійти навіть без P2P-тунелю (друг відмовився, відправник
// скасував чи закрив лаунчер), їдуть службовим повідомленням чату
// (share.ControlPrefix, див. internal/share/control.go). Підсумки передач лежать у
// share/history.json — картка в чаті показує їх і після перезапуску.
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

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/export"
	"shaurma-launcher-backend/internal/p2p"
	"shaurma-launcher-backend/internal/servers"
	"shaurma-launcher-backend/internal/share"
	"shaurma-launcher-backend/internal/social"
	"shaurma-launcher-backend/internal/sysinfo"
)

const (
	shareCardPrefix = "shaurma-share:v1:"
	shareMaxLife    = 12 * time.Hour
	// Вхід гостьового P2P-сервісу (p2p.guestExternalAddr) — туди пишемо хендшейк.
	shareGuestGateway = "127.0.0.2:25565"
	// shareStageMaxAge — недокачане старше за це прибирається при старті.
	shareStageMaxAge = 30 * 24 * time.Hour
	// shareRevokeGrace — після закриття доступу хост ще стільки живе, щоб активні
	// одержувачі отримали 410 з точною причиною, а не просто обрив.
	shareRevokeGrace = 3 * time.Second
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
	// Resolving — шукаємо файли на Modrinth (частина підготовки).
	Resolving bool `json:"resolving,omitempty"`
	// CDNFiles / CDNBytes — скільки з переданого знайдено на Modrinth: друзі візьмуть це
	// напряму з CDN, а від нас — лише решту.
	CDNFiles int   `json:"cdnFiles,omitempty"`
	CDNBytes int64 `json:"cdnBytes,omitempty"`
	// Names — ніки одержувачів на момент надсилання (UI показує їх і для колишніх друзів).
	Names map[string]string `json:"names,omitempty"`
	Error string            `json:"error,omitempty"`
	// Reason — чому передачу закрито (share.Reason*), порожньо — усі вирішили самі.
	Reason    string `json:"reason,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// ShareInView — стан приймання для одержувача.
type ShareInView struct {
	ID          string `json:"id"` // sid картки
	MID         string `json:"mid"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	From        string `json:"from"`
	FromAccount string `json:"fromAccount,omitempty"`
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
	// Phase: connecting | verifying | downloading | paused | installing | done | error | declined | cancelled
	Phase    string         `json:"phase"`
	Progress share.Progress `json:"progress"`
	Error    string         `json:"error,omitempty"`
	// Reason — код причини для error/cancelled (share.Reason*).
	Reason string `json:"reason,omitempty"`
	// ResultID — id створеної збірки/сервера після завершення.
	ResultID  string `json:"resultId,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
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
	// closeReason — чому закриваємо (виставляє той, хто скасовує ctx).
	closeReason string
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

	shareHistMu   sync.Mutex
	shareHistInst *share.History
	shareBootOnce sync.Once
	// shareQuitting — лаунчер закривається: зупинка прийому — це «перервано», а не
	// «одержувач скасував» (відправнику про це не пишемо, після запуску можна продовжити).
	shareQuitting bool
)

func (a *App) shareRoot() string { return filepath.Join(a.cfg.Dir(), "share") }

func (a *App) shareStageDir(mid string) string { return filepath.Join(a.shareRoot(), "in", mid) }

func (a *App) shareHist() *share.History {
	shareHistMu.Lock()
	defer shareHistMu.Unlock()
	if shareHistInst == nil {
		shareHistInst = share.NewHistory(filepath.Join(a.shareRoot(), "history.json"), 80)
	}
	return shareHistInst
}

func nowMs() int64 { return time.Now().UnixMilli() }

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

var errShareNotFriend = errors.New("передавати збірки можна лише друзям — додайте цю людину в друзі")

func newShareID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// shareFriendSet — accountId → нік поточних друзів (порожньо, якщо соцсистема не працює).
func shareFriendSet(svc *social.Service) map[string]string {
	out := map[string]string{}
	if svc == nil {
		return out
	}
	for _, f := range svc.Snapshot().Friends {
		out[f.AccountID] = cleanNotifyText(f.Nickname)
	}
	return out
}

// shareSendControl — службове повідомлення в чат (best-effort, у фоні).
func (a *App) shareSendControl(to string, c share.Control, human string) {
	if to == "" {
		return
	}
	go func() {
		defer crashguard.Recover("shareControl")
		// Соцсистема могла ще не піднятись (старт лаунчера) — чекаємо трохи.
		for i := 0; i < 24; i++ {
			if svc := currentSocialService(); svc != nil {
				if _, err := svc.SendChatMessage(to, share.EncodeControl(c, human)); err == nil {
					return
				}
			}
			time.Sleep(5 * time.Second)
		}
	}()
}

// shareIsControlText — службове повідомлення передачі (тост «нове повідомлення» не потрібен).
func shareIsControlText(text string) bool {
	_, ok := share.ParseControl(text)
	return ok
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

func shareInActive(phase string) bool {
	switch phase {
	case "connecting", "verifying", "downloading", "paused", "installing":
		return true
	}
	return false
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
		if shareInActive(p) {
			return true
		}
	}
	return false
}

// shareShutdown — при виході: зупиняємо передачі (журнал одержувача вже на диску).
// Друзям-одержувачам пишемо, що передачу перервано (повідомлення може не встигнути
// піти — тоді його надішле наступний запуск, див. shareBoot).
func (a *App) shareShutdown() {
	shareMu.Lock()
	shareQuitting = true
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
		o.mu.Lock()
		o.closeReason = share.ReasonSenderOffline
		o.mu.Unlock()
		o.cancel()
	}
	for _, i := range ins {
		i.cancel()
	}
}

// ── Відправник ───────────────────────────────────────────────────────────

func (o *shareOut) snapshot() ShareOutView {
	o.mu.Lock()
	defer o.mu.Unlock()
	v := o.view
	v.Recipients = append([]share.RecipientState{}, o.view.Recipients...)
	if o.view.Names != nil {
		v.Names = make(map[string]string, len(o.view.Names))
		for k, n := range o.view.Names {
			v.Names[k] = n
		}
	}
	return v
}

func (a *App) emitShareOut(o *shareOut) {
	a.emit("share:out", o.snapshot())
}

// saveShareOut — підсумок передачі в історію (без довгого списку пропущених файлів).
func (a *App) saveShareOut(o *shareOut) {
	v := o.snapshot()
	if len(v.Skipped) > 20 {
		v.Skipped = v.Skipped[:20]
	}
	_ = a.shareHist().Put("out", v.ID, v)
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

// shareCheckFriends — лише друзі, без повторів, не більше ліміту.
func shareCheckFriends(svc *social.Service, ids []string) ([]string, map[string]string, error) {
	friends := shareFriendSet(svc)
	seen := map[string]bool{}
	var out []string
	names := map[string]string{}
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		nick, ok := friends[id]
		if !ok {
			return nil, nil, errShareNotFriend
		}
		out = append(out, id)
		names[id] = nick
	}
	if len(out) == 0 || len(out) > share.MaxRecipients {
		return nil, nil, fmt.Errorf("оберіть від 1 до %d друзів", share.MaxRecipients)
	}
	return out, names, nil
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
	friendIDs, names, err := shareCheckFriends(svc, req.FriendIDs)
	if err != nil {
		return ShareOutView{}, err
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
		if err := a.shareServerStoppedErr(req.RefID); err != nil {
			return ShareOutView{}, err
		}
		root, name = sv.Dir, sv.Name
		eula, _ := os.ReadFile(filepath.Join(root, "eula.txt"))
		if !strings.Contains(string(eula), "eula=true") {
			return ShareOutView{}, errors.New("у сервері не прийнято EULA (eula.txt) — друг не зможе його додати")
		}
		meta = map[string]string{"core": sv.Core, "mc": sv.MCVersion, "ram": strconv.Itoa(sv.RAMMB)}
		// Вибір файлів, як у збірки; те, без чого сервер не запуститься, — завжди.
		inc := shareInclude(req.Kind)
		pf := newSharePathFilter(req.Included, req.Excluded)
		required := shareServerRequired(root)
		filter = func(rel string, isDir bool) bool {
			if !inc(rel, isDir) {
				return false
			}
			return isDir || shareRequiredHit(required, rel) || pf(rel)
		}
	default:
		return ShareOutView{}, errors.New("невідомий тип передачі")
	}

	ctx, cancel := context.WithCancel(context.Background())
	o := &shareOut{cancel: cancel, keys: map[string]string{}}
	o.view = ShareOutView{ID: newShareID(), Kind: req.Kind, RefID: req.RefID, Name: name, Phase: "preparing",
		Names: names, CreatedAt: nowMs(), UpdatedAt: nowMs()}
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
				o.view.Phase, o.view.Error, o.view.Reason = "error", "внутрішня помилка підготовки передачі", share.ReasonError
				o.mu.Unlock()
				a.finishShareOut(o)
			}
		}()
		if err := a.runShareOut(ctx, o, svc, accountID, root, meta, filter, friendIDs); err != nil {
			o.mu.Lock()
			if errors.Is(err, context.Canceled) {
				// Відправник сам закрив передачу під час підготовки — це не помилка.
				o.view.Phase = "closed"
				if o.closeReason == "" {
					o.closeReason = share.ReasonCancelledSender
				}
			} else {
				o.view.Phase, o.view.Error, o.view.Reason = "error", err.Error(), share.ReasonError
			}
			o.mu.Unlock()
			a.finishShareOut(o)
		}
	}()
	return o.snapshot(), nil
}

// ShareServerTree — дерево файлів сервера для вибору «що передати».
type ShareServerTree struct {
	Items []export.ExportCandidate `json:"items"`
	// Required — шляхи (файли чи теки), без яких сервер у друга не запуститься: завжди
	// передаються, у дереві заблоковані.
	Required []string `json:"required"`
}

// shareServerRequired — eula.txt, server.properties, jar ядра, а для Forge/NeoForge
// (запуск через @libraries/…/args.txt) — тека libraries і user_jvm_args.txt.
func shareServerRequired(dir string) []string {
	var out []string
	add := func(rel string) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			out = append(out, rel)
		}
	}
	add("eula.txt")
	add("server.properties")
	if res, err := servers.DetectCore(dir); err == nil {
		if j := res.ServerJar; j != "" && !strings.ContainsAny(j, "/\\") {
			add(j)
		}
		switch strings.ToLower(res.Core) {
		case "forge", "neoforge":
			add("libraries")
			add("user_jvm_args.txt")
		}
	}
	return out
}

func shareRequiredHit(required []string, rel string) bool {
	for _, r := range required {
		if rel == r || strings.HasPrefix(rel, r+"/") {
			return true
		}
	}
	return false
}

// shareServerShallow — теки, що показуються одним вузлом (без тисяч файлів усередині):
// світи (будь-яка тека з level.dat), бібліотеки, кеші, бекапи.
func shareServerShallow(dir string) func(rel string) bool {
	return func(rel string) bool {
		if strings.Contains(rel, "/") {
			return false
		}
		switch strings.ToLower(rel) {
		case "libraries", "versions", "cache", "backups", "bundler", ".fabric", ".quilt":
			return true
		}
		_, err := os.Stat(filepath.Join(dir, rel, "level.dat"))
		return err == nil
	}
}

// shareServerStoppedErr — сервер має бути зупинений: під час роботи файли світу змінюються,
// і контрольні суми в друга не зійдуться. Перевіряється вже на першому кроці (дерево файлів),
// а не лише після вибору друзів.
func (a *App) shareServerStoppedErr(id string) error {
	switch a.srvAPI.ServerStatus(id).Status {
	case "", "stopped", "crashed", "error":
		return nil
	}
	return errors.New("зупиніть сервер перед передачею — під час роботи його файли змінюються")
}

// GetShareServerTree — що можна передати з сервера (усе, крім логів, вибрано; бекапи й
// кеш — ні). Required показуються заблокованими й передаються завжди.
func (a *App) GetShareServerTree(serverID string) (ShareServerTree, error) {
	sv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		return ShareServerTree{}, err
	}
	if err := a.shareServerStoppedErr(serverID); err != nil {
		return ShareServerTree{}, err
	}
	items, err := export.ScanCandidatesShallow(sv.Dir, shareServerShallow(sv.Dir))
	if err != nil {
		return ShareServerTree{}, err
	}
	required := shareServerRequired(sv.Dir)
	keep := items[:0]
	for _, c := range items {
		top := strings.ToLower(strings.SplitN(c.Path, "/", 2)[0])
		if top == ".shaurma" {
			continue
		}
		c.DefaultSelected = top != "backups" && top != "cache" || shareRequiredHit(required, c.Path)
		keep = append(keep, c)
	}
	return ShareServerTree{Items: keep, Required: required}, nil
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
	var sha1Mu sync.Mutex
	sha1s := map[string]string{}
	m, skipped, err := share.BuildManifest(ctx, root, o.view.Kind, o.view.Name, meta, share.BuildOptions{
		Include:  filter,
		WantSHA1: shareWantSHA1,
		OnSHA1: func(rel, sum string) {
			sha1Mu.Lock()
			sha1s[rel] = sum
			sha1Mu.Unlock()
		},
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
	o.mu.Lock()
	o.view.Resolving = true
	o.mu.Unlock()
	a.emitShareOut(o)
	cdnFiles, cdnBytes := a.shareResolveModrinth(ctx, m, sha1s)
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	o.view.Resolving, o.view.CDNFiles, o.view.CDNBytes = false, cdnFiles, cdnBytes
	o.mu.Unlock()
	host, err := share.NewHost(root, m, func(rs share.RecipientState) {
		o.mu.Lock()
		replaced, changed := false, true
		for i := range o.view.Recipients {
			if o.view.Recipients[i].ID == rs.ID {
				prev := o.view.Recipients[i]
				changed = prev.State != rs.State || prev.Offline != rs.Offline
				o.view.Recipients[i], replaced = rs, true
			}
		}
		if !replaced {
			o.view.Recipients = append(o.view.Recipients, rs)
		}
		o.view.UpdatedAt = nowMs()
		o.mu.Unlock()
		a.emitShareOut(o)
		if changed {
			a.saveShareOut(o) // зміна стану (не кожен звіт прогресу) — в історію
		}
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
	o.view.UpdatedAt = nowMs()
	kind, name, sid := o.view.Kind, o.view.Name, o.view.ID
	o.cardFn = func(key string) ShareCard {
		return ShareCard{V: 1, Kind: kind, Name: name, Addr: addr, Key: key, MID: m.ID(), SID: sid,
			Files: len(m.Files), Bytes: m.TotalBytes, Meta: shareCardMeta(meta), Dev: a.shareDeviceTag()}
	}
	o.mu.Unlock()
	a.emitShareOut(o)
	a.saveShareOut(o)

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

// shareContentDirs — теки вмісту, файли з яких шукаємо на Modrinth і показуємо списком.
var shareContentDirs = map[string]bool{"mods": true, "resourcepacks": true, "shaderpacks": true, "plugins": true, "datapacks": true}

func shareTopDir(rel string) string {
	if i := strings.IndexByte(rel, '/'); i > 0 {
		return strings.ToLower(rel[:i])
	}
	return ""
}

func shareContentFile(rel string) bool {
	l := strings.ToLower(rel)
	return strings.HasSuffix(l, ".jar") || strings.HasSuffix(l, ".zip") || strings.HasSuffix(l, ".jar.disabled")
}

// shareWantSHA1 — для яких файлів рахувати sha1 (Modrinth шукає лише за sha1/sha512).
func shareWantSHA1(rel string) bool {
	return shareContentDirs[shareTopDir(rel)] && shareContentFile(rel) && !strings.HasSuffix(strings.ToLower(rel), ".disabled")
}

// shareResolveModrinth — одним пакетним запитом дізнається, які файли є на Modrinth, і
// позначає їх у маніфесті (src): друзі візьмуть їх напряму з CDN. Будь-який збій тут —
// не помилка передачі: тоді все просто їде через P2P.
func (a *App) shareResolveModrinth(ctx context.Context, m *share.Manifest, sha1s map[string]string) (int, int64) {
	if a.modrinth == nil || len(sha1s) == 0 {
		return 0, 0
	}
	seen := map[string]bool{}
	var list []string
	for _, s := range sha1s {
		if !seen[s] {
			seen[s] = true
			list = append(list, s)
		}
	}
	type res struct {
		v   map[string]*api.FullVersion
		err error
	}
	ch := make(chan res, 1)
	go func() {
		defer crashguard.Recover("shareResolveModrinth")
		v, err := a.modrinth.GetVersionsByHashes(list)
		ch <- res{v, err}
	}()
	var found map[string]*api.FullVersion
	select {
	case r := <-ch:
		if r.err != nil {
			return 0, 0
		}
		found = r.v
	case <-ctx.Done():
		return 0, 0
	case <-time.After(25 * time.Second):
		return 0, 0
	}
	files, bytes := 0, int64(0)
	for i := range m.Files {
		fe := &m.Files[i]
		sum := sha1s[fe.Path]
		v := found[sum]
		if sum == "" || v == nil {
			continue
		}
		var url string
		for _, f := range v.Files {
			if (f.Sha1 != "" && strings.EqualFold(f.Sha1, sum)) || (f.Sha1 == "" && f.Primary) {
				url = f.URL
				break
			}
		}
		if url == "" && len(v.Files) == 1 {
			url = v.Files[0].URL
		}
		src := &share.FileSource{Modrinth: v.ProjectID, Version: v.ID, URL: url}
		fe.Src = src
		if !src.Valid() { // неочікуваний id чи URL — це джерело не беремо
			fe.Src = nil
			continue
		}
		files++
		bytes += fe.Size
	}
	return files, bytes
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
			// Картка не дійшла — друг про передачу не знає; одразу закриваємо йому доступ.
			host.Revoke(f)
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
	ids, names, err := shareCheckFriends(svc, friendIDs)
	if err != nil {
		return err
	}
	o.mu.Lock()
	if o.view.Names == nil {
		o.view.Names = map[string]string{}
	}
	for k, n := range names {
		o.view.Names[k] = n
	}
	o.mu.Unlock()
	return a.shareInvite(o, svc, ids)
}

func (a *App) watchShareOut(ctx context.Context, o *shareOut, svc *social.Service) {
	reason := ""
	defer func() {
		o.mu.Lock()
		if o.closeReason == "" {
			o.closeReason = reason
		}
		o.mu.Unlock()
		a.finishShareOut(o)
	}()
	deadline := time.After(shareMaxLife)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	n := 0
	settledAt := time.Time{}
	for {
		select {
		case <-ctx.Done():
			reason = share.ReasonCancelledSender
			return
		case <-deadline:
			reason = share.ReasonTimeout
			return
		case <-tick.C:
		}
		o.mu.Lock()
		host, invID := o.host, o.invID
		o.mu.Unlock()
		host.Sweep(time.Now()) // «друг офлайн» / тайм-аути прийняття
		if n++; n%3 == 0 {
			if alive, err := svc.TempInviteBeat(invID, 0); err == nil && !alive {
				reason = share.ReasonAccess
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

// shareCtlHuman — людський рядок службового повідомлення (для старих версій лаунчера).
func shareCtlHuman(event, reason, name string) string {
	switch event {
	case share.CtlDeclined:
		return fmt.Sprintf("✖ Відмова від «%s»", name)
	case share.CtlFailed:
		return fmt.Sprintf("⚠ Не вдалося прийняти «%s»", name)
	}
	if reason == share.ReasonCancelledReceiver {
		return fmt.Sprintf("⏹ Прийом «%s» зупинено", name)
	}
	return fmt.Sprintf("⏹ Передачу «%s» скасовано", name)
}

func (a *App) finishShareOut(o *shareOut) {
	o.mu.Lock()
	host, sub, invID, reason, sid, name := o.host, o.sub, o.invID, o.closeReason, o.view.ID, o.view.Name
	o.mu.Unlock()
	var hit []string
	if host != nil {
		if reason == "" {
			reason = share.ReasonCancelledSender
		}
		hit = host.RevokeAll(reason)
	}
	// Підсумок — в UI одразу: пауза нижче й закриття P2P/запрошення (мережа) тривають
	// кілька секунд, і вікно передачі весь цей час виглядало «живим» і зависло.
	o.mu.Lock()
	if o.view.Phase != "error" {
		o.view.Phase = "closed"
		if len(hit) > 0 {
			o.view.Reason = reason
		}
	}
	o.view.UpdatedAt = nowMs()
	o.mu.Unlock()
	a.emitShareOut(o)
	if host != nil {
		if len(hit) > 0 {
			// Хай активні одержувачі отримають 410 із причиною, а не голий обрив.
			time.Sleep(shareRevokeGrace)
		}
		host.Close()
	}
	for _, id := range hit {
		// Ті, хто ще не відповів чи качав, мають дізнатись навіть без тунелю.
		ctlReason := reason
		if ctlReason != share.ReasonTimeout && ctlReason != share.ReasonSenderOffline {
			ctlReason = share.ReasonCancelledSender
		}
		a.shareSendControl(id, share.Control{SID: sid, Event: share.CtlCancelled, Reason: ctlReason, Name: name},
			shareCtlHuman(share.CtlCancelled, ctlReason, name))
	}
	if sub != "" {
		if err := a.p2pAPI.ReleaseTempAddress(sub); err != nil {
			_ = a.p2pAPI.StopP2PHost(sub)
		}
	}
	if svc := a.socialService(); svc != nil && invID != "" {
		_ = svc.TempInviteClose(invID)
	}
	a.saveShareOut(o)
	shareMu.Lock()
	delete(shareOuts, sid)
	shareMu.Unlock()
	a.emit("share:out-closed", sid)
}

// ShareStop — відправник закриває передачу (усім одержувачам). Прийняте ними лишається.
func (a *App) ShareStop(id string) error {
	shareMu.Lock()
	o := shareOuts[id]
	shareMu.Unlock()
	if o == nil {
		return nil
	}
	o.mu.Lock()
	if o.closeReason == "" {
		o.closeReason = share.ReasonCancelledSender
	}
	o.mu.Unlock()
	o.cancel()
	return nil
}

func (a *App) shareOutHost(id string) (*shareOut, *share.Host, error) {
	shareMu.Lock()
	o := shareOuts[id]
	shareMu.Unlock()
	if o == nil {
		return nil, nil, errors.New("передачу не знайдено")
	}
	o.mu.Lock()
	host := o.host
	o.mu.Unlock()
	if host == nil {
		return nil, nil, errors.New("передача ще готується")
	}
	return o, host, nil
}

// ShareSenderPause — пауза/відновлення з боку відправника для одного одержувача.
func (a *App) ShareSenderPause(id, accountID string, paused bool) error {
	_, host, err := a.shareOutHost(id)
	if err != nil {
		return err
	}
	host.SetPaused(accountID, paused)
	return nil
}

// ShareRevokeRecipient — скасувати передачу одному одержувачу.
func (a *App) ShareRevokeRecipient(id, accountID string) error {
	o, host, err := a.shareOutHost(id)
	if err != nil {
		return err
	}
	if host.Revoke(accountID) {
		o.mu.Lock()
		name := o.view.Name
		o.mu.Unlock()
		a.shareSendControl(accountID, share.Control{SID: id, Event: share.CtlCancelled, Reason: share.ReasonCancelledSender, Name: name},
			shareCtlHuman(share.CtlCancelled, share.ReasonCancelledSender, name))
	}
	return nil
}

// ListShareOut — активні передачі відправника + недавні підсумки (з історії).
func (a *App) ListShareOut() []ShareOutView {
	a.shareBoot()
	shareMu.Lock()
	out := make([]ShareOutView, 0, len(shareOuts))
	live := map[string]bool{}
	for _, o := range shareOuts {
		v := o.snapshot()
		live[v.ID] = true
		out = append(out, v)
	}
	shareMu.Unlock()
	for _, raw := range a.shareHist().List("out") {
		var v ShareOutView
		if json.Unmarshal(raw, &v) == nil && v.ID != "" && !live[v.ID] {
			out = append(out, v)
		}
	}
	return out
}

// ── Службові повідомлення з чату ─────────────────────────────────────────

// shareOnSocial — хук на події соцсистеми: службові повідомлення передач.
func (a *App) shareOnSocial(event string, payload any) {
	if event != "chat:message" {
		return
	}
	ev, ok := payload.(social.ChatEvent)
	if !ok || ev.Mine || ev.DecryptFailed || ev.From == "" || (ev.BodyType != "" && ev.BodyType != "text") {
		return
	}
	c, ok := share.ParseControl(ev.Plaintext)
	if !ok {
		return
	}
	nick := a.socialNickname(ev.From)
	if nick == "" {
		return // лише від друзів
	}
	go func() {
		defer crashguard.Recover("shareControlIn")
		a.applyShareControl(c, ev.From, nick)
	}()
}

func (a *App) applyShareControl(c share.Control, from, nick string) {
	// 1. Я — відправник, подія від одержувача.
	shareMu.Lock()
	o := shareOuts[c.SID]
	shareMu.Unlock()
	if o != nil {
		o.mu.Lock()
		host := o.host
		o.mu.Unlock()
		if host != nil {
			host.ApplyControl(from, c.Event, c.Reason)
		}
		return
	}
	// Передачу вже закрито — оновлюємо підсумок в історії (друг відмовився пізніше).
	var hv ShareOutView
	if a.shareHist().Get("out", c.SID, &hv) {
		for i := range hv.Recipients {
			r := &hv.Recipients[i]
			if r.ID != from || share.IsFinalState(r.State) && r.State != share.StateRevoked && r.State != share.StateTimeout {
				continue
			}
			switch c.Event {
			case share.CtlDeclined:
				r.State, r.Reason = share.StateDeclined, share.ReasonDeclined
			case share.CtlCancelled:
				r.State, r.Reason = share.StateCancelled, share.ReasonCancelledReceiver
			case share.CtlFailed:
				r.State, r.Reason = share.StateFailed, share.CleanReason(c.Reason)
			}
			hv.UpdatedAt = nowMs()
			_ = a.shareHist().Put("out", hv.ID, hv)
			a.emit("share:out", hv)
		}
		return
	}
	// 2. Я — одержувач: відправник скасував / закрив доступ.
	if c.Event != share.CtlCancelled {
		return
	}
	reason := c.Reason
	if reason != share.ReasonTimeout && reason != share.ReasonSenderOffline {
		reason = share.ReasonCancelledSender
	}
	if in := a.getShareIn(c.SID); in != nil {
		in.mu.Lock()
		own := in.fromAcc == "" || in.fromAcc == from
		final := in.view.Phase == "done" || in.view.Phase == "declined"
		in.mu.Unlock()
		if !own || final {
			return
		}
		in.cancel()
		a.setShareInPhase(in, "cancelled", "", reason, true)
		return
	}
	var prev ShareInView
	if a.shareHist().Get("in", c.SID, &prev) && (prev.Phase == "done" || prev.Phase == "declined") {
		return
	}
	v := ShareInView{ID: c.SID, Name: c.Name, From: nick, FromAccount: from, Phase: "cancelled", Reason: reason,
		CreatedAt: nowMs(), UpdatedAt: nowMs()}
	if prev.ID != "" {
		v = prev
		v.Phase, v.Reason, v.UpdatedAt = "cancelled", reason, nowMs()
	}
	_ = a.shareHist().Put("in", v.ID, v)
	a.emit("share:in", v)
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
		len(c.Name) == 0 || len(c.Name) > 200 || c.Bytes < 0 || c.Bytes > share.MaxTotalBytes ||
		c.Files < 0 || c.Files > share.MaxFiles {
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

func shareInFinal(phase string) bool {
	return phase == "done" || phase == "declined" || phase == "cancelled" || phase == "error"
}

// setShareInPhase — зміна фази прийому. Остаточні фази (відмова, скасування, готово) не
// перезаписуються пізнішими подіями горутини прийому — крім force (явна дія людини/відправника).
func (a *App) setShareInPhase(in *shareIn, phase, errText, reason string, force bool) {
	in.mu.Lock()
	cur := in.view.Phase
	if !force && (cur == "declined" || cur == "cancelled" || cur == "done") {
		in.mu.Unlock()
		return
	}
	in.view.Phase, in.view.Error, in.view.Reason = phase, errText, reason
	in.view.UpdatedAt = nowMs()
	v := in.view
	in.mu.Unlock()
	a.emit("share:in", v)
	if shareInFinal(phase) || cur == "" || (phase == "installing" && cur != phase) {
		_ = a.shareHist().Put("in", v.ID, v)
	}
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
	// Лише від друзів: чат може доставити повідомлення й від не-друга (налаштування
	// «писати можуть усі»), а приймати від нього файли — ні.
	friends := shareFriendSet(a.socialService())
	nick, ok := friends[fromAccountID]
	if !ok {
		return ShareInView{}, errShareNotFriend
	}
	if nick != "" {
		fromName = nick
	}
	// Повністю прийняте встановлюється й без відправника — тоді його скасування не заважає.
	info, _ := share.ReadStage(a.shareStageDir(card.MID))
	var hist ShareInView
	if !(info.Complete && info.ManifestID == card.MID) && a.shareHist().Get("in", card.SID, &hist) && hist.Phase == "cancelled" &&
		(hist.Reason == share.ReasonCancelledSender || hist.Reason == share.ReasonTimeout) {
		return hist, errors.New("відправник скасував цю передачу — попросіть надіслати збірку ще раз (прийняте раніше не пропаде)")
	}
	shareMu.Lock()
	prev := shareIns[card.SID]
	if prev != nil {
		prev.mu.Lock()
		v := prev.view
		prev.mu.Unlock()
		if shareInActive(v.Phase) {
			shareMu.Unlock()
			return v, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	in := &shareIn{cancel: cancel, card: card, stage: a.shareStageDir(card.MID), fromAcc: fromAccountID, done: make(chan struct{})}
	in.view = ShareInView{ID: card.SID, MID: card.MID, Kind: card.Kind, Name: card.Name, From: fromName, FromAccount: fromAccountID,
		Files: card.Files, Bytes: card.Bytes, CreatedAt: nowMs(), UpdatedAt: nowMs()}
	shareIns[card.SID] = in
	shareMu.Unlock()

	// Попередня спроба тієї самої картки ще може дописувати stage — дочекаймось її.
	if prev != nil && prev.done != nil {
		select {
		case <-prev.done:
		case <-time.After(5 * time.Second):
		}
	}
	a.setShareInPhase(in, "connecting", "", "", true)

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
			if shareInActive(p) {
				a.setShareInPhase(in, "error", "внутрішня помилка приймання", share.ReasonError, false)
			}
		}()
		a.runShareIn(ctx, in)
	}()
	in.mu.Lock()
	defer in.mu.Unlock()
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

// shareFreeSpace — вільне місце на диску теки (байти), -1 — невідомо.
func shareFreeSpace(dir string) int64 {
	mb := sysinfo.Collect(dir).FreeDiskMB
	if mb <= 0 {
		return -1
	}
	return mb << 20
}

// shareSenderOffline — присутність каже, що відправник офлайн (не «невідомо»).
func shareSenderOffline(accountID string) bool {
	svc := currentSocialService()
	if svc == nil || accountID == "" {
		return false
	}
	for _, f := range svc.Snapshot().Friends {
		if f.AccountID == accountID {
			return f.Presence.Status == social.StatusOffline
		}
	}
	return false
}

// shareReportsToSender — причини збою, про які відправник інакше не дізнається
// (тунель міг уже впасти, а на його боці «просто стало тихо»).
func shareReportsToSender(reason string) bool {
	switch reason {
	case share.ReasonDiskFull, share.ReasonIntegrity, share.ReasonChanged:
		return true
	}
	return false
}

func (a *App) runShareIn(ctx context.Context, in *shareIn) {
	fail := func(err error) {
		reason := share.ReasonOf(err)
		switch {
		case ctx.Err() != nil || errors.Is(err, context.Canceled):
			// Явна зупинка вже виставила фазу; сюди потрапляємо при виході з лаунчера.
			r := share.ReasonCancelledReceiver
			if shareQuittingNow() {
				r = share.ReasonInterrupted
			}
			a.setShareInPhase(in, "cancelled", "", r, false)
			return
		case errors.Is(err, context.DeadlineExceeded):
			reason = share.ReasonSenderOffline
		}
		a.setShareInPhase(in, "error", err.Error(), reason, false)
		if shareReportsToSender(reason) {
			a.shareSendControl(in.fromAcc, share.Control{SID: in.card.SID, Event: share.CtlFailed, Reason: reason, Name: in.card.Name},
				shareCtlHuman(share.CtlFailed, reason, in.card.Name))
		}
	}
	// Встановлення, перерване крахом, — довести до чистого стану.
	if m, done, err := share.RecoverInstall(in.stage, a.shareInstallRegistered); err != nil {
		fail(fmt.Errorf("не вдалося відновити перерване встановлення: %w", err))
		return
	} else if done {
		_ = os.RemoveAll(in.stage)
		in.mu.Lock()
		in.view.ResultID = a.shareResultIDFor(m)
		in.mu.Unlock()
		a.setShareInPhase(in, "done", "", "", false)
		return
	}
	// Завершення без зв'язку: усе вже прийняте до краху.
	if info, ok := share.ReadStage(in.stage); ok && info.Complete && info.ManifestID == in.card.MID {
		if m, err := share.LoadStageManifest(in.stage); err == nil && m.ID() == in.card.MID {
			a.finalizeShareIn(in, m)
			return
		}
	}
	dial, err := a.shareDialer(ctx, in.card)
	if err != nil {
		if ctx.Err() == nil {
			err = fmt.Errorf("%w: %v", share.ErrSenderOffline, err)
		}
		fail(err)
		return
	}
	var lastEmit time.Time
	from := in.fromAcc
	dl := share.NewDownloader(share.Options{
		Dial: dial, Key: in.card.Key, StageDir: in.stage, ExpectManifestID: in.card.MID,
		ExpectBytes: in.card.Bytes, ExpectFiles: in.card.Files,
		FreeSpace:  shareFreeSpace,
		SenderGone: func() bool { return shareSenderOffline(from) },
		OnProgress: func(p share.Progress) {
			in.mu.Lock()
			if shareInFinal(in.view.Phase) {
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
			case share.ProgVerifying:
				in.view.Phase = "verifying"
			}
			in.view.UpdatedAt = nowMs()
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
		if errors.Is(merr, context.DeadlineExceeded) && ctx.Err() == nil {
			merr = fmt.Errorf("%w: відправник не відповідає — можливо, він закрив доступ", share.ErrSenderOffline)
		}
		fail(merr)
		return
	}

	if err := dl.Run(ctx); err != nil {
		fail(err)
		return
	}
	m, err := dl.Manifest(ctx)
	if err != nil {
		fail(err)
		return
	}
	a.finalizeShareIn(in, m)
}

func shareQuittingNow() bool {
	shareMu.Lock()
	defer shareMu.Unlock()
	return shareQuitting
}

// shareInstallRegistered — чи лаунчер уже знає збірку/сервер із маркера встановлення.
func (a *App) shareInstallRegistered(m share.InstallMarker) bool {
	if m.ResultID != "" {
		if _, ok := a.customPacks.Get(m.ResultID); ok {
			return true
		}
	}
	return a.shareServerIDByDir(m.Dst) != ""
}

func (a *App) shareServerIDByDir(dir string) string {
	if a.srvAPI == nil || dir == "" {
		return ""
	}
	list, err := a.srvAPI.ListServers()
	if err != nil {
		return ""
	}
	want := strings.ToLower(filepath.Clean(dir))
	for _, s := range list {
		if strings.ToLower(filepath.Clean(s.Dir)) == want {
			return s.ID
		}
	}
	return ""
}

func (a *App) shareResultIDFor(m share.InstallMarker) string {
	if m.ResultID != "" {
		return m.ResultID
	}
	return a.shareServerIDByDir(m.Dst)
}

// finalizeShareIn переносить прийняті файли на місце й реєструє збірку/сервер.
func (a *App) finalizeShareIn(in *shareIn, m *share.Manifest) {
	a.setShareInPhase(in, "installing", "", "", false)
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
		a.setShareInPhase(in, "error", err.Error(), share.ReasonError, false)
		return
	}
	_ = os.RemoveAll(in.stage)
	in.mu.Lock()
	in.view.ResultID = resultID
	in.mu.Unlock()
	a.setShareInPhase(in, "done", "", "", false)
}

func (a *App) installSharedBuild(in *shareIn, m *share.Manifest, files string) (string, error) {
	newID, newName := a.customPacks.UniqueIDAndName(m.Name)
	for i := 2; a.packIDExists(newID) || dirExists(a.packDir(newID, "")); i++ {
		newID = fmt.Sprintf("%s-%d", newID, i)
	}
	dst := filepath.Join(a.packDir(newID, ""), ".minecraft")
	marker := share.InstallMarker{Dst: dst, ResultID: newID}
	if err := share.BeginInstall(in.stage, marker); err != nil {
		return "", fmt.Errorf("не вдалося почати встановлення: %w", err)
	}
	if err := share.MoveTree(files, dst); err != nil {
		share.ClearInstall(in.stage)
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
		_ = os.RemoveAll(a.packDir(newID, ""))
		share.ClearInstall(in.stage)
		return "", fmt.Errorf("не вдалося зберегти збірку: %w", err)
	}
	_ = share.MarkInstalled(in.stage, marker)
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
	marker := share.InstallMarker{Dst: dst}
	if err := share.BeginInstall(in.stage, marker); err != nil {
		return "", fmt.Errorf("не вдалося почати встановлення: %w", err)
	}
	if err := share.MoveTree(files, dst); err != nil {
		share.ClearInstall(in.stage)
		return "", fmt.Errorf("не вдалося встановити сервер: %w", err)
	}
	ram, _ := strconv.Atoi(m.Meta["ram"])
	view, err := a.srvAPI.AddServerFromFolder(dst, m.Name, ram, false)
	if err != nil {
		_ = share.MoveTree(dst, files)
		share.ClearInstall(in.stage)
		return "", fmt.Errorf("не вдалося додати сервер: %w", err)
	}
	marker.ResultID = view.ID
	_ = share.MarkInstalled(in.stage, marker)
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
// Відправник дізнається одразу (звіт через тунель) і гарантовано (службове повідомлення).
func (a *App) ShareCancelIn(sid string) error {
	in := a.getShareIn(sid)
	if in == nil {
		return nil
	}
	in.mu.Lock()
	p := in.view.Phase
	dl, from, name := in.dl, in.fromAcc, in.card.Name
	in.mu.Unlock()
	if !shareInActive(p) || p == "installing" {
		return nil
	}
	in.cancel()
	// Одразу показуємо «зупинено» — не чекаємо, поки горутина прийому (можливо, застрягла
	// в мережевому виклику) помітить скасування.
	a.setShareInPhase(in, "cancelled", "", share.ReasonCancelledReceiver, true)
	go func() {
		defer crashguard.Recover("shareCancelReport")
		if dl != nil {
			dl.ReportCancelled(context.Background())
		}
	}()
	a.shareSendControl(from, share.Control{SID: sid, Event: share.CtlCancelled, Reason: share.ReasonCancelledReceiver, Name: name},
		shareCtlHuman(share.CtlCancelled, share.ReasonCancelledReceiver, name))
	return nil
}

// ShareDiscard — відмовитись остаточно й стерти прийняте. fromAccountID — відправник
// (з чату), щоб він дізнався про відмову навіть без тунелю.
func (a *App) ShareDiscard(sid, encoded, fromAccountID string) error {
	card, err := parseShareCard(encoded)
	if err != nil {
		return err
	}
	prev := a.getShareIn(card.SID)
	if prev != nil {
		prev.cancel()
		prev.mu.Lock()
		if prev.fromAcc != "" {
			fromAccountID = prev.fromAcc
		}
		prev.mu.Unlock()
	}
	// Повідомляємо відправника: швидко через тунель (якщо він живий) і гарантовано — у чат.
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
	if a.socialNickname(fromAccountID) != "" {
		a.shareSendControl(fromAccountID, share.Control{SID: card.SID, Event: share.CtlDeclined, Name: card.Name},
			shareCtlHuman(share.CtlDeclined, "", card.Name))
	}
	if prev != nil {
		a.setShareInPhase(prev, "declined", "", share.ReasonDeclined, true)
	} else {
		v := ShareInView{ID: card.SID, MID: card.MID, Kind: card.Kind, Name: card.Name, From: a.socialNickname(fromAccountID),
			FromAccount: fromAccountID, Files: card.Files, Bytes: card.Bytes, Phase: "declined", Reason: share.ReasonDeclined,
			CreatedAt: nowMs(), UpdatedAt: nowMs()}
		_ = a.shareHist().Put("in", v.ID, v)
		a.emit("share:in", v)
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

// ListShareIn — активні прийоми цього сеансу + недавні підсумки (з історії).
func (a *App) ListShareIn() []ShareInView {
	a.shareBoot()
	shareMu.Lock()
	out := make([]ShareInView, 0, len(shareIns))
	live := map[string]bool{}
	for _, i := range shareIns {
		i.mu.Lock()
		out = append(out, i.view)
		live[i.view.ID] = true
		i.mu.Unlock()
	}
	shareMu.Unlock()
	for _, raw := range a.shareHist().List("in") {
		var v ShareInView
		if json.Unmarshal(raw, &v) == nil && v.ID != "" && !live[v.ID] {
			out = append(out, v)
		}
	}
	return out
}

// ── Після перезапуску ────────────────────────────────────────────────────

// shareBoot — один раз за запуск: доводимо до чистого стану те, що лишилось від
// попереднього сеансу (краш, вбитий процес, вимкнене живлення):
//   - перервані встановлення — відкат або завершення (share.RecoverInstall);
//   - сміття в теці прийому та дуже старі недокачані передачі — видаляються;
//   - незавершені передачі в історії — «перервано»; одержувачам, яким ми віддавали
//     збірку, пишемо, що передачу скасовано (інакше вони чекали б до тайм-ауту).
func (a *App) shareBoot() {
	shareBootOnce.Do(func() {
		inDir := filepath.Join(a.shareRoot(), "in")
		entries, _ := os.ReadDir(inDir)
		for _, e := range entries {
			stage := filepath.Join(inDir, e.Name())
			if !e.IsDir() || !shareKeyRe.MatchString(e.Name()) {
				_ = os.RemoveAll(stage)
				continue
			}
			if _, done, err := share.RecoverInstall(stage, a.shareInstallRegistered); err == nil && done {
				_ = os.RemoveAll(stage)
				continue
			}
			st, err := os.Stat(filepath.Join(stage, "card.json"))
			if err != nil || time.Since(st.ModTime()) > shareStageMaxAge {
				_ = os.RemoveAll(stage)
			}
		}
		h := a.shareHist()
		for _, raw := range h.List("in") {
			var v ShareInView
			if json.Unmarshal(raw, &v) == nil && shareInActive(v.Phase) {
				v.Phase, v.Reason, v.UpdatedAt = "cancelled", share.ReasonInterrupted, nowMs()
				v.Progress.SpeedBPS, v.Progress.Reconnecting = 0, false
				_ = h.Put("in", v.ID, v)
			}
		}
		for _, raw := range h.List("out") {
			var v ShareOutView
			if json.Unmarshal(raw, &v) != nil || (v.Phase != "preparing" && v.Phase != "ready") {
				continue
			}
			v.Phase, v.Reason, v.UpdatedAt = "closed", share.ReasonInterrupted, nowMs()
			for i := range v.Recipients {
				r := &v.Recipients[i]
				if share.IsFinalState(r.State) {
					continue
				}
				r.State, r.Reason, r.Offline, r.SpeedBPS = share.StateRevoked, share.ReasonInterrupted, false, 0
				a.shareSendControl(r.ID, share.Control{SID: v.ID, Event: share.CtlCancelled, Reason: share.ReasonSenderOffline, Name: v.Name},
					shareCtlHuman(share.CtlCancelled, share.ReasonSenderOffline, v.Name))
			}
			_ = h.Put("out", v.ID, v)
		}
	})
}

// ShareRecoverable — недокачані прийоми з попереднього сеансу (після краху/закриття):
// лаунчер пропонує «Продовжити». Прийняті шматки лишились на диску.
type ShareRecoverable struct {
	MID       string `json:"mid"`
	Name      string `json:"name"`
	From      string `json:"from"`
	FilesDone int    `json:"filesDone"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
	Kind      string `json:"kind"`
	// Complete — усе прийнято й перевірено, лишилось встановити (працює без відправника).
	Complete bool `json:"complete"`
	// Reason — чим закінчилась ця передача востаннє (share.Reason*), якщо відомо.
	Reason string `json:"reason,omitempty"`
	Card   string `json:"card"`
}

func (a *App) ListShareRecoverable() []ShareRecoverable {
	a.shareBoot()
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
		if err != nil {
			continue
		}
		if in := a.getShareIn(card.SID); in != nil {
			in.mu.Lock()
			p := in.view.Phase
			in.mu.Unlock()
			if shareInActive(p) || p == "done" {
				continue
			}
		}
		info, _ := share.ReadStage(stage)
		r := ShareRecoverable{MID: card.MID, Name: card.Name, From: cj.From, FilesDone: info.FilesDone, Files: card.Files,
			Bytes: card.Bytes, Kind: card.Kind, Complete: info.Complete && info.ManifestID == card.MID, Card: cj.Card}
		var hv ShareInView
		if a.shareHist().Get("in", card.SID, &hv) {
			r.Reason = hv.Reason
		}
		out = append(out, r)
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

// ── Перегляд вмісту до прийняття ─────────────────────────────────────────

// SharePreviewItem — один мод / ресурспак / шейдер / плагін у передачі.
type SharePreviewItem struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Category string `json:"category"` // mods | resourcepacks | shaderpacks | plugins | datapacks
	Disabled bool   `json:"disabled,omitempty"`
	// Modrinth — є на Modrinth (тоді приходить напряму з CDN і відкривається в браузері контенту).
	ProjectID string `json:"projectId,omitempty"`
	Title     string `json:"title,omitempty"`
	IconURL   string `json:"iconUrl,omitempty"`
	Slug      string `json:"slug,omitempty"`
}

// SharePreviewGroup — підсумок за категорією (моди, конфіги, світи…).
type SharePreviewGroup struct {
	Key   string `json:"key"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// SharePreviewView — що всередині передачі.
type SharePreviewView struct {
	SID       string              `json:"sid"`
	Kind      string              `json:"kind"`
	Files     int                 `json:"files"`
	Bytes     int64               `json:"bytes"`
	Groups    []SharePreviewGroup `json:"groups"`
	Items     []SharePreviewItem  `json:"items"`
	CDNFiles  int                 `json:"cdnFiles"`
	CDNBytes  int64               `json:"cdnBytes"`
	Truncated bool                `json:"truncated,omitempty"`
	Meta      map[string]string   `json:"meta,omitempty"`
}

const sharePreviewMaxItems = 1500

var (
	sharePreviewMu    sync.Mutex
	sharePreviewCache = map[string]SharePreviewView{}
)

// sharePreviewGroup — категорія файлу для підсумку.
func sharePreviewGroup(kind, rel string) string {
	top := shareTopDir(rel)
	switch {
	case shareContentDirs[top], top == "config":
		return top
	case top == "saves", kind == share.KindServer && strings.HasPrefix(top, "world"):
		return "worlds"
	}
	return "other"
}

// buildSharePreview — список вмісту з маніфесту (без мережі).
func buildSharePreview(sid string, m *share.Manifest) SharePreviewView {
	v := SharePreviewView{SID: sid, Kind: m.Kind, Files: len(m.Files), Bytes: m.TotalBytes, Meta: shareCardMeta(m.Meta)}
	groups := map[string]*SharePreviewGroup{}
	var order []string
	for _, f := range m.Files {
		g := sharePreviewGroup(m.Kind, f.Path)
		if groups[g] == nil {
			groups[g] = &SharePreviewGroup{Key: g}
			order = append(order, g)
		}
		groups[g].Files++
		groups[g].Bytes += f.Size
		if f.Src != nil {
			v.CDNFiles++
			v.CDNBytes += f.Size
		}
		top := shareTopDir(f.Path)
		if !shareContentDirs[top] || !shareContentFile(f.Path) || strings.Count(f.Path, "/") > 1 {
			continue
		}
		if len(v.Items) >= sharePreviewMaxItems {
			v.Truncated = true
			continue
		}
		name := f.Path[strings.LastIndexByte(f.Path, '/')+1:]
		it := SharePreviewItem{Path: f.Path, Name: name, Size: f.Size, Category: top,
			Disabled: strings.HasSuffix(strings.ToLower(name), ".disabled")}
		if f.Src != nil {
			it.ProjectID = f.Src.Modrinth
		}
		v.Items = append(v.Items, it)
	}
	for _, k := range order {
		v.Groups = append(v.Groups, *groups[k])
	}
	return v
}

// enrichSharePreview — назви й іконки з Modrinth (напряму, не через VPS). Збій — не біда:
// лишаються імена файлів.
func (a *App) enrichSharePreview(v *SharePreviewView) {
	if a.modrinth == nil {
		return
	}
	seen := map[string]bool{}
	var ids []string
	for _, it := range v.Items {
		if it.ProjectID != "" && !seen[it.ProjectID] {
			seen[it.ProjectID] = true
			ids = append(ids, it.ProjectID)
		}
	}
	if len(ids) == 0 {
		return
	}
	ch := make(chan map[string]*api.ModrinthProjectInfo, 1)
	go func() {
		defer crashguard.Recover("sharePreviewProjects")
		info, err := a.modrinth.GetProjectsBatch(ids)
		if err != nil {
			info = nil
		}
		ch <- info
	}()
	var info map[string]*api.ModrinthProjectInfo
	select {
	case info = <-ch:
	case <-time.After(15 * time.Second):
	}
	for i := range v.Items {
		if p := info[v.Items[i].ProjectID]; p != nil {
			v.Items[i].Title, v.Items[i].IconURL, v.Items[i].Slug = cleanNotifyText(p.Title), p.IconURL, p.Slug
		}
	}
}

// SharePreview — що всередині передачі (моди з іконками, ресурспаки, конфіги, світи) ДО
// прийняття. Маніфест береться у відправника через той самий P2P (без VPS), відправник
// при цьому не вважає вас підключеним. Старі відправники — помилка «оновіть лаунчер».
func (a *App) SharePreview(encoded, fromAccountID string) (SharePreviewView, error) {
	card, err := parseShareCard(encoded)
	if err != nil {
		return SharePreviewView{}, err
	}
	sharePreviewMu.Lock()
	if v, ok := sharePreviewCache[card.SID]; ok {
		sharePreviewMu.Unlock()
		return v, nil
	}
	sharePreviewMu.Unlock()
	if a.p2pDisabled() {
		return SharePreviewView{}, errP2PDisabled()
	}
	if tag := a.shareDeviceTag(); tag != "" && card.Dev == tag {
		return SharePreviewView{}, errShareSelf()
	}
	if _, ok := shareFriendSet(a.socialService())[fromAccountID]; !ok {
		return SharePreviewView{}, errShareNotFriend
	}
	// Повністю чи частково прийняте вже лежить у stage — тоді й мережа не потрібна.
	var m *share.Manifest
	if sm, err := share.LoadStageManifest(a.shareStageDir(card.MID)); err == nil && sm.ID() == card.MID {
		m = sm
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), shareConnectTimeout)
		defer cancel()
		dial, err := a.shareDialer(ctx, card)
		if err != nil {
			return SharePreviewView{}, err
		}
		dl := share.NewDownloader(share.Options{Dial: dial, Key: card.Key, StageDir: a.shareStageDir(card.MID),
			ExpectManifestID: card.MID, ExpectBytes: card.Bytes, ExpectFiles: card.Files})
		m, err = dl.Preview(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("відправник не відповідає — можливо, він закрив доступ")
			}
			return SharePreviewView{}, err
		}
	}
	v := buildSharePreview(card.SID, m)
	a.enrichSharePreview(&v)
	sharePreviewMu.Lock()
	if len(sharePreviewCache) > 50 {
		sharePreviewCache = map[string]SharePreviewView{}
	}
	sharePreviewCache[card.SID] = v
	sharePreviewMu.Unlock()
	return v, nil
}
