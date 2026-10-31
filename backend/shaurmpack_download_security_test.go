package main

import "testing"

func TestPackDownloadURLPolicy(t *testing.T) {
	for _, raw := range []string{"http://cdn.modrinth.com/file", "https://evil.test/?forgecdn.net", "https://cdn.modrinth.com.evil.test/file", "https://edge.forgecdn.net@evil.test/file"} {
		if validatePackDownloadURL(raw) == nil {
			t.Errorf("untrusted URL accepted: %s", raw)
		}
	}
	for _, raw := range []string{"https://cdn.modrinth.com/data/file.jar", "https://edge.forgecdn.net/file.jar"} {
		if err := validatePackDownloadURL(raw); err != nil {
			t.Error(err)
		}
	}
}
