//go:build shaurma

package main

import (
	"os"
	"path/filepath"
	"testing"

	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/share"
)

// Одержувач ставить колір і пресет відправника, а іконку переносить поруч із .minecraft.
func TestApplySharedLook(t *testing.T) {
	packDir := t.TempDir()
	gameDir := filepath.Join(packDir, ".minecraft")
	rel := share.SharedIconDir + "/icon.png"
	if err := os.MkdirAll(filepath.Join(gameDir, share.SharedIconDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, filepath.FromSlash(rel)), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &share.Manifest{Meta: map[string]string{"color": "#12abEF", "icon": "ti-sword"},
		Files: []share.FileEntry{{Path: rel, Size: 3}}}
	var p custompack.Pack
	applySharedLook(&p, m, gameDir)
	if p.Color != "#12abEF" || p.Icon != "ti-sword" || p.IconPath != "icon.png" {
		t.Fatalf("вигляд не застосовано: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(packDir, "icon.png")); err != nil {
		t.Fatalf("іконку не перенесено: %v", err)
	}
	if _, err := os.Stat(filepath.Join(gameDir, share.SharedIconDir)); !os.IsNotExist(err) {
		t.Fatal("службова тека лишилась у .minecraft")
	}

	// Чужі значення не проходять.
	var q custompack.Pack
	applySharedLook(&q, &share.Manifest{Meta: map[string]string{"color": "red;x", "icon": "../evil"}}, gameDir)
	if q.Color != "" || q.Icon != "" || q.IconPath != "" {
		t.Fatalf("недійсні значення прийнято: %+v", q)
	}
}
