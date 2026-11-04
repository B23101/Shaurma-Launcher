package newupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shrm-updater-backend/internal/install"
	"shrm-updater-backend/internal/manifest"

	"shaurma-trust"
)

// Options — що оновлювач знає про себе.
type Options struct {
	InstallDir     string
	Channel        string // stable | beta
	Edition        string // shaurma | base (міст R1); у єдиному лаунчері — shaurma
	ShellName      string // ім'я виконуваного файлу shell у теці (роль shell ставиться під ним)
	CurrentVersion string
}

// Plan — перевірене оновлення, готове до завантаження.
type Plan struct {
	Manifest      *trust.Manifest
	Remote        *manifest.Manifest // у форматі, який розуміють Diff/install.Apply
	BaseURL       string             // https://github.com/<repo>/releases/download/<tag>
	AllowRedirect func(string) bool
	Required      bool
	Authenticode  bool
	Signers       []trust.AuthenticodeSigner
}

// Причини, з якими лаунчер працює далі, але НЕ оновлюється.
var (
	ErrCertGate = errors.New("сертифікат ще не збережено на диску — оновлення відкладено")
	ErrClock    = errors.New("системний час на ПК неправильний — перевірте дату й час, оновлення відкладено")
	ErrDeferred = errors.New("оновлення відкладено сервером")
	ErrBusy     = errors.New("інше оновлення вже виконується")
)

// Engine — один запуск оновлювача.
type Engine struct {
	opt    Options
	state  *State
	ks     *trust.Keyset
	src    *source
	lock   *os.File
	Notice string // повідомлення для splash (відкат, проблеми з сертифікатом)
}

// Platform — платформа клієнта у форматі маніфесту.
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// Open готує двигун: завершує/відкочує попереднє оновлення, відкриває ворота
// сертифіката, обирає чинний набір ключів. Нічого не качає.
func Open(opt Options) (*Engine, error) {
	if !Enabled() {
		return nil, errors.New("нова система оновлень не вшита в цю збірку")
	}
	e := &Engine{opt: opt, state: loadState(opt.InstallDir)}
	e.finishPreviousUpdate()
	e.secureCert()
	ks, err := e.loadKeyset()
	if err != nil {
		// Без чинного keyset довіряти нічому не можна. Лаунчер працює далі.
		return nil, err
	}
	e.ks = ks
	e.src = newSource(ks)
	_ = saveState(opt.InstallDir, e.state)
	return e, nil
}

// ---- двофазне оновлення: commit або відкат (§8.3) --------------------------

// maxHealthAttempts — скільки стартів поспіль нова версія може не дійти до
// «живий», перш ніж відкотимося.
const maxHealthAttempts = 3

func (e *Engine) finishPreviousUpdate() {
	s := e.state
	if s.Phase != PhaseSwapped {
		return
	}
	dir := e.opt.InstallDir
	if h := loadHealth(dir); h != nil && trust.CompareVersions(h.Version, s.PendingVersion) == 0 && h.At.After(s.SwappedAt.Add(-time.Minute)) {
		for _, p := range s.PendingPaths {
			_ = os.Remove(filepath.Join(dir, filepath.FromSlash(p)) + install.BackupSuffix)
		}
		_ = os.Remove(filepath.Join(dir, "launcher-version.json.previous"))
		s.Phase = PhaseCommitted
		s.Attempts = 0
		return
	}
	s.Attempts++
	if s.Attempts < maxHealthAttempts {
		return
	}
	// Нова версія тричі не стартувала: повертаємо попередні файли.
	for _, p := range s.PendingPaths {
		_ = install.Rollback(dir, p)
	}
	prev := filepath.Join(dir, "launcher-version.json.previous")
	if data, err := os.ReadFile(prev); err == nil {
		_ = writeFileAtomic(filepath.Join(dir, "launcher-version.json"), data, 0o644)
	}
	e.Notice = fmt.Sprintf("Версія %s не запустилась — повернуто %s", s.PendingVersion, s.PreviousVersion)
	s.FailedVersion = s.PendingVersion
	s.Phase = PhaseNone
	s.Attempts = 0
}

