package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Pending files are executable code. Their accompanying proof contains the
// original signed CDN bytes, so restart verification also works offline.
func verifyPendingComponent(dir, name string) error {
	if !manifestSigEnabled() {
		return nil
	} // explicit development builds only
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	var proof struct {
		Payload   []byte `json:"payload"`
		Signature string `json:"signature"`
	}
	proofFile, err := root.Open(name + ".new.proof")
	if os.IsNotExist(err) {
		// Upgrade from an older updater: it did not save proofs. Authenticate
		// its pending bytes against a currently signed CDN manifest. Offline,
		// leave the installed version intact and retry on the next launch.
		return verifyLegacyPendingComponent(dir, name)
	}
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(proofFile, (2<<20)+1))
	proofFile.Close()
	if err != nil {
		return err
	}
	if len(raw) > 2<<20 {
		return fmt.Errorf("update proof too large")
	}
	if err := json.Unmarshal(raw, &proof); err != nil {
		return err
	}
	return verifyPendingPayload(root, name, proof.Payload, proof.Signature)
}

func verifyLegacyPendingComponent(dir, name string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	var last error
	for _, file := range []string{"launcher-version.json", "launcher-version-beta.json"} {
		url := cdnBase + "/launcher/" + platformSegment() + file
		payload, err := fetchWithToken(url)
		if err != nil {
			last = err
			continue
		}
		signature, err := fetchWithToken(url + ".sig")
		if err != nil {
			last = err
			continue
		}
		if err := verifyPendingPayload(root, name, payload, string(signature)); err == nil {
			return nil
		} else {
			last = err
		}
	}
	return last
}

func verifyPendingPayload(root *os.Root, name string, payload []byte, signature string) error {
	if err := verifyManifestSig(payload, signature); err != nil {
		return err
	}
	var m bootstrapRemoteManifest
	if err := json.Unmarshal(bytes.TrimPrefix(payload, []byte{0xef, 0xbb, 0xbf}), &m); err != nil {
		return err
	}
	for _, c := range m.Components {
		if c.Path != name || filepath.Base(c.Path) != c.Path || c.Kind == "archive" {
			continue
		}
		if err := validateComponent(c); err != nil {
			return err
		}
		f, err := root.Open(name + ".new")
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, c.Size+1))
		if err != nil {
			return err
		}
		if n != c.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), c.SHA256) {
			return fmt.Errorf("pending component integrity check failed: %s", name)
		}
		return nil
	}
	return fmt.Errorf("pending component is absent from signed manifest: %s", name)
}
