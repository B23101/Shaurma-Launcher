package main

import "testing"

func TestCFEnvOfGameVersions(t *testing.T) {
	cases := []struct {
		name string
		gv   []string
		want cfFileEnv
	}{
		{"no tags", []string{"Fabric", "1.20.1"}, cfEnvUnknown},
		{"client only", []string{"Client", "Fabric", "1.20.1"}, cfEnvClientOnly},
		{"server only", []string{"Server", "Fabric", "1.20.1"}, cfEnvServerCapable},
		{"both", []string{"Client", "Server", "Fabric", "1.20.1"}, cfEnvServerCapable},
		{"case+space", []string{" client "}, cfEnvClientOnly},
		{"empty", nil, cfEnvUnknown},
	}
	for _, c := range cases {
		if got := cfEnvOfGameVersions(c.gv); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestCFProjectIsClientOnly(t *testing.T) {
	client := []string{"Client", "Fabric", "1.20.1"}
	both := []string{"Client", "Server", "Fabric", "1.20.1"}
	none := []string{"Fabric", "1.20.1"}

	if cfProjectIsClientOnly(nil) {
		t.Error("no files must NOT be client-only (fail-open)")
	}
	if !cfProjectIsClientOnly([][]string{client, client}) {
		t.Error("all files Client-only -> client-only")
	}
	if cfProjectIsClientOnly([][]string{client, both}) {
		t.Error("one file with Server -> keep the mod")
	}
	if cfProjectIsClientOnly([][]string{client, none}) {
		t.Error("one untagged file -> not proven client-only, keep")
	}
	if cfProjectIsClientOnly([][]string{none, none}) {
		t.Error("untagged mod must be kept")
	}
}
