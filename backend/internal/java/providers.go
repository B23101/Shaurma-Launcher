package java

// ── Провайдери Java (як у Modrinth App / Prism) ──────────────────────────
//
// mojang   — рантайми з маніфесту Mojang (типово, installer.go);
// adoptium — Eclipse Temurin (api.adoptium.net);
// zulu     — Azul Zulu (api.azul.com).
//
// Кожен провайдер × мажор живе у своїй теці <javaDir>/<провайдер>-<мажор>/,
// тож Java від різних провайдерів не перетирають одна одну. Архів качається
// лише з https-хостів провайдера, звіряється з SHA-256 з API провайдера і
// розпаковується через os.Root (жоден запис архіву не вийде за межі теки).

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	ProviderMojang   = "mojang"
	ProviderAdoptium = "adoptium"
	ProviderZulu     = "zulu"
)

// Ліміти архіву: JRE важить 40–60 МБ, JDK — до 200 МБ; розпакований — до ~400 МБ.
const (
	vendorMaxArchive  = 400 << 20
	vendorMaxUnpacked = 1 << 30
	vendorMaxEntries  = 30000
)

// ValidProvider — чи відомий провайдер ("" = mojang).
func ValidProvider(p string) bool {
	switch p {
	case "", ProviderMojang, ProviderAdoptium, ProviderZulu:
		return true
	}
	return false
}

// ProviderTitle — людська назва провайдера для повідомлень прогресу.
func ProviderTitle(p string) string {
	switch p {
	case ProviderAdoptium:
		return "Eclipse Temurin"
	case ProviderZulu:
		return "Azul Zulu"
	}
	return "Mojang"
}

type vendorPackage struct {
	URL     string
	SHA256  string
	Size    int64
	Version string
	Archive string // zip | tar.gz
}

type vendorMarker struct {
	Provider    string    `json:"provider"`
	Version     string    `json:"version"`
	SHA256      string    `json:"sha256"`
	InstalledAt time.Time `json:"installedAt"`
}

const vendorMarkerName = ".shaurma-vendor.json"

// ProviderDir — тека Java конкретного провайдера й мажора.
func (j *Installer) ProviderDir(provider string, major int) string {
	return filepath.Join(j.dir, fmt.Sprintf("%s-%d", provider, major))
}

// ProviderExePath — очікуваний шлях до виконуваного файлу Java провайдера.
func (j *Installer) ProviderExePath(provider string, major int) string {
	return filepath.Join(j.ProviderDir(provider, major), "bin", exeName())
}

// EnsureProviderMajor ставить (за потреби) Java потрібного мажора від обраного
// провайдера і повертає шлях до виконуваного файлу. mojang/"" — звичайний шлях
// через маніфест Mojang (component з version.json враховується).
func (j *Installer) EnsureProviderMajor(provider string, major int, component string) (string, error) {
	if provider == "" || provider == ProviderMojang {
		return j.EnsureMajorWithComponent(major, component)
	}
	if !ValidProvider(provider) {
		return "", fmt.Errorf("невідомий провайдер Java: %s", provider)
	}
	if major <= 0 || major > 99 {
		return "", fmt.Errorf("невірна версія Java: %d", major)
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	exe := j.ProviderExePath(provider, major)
	if DetectMajor(exe) == major {
		return exe, nil
	}
	title := ProviderTitle(provider)
	j.reportProgress("manifest", 0, fmt.Sprintf("Пошук Java %d (%s)...", major, title))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	pkg, err := j.resolveVendor(ctx, provider, major)
	cancel()
	if err != nil {
		return "", fmt.Errorf("%s: %w", title, err)
	}
	if err := j.installVendor(provider, major, pkg); err != nil {
		return "", err
	}
	if got := DetectMajor(exe); got != major {
		return "", fmt.Errorf("%s: після встановлення знайдено Java %d замість %d", title, got, major)
	}
	return exe, nil
}

// ── Пошук пакета в API провайдера ────────────────────────────────────────

func vendorOS() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "mac"
	}
	return "linux"
}

