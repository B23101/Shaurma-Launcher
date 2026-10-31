package servers

import "testing"

// CheckRAM не повинен віднімати «резерв ОС» і RAM запущених серверів поверх
// вже вільної пам'яті (регресія: 4065 МБ вільно → «доступно 2565 МБ»).
func TestCheckRAMUsesRealFreeMemory(t *testing.T) {
	_, avail := getSystemRAM()
	if avail <= 0 {
		t.Skip("вільна пам'ять невідома на цій платформі")
	}
	r := CheckRAM(1024, 99999) // usedByServers не впливає на результат
	if r.Available != avail {
		t.Fatalf("Available=%d, очікували реально вільні %d", r.Available, avail)
	}
}