// ---- ворота сертифіката (§14 етап B) ----------------------------------------

// secureCert розпаковує вшитий сертифікат (повна збірка мосту) у теку
// лаунчера і перевіряє, що на диску лежить чинний файл. Лише після цього
// дозволено качати наступну версію: вона вже не міститиме вшитого cert.
func (e *Engine) secureCert() {
	embedded := embeddedCert()
	if len(embedded) == 0 || e.opt.Edition != "shaurma" {
		return
	}
	roots := rootKeys()
	if _, err := trust.ParseCert(roots, embedded); err != nil {
		e.state.CertSecured = false
		e.Notice = "Вшитий сертифікат недійсний: " + err.Error()
		return
	}
	path := filepath.Join(e.opt.InstallDir, CertFileName)
	if cur, err := os.ReadFile(path); err == nil {
		if _, perr := trust.ParseCert(roots, cur); perr == nil {
			e.state.CertSecured = true
			return
		}
	}
	// Сертифіката немає або він битий. Якщо пристрій уже активовано (backend
	// видалив cert після збереженого ключа), повторно не розпаковуємо.
	if _, err := os.Stat(filepath.Join(e.opt.InstallDir, AccessFileName)); err == nil {
		e.state.MigrationState = "activated"
		e.state.CertSecured = true
		return
	}
	if e.state.MigrationState == "activated" {
		e.state.CertSecured = true
		return
	}
	if err := writeFileAtomic(path, embedded, 0o644); err != nil {
		e.state.CertSecured = false
		e.Notice = "Не вдалося зберегти сертифікат: " + err.Error()
		return
	}
	back, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(back, embedded) {
		e.state.CertSecured = false
		return
	}
	e.state.CertSecured = true
	if e.state.MigrationState == "" {
		e.state.MigrationState = "cert_secured"
	}
}

// certGateOpen — чи можна качати оновлення з огляду на сертифікат.
func (e *Engine) certGateOpen() bool {
	if len(embeddedCert()) == 0 || e.opt.Edition != "shaurma" {
		return true
	}
	return e.state.CertSecured
}

// ---- набір ключів -------------------------------------------------------------

func (e *Engine) loadKeyset() (*trust.Keyset, error) {
	roots := rootKeys()
	var best *trust.Keyset
	var bestRaw []byte
	var bestSig string
	try := func(raw []byte, sig string) {
		if len(raw) == 0 {
			return
		}
		ks, err := trust.ParseKeyset(roots, raw, sig)
		if err != nil {
			return
		}
		if best == nil || ks.Version > best.Version {
			best, bestRaw, bestSig = ks, raw, sig
		}
	}
	if raw, sig, err := embeddedKeyset(); err == nil {
		try(raw, sig)
	}
	try(e.state.Keyset, e.state.KeysetSig)
	if best == nil {
		return nil, errors.New("немає чинного набору ключів (keyset)")
	}
	if best.Version < e.state.KeysetVersion {
		// Кеш стерли, а вшитий старіший за бачений: не відкочуємо довіру.
		return nil, errors.New("набір ключів старіший за вже прийнятий — оновлення зупинено")
	}
	e.state.Keyset, e.state.KeysetSig, e.state.KeysetVersion = bestRaw, bestSig, best.Version
	return best, nil
}

// adoptKeyset приймає новіший keyset з релізу (ротація ключів без дій
// користувача, §7).
func (e *Engine) adoptKeyset(raw []byte, sig string) {
	ks, err := trust.ParseKeyset(rootKeys(), raw, sig)
	if err != nil || ks.Version <= e.ks.Version {
		return
	}
	e.ks = ks
	e.state.Keyset, e.state.KeysetSig, e.state.KeysetVersion = raw, sig, ks.Version
	e.src = newSource(ks)
	_ = saveState(e.opt.InstallDir, e.state)
}

