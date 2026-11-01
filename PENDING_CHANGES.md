# PENDING CHANGES

<!--
ЦЕЙ ФАЙЛ ВЕДЕ НЕЙРОНКА. Кожна зміна проєкту = новий пункт тут (правило з AGENTS.md).
Pack Manager після успішної збірки сам зробить git commit + push, візьме звідси
текст для commit-повідомлення і ОЧИСТИТЬ відправлені пункти. Нічого руками не чисти.

Формат: новий блок "## YYYY-MM-DD HH:MM" внизу, під ним пункти:
  - [тип] Опис українською || English commit text (необов'язково)
Типи: feat, fix, perf, refactor, docs, build, ci, chore.
Пункт = одна змістовна зміна, одним рядком, без секретів і без згадок ШІ.
-->

## 2026-11-01 18:30

- [build] Додано локальну повну збірку з інсталятором Inno Setup без CDN: `scripts/build-local-full.ps1` і режим `-LocalInstall` у `scripts/build-windows.ps1` || add local full build with an offline Inno Setup installer
- [build] Токен Shaurma вшивається лише в backend локальної збірки, апдейтер і shell без токена, оновлення вимикає `local-only.flag` || embed the shaurma token only in the local backend and keep updater and shell token-free
- [build] `installer/installer.iss` отримав умовну гілку `LocalInstall`, стандартний bootstrap-інсталятор не змінено || add a conditional LocalInstall branch to the Inno Setup script without changing the default installer
- [ci] Додано workflow `release-full`: повна збірка та реліз за версією X.Y.Z від Pack Manager || add a full-release workflow for complete X.Y.Z versions dispatched by Pack Manager
- [docs] Описано локальну повну збірку та повний реліз у `README.md` || document the local full build and full release in README.md
- [fix] `collect-licenses.ps1` більше не падає у Windows PowerShell 5.1 на масиві npm-пакетів з `ConvertFrom-Json` || fix collect-licenses failing on the npm package array under Windows PowerShell 5.1
- [build] Локальна збірка пропускає перевірку PE-метаданих SignPath, релізна — без змін || skip the SignPath PE metadata check for local builds only

## 2026-11-01 18:00

- [fix] Bootstrap більше не падає на «TLS handshake timeout» при отриманні маніфесту: довший TLS-таймаут, до 4 спроб із паузою, останні — лише через IPv4 || fix bootstrap failing with tls handshake timeout by raising the timeout and retrying, falling back to ipv4

- [fix] Збірка лаунчера більше не падає на «Access is denied» при розпакуванні updater-frontend/frontend у staging: повтори видалення й перейменування, запасне копіювання || fix staging build failing with access denied when renaming the unpacked frontend dir by retrying and falling back to copy
- [chore] Видалено internal/deploy/page_design.go (і його тест), який не збирався || remove the broken internal/deploy/page_design.go and its test
