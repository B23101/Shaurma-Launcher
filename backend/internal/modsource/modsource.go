// Package modsource — «єдине джерело» модів: замість того, щоб після
// кожної компіляції вручну копіювати jar у mods/ кількох збірок, розробник
// прикріплює до збірки/сервера ДЖЕРЕЛО, а лаунчер сам синхронізує його з
// mods/ перед запуском гри, запуском сервера і пакуванням у модпак.
//
// Два види джерел:
//
//   - KindFile   — один jar-файл з довільного місця на диску. У mods/ лежить
//     його копія з тим самим ім'ям; якщо хеш відрізняється — копія
//     перезаписується, якщо копію видалили — відновлюється.
//   - KindFolder — уся тека mods (напр. з іншої збірки). Вона стає єдиним
//     джерелом усіх модів цієї збірки: mods/ дзеркалить теку, а користувач
//     нічого в ній не змінює. Вид виключний — з файловими джерелами не
//     поєднується.
//
// Конфіг живе поруч зі ЦІЛЛЮ (збіркою чи сервером), у <root>/.shaurma/, а
// не в mods/ — тека mods належить гравцеві (див. mods.Manager.cacheDir).
package modsource

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	KindFile   = "file"
	KindFolder = "folder"

	// NameMods / NamePlugins — яку теку контенту обслуговує конфіг. Сервер
	// може мати і mods/ (Forge/Fabric), і plugins/ (Paper) — файли конфігу
	// окремі, щоб джерела не змішувались.
	NameMods    = "mods"
	NamePlugins = "plugins"

	disabledSuffix = ".disabled"
)

// Source — одне прикріплене джерело.
type Source struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // file | folder
	// Path — абсолютний шлях: до .jar (file) або до теки (folder).
	Path string `json:"path"`
	// FileName — ім'я копії у mods/ (лише file). За замовчуванням — ім'я
	// файлу-джерела.
	FileName string `json:"fileName,omitempty"`
	AddedAt  int64  `json:"addedAt"`
}

// Config — усі джерела однієї цілі (збірки/сервера) для однієї теки.
type Config struct {
	Sources []Source `json:"sources"`
}

// state — службовий стан дзеркала теки: які файли у mods/ поставило саме
// джерело (лише їх можна безпечно прибирати, коли вони зникли з джерела).
type state struct {
	Managed []string `json:"managed"`
}

// Store — доступ до конфігу однієї цілі.
type Store struct {
	Root string // тека цілі: <instance>/ або <serverDir>/
	Name string // NameMods | NamePlugins
}

func (s Store) name() string {
	if s.Name == "" {
		return NameMods
	}
	return s.Name
}

func (s Store) dir() string          { return filepath.Join(s.Root, ".shaurma") }
func (s Store) configPath() string   { return filepath.Join(s.dir(), s.name()+"-sources.json") }
func (s Store) statePath() string    { return filepath.Join(s.dir(), s.name()+"-sources-state.json") }
func (s Store) preSourceDir() string { return filepath.Join(s.dir(), s.name()+"-before-source") }

// Load читає конфіг; відсутній файл = порожній конфіг.
func (s Store) Load() (Config, error) {
	var cfg Config
	data, err := os.ReadFile(s.configPath())
	if errors.Is(err, os.ErrNotExist) {
		return Config{Sources: []Source{}}, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{Sources: []Source{}}, fmt.Errorf("mod-sources: пошкоджений конфіг: %w", err)
	}
	if cfg.Sources == nil {
		cfg.Sources = []Source{}
	}
	return cfg, nil
}

func (s Store) save(cfg Config) error {
	if cfg.Sources == nil {
		cfg.Sources = []Source{}
	}
	if len(cfg.Sources) == 0 {
		_ = os.Remove(s.configPath())
		return nil
	}
	return writeJSONAtomic(s.configPath(), cfg)
}

func (s Store) loadState() (state, bool) {
	var st state
	data, err := os.ReadFile(s.statePath())
	if err != nil {
		return st, false
	}
	if json.Unmarshal(data, &st) != nil {
		return state{}, false
	}
	return st, true
}

