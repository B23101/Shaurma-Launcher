//go:build windows

package authenticode

import (
	"os"
	"path/filepath"
	"testing"

	"shaurma-trust"
)

func TestSignedBinary(t *testing.T) {
	path := `C:\Program Files\GitHub CLI\gh.exe`
	if _, err := os.Stat(path); err != nil {
		t.Skip("підписаного зразка немає на цьому ПК")
	}
	subject, thumb, err := Signer(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(path, nil); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := Verify(path, []trust.AuthenticodeSigner{{Subject: subject, Thumbprints: []string{thumb}}}); err != nil {
		t.Fatalf("matching signer rejected: %v", err)
	}
	if err := Verify(path, []trust.AuthenticodeSigner{{Subject: "SignPath Foundation"}}); err == nil {
		t.Fatal("wrong publisher accepted")
	}
	if err := Verify(path, []trust.AuthenticodeSigner{{Subject: subject, Thumbprints: []string{"00"}}}); err == nil {
		t.Fatal("wrong thumbprint accepted")
	}
}

func TestUnsignedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.exe")
	os.WriteFile(p, []byte("MZ not really"), 0o644)
	if err := Verify(p, nil); err == nil {
		t.Fatal("unsigned file accepted")
	}
}
