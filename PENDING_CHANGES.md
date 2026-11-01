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

## 2026-11-01 01:17
- [fix] Ігрову авторизацію спрямовано з локального проксі напряму до Mojang; закрито пересилання невідомих запитів і перенаправлення авторизації || route game authentication directly to mojang and reject unsafe proxy routes
- [fix] При відмові локальних скінів ліцензійна гра запускається через Mojang, а прямий шлях Shaurma дозволено лише офлайн-акаунтам без токенів || restrict direct skin fallback to tokenless offline accounts
- [fix] Завершений локальний сервер скінів перестворюється при наступному запуску гри; додано перевірки маршрутів і відмов || restart stopped local skin proxies and test credential routing failures
- [docs] Оновлено пояснення маршрутизації скінів і сесій у коді та політиці приватності || document local skin routing and official session authentication
