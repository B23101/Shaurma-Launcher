package main

import (
	"testing"
)

func TestSnapshotArticleSlugs(t *testing.T) {
	cases := map[string]string{
		"26.4-snapshot-5": "minecraft-26-4-snapshot-5",
		"26.3-pre-2":      "minecraft-26-3-pre-release-2",
		"26.3-rc-2":       "minecraft-26-3-release-candidate-2",
		"25w51a":          "minecraft-snapshot-25w51a",
		"1.21.5-pre1":     "minecraft-1-21-5-pre-release-1",
		"1.21.5-rc1":      "minecraft-1-21-5-release-candidate-1",
	}
	for v, want := range cases {
		got := snapshotArticleSlugs(v)
		if len(got) == 0 || got[0] != want {
			t.Errorf("%s: got %v, want first %q", v, got, want)
		}
	}
	if snapshotArticleSlugs("") != nil {
		t.Error("empty version must give nil")
	}
}

func TestSnapshotHumanTitle(t *testing.T) {
	if got := snapshotHumanTitle("26.4-snapshot-5"); got != "26.4 Snapshot 5" {
		t.Errorf("got %q", got)
	}
	if got := snapshotHumanTitle("26.3-rc-2"); got != "26.3 Release Candidate 2" {
		t.Errorf("got %q", got)
	}
	if got := snapshotHumanTitle("25w51a"); got != "" {
		t.Errorf("weekly must be empty, got %q", got)
	}
}

func TestParseArticleOGImage(t *testing.T) {
	want := "https://www.minecraft.net/content/dam/minecraftnet/article-asset/2026/minecraft-26-4-snapshot-5/new-article-hero-image.jpg"
	// property перед content
	a := []byte(`<head><meta property="og:image" content="` + want + `"></head>`)
	if got := parseArticleOGImage(a); got != want {
		t.Fatalf("a: %q", got)
	}
	// content перед property + відносний шлях
	b := []byte(`<meta content="/content/dam/minecraftnet/article-asset/2026/x/hero.png" property="og:image"/>`)
	if got := parseArticleOGImage(b); got != "https://www.minecraft.net/content/dam/minecraftnet/article-asset/2026/x/hero.png" {
		t.Fatalf("b: %q", got)
	}
	// логотип відкидається, лишається twitter:image
	c := []byte(`<meta property="og:image" content="https://www.minecraft.net/content/dam/x/logos/a.png">` +
		`<meta name="twitter:image" content="` + want + `">`)
	if got := parseArticleOGImage(c); got != want {
		t.Fatalf("c: %q", got)
	}
	if parseArticleOGImage([]byte("<html></html>")) != "" {
		t.Fatal("expected empty")
	}
}

func TestSnapshotSlugYear(t *testing.T) {
	if got := snapshotSlugYear("26.4-snapshot-5"); len(got) == 0 || got[0] != 2026 {
		t.Errorf("got %v", got)
	}
	if got := snapshotSlugYear("25w51a"); len(got) == 0 || got[0] != 2025 {
		t.Errorf("got %v", got)
	}
}
