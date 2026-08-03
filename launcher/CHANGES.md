# Зміни в launcher порівняно з вихідним shaurma-launcher-source.zip

## 1. Іконка (main.go, build/windows/icon.ico)

**Було:** іконки не було ні на exe, ні в панелі задач, ні у вікні —
`build/` містив лише `appicon.png`, `options.App` не мав поля `Icon`.

**Стало:**
- `build/windows/icon.ico` — покладено готовий .ico з повним набором
  розмірів (16–256px). Wails при `wails build` сам вшиває його в
  ресурси exe — це дає іконку в провіднику/панелі задач/Alt-Tab.
- `main.go`: `//go:embed build/appicon.png` → `Icon: iconPNG` в
  `options.App` — це дає іконку самого вікна і трею під час роботи
  процесу (окремо від .ico, який відповідає лише за exe-файл на диску).

## 2. Крок майстра першого запуску більше не скидається

**Було:** `wizardStep` жив лише в пам'яті Svelte-компонента
(`$state` без персистенції). Закриття лаунчера на кроці 2 → наступний
запуск починав з кроку 1 знову.

**Стало:**
- `internal/model/types.go`: новий тип `SetupProgress{Step, InstanceDir,
  JavaPath, Language}`.
- `internal/config/config.go`: `SaveSetupProgress`/`LoadSetupProgress`
  — атомарний запис (`.tmp` + `rename`) у `setup-progress.json`,
  окремо від `settings.json`. `SetupComplete` видаляє цей файл після
  успішного завершення майстра.
- `app.go`: біндинги `GetSetupProgress`/`SaveSetupProgress` для фронтенду.
- `frontend/src/App.svelte`: `startWizard()` спершу питає бекенд про
  збережений прогрес і відновлює саме той крок з тими самими даними;
  `persistWizardStep()` викликається при кожному Далі/Назад/зміні поля.

## 3. Трей + коректна поведінка при закритті під час гри

**Було:** закриття вікна завжди вбивало весь процес — разом з ним
падала й запущена гра (все в одному процесі).

**Стало:**
- `tray.go` (новий файл): `onBeforeClose` — якщо гра запущена, ховає
  вікно (`runtime.WindowHide`) і піднімає системний трей замість
  реального закриття процесу. Трей: "Показати лаунчер" / "Вийти".
- `main.go`: підключено `OnBeforeClose: app.onBeforeClose`,
  `OnShutdown: app.shutdown`.
- `app.go`: `RestoreWindowAfterGame()` — викликається на подію
  `game:exit`, автоматично показує вікно назад, якщо воно було сховане.
- Залежність: `github.com/energye/systray` додано в `go.mod` (версія
  орієнтовна, `go mod tidy` підбере точну).

## Що НЕ чіпалось

Уся інша логіка (моди, збірки, акаунти, download-логіка) — оригінальний
код з `shaurma-launcher-source.zip`, без змін.
