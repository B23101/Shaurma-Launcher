package custompack

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	shsync "shaurma-launcher-backend/internal/sync"
	"strings"
)

// Follow the public Modrinth format policy, with GitHub's release CDN redirect hosts.
func mrpackDownloadURL(raw string, redirect bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf("mrpack requires HTTPS download URLs")
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "cdn.modrinth.com", "github.com", "raw.githubusercontent.com", "gitlab.com":
		return nil
	}
	if redirect && (host == "release-assets.githubusercontent.com" || host == "objects.githubusercontent.com") {
		return nil
	}
	return fmt.Errorf("untrusted mrpack download host: %s", host)
}

func validateMrpackIndex(idx *mrpackIndex) error {
	if idx.FormatVersion != 1 || idx.Game != "minecraft" || len(idx.Files) > 100000 {
		return fmt.Errorf("unsupported or oversized mrpack index")
	}
	seen := map[string]bool{}
	for _, f := range idx.Files {
		if err := shsync.ValidateRelPath(f.Path); err != nil {
			return err
		}
		key := strings.ToLower(path.Clean(strings.ReplaceAll(f.Path, "\\", "/")))
		if seen[key] {
			return fmt.Errorf("duplicate mrpack file path: %s", f.Path)
		}
		seen[key] = true
		if f.FileSize < 0 || f.FileSize > 2<<30 {
			return fmt.Errorf("invalid mrpack file size")
		}
		for algorithm, size := range map[string]int{"sha1": 20, "sha512": 64} {
			b, err := hex.DecodeString(f.Hashes[algorithm])
			if err != nil || len(b) != size {
				return fmt.Errorf("missing or invalid mrpack %s hash", algorithm)
			}
		}
		if len(f.Downloads) == 0 {
			return fmt.Errorf("missing mrpack download URL")
		}
		for _, raw := range f.Downloads {
			if err := mrpackDownloadURL(raw, false); err != nil {
				return err
			}
		}
	}
	return nil
}
