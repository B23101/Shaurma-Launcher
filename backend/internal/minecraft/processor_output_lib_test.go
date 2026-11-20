package minecraft

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestIsProcessorOutputLibrary(t *testing.T) {
	cases := map[string]bool{
		"net.minecraftforge:forge:26.3-66.1.9:client":      true,
		"net.minecraftforge:forge:1.20.1-47.2.0:slim":      true,
		"net.neoforged:neoforge:21.1.95:client":            true,
		"net.neoforged:forge:1.20.1-47.1.106:client":       true,
		"net.minecraftforge:forge:1.20.1-47.2.0:universal": false,
		"net.minecraftforge:forge:1.12.2-14.23.5.2860":     false,
		"net.minecraftforge:forge:1.7.10-10.13.4.1614@zip": false,
		"com.example:lib:1.0:client":                       false,
	}
	for name, want := range cases {
		if got := isProcessorOutputLibrary(name); got != want {
			t.Errorf("isProcessorOutputLibrary(%q) = %v, want %v", name, got, want)
		}
	}
}

// ensureLibraries не має качати forge-*-client.jar (його створюють processors):
// раніше це давало "library net.minecraftforge:forge:26.3-66.1.9:client: HTTP 404".
func TestEnsureLibrariesSkipsProcessorOutput(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	inst := NewInstaller(filepath.Join(t.TempDir(), "mc"))
	vj := &VersionJSON{ID: "26.3-forge-66.1.9"}
	vj.Libraries = []LibraryJSON{{
		Name: "net.minecraftforge:forge:26.3-66.1.9:client",
		URL:  srv.URL,
	}}
	if err := inst.ensureLibraries(vj); err != nil {
		t.Fatalf("ensureLibraries: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("очікував 0 запитів до maven, отримав %d", hits.Load())
	}
}

// Після обірваної установки на диску лежать universal/shim jar (їх качає
// ensureLibraries), але client.jar (його створюють processors) немає.
// checkForgeInstalled НЕ має вважати такий Forge встановленим — інакше
// processors не запускаються і гра падає з ".forge_patched_minecraft".
func TestCheckForgeInstalledRequiresProcessorOutput(t *testing.T) {
	baseDir := t.TempDir()
	inst := NewInstaller(baseDir)

	id := "26.3-forge-66.1.9"
	vdir := filepath.Join(baseDir, "versions", id)
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	vjson := `{"id":"` + id + `","inheritsFrom":"26.3","libraries":[` +
		`{"name":"net.minecraftforge:forge:26.3-66.1.9:universal","downloads":{"artifact":{"path":"net/minecraftforge/forge/26.3-66.1.9/forge-26.3-66.1.9-universal.jar","url":"x"}}},` +
		`{"name":"net.minecraftforge:forge:26.3-66.1.9:client","downloads":{"artifact":{"path":"net/minecraftforge/forge/26.3-66.1.9/forge-26.3-66.1.9-client.jar","url":""}}}]}`
	if err := os.WriteFile(filepath.Join(vdir, id+".json"), []byte(vjson), 0o644); err != nil {
		t.Fatal(err)
	}
	libDir := filepath.Join(baseDir, "libraries", "net", "minecraftforge", "forge", "26.3-66.1.9")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "forge-26.3-66.1.9-universal.jar"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := inst.checkForgeInstalled("26.3", "66.1.9", false); ok {
		t.Fatal("client.jar відсутній, а checkForgeInstalled вважає Forge встановленим")
	}
}
