//go:build !windows

package main

import "time"

// systemIdleDuration — лише Windows; інакше простій не відстежуємо (WebKitGTK
// керує пам'яттю сам, а неактивність вікна відстежується за подією window:active).
func systemIdleDuration() (time.Duration, bool) { return 0, false }
