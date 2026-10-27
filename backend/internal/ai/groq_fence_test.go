package ai

import "testing"

// TestStripFencePlain — текст без обгортки лишається без змін (найчастіший
// випадок: модель і так повертає чистий markdown, як просить промт).
func TestStripFencePlain(t *testing.T) {
	in := "## Назва мода\n**Що це:** опис.\n"
	if got := stripFence(in); got != in {
		t.Errorf("текст без ``` не мав змінитись, отримали: %q", got)
	}
}

// TestStripFenceWithLangLabel — типовий випадок обгортки з міткою мови:
// ```markdown\n...\n``` — має лишитись лише вміст без fence і без мітки.
func TestStripFenceWithLangLabel(t *testing.T) {
	in := "```markdown\n## Назва мода\nТекст туторіалу.\n```"
	want := "## Назва мода\nТекст туторіалу."
	if got := stripFence(in); got != want {
		t.Errorf("stripFence(%q) = %q, want %q", in, got, want)
	}
}

// TestStripFenceBare — обгортка без мітки мови (просто ```).
func TestStripFenceBare(t *testing.T) {
	in := "```\n## Назва\nТекст.\n```"
	want := "## Назва\nТекст."
	if got := stripFence(in); got != want {
		t.Errorf("stripFence(%q) = %q, want %q", in, got, want)
	}
}

// TestStripFenceKeepsInnerCodeBlock — код-блок ВСЕРЕДИНІ легітимного
// туторіалу (наприклад приклад команди) не має постраждати: fence
// знімається лише якщо він обрамляє ВЕСЬ текст.
func TestStripFenceKeepsInnerCodeBlock(t *testing.T) {
	in := "## Як встановити\nНатисніть «Встановити».\n\n```\n/give @s minecraft:diamond\n```\n\nГотово."
	if got := stripFence(in); got != in {
		t.Errorf("внутрішній код-блок не мав постраждати, отримали: %q", got)
	}
}

// TestStripFenceNoTrailingCloser — відповідь обрізана лімітом токенів і
// закриваючих ``` немає: знімаємо лише відкриваючий fence + мітку мови,
// а не повертаємо порожньо чи оригінал з зайвим сміттям на початку.
func TestStripFenceNoTrailingCloser(t *testing.T) {
	in := "```markdown\n## Назва\nТекст без кінця обірвався тут"
	want := "## Назва\nТекст без кінця обірвався тут"
	if got := stripFence(in); got != want {
		t.Errorf("stripFence(%q) = %q, want %q", in, got, want)
	}
}
