//go:build !windows

package main

// memtrim_notrim.go — поза Windows робочі набори процесів не чіпаємо:
// на Linux пам'ять повертає malloc_trim + FreeOSMemory (memtrim_linux.go), а
// «притиснути» WebKitGTK-процеси ззовні без ривків UI неможливо (він керує
// своєю пам'яттю сам — див. шапку memtrim.go).

func trimProcessWorkingSets() int { return 0 }

func logWorkingSetTrim(int, uint64) {}
