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

## 2026-11-04 22:23
- [feat] Додано іконку, біо та колір профілю до вебзапрошення за згодою власника || include optional player portraits and profile styling in friend invites

## 2026-11-04 22:28
- [fix] Перенесено перевірку дозволу для вебзапрошень на VPS || resolve public invite profiles through server privacy settings
- [build] Додано локальні перевірки Go та фронтенду в scripts перед публікацією || verify launcher builds locally before committing a release
- [fix] Оформлено поля та журнал нового потоку Pack Manager || style the release form and build log

## 2026-11-04 23:10
- [fix] Виправлено маніфест Windows-лаунчера (ім'я assembly і версія a.b.c.d), через що exe не стартував з помилкою side-by-side; додано перевірку маніфесту в збірці та CI || fix launcher exe failing to start with side-by-side configuration error, validate manifest at build time
