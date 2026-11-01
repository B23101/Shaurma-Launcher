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
## 2026-11-01 23:10
- [feat] Pack Manager після локальної збірки і пушу коду сам запускає збірку на GitHub (workflow_dispatch) для цієї версії; у формі збірки додано галочку || trigger github build for the same version from pack manager after local build and push
- [ci] Workflow build-windows більше не стартує від тегу v*: лише workflow_dispatch з Pack Manager або вручну; чернетка релізу створюється за версією з inputs (зміни в .github/ — на окреме рев'ю) || run build-windows only via workflow_dispatch and create release draft from version input
- [build] Додано scripts/sync-github-secrets.ps1: читає build-secrets.json Pack Manager і виставляє GitHub Actions secrets через gh без ручного вводу (зміни в scripts/ — на окреме рев'ю) || add script to sync pack manager secrets to github actions secrets

## 2026-11-01 23:25
- [fix] Оновлено frontend/package-lock.json: синхронізовано з package.json, щоб npm ci не падав у CI || sync frontend package-lock with package.json so npm ci passes in ci

## 2026-11-01 23:35
- [ci] Відновлено оригінальний scripts/build-windows.ps1 (його перезаписав новий скрипт і зламав test-build-policy та release workflows); новий скрипт перейменовано на build-windows-standalone.ps1, workflow build-windows викликає його (зміни в .github/ і scripts/ — на окреме рев'ю) || restore original build-windows.ps1 and move standalone ci build to build-windows-standalone.ps1

## 2026-11-01 23:45
- [fix] Дозволено в git лише backend/build/appicon.png (його вбудовує tray_menu.go через go:embed), решта backend/build лишається ігнорованою; без нього go test у CI падав (змінено .gitignore — на окреме рев'ю) || track backend/build/appicon.png needed by go:embed so ci tests build

## 2026-11-01 23:55
- [ci] У кроці gitleaks додано -v, щоб у логу було видно файл, коміт і правило знахідки (секрет лишається замаскованим) (зміни в .github/ — на окреме рев'ю) || print gitleaks finding details in ci log with redacted secrets
