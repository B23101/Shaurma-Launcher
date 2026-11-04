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

## 2026-11-04 23:40
- [build] Виправлено помилку парсингу PowerShell у build-windows.ps1 (змінна з двокрапкою в рядку) || fix powershell parse error in manifest validation
- [fix] Деінсталятор більше ніколи не видаляє папку даних .shaurm, shaurma.db та налаштування || never delete user data folder on uninstall
- [fix] Додано захист safedel: корінь даних, теки збірок і серверів неможливо видалити з коду лаунчера || add safedel guard for data roots
- [fix] Міграція папки даних прибирає стару теку лише якщо вона порожня || remove old data dir after migration only when empty
