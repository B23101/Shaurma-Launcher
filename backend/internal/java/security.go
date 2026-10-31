package java

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func runtimeURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf("invalid Java download URL")
	}
	switch strings.ToLower(u.Hostname()) {
	case "launchermeta.mojang.com", "piston-meta.mojang.com", "piston-data.mojang.com", "launcher.mojang.com", "resources.download.minecraft.net":
		return nil
	}
	return fmt.Errorf("untrusted Java download host")
}

func (j *Installer) runtimeClient() *http.Client {
	cp := *j.client
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many Java redirects")
		}
		if err := runtimeURL(req.URL.String()); err != nil {
			return err
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Del("X-Shaurma-Token")
		req.Header.Del("x-api-key")
		return nil
	}
	return &cp
}

func validRuntimeHash(hash string) bool {
	b, err := hex.DecodeString(hash)
	return err == nil && len(b) == sha1.Size
}

func (j *Installer) runtimeBytes(raw, hash string, limit int64) ([]byte, error) {
	if err := runtimeURL(raw); err != nil {
		return nil, err
	}
	if !validRuntimeHash(hash) {
		return nil, fmt.Errorf("missing or invalid Java manifest SHA1")
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	resp, err := j.runtimeClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Java manifest HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("Java manifest too large")
	}
	sum := sha1.Sum(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), hash) {
		return nil, fmt.Errorf("Java manifest SHA1 mismatch")
	}
	return data, nil
}

func runtimePath(rel string) error {
	if !filepath.IsLocal(rel) || strings.ContainsAny(rel, "\\:\x00") {
		return fmt.Errorf("unsafe Java path: %s", rel)
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." || part == "." || part == "" || strings.TrimRight(part, ". ") != part {
			return fmt.Errorf("unsafe Java path: %s", rel)
		}
	}
	return nil
}

func validateRuntimePackage(pkg *mojangPackageManifest) error {
	if pkg == nil || len(pkg.Files) > 100000 {
		return fmt.Errorf("invalid Java package")
	}
	var total int64
	for rel, f := range pkg.Files {
		if err := runtimePath(rel); err != nil {
			return err
		}
		switch f.Type {
		case "file":
			total += f.Downloads.Raw.Size
			if total > 8<<30 {
				return fmt.Errorf("Java package exceeds 8 GiB")
			}
			if !validRuntimeHash(f.Downloads.Raw.SHA1) || f.Downloads.Raw.Size < 0 || f.Downloads.Raw.Size > 2<<30 {
				return fmt.Errorf("invalid Java file metadata: %s", rel)
			}
			if err := runtimeURL(f.Downloads.Raw.URL); err != nil {
				return err
			}
		case "directory":
		case "link":
			if f.Target == "" || strings.ContainsAny(f.Target, "\\:\x00") || path.IsAbs(f.Target) || !filepath.IsLocal(filepath.FromSlash(path.Join(path.Dir(rel), f.Target))) {
				return fmt.Errorf("unsafe Java link: %s", rel)
			}
		default:
			return fmt.Errorf("unknown Java entry type: %s", f.Type)
		}
	}
	return nil
}

func (j *Installer) downloadRuntimeFile(root *os.Root, rel string, f mojangPackageFile) error {
	if j.IsCancelRequested() {
		return ErrInstallCancelled
	}
	raw := f.Downloads.Raw
	if err := runtimeURL(raw.URL); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, raw.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	resp, err := j.runtimeClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Java download HTTP %d", resp.StatusCode)
	}
	tmp := rel + ".part-" + rand.Text()
	out, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	h := sha1.New()
	n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, raw.Size+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != raw.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), raw.SHA1) {
		return fmt.Errorf("Java file integrity mismatch: %s", rel)
	}
	return root.Rename(tmp, rel)
}
