package shaurmpack

// pipeline.go — спільний конвеєр експорту для .shaurmpack (Export) і
// .shaurmbundle (ExportBundle).
//
// Етапи:
//  1. Збір кандидатів (Walk + фільтри) — послідовно, дешево.
//  2. Пул воркерів (workerCount): SHA-1 (з опційним кешем) + Murmur2 для
//     файлів, які взагалі можуть мати онлайн-джерело.
//  3. Резолв по унікальних хешах: Modrinth і CurseForge BATCH-запитами
//     (якщо задані batch-резолвери), інакше — per-file резолвери в тому ж пулі.
//  4. Класифікація кожного файлу:
//     - є робоче джерело            → у ZIP НЕ пишеться (лише маніфест);
//     - можливе джерело, але нема   → bundled/;
//     - принципово без джерела      → overrides/ (config, options, datapacks…).
//  5. Запис у zip.Writer — послідовно (робить викликач), лише bundled/overrides.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"shaurma-launcher-backend/internal/export"
)

// workerCount — розмір пулу для хешування й per-file резолву.
const workerCount = 10

// CFLookup — запит до CurseForge-резолвера для одного унікального файлу.
type CFLookup struct {
	SHA1        string // SHA-1 вмісту (ключ у відповіді резолвера)
	Fingerprint string // Murmur2 fingerprint (десяткове число)
	RelPath     string // відносний шлях у збірці (Unix '/')
	FileName    string
	Size        int64
	// NeedDetails=true — файлу немає на Modrinth, тож потрібні повні дані
	// (DownloadURL, Blocked) через GetModFile. false — файл уже має робоче
	// Modrinth-джерело: достатньо projectId/fileId з fingerprint-відповіді
	// (GetModFile пропускаємо — економимо запити під rate-limiter CF).
	NeedDetails bool
}

// ModrinthBatchResolver резолвить багато SHA-1 за раз. Ключ відповіді — SHA-1.
type ModrinthBatchResolver func(ctx context.Context, sha1s []string) map[string]*ModrinthSource

// CurseForgeBatchResolver резолвить багато файлів за раз. Ключ відповіді — SHA-1.
type CurseForgeBatchResolver func(ctx context.Context, files []CFLookup) map[string]*CurseForgeSource

// resolverSet — набір резолверів, спільний для Export і ExportBundle.
type resolverSet struct {
	// per-file (старий контракт; fallback, коли batch не задано)
	modrinth   func(sha1hex, fileName string, size int64) *ModrinthSource
	curseforge func(sha1hex, fileName string, size int64) *CurseForgeSource
	// batch (пріоритетні)
	modrinthBatch   ModrinthBatchResolver
	curseforgeBatch CurseForgeBatchResolver
	// sha1Of — опційний кешований SHA-1 (nil → читаємо файл)
	sha1Of func(absPath string) (string, error)
}

func (r resolverSet) hasModrinth() bool   { return r.modrinth != nil || r.modrinthBatch != nil }
func (r resolverSet) hasCurseForge() bool { return r.curseforge != nil || r.curseforgeBatch != nil }

type destKind int

const (
	destOverride destKind = iota // файл без можливого джерела → overrides/
	destBundled                  // джерело можливе, але не знайдено/заблоковано → bundled/
	destSource                   // є робоче джерело → тільки маніфест
)

// candidate — файл-кандидат на включення в пак.
type candidate struct {
	abs  string
	rel  string
	size int64
}

// plannedFile — результат конвеєра для одного файлу.
type plannedFile struct {
	candidate
	hash string
	pf   PackFile
	dest destKind
	err  error // помилка хешування
}

