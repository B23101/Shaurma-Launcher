package newupdate

import (
	"os"
	"testing"

	"shaurma-trust"
)

// TestEmbeddedFromLdflags — перевірка збірки: значення, вшиті через -ldflags -X
// (як це робить Pack Manager), справді доходять до змінних і проходять
// перевірку root. Запуск: SHAURMA_LDFLAGS_TEST=1 go test -ldflags "-X ..." .
// (неправильний шлях змінної linker мовчки ігнорує — тест це ловить).
func TestEmbeddedFromLdflags(t *testing.T) {
	if os.Getenv("SHAURMA_LDFLAGS_TEST") != "1" {
		t.Skip("лише для перевірки збірки з -ldflags")
	}
	if !Enabled() {
		t.Fatal("RootKeys не вшито")
	}
	raw, sig, err := embeddedKeyset()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trust.ParseKeyset(rootKeys(), raw, sig); err != nil {
		t.Fatalf("вшитий keyset не проходить root: %v", err)
	}
	if os.Getenv("SHAURMA_EXPECT_CERT") == "1" {
		if _, err := trust.ParseCert(rootKeys(), embeddedCert()); err != nil {
			t.Fatalf("вшитий cert: %v", err)
		}
	} else if len(embeddedCert()) != 0 {
		t.Fatal("cert вшито туди, де його не має бути")
	}
}