func vendorArch() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x64"
}

func vendorArchive() string {
	if runtime.GOOS == "windows" {
		return "zip"
	}
	return "tar.gz"
}

func (j *Installer) resolveVendor(ctx context.Context, provider string, major int) (vendorPackage, error) {
	switch provider {
	case ProviderAdoptium:
		// JRE є не для кожного мажора — тоді беремо JDK (теж запускає гру).
		for _, image := range []string{"jre", "jdk"} {
			pkg, err := j.resolveAdoptium(ctx, major, image)
			if err == nil {
				return pkg, nil
			}
			if !errors.Is(err, errNoVendorPackage) {
				return vendorPackage{}, err
			}
		}
	case ProviderZulu:
		for _, kind := range []string{"jre", "jdk"} {
			pkg, err := j.resolveZulu(ctx, major, kind)
			if err == nil {
				return pkg, nil
			}
			if !errors.Is(err, errNoVendorPackage) {
				return vendorPackage{}, err
			}
		}
	}
	return vendorPackage{}, fmt.Errorf("немає Java %d для цієї системи", major)
}

var errNoVendorPackage = errors.New("no vendor package")

func (j *Installer) vendorJSON(ctx context.Context, raw string, out any) error {
	if err := vendorURL(raw); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	req.Header.Set("Accept", "application/json")
	resp, err := j.vendorClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNoVendorPackage
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

func (j *Installer) resolveAdoptium(ctx context.Context, major int, image string) (vendorPackage, error) {
	q := url.Values{}
	q.Set("architecture", vendorArch())
	q.Set("image_type", image)
	q.Set("os", vendorOS())
	q.Set("vendor", "eclipse")
	raw := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?%s", major, q.Encode())
	var list []struct {
		Binary struct {
			Package struct {
				Checksum string `json:"checksum"`
				Link     string `json:"link"`
				Name     string `json:"name"`
				Size     int64  `json:"size"`
			} `json:"package"`
		} `json:"binary"`
		ReleaseName string `json:"release_name"`
	}
	if err := j.vendorJSON(ctx, raw, &list); err != nil {
		return vendorPackage{}, err
	}
	want := "." + vendorArchive()
	for _, it := range list {
		p := it.Binary.Package
		if !strings.HasSuffix(strings.ToLower(p.Name), want) {
			continue
		}
		return vendorPackage{URL: p.Link, SHA256: p.Checksum, Size: p.Size, Version: it.ReleaseName, Archive: vendorArchive()}, nil
	}
	return vendorPackage{}, errNoVendorPackage
}

func (j *Installer) resolveZulu(ctx context.Context, major int, kind string) (vendorPackage, error) {
	osName := vendorOS()
	if osName == "mac" {
		osName = "macos"
	}
	q := url.Values{}
	q.Set("java_version", fmt.Sprint(major))
	q.Set("os", osName)
	q.Set("arch", vendorArch())
	q.Set("archive_type", vendorArchive())
	q.Set("java_package_type", kind)
	q.Set("javafx_bundled", "false")
	q.Set("release_status", "ga")
	q.Set("availability_types", "CA")
	q.Set("latest", "true")
	q.Set("page", "1")
	q.Set("page_size", "1")
	var list []struct {
		UUID string `json:"package_uuid"`
		Name string `json:"name"`
	}
	if err := j.vendorJSON(ctx, "https://api.azul.com/metadata/v1/zulu/packages/?"+q.Encode(), &list); err != nil {
		return vendorPackage{}, err
	}
	if len(list) == 0 || !safeToken(list[0].UUID) {
		return vendorPackage{}, errNoVendorPackage
	}
	var det struct {
		DownloadURL string `json:"download_url"`
		SHA256      string `json:"sha256_hash"`
		Size        int64  `json:"size"`
		Name        string `json:"name"`
		JavaVersion []int  `json:"java_version"`
	}
	if err := j.vendorJSON(ctx, "https://api.azul.com/metadata/v1/zulu/packages/"+list[0].UUID, &det); err != nil {
		return vendorPackage{}, err
	}
	ver := det.Name
	if len(det.JavaVersion) > 0 {
		parts := make([]string, len(det.JavaVersion))
		for i, n := range det.JavaVersion {
			parts[i] = fmt.Sprint(n)
		}
		ver = strings.Join(parts, ".")
	}
	return vendorPackage{URL: det.DownloadURL, SHA256: det.SHA256, Size: det.Size, Version: ver, Archive: vendorArchive()}, nil
}

func safeToken(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// ── Мережа: лише https-хости провайдерів ────────────────────────────────

func vendorURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf("invalid Java download URL")
	}
	switch strings.ToLower(u.Hostname()) {
	case "api.adoptium.net", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com",
		"api.azul.com", "cdn.azul.com":
		return nil
	}
	return fmt.Errorf("untrusted Java download host")
}