// ---- перевірка оновлення ------------------------------------------------------

// Check шукає оновлення. (nil, nil) — оновлення немає. Помилки ErrCertGate,
// ErrClock, ErrDeferred — штатні причини не оновлюватись зараз.
func (e *Engine) Check(ctx context.Context) (*Plan, error) {
	if !e.certGateOpen() {
		return nil, ErrCertGate
	}
	signal, sigErr := e.src.Signal(ctx, e.opt.Channel, e.opt.CurrentVersion, Platform())
	tag := ""
	if sigErr == nil {
		if trust.CompareVersions(signal.Version, e.opt.CurrentVersion) <= 0 {
			return nil, nil
		}
		tag = signal.Tag
	} else {
		// Worker мовчить: беремо останній стабільний реліз GitHub напряму
		// (бета без сигналу теж отримує стабільний).
		signal = nil
	}

	// keyset з того самого релізу (може не бути — це нормально).
	if raw, sig, err := e.src.Asset(ctx, tag, trust.KeysetAsset); err == nil {
		e.adoptKeyset(raw, sig)
	}

	channel := e.opt.Channel
	assetName := trust.ManifestAssetFor(channel)
	if tag == "" {
		channel, assetName = "stable", trust.ManifestAsset
	}
	payload, sig, err := e.src.Asset(ctx, tag, assetName)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t := now()
	if !e.src.serverDate.IsZero() {
		if d := e.src.serverDate.Sub(t); d > 24*time.Hour || d < -24*time.Hour {
			return nil, ErrClock
		}
	}
	m, err := trust.ParseManifest(e.ks, payload, sig, t)
	if err != nil {
		e.src.Report("", err.Error())
		return nil, err
	}
	pol := trust.UpdatePolicy{Channel: channel, CurrentVersion: e.opt.CurrentVersion, HighestSeen: e.state.HighestSeen, Now: t}
	if signal != nil {
		pol.SignaledVersion = signal.Version
	}
	if err := trust.CheckUpdate(m, e.ks, pol); err != nil {
		if errors.Is(err, trust.ErrNotNewer) {
			return nil, nil
		}
		e.src.Report(m.Version, err.Error())
		return nil, err
	}
	if tag == "" {
		tag = m.Tag // файли беремо з релізу, названого підписаним маніфестом
	}
	if trust.CompareVersions(m.Version, e.state.FailedVersion) == 0 && !m.Required(e.opt.CurrentVersion) {
		return nil, nil
	}
	if signal != nil && !trust.RolloutAllows(m, signal.Rollout, e.opt.CurrentVersion, t) {
		return nil, ErrDeferred
	}
	remote, err := e.toLegacy(m)
	if err != nil {
		return nil, err
	}
	remote.Proof = &manifest.UpdateProof{Format: 2, Payload: payload, Signature: sig, Keyset: e.state.Keyset, KeysetSig: e.state.KeysetSig}
	return &Plan{
		Manifest:      m,
		Remote:        remote,
		BaseURL:       strings.TrimSuffix(e.src.AssetURL(tag, "x"), "/x"),
		AllowRedirect: e.src.allowHost,
		Required:      m.Required(e.opt.CurrentVersion),
		Authenticode:  m.AuthenticodeRequired(),
		Signers:       e.ks.AuthenticodeSigners,
	}, nil
}

