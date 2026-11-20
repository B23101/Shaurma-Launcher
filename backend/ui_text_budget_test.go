package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"unicode/utf8"
)

// Бюджет довжини підказок інтерфейсу (див. frontend/UI_RULES.md): надто довгий
// пояснювальний текст — це шум. Тест падає, якщо хтось додає розлогий опис.
var uiHintKey = regexp.MustCompile(`(Desc|Hint|\.hint|\.desc|Sub|Note|Intro|\.intro)$`)

func TestUITextBudget(t *testing.T) {
	const maxHint = 140 // підказка до рядка налаштування, опис, інтро
	const maxProp = 80  // props.<ключ>.hint (server.properties)
	for _, lang := range []string{"uk", "en"} {
		p := filepath.Join("..", "frontend", "src", "lib", "langs", lang+".json")
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Skipf("немає %s: %v", p, err)
		}
		var d map[string]any
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		for k, v := range d {
			s, ok := v.(string)
			if !ok {
				continue
			}
			n := utf8.RuneCountInString(s)
			if len(k) > 6 && k[:6] == "props." && len(k) > 5 && k[len(k)-5:] == ".hint" {
				if n > maxProp {
					t.Errorf("[%s] %s: %d символів (макс %d) — скоротіть", lang, k, n, maxProp)
				}
				continue
			}
			if uiHintKey.MatchString(k) && n > maxHint {
				t.Errorf("[%s] %s: %d символів (макс %d) — скоротіть", lang, k, n, maxHint)
			}
		}
	}
}
