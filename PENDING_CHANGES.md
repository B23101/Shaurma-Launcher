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

## 2026-11-06 14:40
- [perf] Група відкривається швидше: перша сторінка історії — 30 повідомлень замість 50, старіші підвантажуються при прокрутці вгору || load 30 messages on group open and fetch older on scroll
- [perf] Історія групи більше не вантажиться двічі при відкритті, а наведення чи натискання на групу вантажить її наперед || prefetch group history on hover and drop duplicate history request
- [perf] Порядок груп після підключення звіряється лише для груп, де змінився id останнього повідомлення || verify group order only for groups whose last message id changed
- [fix] Список чатів не показується в тимчасовому порядку й не «стрибає» після завантаження: до готовності порядку видно заготовки || hold chat list until order is known to avoid reordering after load

## 2026-11-06 15:05
- [feat] Лаунчер читає з сервера час останнього повідомлення кожного чату й груп і одразу впорядковує список без запитів історії || read last message time of chats and groups from the server snapshot
- [perf] Після підключення історія чатів більше не опитується по одному: запит лише для діалогів, яких сервер не знає || skip per-chat history requests when the server provides chat activity
