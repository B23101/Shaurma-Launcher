package main

import "testing"

func TestDecodeImageDataURL(t *testing.T) {
	if m, b, ok := decodeImageDataURL("data:image/png;base64,iVBORw0KGgo="); !ok || m != "image/png" || len(b) == 0 {
		t.Fatalf("валідний PNG data URL відхилено: %v %q %d", ok, m, len(b))
	}
	for _, bad := range []string{
		"",
		"data:text/html;base64,PGI+",
		"data:image/svg+xml;base64,PHN2Zz4=",
		"data:image/png,rawnotbase64",
		"data:image/png;base64,!!!",
		"data:image/png;base64,",
	} {
		if _, _, ok := decodeImageDataURL(bad); ok {
			t.Fatalf("небезпечний/битий data URL прийнято: %q", bad)
		}
	}
}

func TestHostAllowed(t *testing.T) {
	if !hostAllowed("127.0.0.1:5000", 5000) || !hostAllowed("localhost:5000", 5000) {
		t.Fatal("loopback-хост має бути дозволений")
	}
	if hostAllowed("evil.example:5000", 5000) || hostAllowed("127.0.0.1:5001", 5000) {
		t.Fatal("чужий хост/порт має бути відхилений")
	}
}