// toLegacy перекладає файли платформи у компоненти, які розуміють наявні
// Diff і install.Apply. URL — лише ім'я ассета відносно релізу.
func (e *Engine) toLegacy(m *trust.Manifest) (*manifest.Manifest, error) {
	files := m.FilesFor(Platform())
	if len(files) == 0 {
		return nil, fmt.Errorf("у маніфесті %s немає файлів для %s", m.Version, Platform())
	}
	out := &manifest.Manifest{Version: m.Version, ReleaseNotes: m.Notes}
	for _, f := range files {
		path := f.Path
		if f.Role == trust.FileShell && e.opt.ShellName != "" {
			path = e.opt.ShellName
		}
		if f.Role == trust.FileBootstrap {
			continue // bootstrap ставить лише інсталятор
		}
		out.Components = append(out.Components, manifest.Component{
			Path: path, SHA256: strings.ToLower(f.SHA256), Size: f.Size,
			URL: "/" + f.Name, Kind: f.Kind, ExtractTo: f.ExtractTo,
		})
	}
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- блокування і запис стану навколо встановлення -----------------------------

// Lock — lock-файл проти двох одночасних оновлень (§5.4). Застарілий (понад
// 30 хв, процес упав) прибирається.
func (e *Engine) Lock() error {
	path := filepath.Join(e.opt.InstallDir, ".update.lock")
	for i := 0; i < 2; i++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			e.lock = f
			return nil
		}
		fi, serr := os.Stat(path)
		if serr != nil || time.Since(fi.ModTime()) < 30*time.Minute {
			return ErrBusy
		}
		_ = os.Remove(path)
	}
	return ErrBusy
}

// Close знімає lock і зберігає стан.
func (e *Engine) Close() {
	if e.lock != nil {
		name := e.lock.Name()
		e.lock.Close()
		_ = os.Remove(name)
		e.lock = nil
	}
	_ = saveState(e.opt.InstallDir, e.state)
}

// CheckDiskSpace — вільне місце до завантаження: двічі розмір оновлення
// (тимчасові файли + .previous) і запас.
func (e *Engine) CheckDiskSpace(toDownload []manifest.Component) error {
	var need int64 = 64 << 20
	for _, c := range toDownload {
		need += 2 * c.Size
	}
	free, err := freeBytes(e.opt.InstallDir)
	if err != nil {
		return nil // не вміємо порахувати — не блокуємо
	}
	if free < need {
		return fmt.Errorf("замало місця на диску: потрібно %d МБ, вільно %d МБ", need>>20, free>>20)
	}
	return nil
}

// BeforeApply — зберегти попередній локальний маніфест для відкату.
func (e *Engine) BeforeApply(p *Plan) error {
	dir := e.opt.InstallDir
	if data, err := os.ReadFile(filepath.Join(dir, "launcher-version.json")); err == nil {
		if err := writeFileAtomic(filepath.Join(dir, "launcher-version.json.previous"), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// AfterApply — фаза swapped: наступний старт або підтвердить версію (backend
// написав update-health.json), або відкотить її.
func (e *Engine) AfterApply(p *Plan, applied []manifest.Component) error {
	s := e.state
	s.PreviousVersion = e.opt.CurrentVersion
	s.PendingVersion = p.Manifest.Version
	s.PendingPaths = s.PendingPaths[:0]
	for _, c := range applied {
		if c.Kind == "archive" || manifest.IsLockedComponent(c.Path) {
			continue // архів розпаковано в теку, locked ставить shell — .previous немає
		}
		s.PendingPaths = append(s.PendingPaths, filepath.ToSlash(c.Path))
	}
	s.Phase = PhaseSwapped
	s.Attempts = 0
	s.SwappedAt = now()
	if trust.CompareVersions(p.Manifest.Version, s.HighestSeen) > 0 {
		s.HighestSeen = p.Manifest.Version
	}
	return saveState(e.opt.InstallDir, s)
}

// RecordInstalled — після оновлення старим шляхом (міст R1.x зі старого CDN)
// теж піднімаємо highest_seen, щоб захист від відкату діяв і там.
func (e *Engine) RecordInstalled(version string) {
	if trust.CompareVersions(version, e.state.HighestSeen) > 0 {
		e.state.HighestSeen = version
	}
}
