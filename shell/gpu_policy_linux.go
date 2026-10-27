//go:build linux

package main

// gpu_policy_linux.go — узгодження GPU-політики webview з env-фолбеком
// (див. webview_compat_linux.go). Коротко: на Linux GTK4/WebKitGTK 6.0
// Wails за замовчуванням виставляє WEBKIT_HARDWARE_ACCELERATION_POLICY_
// ALWAYS — тобто «прискорення увімкнене ЗАВЖДИ». Якщо в оточенні
// робочого GPU нема (WSL2, headless, VNC) env-фолбек примусово переводить
// рендеринг у софтверний (WEBKIT_DISABLE_COMPOSITING_MODE=1,
// LIBGL_ALWAYS_SOFTWARE=1) — але сама політика ALWAYS лишалась в силі, і
// WebKit далі намагався підняти GPU-шлях там, де драйвера нема. Саме це
// давало «лаунчер гальмує/фрізить/біле вікно» на Linux: замість одного
// софтверного шляху WebKit пробує обидва.
//
// Тому політика тепер дзеркалить ту саму перевірку, що й env-фолбек:
// нема робочого GPU → WebviewGpuPolicyNever (чистий софтверний шлях).

// noWorkingGpu — чи треба вимикати апаратне прискорення webview.
func noWorkingGpu() bool {
	return !hasWorkingGPU()
}