// collectCandidates обходить gameDir і повертає файли, що пройшли фільтри.
func collectCandidates(gameDir string, filter *export.PathFilter) []candidate {
	var out []candidate
	_ = filepath.Walk(gameDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, gameDir))
		rel = strings.TrimPrefix(rel, "/")
		if shouldSkipFile(rel) {
			return nil
		}
		if filter != nil && !filter.Includes(rel) {
			return nil
		}
		out = append(out, candidate{abs: path, rel: rel, size: info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out
}

// workingModrinth — чи Modrinth-джерело справді можна завантажити.
func workingModrinth(s *ModrinthSource) bool { return s != nil && s.DownloadURL != "" }

// workingCurseForge — чи CurseForge-джерело справді можна завантажити.
func workingCurseForge(s *CurseForgeSource) bool {
	return s != nil && !s.Blocked && s.DownloadURL != ""
}

// HasWorkingSource — чи є в PackFile хоч одне робоче онлайн-джерело.
func (pf PackFile) HasWorkingSource() bool {
	return workingModrinth(pf.Modrinth) || workingCurseForge(pf.CurseForge)
}

// planFiles — етапи 2-4. progress (може бути nil) викликається під м'ютексом
// з воркерів на етапі хешування (done < total).
func planFiles(ctx context.Context, cands []candidate, rs resolverSet, progress func(done int, name string)) []plannedFile {
	if ctx == nil {
		ctx = context.Background()
	}
	out := make([]plannedFile, len(cands))
	fingerprints := make([]string, len(cands))
	wantFP := rs.hasCurseForge()

	// ── Етап 2: хешування пулом ──
	var progMu sync.Mutex
	done := 0
	runPool(ctx, len(cands), func(i int) {
		c := cands[i]
		ft := detectFileType(c.rel)
		pl := plannedFile{candidate: c}
		pl.pf = PackFile{Path: c.rel, Size: c.size, FileType: ft}
		var hash string
		var err error
		if rs.sha1Of != nil {
			hash, err = rs.sha1Of(c.abs)
		}
		if err == nil && hash == "" { // нема хука або кеш не застосовний
			hash, err = fileSHA1(c.abs)
		}
		if err != nil {
			pl.err = fmt.Errorf("хешування %s: %w", c.rel, err)
		} else {
			pl.hash = hash
			pl.pf.Hash = hash
			if wantFP && resolvableFile(ft, c.rel) {
				if fp, ferr := export.Murmur2Fingerprint(c.abs); ferr == nil {
					fingerprints[i] = fp
				}
			}
		}
		out[i] = pl
		if progress != nil {
			progMu.Lock()
			done++
			progress(done, c.rel)
			progMu.Unlock()
		}
	})

	// ── Етап 3: резолв унікальних хешів ──
	type rep struct {
		idx int
	}
	uniq := map[string]rep{}
	var order []string // стабільний порядок унікальних хешів
	for i := range out {
		if out[i].err != nil || !resolvableFile(out[i].pf.FileType, out[i].rel) {
			continue
		}
		if _, seen := uniq[out[i].hash]; !seen {
			uniq[out[i].hash] = rep{idx: i}
			order = append(order, out[i].hash)
		}
	}

	mr := map[string]*ModrinthSource{}
	cf := map[string]*CurseForgeSource{}

	if len(order) > 0 && ctx.Err() == nil {
		// Modrinth
		switch {
		case rs.modrinthBatch != nil:
			for h, s := range rs.modrinthBatch(ctx, order) {
				if workingModrinth(s) {
					mr[h] = s
				}
			}
		case rs.modrinth != nil:
			var mu sync.Mutex
			runPool(ctx, len(order), func(k int) {
				h := order[k]
				c := out[uniq[h].idx]
				if s := rs.modrinth(h, filepath.Base(c.abs), c.size); workingModrinth(s) {
					mu.Lock()
					mr[h] = s
					mu.Unlock()
				}
			})
		}

		// CurseForge
		switch {
		case rs.curseforgeBatch != nil:
			lookups := make([]CFLookup, 0, len(order))
			for _, h := range order {
				i := uniq[h].idx
				if fingerprints[i] == "" {
					continue
				}
				lookups = append(lookups, CFLookup{
					SHA1: h, Fingerprint: fingerprints[i], RelPath: out[i].rel,
					FileName: filepath.Base(out[i].abs), Size: out[i].size,
					NeedDetails: mr[h] == nil,
				})
			}
			if len(lookups) > 0 {
				for h, s := range rs.curseforgeBatch(ctx, lookups) {
					if s != nil {
						cf[h] = s
					}
				}
			}
		case rs.curseforge != nil:
			var mu sync.Mutex
			runPool(ctx, len(order), func(k int) {
				h := order[k]
				c := out[uniq[h].idx]
				if s := rs.curseforge(h, filepath.Base(c.abs), c.size); s != nil {
					mu.Lock()
					cf[h] = s
					mu.Unlock()
				}
			})
		}
	}

	// ── Етап 4: класифікація ──
	for i := range out {
		pl := &out[i]
		if pl.err != nil {
			continue
		}
		if !resolvableFile(pl.pf.FileType, pl.rel) {
			pl.dest = destOverride
			continue
		}
		m, c := mr[pl.hash], cf[pl.hash]
		okM, okC := workingModrinth(m), workingCurseForge(c)
		switch {
		case okM || okC:
			pl.dest = destSource
			if okM {
				pl.pf.Modrinth = m
			}
			// CF-запис пишемо, лише якщо він не заблокований (Blocked-записи
			// у маніфест не потрапляють). Поряд з робочим Modrinth лишаємо
			// і «легкий» CF-запис (projectId/fileId без URL) — для атрибуції.
			if c != nil && !c.Blocked && (okC || okM) {
				pl.pf.CurseForge = c
			}
		default:
			pl.dest = destBundled
			pl.pf.Bundled = true
		}
	}
	return out
}

// runPool виконує fn(0..n-1) на workerCount горутинах; зупиняється при
// скасуванні ctx (необроблені індекси просто пропускаються).
func runPool(ctx context.Context, n int, fn func(i int)) {
	if n == 0 {
		return
	}
	workers := workerCount
	if n < workers {
		workers = n
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}