func (s Store) saveState(st state) error {
	sort.Strings(st.Managed)
	return writeJSONAtomic(s.statePath(), st)
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// FolderSource повертає джерело-теку, якщо воно є.
func (c Config) FolderSource() *Source {
	for i := range c.Sources {
		if c.Sources[i].Kind == KindFolder {
			return &c.Sources[i]
		}
	}
	return nil
}

// FileSource шукає файлове джерело за іменем копії у mods/ (з чи без
// суфікса .disabled, без урахування регістру — Windows).
func (c Config) FileSource(fileName string) *Source {
	want := strings.ToLower(strings.TrimSuffix(fileName, disabledSuffix))
	for i := range c.Sources {
		if c.Sources[i].Kind == KindFile && strings.ToLower(c.Sources[i].FileName) == want {
			return &c.Sources[i]
		}
	}
	return nil
}

// Locks — чи заборонено користувачу змінювати цей файл у mods/: усе
// заблоковано, якщо є джерело-тека; інакше — лише файли з файлових джерел.
func (c Config) Locks(fileName string) (*Source, bool) {
	if f := c.FolderSource(); f != nil {
		return f, true
	}
	if s := c.FileSource(fileName); s != nil {
		return s, true
	}
	return nil, false
}

func newID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func sameDir(a, b string) bool {
	ca, cb := canon(a), canon(b)
	return ca != "" && ca == cb
}

func canon(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	abs = filepath.Clean(abs)
	if filepath.Separator == '\\' {
		abs = strings.ToLower(abs)
	}
	return abs
}

func isInside(child, parent string) bool {
	c, p := canon(child), canon(parent)
	if c == "" || p == "" {
		return false
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// Add прикріплює джерело. modsDir — цільова тека контенту цієї цілі (щоб
// відхилити джерело, яке лежить у ній самій).
func (s Store) Add(kind, path, modsDir string) (Source, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Source{}, errors.New("не вказано шлях до джерела")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Source{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Source{}, fmt.Errorf("джерело недоступне: %w", err)
	}
	cfg, err := s.Load()
	if err != nil {
		return Source{}, err
	}
	src := Source{ID: newID(), Kind: kind, Path: abs, AddedAt: time.Now().Unix()}

	switch kind {
	case KindFile:
		if info.IsDir() {
			return Source{}, errors.New("оберіть файл .jar, а не теку")
		}
		base := filepath.Base(abs)
		base = strings.TrimSuffix(base, disabledSuffix)
		if !strings.HasSuffix(strings.ToLower(base), ".jar") {
			return Source{}, errors.New("джерелом мода може бути лише файл .jar")
		}
		if cfg.FolderSource() != nil {
			return Source{}, errors.New("уся тека модів уже є джерелом — окремі моди додавати не можна")
		}
		if isInside(abs, modsDir) {
			return Source{}, errors.New("джерело не може лежати в теці модів цієї ж збірки")
		}
		src.FileName = base
		if cfg.FileSource(base) != nil {
			return Source{}, fmt.Errorf("мод %s уже прикріплений до джерела", base)
		}
	case KindFolder:
		if !info.IsDir() {
			return Source{}, errors.New("оберіть теку, а не файл")
		}
		if len(cfg.Sources) > 0 {
			return Source{}, errors.New("спершу відкріпіть наявні джерела: тека модів може бути лише єдиним джерелом")
		}
		if sameDir(abs, modsDir) || isInside(modsDir, abs) || isInside(abs, modsDir) {
			return Source{}, errors.New("тека-джерело не може збігатися з текою модів цієї ж збірки або вкладатися в неї")
		}
	default:
		return Source{}, fmt.Errorf("невідомий вид джерела: %q", kind)
	}

	cfg.Sources = append(cfg.Sources, src)
	if err := s.save(cfg); err != nil {
		return Source{}, err
	}
	return src, nil
}

// Remove відкріплює джерело. Уже скопійовані файли ЛИШАЮТЬСЯ в mods/ як
// звичайні моди (їх знову можна вимикати/видаляти) — відкріплення нічого
// не видаляє.
func (s Store) Remove(id string) error {
	cfg, err := s.Load()
	if err != nil {
		return err
	}
	out := cfg.Sources[:0:0]
	found := false
	for _, x := range cfg.Sources {
		if x.ID == id {
			found = true
			if x.Kind == KindFolder {
				_ = os.Remove(s.statePath())
			}
			forget(x)
			continue
		}
		out = append(out, x)
	}
	if !found {
		return errors.New("джерело не знайдено")
	}
	cfg.Sources = out
	return s.save(cfg)
}

// ── Синхронізація ─────────────────────────────────────────────────────

// Progress — стан для візуального прогресу.
type Progress struct {
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	File    string `json:"file"`
	Action  string `json:"action"` // check | copy | remove
	Percent int    `json:"percent"`
}

// Missing — джерело, якого немає на диску.
type Missing struct {
	SourceID string `json:"sourceId"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	FileName string `json:"fileName,omitempty"`
}

// Result — підсумок синхронізації.
type Result struct {
	Checked  int       `json:"checked"`
	Copied   int       `json:"copied"`   // нові + оновлені
	Restored int       `json:"restored"` // відсутні у mods/, повернуті з джерела
	Removed  int       `json:"removed"`  // зникли з теки-джерела
	Moved    int       `json:"moved"`    // сторонні моди, прибрані перед першим дзеркалюванням
	Missing  []Missing `json:"missing"`
	Errors   []string  `json:"errors"`
}

// Changed — чи змінився вміст mods/.
func (r Result) Changed() bool { return r.Copied+r.Restored+r.Removed+r.Moved > 0 }

var (
	reportedMu sync.Mutex
	reported   = map[string]bool{}
)

func reportKey(m Missing) string { return m.SourceID + "|" + m.Path }

// firstReport — true лише при першому повідомленні про це зниклий джерело
// за час роботи лаунчера: «повідомить і більше не перевіряє до наступного
// запуску».
func firstReport(m Missing) bool {
	reportedMu.Lock()
	defer reportedMu.Unlock()
	k := reportKey(m)
	if reported[k] {
		return false
	}
	reported[k] = true
	return true
}

func isReported(m Missing) bool {
	reportedMu.Lock()
	defer reportedMu.Unlock()
	return reported[reportKey(m)]
}

func forget(x Source) {
	reportedMu.Lock()
	defer reportedMu.Unlock()
	delete(reported, x.ID+"|"+x.Path)
}

// ResetReported скидає «вже повідомлено» (для тестів і ручного оновлення).
func ResetReported() {
	reportedMu.Lock()
	defer reportedMu.Unlock()
	reported = map[string]bool{}
}

type item struct {
	name string // ім'я у mods/
	src  string
}

// Sync приводить modsDir у відповідність до джерел. Ніколи не видаляє
// файли, які не належать джерелу. Помилки окремих файлів збираються в
// Result.Errors, а не обривають синхронізацію.
func (s Store) Sync(modsDir string, progress func(Progress)) (Result, error) {
	res := Result{Missing: []Missing{}, Errors: []string{}}
	cfg, err := s.Load()
	if err != nil {
		return res, err
	}
	if len(cfg.Sources) == 0 {
		return res, nil
	}
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return res, err
	}

	var items []item
	var removals []string
	var folder *Source

	for i := range cfg.Sources {
		src := cfg.Sources[i]
		switch src.Kind {
		case KindFile:
			m := Missing{SourceID: src.ID, Kind: src.Kind, Path: src.Path, FileName: src.FileName}
			if _, err := os.Stat(src.Path); err != nil {
				// Відсутнє джерело: повідомляємо один раз за запуск, далі
				// мовчки пропускаємо. Копію в mods/ не чіпаємо.
				if firstReport(m) {
					res.Missing = append(res.Missing, m)
				}
				continue
			}
			forget(src)
			items = append(items, item{name: src.FileName, src: src.Path})
		case KindFolder:
			m := Missing{SourceID: src.ID, Kind: src.Kind, Path: src.Path}
			entries, err := os.ReadDir(src.Path)
			if err != nil {
				if firstReport(m) {
					res.Missing = append(res.Missing, m)
				}
				continue
			}
			forget(src)
			folder = &cfg.Sources[i]
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				n := e.Name()
				ln := strings.ToLower(n)
				if strings.HasSuffix(ln, ".jar") || strings.HasSuffix(ln, ".jar.disabled") {
					items = append(items, item{name: n, src: filepath.Join(src.Path, n)})
				}
			}
		}
	}

	// Дзеркало теки: що прибрати з mods/.
	if folder != nil {
		want := map[string]bool{}
		for _, it := range items {
			want[strings.ToLower(it.name)] = true
		}
		st, had := s.loadState()
		if !had {
			// Перше дзеркалювання: моди, які були в збірці до підключення
			// джерела, не можна ні мовчки лишити (вони б змішались із
			// джерелом), ні знищити — відкладаємо в теку «до джерела».
			removals = append(removals, s.moveForeign(modsDir, want, &res)...)
		} else {
			for _, n := range st.Managed {
				if !want[strings.ToLower(n)] {
					removals = append(removals, n)
				}
			}
		}
		// Якщо джерело в mods/ було відключене (.jar.disabled) — розв'язок
		// однозначний: береться стан із джерела.
		defer func(names []string) {
			_ = s.saveState(state{Managed: names})
		}(namesOf(items))
	}

	total := len(items) + len(removals)
	done := 0
	report := func(file, action string) {
		if progress == nil {
			return
		}
		pct := 100
		if total > 0 {
			pct = done * 100 / total
		}
		progress(Progress{Done: done, Total: total, File: file, Action: action, Percent: pct})
	}
	report("", "check")

	for _, it := range items {
		res.Checked++
		dst := filepath.Join(modsDir, it.name)
		action, err := syncOne(it.src, dst, modsDir, it.name, report)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", it.name, err))
		} else {
			switch action {
			case actUpdated:
				res.Copied++
			case actRestored:
				res.Restored++
			}
		}
		done++
		report(it.name, "check")
	}

	for _, n := range removals {
		report(n, "remove")
		if err := os.Remove(filepath.Join(modsDir, n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", n, err))
		} else {
			res.Removed++
		}
		done++
		report(n, "remove")
	}
	return res, nil
}

func namesOf(items []item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.name)
	}
	return out
}

// moveForeign відкладає у <root>/.shaurma/<name>-before-source/ jar-файли
// з modsDir, яких немає в джерелі. Повертає порожній список (усе вже
// зроблено) — сигнатура лишає місце для розширення.
func (s Store) moveForeign(modsDir string, want map[string]bool, res *Result) []string {
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return nil
	}
	dest := filepath.Join(s.preSourceDir(), time.Now().Format("20060102-150405"))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ln := strings.ToLower(e.Name())
		if !(strings.HasSuffix(ln, ".jar") || strings.HasSuffix(ln, ".jar.disabled")) {
			continue
		}
		if want[ln] {
			continue
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			res.Errors = append(res.Errors, err.Error())
			return nil
		}
		if err := moveFile(filepath.Join(modsDir, e.Name()), filepath.Join(dest, e.Name())); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		res.Moved++
	}
	return nil
}

const (
	actNone     = 0
	actUpdated  = 1
	actRestored = 2
)

func syncOne(src, dst, modsDir, name string, report func(string, string)) (int, error) {
	// Закріплений мод не може бути вимкненим: якщо є лише .jar.disabled —
	// це порушення, повертаємо стан із джерела.
	disabled := dst + disabledSuffix
	if !strings.HasSuffix(strings.ToLower(name), disabledSuffix) {
		if _, err := os.Stat(disabled); err == nil {
			_ = os.Remove(disabled)
		}
	}

	dInfo, dErr := os.Stat(dst)
	sInfo, sErr := os.Stat(src)
	if sErr != nil {
		return actNone, sErr
	}
	missing := dErr != nil
	if !missing {
		if dInfo.Size() == sInfo.Size() {
			same, err := sameContent(src, dst)
			if err != nil {
				return actNone, err
			}
			if same {
				return actNone, nil
			}
		}
	}
	report(name, "copy")
	if err := copyAtomic(src, dst); err != nil {
		return actNone, err
	}
	if missing {
		return actRestored, nil
	}
	return actUpdated, nil
}

type hashKey struct {
	path  string
	size  int64
	mtime int64
}

var (
	hashMu    sync.Mutex
	hashCache = map[hashKey]string{}
)

// Sha256 — хеш файлу з кешем за (шлях, розмір, mtime): незмінені файли не
// перечитуються при кожному запуску.
func Sha256(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	k := hashKey{path, info.Size(), info.ModTime().UnixNano()}
	hashMu.Lock()
	if h, ok := hashCache[k]; ok {
		hashMu.Unlock()
		return h, nil
	}
	hashMu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	hashMu.Lock()
	if len(hashCache) > 4096 {
		hashCache = map[hashKey]string{}
	}
	hashCache[k] = sum
	hashMu.Unlock()
	return sum, nil
}

func sameContent(a, b string) (bool, error) {
	ha, err := Sha256(a)
	if err != nil {
		return false, err
	}
	hb, err := Sha256(b)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

func copyAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".shaurma-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// На Windows Rename поверх існуючого файлу працює (Go використовує
	// MoveFileEx з REPLACE_EXISTING); зайнятий грою файл дасть помилку,
	// яка піде в Result.Errors.
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyAtomic(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// ── Подання для інтерфейсу та захист від ручних змін ───────────────────

// SourceView — джерело + чи існує воно зараз на диску.
type SourceView struct {
	Source
	Missing bool `json:"missing"`
}

// View — усе, що потрібно UI менеджера модів.
type View struct {
	Sources []SourceView `json:"sources"`
	// Locked — прикріплено ТЕКУ: жодних ручних змін у mods/ цієї цілі.
	Locked     bool   `json:"locked"`
	FolderPath string `json:"folderPath,omitempty"`
}

// View читає конфіг і перевіряє наявність джерел.
func (s Store) View() (View, error) {
	cfg, err := s.Load()
	v := View{Sources: []SourceView{}}
	if err != nil {
		return v, err
	}
	for _, src := range cfg.Sources {
		_, statErr := os.Stat(src.Path)
		v.Sources = append(v.Sources, SourceView{Source: src, Missing: statErr != nil})
	}
	if f := cfg.FolderSource(); f != nil {
		v.Locked = true
		v.FolderPath = f.Path
	}
	return v, nil
}

// LockedErr повертає помилку, якщо ручна зміна fileName заборонена
// джерелом. fileName == "" — перевірка для дій без конкретного файлу
// (встановлення нового мода, імпорт): блокує лише тека-джерело.
func (s Store) LockedErr(fileName string) error {
	cfg, err := s.Load()
	if err != nil {
		return nil // пошкоджений конфіг не повинен блокувати роботу
	}
	if f := cfg.FolderSource(); f != nil {
		return fmt.Errorf("усі моди беруться з теки-джерела %s — змінювати їх тут не можна; відкріпіть джерело, щоб редагувати", f.Path)
	}
	if fileName == "" {
		return nil
	}
	if src := cfg.FileSource(fileName); src != nil {
		return fmt.Errorf("мод %s походить з джерела %s — його не можна вимкнути, видалити чи оновити; відкріпіть джерело", src.FileName, src.Path)
	}
	return nil
}

// Annotation — позначка для рядка списку модів.
type Annotation struct {
	Kind string
	Path string
	// Missing — джерела зараз немає на диску.
	Missing bool
}

// Annotate повертає позначку для файлу mods/ (nil — файл звичайний).
func (s Store) Annotate(cfg Config, fileName string) *Annotation {
	src, ok := cfg.Locks(fileName)
	if !ok {
		return nil
	}
	_, err := os.Stat(src.Path)
	return &Annotation{Kind: src.Kind, Path: src.Path, Missing: err != nil}
}

// SamePath — чи два шляхи вказують на одну теку/файл (з урахуванням
// символічних посилань і регістру на Windows).
func SamePath(a, b string) bool { return sameDir(a, b) }
