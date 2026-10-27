//go:build windows

package minecraft

// gpu_windows.go — «Use dedicated GPU» (налаштування лаунчера).
//
// На ноутбуках з двома графіками (Intel iGPU + NVIDIA/AMD dGPU) Windows за
// замовчуванням запускає Java на енергоефективній вбудованій — Minecraft
// тоді видає низький FPS. Перемикач «Використовувати дискретну графіку»
// записує той самий запис, який робить Windows «Параметри → Система →
// Дисплей → Графіка → Висока продуктивність»: реєстрове значення
// GpuPreference=2 для конкретного .exe у
// HKCU\Software\Microsoft\DirectX\UserGpuPreferences.
//
// GpuPreference значення: 0 = системний дефолт, 1 = енергоефективна,
// 2 = висока продуктивність (дискретна). Застосовується ДО запуску гри —
// Windows читає запис при старті процесу, на льоту працює не завжди.

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const gpuPrefsKey = `Software\Microsoft\DirectX\UserGpuPreferences`

// setDedicatedGPU позначає javaPath (повний шлях до .exe) як «висока
// продуктивність». Тиха no-op для порожнього шляху чи відносного
// «java» (LookPath-резолв відбувається пізніше в Launch). На системах
// з однією GPU цей запис просто ігнорується Windows — шкоди немає, тому
// перемикач вмикається за замовчуванням завжди, без детекції кількості GPU.
func setDedicatedGPU(javaPath string) error {
	if javaPath == "" || javaPath == "java" {
		return nil
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, gpuPrefsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("gpu pref key: %w", err)
	}
	defer k.Close()
	// Формат значення: "GpuPreference=2;" (з крапкою з комою — як пише сама
	// Windows у «Параметрах графіки»).
	if err := k.SetStringValue(javaPath, "GpuPreference=2;"); err != nil {
		return fmt.Errorf("gpu pref value: %w", err)
	}
	return nil
}
