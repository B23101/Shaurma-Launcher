package main

import (
	"strings"
	"testing"
)

// Тест виконує init() пакета main (включно з усіма regexp.MustCompile).
// Регресія: 2026-08-09 — smdItalicRe містив lookahead `(?=\s|$)`, який RE2
// не підтримує; паніка ставалась у init() і backend.dll тихо не
// завантажувався (лаунчер закривався одразу після старту). Цей тест
// тримає всі package-level regex-и під перевіркою.
func TestStripMarkdownHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		// smdMDRe прибирає жирний/підкреслений/закреслений/код ЦІЛКОМ
		// (разом з маркерами) — це давня поведінка, не регресія.
		{"**bold text**", ""},
		{"__under__", ""},
		{"~~strike~~", ""},
		{"`code`", ""},
		// Одинарний курсив *text* — лишається сам текст (фікс lookahead).
		{"*italic*", "italic"},
		{"text *it* end", "text it end"},
		{"[link](https://example.com)", ""},
		{"![img](https://example.com/a.png)", ""},
		{"<b>tag</b> &amp; text", "tag   text"},
		{"# Header", "Header"},
		{"- item one\n- item two", "• item one\n• item two"},
		// Список "* item" не з'їдається курсивом (закриваючої * немає),
		// маркер перетворюється на буллет наприкінці функції ("* " → "• ").
		{"* item", "• item"},
		{"plain text", "plain text"},
		// Порожні рядки стискаються.
		{"a\n\n\n\nb", "a\n\nb"},
	}
	for _, c := range cases {
		got := stripMarkdownHTML(c.in)
		if got != c.want {
			t.Errorf("stripMarkdownHTML(%q) = %q, очікувано %q", c.in, got, c.want)
		}
	}
	// Ініціалізація з реальним markdown-описом (з різними конструкціями)
	// не має падати.
	long := "## Title\n\n**Bold** and *italic* with [link](https://x.com)\n\n- a\n- b\n\n```code```"
	if out := stripMarkdownHTML(long); !strings.Contains(out, "Title") {
		t.Errorf("довгий опис обрізав заголовок: %q", out)
	}
}
