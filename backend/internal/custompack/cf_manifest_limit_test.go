package custompack

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCFManifestSizeLimit(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		body := `{"manifestType":"minecraftModpack","name":"Normal pack","minecraft":{"version":"1.20.1"}}`
		if oversized {
			body = strings.Repeat(" ", 16<<20) + body
		}
		if _, err = w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
		if err = zw.Close(); err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatal(err)
		}
		m, err := readCFManifest(zr.File[0])
		if oversized {
			if !errors.Is(err, errCFManifestTooLarge) {
				t.Fatalf("oversized manifest accepted: %v", err)
			}
			archive := filepath.Join(t.TempDir(), "pack.zip")
			if err := os.WriteFile(archive, buf.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := DetectArchive(archive); !errors.Is(err, errCFManifestTooLarge) {
				t.Fatalf("preview fell back to generic import: %v", err)
			}
		} else if err != nil || m.Name != "Normal pack" {
			t.Fatalf("valid manifest rejected: %v", err)
		}
	}
}
