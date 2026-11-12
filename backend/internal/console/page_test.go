package console

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"
)

func TestPageWindowsAndFilters(t *testing.T) {
	b := NewBuffer(1000)
	for i := 0; i < 1000; i++ {
		if i%10 == 0 {
			b.AppendLevel("error", "needle")
		} else {
			b.AppendLevel("info", "normal")
		}
	}
	p := b.Page(-1, 100, "all", "", 0)
	if p.Total != 1000 || p.Offset != 900 || len(p.Lines) != 100 || p.Errors != 100 {
		t.Fatalf("tail: %+v", p)
	}
	p = b.Page(0, 20, "error", "NEEDLE", 0)
	if p.Total != 100 || len(p.Lines) != 20 || p.Lines[1].ID != 11 {
		t.Fatalf("filter: %+v", p)
	}
	p = b.Page(0, 99999, "all", "", 900)
	if p.Count != 100 || len(p.Lines) != 100 || p.Lines[0].ID != 901 {
		t.Fatalf("clear cutoff: %+v", p)
	}
	p.Lines[0].Text = "changed"
	if b.Page(0, 1, "all", "", 900).Lines[0].Text == "changed" {
		t.Fatal("page mutated buffer")
	}
	if len(b.Page(0, 99999, "all", "", 0).Lines) != 500 {
		t.Fatal("missing page cap")
	}
	b.SetMaxLines(50)
	p = b.Page(99999, 100, "all", "", 0)
	if p.Total != 50 || p.Offset != 0 || len(p.Lines) != 50 {
		t.Fatalf("trim: %+v", p)
	}
	b.Clear()
	if p = b.Page(-1, 100, "all", "", 0); p.Total != 0 || len(p.Lines) != 0 {
		t.Fatal("clear")
	}
}

func TestRepeatedTextSharesStorageAndPoolIsBounded(t *testing.T) {
	b := NewBuffer(100)
	a := b.AppendLevel("info", strings.Clone("repeated message"))
	c := b.AppendLevel("info", strings.Clone("repeated message"))
	if unsafe.StringData(a.Text) != unsafe.StringData(c.Text) {
		t.Fatal("duplicate string storage")
	}
	for i := 0; i < 200; i++ {
		b.Append(fmt.Sprint(i))
	}
	if len(b.textPool) > 32 {
		t.Fatal("unbounded dictionary")
	}
	b.Clear()
	if len(b.textPool) != 0 {
		t.Fatal("dictionary retained after clear")
	}
}
