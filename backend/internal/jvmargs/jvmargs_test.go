package jvmargs

import (
	"reflect"
	"testing"
)

func TestLevels(t *testing.T) {
	// arg, blockedForPack (A), userAllowed (B)
	cases := []struct {
		arg  string
		a, b bool
	}{
		{"-javaagent:x.jar", true, false},
		{"-JavaAgent:x.jar", true, false}, // без урахування регістру
		{"-agentlib:jdwp=transport=dt_socket", true, false},
		{"-agentpath:/x.so", true, false},
		{`-XX:OnError="calc"`, true, false},
		{"-XX:OnOutOfMemoryError=calc", true, false},
		{"-XX:VMOptionsFile=x", true, false},
		{"-Xbootclasspath/a:x", true, false},
		{"-cp", true, false},
		{"-classpath", true, false},
		{"-p", true, false},
		{"--module-path=x", true, false},
		{"-jar", true, false},
		{"@file", false, false},
		{"net.minecraft.Main", false, false},
		{"-Dlog4j2.configurationFile=http://evil/x.xml", true, false},
		{"-Djava.library.path=C:\\x", true, false},
		{"-Dfabric.addMods=x", true, false},
		{"-Xshare:dump", true, false},
		{"-Xlog:gc:file=x", true, false},
		{"-Xmx4G", false, true},
		{"-Xms512m", false, true},
		{"-Xmx4G;calc", false, false},
		{"-Xmx", false, false},
		{"-XX:+UseG1GC", false, true},
		{"-XX:-UseG1GC", false, true},
		{"-XX:+SomethingUnknown", false, false},
		{"-XX:MaxGCPauseMillis=50", false, true},
		{"-XX:MaxGCPauseMillis=../x", false, false},
		{"-XX:G1HeapRegionSize=8M", false, true},
		{"-XX:MaxRAMPercentage=75.0", false, true},
		{"-Dfml.readTimeout=30", false, true},
		{"-Dfile.encoding=UTF-8", false, true},
		{"-Dusing.aikars.flags=https://mcflags.emc.gs", false, true},
		{"-Dfml.readTimeout=C:\\x", false, false},
		{"-Dunknown.prop=1", false, false},
		{"--add-opens=java.base/java.lang=ALL-UNNAMED", false, true},
		{"--add-opens=java.base/java.lang=ALL-UNNAMED;x", false, false},
		{"--enable-native-access=ALL-UNNAMED", false, true},
		{"--sun-misc-unsafe-memory-access=allow", false, true},
		{"-server", false, true},
		{"", false, false},
	}
	for _, c := range cases {
		if got := BlockedForPack(c.arg); got != c.a {
			t.Errorf("BlockedForPack(%q)=%v, want %v", c.arg, got, c.a)
		}
		if got := UserAllowed(c.arg); got != c.b {
			t.Errorf("UserAllowed(%q)=%v, want %v (%s)", c.arg, got, c.b, userReason(c.arg))
		}
	}
}

func TestSanitizeUser(t *testing.T) {
	kept, rej := SanitizeUser("-Xmx4G -javaagent:evil.jar -XX:+UseG1GC -XX:OnError=calc " +
		"--add-opens java.base/java.lang=ALL-UNNAMED --add-opens bad -Dx=1")
	wantKept := []string{"-Xmx4G", "-XX:+UseG1GC", "--add-opens", "java.base/java.lang=ALL-UNNAMED"}
	if !reflect.DeepEqual(kept, wantKept) {
		t.Errorf("kept=%v", kept)
	}
	if len(rej) != 4 {
		t.Errorf("rejected=%v", rej)
	}
	for _, r := range rej {
		if r.Reason == "" {
			t.Errorf("empty reason for %q", r.Arg)
		}
	}
	if k, r := SanitizeUser("   "); len(k) != 0 || len(r) != 0 {
		t.Errorf("empty input: %v %v", k, r)
	}
}

func TestFilterPack(t *testing.T) {
	in := []string{"-Dfoo=bar", "-javaagent:x.jar", "-XX:OnError=calc", "--server", "play.example.com",
		"-Xbootclasspath/a:x", "@args.txt", "  ", "-Xmx2G"}
	kept, rej := FilterPack(in)
	want := []string{"-Dfoo=bar", "--server", "play.example.com", "-Xmx2G"}
	if !reflect.DeepEqual(kept, want) {
		t.Errorf("kept=%v", kept)
	}
	if len(rej) != 4 {
		t.Errorf("rejected=%v", rej)
	}
}
