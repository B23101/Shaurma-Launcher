package main

import "testing"

func TestDownloadRejectsMissingIntegrityBeforeNetwork(t *testing.T) {
	for _, c := range []*manifestComponent{nil, {}, {Size: 1, SHA256: "bad"}} {
		if err := downloadStream(c, "unused", func(int) {}); err == nil {
			t.Fatal("invalid component accepted")
		}
	}
}
