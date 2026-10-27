//go:build !windows

package minecraft

// gpu_other.go — no-op версія setDedicatedGPU для не-Windows платформ
// (лаунчер Windows-only, але пакет має збиратись у будь-якому середовищі,
// зокрема для тестів на Linux/macOS).

func setDedicatedGPU(javaPath string) error { return nil }
