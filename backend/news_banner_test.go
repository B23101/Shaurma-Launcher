package main

import "testing"

func TestTimeline(t *testing.T) {
	html := []byte(`<img src="https://www.minecraft.net/content/dam/minecraftnet/games/minecraft/key-art/Minecraft_WildernessBound.Net_277x277.jpg">
<img src="/content/dam/minecraftnet/games/minecraft/screenshots/MC-Updates_Carousel-N-0_2026_FGD-26_01_1000x563.png">
<img src="/content/dam/minecraftnet/games/minecraft/screenshots/MC-Updates_Carousel-N-0_2026_SGD-26_01_1000x563.jpg">`)
	want := "https://www.minecraft.net/content/dam/minecraftnet/games/minecraft/screenshots/MC-Updates_Carousel-N-0_2026_FGD-26_01_1000x563.png"
	if got := parseTimelineBannerURL(html); got != want {
		t.Fatalf("got %q", got)
	}
	abs := []byte(`![x](https://www.minecraft.net/content/dam/minecraftnet/games/minecraft/key-art/MC-Updates_Carousel-N-0_2025_Mounts-of-Mayhem_01_1000x563.jpg)`)
	if got := parseTimelineBannerURL(abs); got != "https://www.minecraft.net/content/dam/minecraftnet/games/minecraft/key-art/MC-Updates_Carousel-N-0_2025_Mounts-of-Mayhem_01_1000x563.jpg" {
		t.Fatalf("abs got %q", got)
	}
	if parseTimelineBannerURL([]byte("<html></html>")) != "" {
		t.Fatal("expected empty")
	}
}

func TestFeedFallbackImage(t *testing.T) {
	feed := &newsFeed{Entries: []newsEntry{
		{Title: "Minecraft 26.3 notes"},                                              // збіг, але без картинки
		{Title: "Java 26.3 released", NewsPageImage: &newsEntryImage{URL: "/a.jpg"}}, // newsPageImage
	}}
	e := findNewsForVersion(feed, "26.3")
	if e == nil || e.imageURL() != "/a.jpg" {
		t.Fatalf("got %+v", e)
	}
	if findNewsForVersion(feed, "26.35") != nil {
		t.Fatal("26.35 must not match")
	}
}
