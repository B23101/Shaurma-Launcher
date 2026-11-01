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

## 2026-11-01 21:55
- [ci] Додано workflow GitHub Actions build-windows: повна збірка Windows-лаунчера (shell, bootstrap, backend.dll, updater.dll, setup.exe) на GitHub із секретів, артефакти і чернетка релізу за тегом vX.Y.Z (зміни в .github/ — на окреме рев'ю) || add build-windows github actions workflow for full windows launcher build
- [build] Додано build-github.ps1/.bat: запуск збірки на GitHub, очікування результату і завантаження інсталяторів || add launcher script to run the github build and download installers

## 2026-11-01 22:05
- [ci] Збірка на GitHub більше не залежить від pack-manager: workflow викликає scripts/build-windows.ps1, що збирає лише з публічних тек (зміни в .github/ і scripts/ — на окреме рев'ю) || build windows launcher in ci from public sources without pack-manager
- [build] Додано scripts/build-windows.ps1: frontend, shell, bootstrap, backend.dll, updater.dll, launcher-version.json і setup.exe з секретів середовища || add standalone windows build script

## 2026-11-01 23:10
- [feat] Pack Manager після локальної збірки і пушу коду сам запускає збірку на GitHub (workflow_dispatch) для цієї версії; у формі збірки додано галочку || trigger github build for the same version from pack manager after local build and push
- [ci] Workflow build-windows більше не стартує від тегу v*: лише workflow_dispatch з Pack Manager або вручну; чернетка релізу створюється за версією з inputs (зміни в .github/ — на окреме рев'ю) || run build-windows only via workflow_dispatch and create release draft from version input
- [build] Додано scripts/sync-github-secrets.ps1: читає build-secrets.json Pack Manager і виставляє GitHub Actions secrets через gh без ручного вводу (зміни в scripts/ — на окреме рев'ю) || add script to sync pack manager secrets to github actions secrets
