//go:build !windows

package pathlink

import (
	"os"
	"path/filepath"
)

// MakeDirLink створює symlink link → target.
func MakeDirLink(link, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	return os.Symlink(abs, link)
}
