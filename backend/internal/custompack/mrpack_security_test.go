package custompack

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mrpackTransport func(*http.Request) (*http.Response, error)

func (f mrpackTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func validMrpackFile() mrpackFile {
	a := sha1.Sum([]byte("expected"))
	b := sha512.Sum512([]byte("expected"))
	return mrpackFile{Path: "mods/test.jar", Hashes: map[string]string{"sha1": hex.EncodeToString(a[:]), "sha512": hex.EncodeToString(b[:])}, FileSize: 8, Downloads: []string{"https://cdn.modrinth.com/test.jar"}}
}
func TestMrpackValidationPolicy(t *testing.T) {
	f := validMrpackFile()
	idx := &mrpackIndex{FormatVersion: 1, Game: "minecraft", Files: []mrpackFile{f}}
	if err := validateMrpackIndex(idx); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"http://cdn.modrinth.com/file", "https://cdn.modrinth.com.evil.test/file", "https://127.0.0.1/file"} {
		idx.Files[0].Downloads = []string{raw}
		if validateMrpackIndex(idx) == nil {
			t.Errorf("unsafe URL: %s", raw)
		}
	}
	idx.Files[0] = validMrpackFile()
	delete(idx.Files[0].Hashes, "sha512")
	if validateMrpackIndex(idx) == nil {
		t.Fatal("missing SHA512 accepted")
	}
	idx.Files = []mrpackFile{validMrpackFile()}
	idx.Files[0].Path = "../outside"
	if validateMrpackIndex(idx) == nil {
		t.Fatal("traversal accepted")
	}
	for _, raw := range []string{"https://github.com/owner/repo/file", "https://raw.githubusercontent.com/owner/file", "https://gitlab.com/owner/file"} {
		if err := mrpackDownloadURL(raw, false); err != nil {
			t.Error(err)
		}
	}
	if err := mrpackDownloadURL("https://release-assets.githubusercontent.com/file", true); err != nil {
		t.Error(err)
	}
}
func TestMrpackChecksSHA512SizeAndRedirects(t *testing.T) {
	f := validMrpackFile()
	client := &http.Client{Transport: mrpackTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("expected")), Header: make(http.Header), Request: r}, nil
	})}
	for _, bad := range []bool{false, true} {
		task := importTask{relPath: f.Path, url: f.Downloads[0], sha1: f.Hashes["sha1"], sha512: f.Hashes["sha512"], fileSize: 8, mrpack: true}
		if bad {
			task.sha512 = strings.Repeat("0", 128)
		}
		dir := t.TempDir()
		_, err := downloadOneFile(context.Background(), client, task, dir, nil)
		if bad && err == nil {
			t.Fatal("bad SHA512 accepted")
		}
		if !bad && err != nil {
			t.Fatal(err)
		}
		_, statErr := os.Stat(filepath.Join(dir, "mods", "test.jar"))
		if bad && !os.IsNotExist(statErr) {
			t.Fatal("published unverified bytes")
		}
	}
	next, _ := http.NewRequest(http.MethodGet, "https://evil.test/file", nil)
	if mrpackDownloadURL(next.URL.String(), true) == nil {
		t.Fatal("foreign redirect accepted")
	}
}

func TestMrpackRedirectRejectedBeforeForeignRequest(t *testing.T) {
	foreign := 0
	client := &http.Client{Transport: mrpackTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "evil.test" {
			foreign++
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("expected")), Request: r}, nil
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/file"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	f := validMrpackFile()
	task := importTask{relPath: f.Path, url: f.Downloads[0], sha1: f.Hashes["sha1"], sha512: f.Hashes["sha512"], fileSize: 8, mrpack: true}
	if _, err := downloadOneFile(context.Background(), client, task, t.TempDir(), nil); err == nil {
		t.Fatal("foreign redirect accepted")
	}
	if foreign != 0 {
		t.Fatalf("contacted foreign origin %d times", foreign)
	}
}