func (j *Installer) vendorClient() *http.Client {
	cp := *j.client
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many Java redirects")
		}
		if err := vendorURL(req.URL.String()); err != nil {
			return err
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		return nil
	}
	return &cp
}

// ── Встановлення ─────────────────────────────────────────────────────────

func (j *Installer) installVendor(provider string, major int, pkg vendorPackage) error {
	title := ProviderTitle(provider)
	if err := vendorURL(pkg.URL); err != nil {
		return err
	}
	sum, err := hex.DecodeString(strings.TrimSpace(pkg.SHA256))
	if err != nil || len(sum) != sha256.Size {
		return fmt.Errorf("%s: немає контрольної суми SHA-256 — встановлення скасовано", title)
	}
	if pkg.Size > vendorMaxArchive {
		return fmt.Errorf("%s: архів Java завеликий", title)
	}
	if err := os.MkdirAll(j.dir, 0755); err != nil {
		return err
	}
	final := j.ProviderDir(provider, major)
	archive := final + ".download"
	staging := final + ".staging"
	_ = os.RemoveAll(staging)
	defer os.Remove(archive)
	defer os.RemoveAll(staging)

	j.status(fmt.Sprintf("Завантаження Java %d (%s %s)...", major, title, pkg.Version))
	if err := j.downloadVendor(pkg, sum, archive, major, title); err != nil {
		return err
	}
	j.reportProgress("extract", 90, fmt.Sprintf("Розпаковка Java %d...", major))
	if err := os.MkdirAll(staging, 0755); err != nil {
		return err
	}
	if pkg.Archive == "zip" {
		err = extractZipSafe(archive, staging, j.IsCancelRequested)
	} else {
		err = extractTarGzSafe(archive, staging, j.IsCancelRequested)
	}
	if err != nil {
		return err
	}
	home, err := findJavaHome(staging)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	if err := writeVendorMarker(home, vendorMarker{Provider: provider, Version: pkg.Version, SHA256: pkg.SHA256, InstalledAt: time.Now()}); err != nil {
		j.status(fmt.Sprintf("Не вдалося записати версію Java %d: %v", major, err))
	}
	if err := swapDir(final, home); err != nil {
		return fmt.Errorf("Java %d зараз використовується — закрийте гру й спробуйте знову: %w", major, err)
	}
	j.reportProgress("extract", 100, "Java встановлено")
	j.status(fmt.Sprintf("Java %d (%s) встановлено: %s", major, title, j.ProviderExePath(provider, major)))
	return nil
}

