//go:build !linux && !windows

package shaurmaid

// На інших ОС сильного джерела немає: HWID не обчислюється (акаунт Шаурми недоступний).
func collectHWIDParts() map[string]string { return map[string]string{} }
