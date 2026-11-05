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

## 2026-11-05 11:33
- [refactor] Групові чати тепер показуються тим самим чатом, що й особисті (ShaurmaChat): ті самі бульбашки, анімації, меню, відповіді, реакції, закріплені й прокрутка; окремий GroupChat зі своїм стилем прибрано || refactor(ui): render group chats with the shared chat component
- [refactor] Джерело даних чату винесено в ChatApi (friends/chatApi.ts): особистий чат іде через App як і раніше, груповий — через groups/groupChatApi.ts || refactor(ui): add ChatApi adapter for private and group chats
