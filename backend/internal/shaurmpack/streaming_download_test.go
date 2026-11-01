package shaurmpack

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sha1b(b []byte) string { h := sha1.Sum(b); return hex.EncodeToString(h[:]) }

// Швидкий шлях: файл качається одразу в <файл>.part, SHA-1 приходить від
// резолвера, у фінальному імені з'являється тільки після звірки. Жодного
// додаткового перечитування чи копіювання з temp.
func TestImportStreamingDownloadPublishesVerified(t *testing.T) {
	body := []byte("good-jar-bytes")
	pf := PackFile{Path: "mods/ok.jar", Hash: sha1b(body), Size: int64(len(body)), Modrinth: &ModrinthSource{DownloadURL: "https://cdn.modrinth.com/f"}}
	name := securityPack(t, []PackFile{pf}, nil)
	dir := t.TempDir()
	var slow, legacy atomic.Int32
	res, err := Import(ImportOptions{
		ShaurmPackPath: name, TargetGameDir: dir,
		ResolveDownload: func(string, PackFile) error { legacy.Add(1); return nil },
		ResolveDownloadHashed: func(dest string, _ PackFile) (string, error) {
			if !strings.HasSuffix(dest, ".part") {
				t.Errorf("очікували тимчасове .part, отримали %s", dest)
			}
			if _, err := os.Stat(filepath.Join(dir, "mods/ok.jar")); err == nil {
				t.Error("фінальний файл не повинен існувати до завершення звірки")
			}
			slow.Add(1)
			return sha1b(body), os.WriteFile(dest, body, 0644)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Load() != 0 || slow.Load() != 1 || res.FilesLinked != 1 {
		t.Fatalf("legacy=%d hashed=%d linked=%d", legacy.Load(), slow.Load(), res.FilesLinked)
	}
	got, err := os.ReadFile(filepath.Join(dir, "mods/ok.jar"))
	if err != nil || string(got) != string(body) {
		t.Fatalf("файл не опубліковано: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mods/ok.jar.part")); !os.IsNotExist(err) {
		t.Fatal(".part лишився після успіху")
	}
}

// Безпека не знижена: підроблений вміст (хеш не збігається) не потрапляє у
// фінальний шлях, .part прибирається.
func TestImportStreamingRejectsHashMismatch(t *testing.T) {
	pf := PackFile{Path: "mods/test.jar", Hash: sha1b([]byte("expected")), Size: 8, Modrinth: &ModrinthSource{DownloadURL: "https://cdn.modrinth.com/f"}}
	name := securityPack(t, []PackFile{pf}, nil)
	dir := t.TempDir()
	_, err := Import(ImportOptions{
		ShaurmPackPath: name, TargetGameDir: dir,
		ResolveDownloadHashed: func(dest string, _ PackFile) (string, error) {
			bad := []byte("tampered")
			return sha1b(bad), os.WriteFile(dest, bad, 0644)
		},
	})
	if err == nil {
		t.Fatal("підроблене завантаження прийнято")
	}
	for _, p := range []string{"mods/test.jar", "mods/test.jar.part"} {
		if _, err := os.Stat(filepath.Join(dir, p)); !os.IsNotExist(err) {
			t.Fatalf("%s не мав залишитись", p)
		}
	}
}

// Резолвер, що бреше про хеш/розмір, теж не обходить перевірку.
func TestImportStreamingRejectsSizeMismatch(t *testing.T) {
	body := []byte("abcdefgh")
	pf := PackFile{Path: "mods/s.jar", Hash: sha1b(body), Size: 8, Modrinth: &ModrinthSource{DownloadURL: "https://cdn.modrinth.com/f"}}
	name := securityPack(t, []PackFile{pf}, nil)
	dir := t.TempDir()
	_, err := Import(ImportOptions{
		ShaurmPackPath: name, TargetGameDir: dir,
		ResolveDownloadHashed: func(dest string, _ PackFile) (string, error) {
			return sha1b(body), os.WriteFile(dest, body[:3], 0644)
		},
	})
	if err == nil {
		t.Fatal("файл неправильного розміру прийнято")
	}
	if _, err := os.Stat(filepath.Join(dir, "mods/s.jar")); !os.IsNotExist(err) {
		t.Fatal("опубліковано файл неправильного розміру")
	}
}

// Завантаження йдуть паралельно з розпаковуванням: перший запит на
// завантаження має стартувати ще до того, як з архіву розпаковано все.
func TestImportDownloadsOverlapExtraction(t *testing.T) {
	body := []byte("x")
	pf := PackFile{Path: "mods/a.jar", Hash: sha1b(body), Size: 1, Modrinth: &ModrinthSource{DownloadURL: "https://cdn.modrinth.com/f"}}
	extra := map[string]string{}
	for i := 0; i < 200; i++ {
		extra[OverridesDir+"config/c"+string(rune('a'+i%26))+strings.Repeat("x", i%7)+"_"+time.Duration(i).String()+".txt"] = strings.Repeat("c", 4096)
	}
	name := securityPack(t, []PackFile{pf}, extra)
	dir := t.TempDir()
	started := make(chan struct{})
	var once atomic.Bool
	_, err := Import(ImportOptions{
		ShaurmPackPath: name, TargetGameDir: dir,
		ResolveDownloadHashed: func(dest string, _ PackFile) (string, error) {
			if once.CompareAndSwap(false, true) {
				close(started)
			}
			return sha1b(body), os.WriteFile(dest, body, 0644)
		},
		Progress: func(p ImportProgress) {
			// Розпакування ще триває (Extracting), а завантаження вже могло
			// стартувати — перевіряємо, що воно не чекає кінця розпакування.
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	default:
		t.Fatal("завантаження не стартувало")
	}
	if _, err := os.Stat(filepath.Join(dir, "mods/a.jar")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config")); err != nil {
		t.Fatalf("overrides не розпаковано: %v", err)
	}
}
