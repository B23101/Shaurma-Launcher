package main

// pending_verify_v2.go — перевірка відкладених (.new) файлів, які поставила
// нова система оновлень (маніфест v2 з GitHub). Доказ містить keyset,
// підписаний root, і маніфест, підписаний release-ключем із цього keyset —
// тож перевірка працює офлайн і не залежить від старого CDN.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shaurma-trust"
)

// rootKeys — публічні root-ключі (base64 через кому), вшиваються при
// збірці: -X main.rootKeys=... Порожньо — докази v2 не приймаються.
var rootKeys = ""

type pendingProofV2 struct {
	Payload   []byte
	Signature string
	Keyset    []byte
	KeysetSig string
}

func verifyPendingV2(root *os.Root, name string, p pendingProofV2) error {
	roots := trust.ParsePublicKeys(rootKeys)
	if len(roots) == 0 {
		return fmt.Errorf("доказ v2 без вшитого root-ключа не приймається")
	}
	ks, err := trust.ParseKeyset(roots, p.Keyset, p.KeysetSig)
	if err != nil {
		return err
	}
	m, err := trust.ParseManifest(ks, p.Payload, p.Signature, time.Now().UTC())
	if err != nil {
		return err
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	ownExe := ""
	if exe, err := os.Executable(); err == nil {
		ownExe = filepath.Base(exe)
	}
	for _, f := range m.FilesFor(platform) {
		if f.Kind == "archive" {
			continue
		}
		match := f.Path == name
		if f.Role == trust.FileShell {
			match = strings.EqualFold(name, ownExe) || f.Path == name
		}
		if !match {
			continue
		}
		file, err := root.Open(name + ".new")
		if err != nil {
			return err
		}
		defer file.Close()
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(file, f.Size+1))
		if err != nil {
			return err
		}
		if n != f.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), f.SHA256) {
			return fmt.Errorf("pending component integrity check failed: %s", name)
		}
		return nil
	}
	return fmt.Errorf("pending component is absent from signed manifest v2: %s", name)
}
