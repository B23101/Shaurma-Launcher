package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestUnpackPageDesign(t *testing.T) {
	archive := func(name, body string) []byte {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	for _, tc := range []struct {
		name    string
		raw     []byte
		badHash bool
		wantErr bool
	}{
		{"html", []byte("<!doctype html><h1>Mafia</h1>"), false, false},
		{"zip", archive("index.html", "<h1>Mafia</h1>"), false, false},
		{"traversal", archive("../index.html", "unsafe"), false, true},
		{"wrong-entry", archive("page.html", "wrong"), false, true},
		{"checksum", []byte("html"), true, true},
		{"utf8", []byte{0xff}, false, true},
		{"empty", nil, false, true},
		{"zip-bomb", archive("index.html", strings.Repeat("a", maxPageDesign+1)), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hash := fmt.Sprintf("%x", sha256.Sum256(tc.raw))
			if tc.badHash {
				hash = strings.Repeat("0", 64)
			}
			_, err := unpackPageDesign(tc.raw, hash)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
