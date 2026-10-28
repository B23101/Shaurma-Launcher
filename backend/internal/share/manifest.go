package share

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// BuildOptions — параметри побудови маніфесту.
type BuildOptions struct {
	// Include — чи брати шлях (rel з '/'). nil = усе. Для теки викликається з isDir=true:
	// false пропускає її ціле.
	Include func(rel string, isDir bool) bool
	// Workers — паралельність хешування (за замовчуванням 4).
	Workers int
	// OnProgress — скільки файлів уже захешовано з усіх.
	OnProgress func(done, total int)
	// WantSHA1 — для яких файлів додатково порахувати sha1 (за той самий прохід читання):
	// за ним лаунчер шукає файл на Modrinth. Результат — у OnSHA1 (з різних горутин).
	WantSHA1 func(rel string) bool
	OnSHA1   func(rel, sha1hex string)
	// Extra — файли ПОЗА root, що теж ідуть у передачу: шлях у маніфесті → шлях на
	// диску (напр. іконка збірки, що лежить поруч із .minecraft). Хост віддає їх
	// через Host.SetExtraFiles з тим самим відображенням.
	Extra map[string]string
}

// SharedIconDir — службова тека передачі з іконкою збірки ("<тека>/icon.<ext>").
// Новий лаунчер переносить її на місце іконки збірки, старий лишає як звичайний файл.
const SharedIconDir = ".shaurma-share"

// skipNames — службове сміття, якого не передаємо ніколи.
var skipNames = map[string]bool{
	".ds_store": true, "thumbs.db": true, "desktop.ini": true, "session.lock": true,
}

// BuildManifest обходить root і рахує sha256 кожного файлу. Симлінки та файли,
// заборонені за розширенням, пропускаються; список пропущеного повертається
// (щоб лаунчер показав, що саме не поїде).
func BuildManifest(ctx context.Context, root, kind, name string, meta map[string]string, opt BuildOptions) (*Manifest, []string, error) {
	if kind != KindBuild && kind != KindServer {
		return nil, nil, fmt.Errorf("невідомий тип %q", kind)
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return nil, nil, fmt.Errorf("теку не знайдено: %s", root)
	}

	type item struct {
		rel  string
		abs  string
		size int64
	}
	var items []item
	var skipped []string

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&fs.ModeSymlink != 0 {
			return skipEntry(d)
		}
		if d.IsDir() {
			if opt.Include != nil && !opt.Include(rel, true) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if skipNames[strings.ToLower(d.Name())] || strings.HasSuffix(strings.ToLower(d.Name()), ".part") {
			return nil
		}
		if opt.Include != nil && !opt.Include(rel, false) {
			return nil
		}
		if err := ValidateRelPath(rel); err != nil {
			skipped = append(skipped, rel)
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		items = append(items, item{rel: rel, abs: p, size: info.Size()})
		if len(items) > MaxFiles {
			return fmt.Errorf("забагато файлів (понад %d) — зніміть зайве у виборі", MaxFiles)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for rel, abs := range opt.Extra {
		if ValidateRelPath(rel) != nil {
			continue
		}
		info, serr := os.Stat(abs)
		if serr != nil || !info.Mode().IsRegular() {
			continue
		}
		items = append(items, item{rel: rel, abs: abs, size: info.Size()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].rel < items[j].rel })

	files := make([]FileEntry, len(items))
	workers := opt.Workers
	if workers <= 0 {
		workers = 4
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		done     int
	)
	jobs := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				var extra hash.Hash
				if opt.WantSHA1 != nil && opt.OnSHA1 != nil && opt.WantSHA1(items[i].rel) {
					extra = sha1.New()
				}
				sum, size, herr := hashFileWith(ctx, items[i].abs, extra)
				if herr == nil && extra != nil {
					opt.OnSHA1(items[i].rel, hex.EncodeToString(extra.Sum(nil)))
				}
				mu.Lock()
				if herr != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", items[i].rel, herr)
					}
				} else {
					files[i] = FileEntry{Path: items[i].rel, Size: size, SHA256: sum}
					done++
					if opt.OnProgress != nil {
						opt.OnProgress(done, len(items))
					}
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for i := range items {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if firstErr != nil {
		return nil, nil, firstErr
	}

	var total int64
	for _, f := range files {
		total += f.Size
	}
	m := &Manifest{V: ProtocolVersion, Kind: kind, Name: name, Meta: meta, Files: files, TotalBytes: total}
	if err := m.Validate(); err != nil {
		return nil, nil, err
	}
	return m, skipped, nil
}

func skipEntry(d fs.DirEntry) error {
	if d.IsDir() {
		return fs.SkipDir
	}
	return nil
}

func hashFile(ctx context.Context, path string) (string, int64, error) {
	return hashFileWith(ctx, path, nil)
}

// hashFileWith — sha256 файлу; extra (якщо не nil) отримує ті самі байти.
func hashFileWith(ctx context.Context, path string, extra hash.Hash) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256<<10)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		k, rerr := f.Read(buf)
		if k > 0 {
			h.Write(buf[:k])
			if extra != nil {
				extra.Write(buf[:k])
			}
			n += int64(k)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", 0, rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