func (j *Installer) downloadVendor(pkg vendorPackage, sum []byte, dest string, major int, title string) error {
	req, err := http.NewRequest(http.MethodGet, pkg.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	resp, err := j.vendorClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", title, resp.StatusCode)
	}
	total := pkg.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 256<<10)
	lastPct := -1
	body := io.LimitReader(resp.Body, vendorMaxArchive+1)
	for {
		if j.IsCancelRequested() {
			out.Close()
			return ErrInstallCancelled
		}
		n, rerr := body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return werr
			}
			h.Write(buf[:n])
			done += int64(n)
			if done > vendorMaxArchive {
				out.Close()
				return fmt.Errorf("%s: архів Java завеликий", title)
			}
			if total > 0 {
				pct := int(done * 88 / total)
				if pct != lastPct && pct <= 88 {
					lastPct = pct
					j.reportProgress("download", pct, fmt.Sprintf("Завантаження Java %d (%s)... %d%%", major, title, pct*100/88))
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Розмір у метаданих Azul округлений, тож повноту гарантує лише SHA-256.
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), hex.EncodeToString(sum)) {
		return fmt.Errorf("%s: контрольна сума архіву Java не збігається — файл пошкоджений або підмінений", title)
	}
	return nil
}

// archivePath нормалізує ім'я запису архіву і відкидає небезпечні.
func archivePath(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, ":\x00") {
		return "", false
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", false
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.TrimRight(part, ". ") != part {
			return "", false
		}
	}
	return clean, true
}

func extractZipSafe(src, dst string, cancelled func() bool) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) > vendorMaxEntries {
		return fmt.Errorf("архів Java містить забагато файлів")
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	var total int64
	for _, f := range zr.File {
		if cancelled() {
			return ErrInstallCancelled
		}
		rel, ok := archivePath(f.Name)
		if !ok {
			return fmt.Errorf("небезпечний шлях в архіві Java: %q", f.Name)
		}
		mode := f.Mode()
		if mode&fs.ModeSymlink != 0 {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := root.MkdirAll(rel, 0755); err != nil {
				return err
			}
			continue
		}
		if dir := path.Dir(rel); dir != "." {
			if err := root.MkdirAll(dir, 0755); err != nil {
				return err
			}
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		n, err := writeRootFile(root, rel, rc, vendorMaxUnpacked-total, mode.Perm()|0644)
		rc.Close()
		if err != nil {
			return err
		}
		total += n
	}
	return nil
}

func extractTarGzSafe(src, dst string, cancelled func() bool) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	tr := tar.NewReader(gz)
	var total int64
	entries := 0
	for {
		if cancelled() {
			return ErrInstallCancelled
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if entries++; entries > vendorMaxEntries {
			return fmt.Errorf("архів Java містить забагато файлів")
		}
		rel, ok := archivePath(hdr.Name)
		if !ok {
			return fmt.Errorf("небезпечний шлях в архіві Java: %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(rel, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if dir := path.Dir(rel); dir != "." {
				if err := root.MkdirAll(dir, 0755); err != nil {
					return err
				}
			}
			n, err := writeRootFile(root, rel, tr, vendorMaxUnpacked-total, fs.FileMode(hdr.Mode).Perm()|0644)
			if err != nil {
				return err
			}
			total += n
		default:
			// посилання й спецфайли не розпаковуємо (для запуску гри не потрібні)
		}
	}
}

func writeRootFile(root *os.Root, rel string, r io.Reader, budget int64, perm fs.FileMode) (int64, error) {
	if budget <= 0 {
		return 0, fmt.Errorf("архів Java завеликий після розпаковки")
	}
	out, err := root.OpenFile(rel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(r, budget+1))
	cerr := out.Close()
	if err != nil {
		return n, err
	}
	if cerr != nil {
		return n, cerr
	}
	if n > budget {
		return n, fmt.Errorf("архів Java завеликий після розпаковки")
	}
	return n, nil
}

// findJavaHome шукає в розпакованому архіві теку, де лежить bin/<java>
// (zip/tar мають кореневу теку, на macOS — ще Contents/Home).
func findJavaHome(dir string) (string, error) {
	var found string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() && strings.Count(rel, string(filepath.Separator)) > 3 {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == exeName() && filepath.Base(filepath.Dir(p)) == "bin" {
			found = filepath.Dir(filepath.Dir(p))
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("в архіві немає bin/%s", exeName())
	}
	return found, nil
}

func writeVendorMarker(dir string, m vendorMarker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, vendorMarkerName), data, 0644)
}
