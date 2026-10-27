package main

// bridge_strings_test.go — регресійний тест межі C ABI (cstrPtr), перенесений
// без змін з видаленого bridge_host_test.go: cstrPtr/goStr тепер живуть у
// bridge_strings.go (платформонезалежний файл, спільний для Windows і
// Linux), тому й тест лишається без build tag.
//
// БАГ: strPtr повертав unsafe.StringData(original) — вказівник на дані
// Go-рядка БЕЗ гарантованого завершального NUL. DLL читає рядки через
// C.GoString (до першого '\0'), тож метод "IsFirstRun" міг дочитатись як
// "IsFirstRun" + сусідній вміст купи (фрагменти JS-бандла) — звідси
// «метод не знайдено: X» майже на кожному виклику (shaurm-invoke.log
// показував method з кілобайтами сміття). cstrPtr робить NUL-завершену
// КОПІЮ, а викликач тримає її живою через runtime.KeepAlive.

import (
	"runtime"
	"testing"
	"unsafe"
)

// goCString імітує семантику C.GoString: читає байти з ptr до першого NUL.
func goCString(ptr unsafe.Pointer) string {
	if ptr == nil {
		return ""
	}
	var out []byte
	for i := 0; ; i++ {
		c := *(*byte)(unsafe.Pointer(uintptr(ptr) + uintptr(i)))
		if c == 0 {
			return string(out)
		}
		out = append(out, c)
	}
}

// TestCstrPtrNulTerminated — для будь-якого рядка (включно з не-ASCII і
// такими, що закінчуються цифрами — до NUL може залізти байт цифри з
// сусідньої алокації) C.GoString-читач має отримати РІВНО вихідний рядок,
// а байт за даними — нуль. Це і є контракт межі shell -> backend.dll/so.
func TestCstrPtrNulTerminated(t *testing.T) {
	cases := []string{
		"IsFirstRun",
		"__dispatch",
		"ListPinnedServers",
		"GetBrowserProjectVersions",
		"StartMSLogin",
		"AnalyzeUnusedJava",
		"метод-з-кирилицею",
		"x",
		"",
	}
	for _, s := range cases {
		ptr, keep := cstrPtr(s)
		if s == "" {
			if ptr != nil {
				t.Fatalf("cstrPtr(%q): очікувано nil для порожнього рядка", s)
			}
			runtime.KeepAlive(keep)
			continue
		}
		got := goCString(ptr)
		if got != s {
			t.Fatalf("cstrPtr(%q): C.GoString-читач отримав %q (зсув/зайві байти!)", s, got)
		}
		// Байт РІВНО за даними мусить бути NUL — інакше межовий читач
		// вийшов би за рядок (корінь бага «метод не знайдено»).
		b := unsafe.Slice((*byte)(ptr), len(s)+1)
		if b[len(s)] != 0 {
			t.Fatalf("cstrPtr(%q): байт[%d] не NUL (0x%02x) — рядок не NUL-завершений", s, len(s), b[len(s)])
		}
		runtime.KeepAlive(keep)
	}
}

// TestCstrPtrIsolatedCopies — дві послідовні алокації не мають «протікати»
// одна в одну: кожна копія незалежна від купи.
func TestCstrPtrIsolatedCopies(t *testing.T) {
	a, aKeep := cstrPtr("IsFirstRun")
	b, bKeep := cstrPtr("GetBrowserProjectVersions")
	defer runtime.KeepAlive(aKeep)
	defer runtime.KeepAlive(bKeep)
	if goCString(a) != "IsFirstRun" {
		t.Fatalf("перший рядок пошкоджено: %q", goCString(a))
	}
	if goCString(b) != "GetBrowserProjectVersions" {
		t.Fatalf("другий рядок пошкоджено: %q", goCString(b))
	}
}
