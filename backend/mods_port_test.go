package main

import "testing"

// Сторона мода при портуванні на сервер: явне environment у jar важливіше
// за Modrinth, «*»/порожнє уточнюється за server_side, решта — на вибір
// користувача.
func TestModSide(t *testing.T) {
	cases := []struct {
		env, serverSide, want string
	}{
		{"client", "required", modSideClient},
		{"server", "unsupported", modSideServer},
		{"*", "unsupported", modSideClient},
		{"", "optional", modSideServer},
		{"*", "required", modSideServer},
		{"*", "unknown", modSideUnknown},
		{"", "", modSideUnknown},
		{" Client ", "", modSideClient},
	}
	for _, c := range cases {
		if got := modSide(c.env, c.serverSide); got != c.want {
			t.Errorf("modSide(%q, %q) = %q, очікували %q", c.env, c.serverSide, got, c.want)
		}
	}
}

func TestIsServerModTarget(t *testing.T) {
	for id, want := range map[string]bool{
		"srv:abc": true, "srvp:abc": true, "srv:": true,
		"my-pack": false, "": false, "server-pack": false,
	} {
		if got := isServerModTarget(id); got != want {
			t.Errorf("isServerModTarget(%q) = %v, очікували %v", id, got, want)
		}
	}
}
