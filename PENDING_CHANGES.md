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

## 2026-11-04 22:04
- [fix] Відкриття зовнішніх посилань сайту в новій вкладці || open external site links in a new tab
- [fix] Обмежено адреси API для запитів зі сторінок сайту || restrict site connections to public API endpoints
- [feat] Додано пошукові назви Шаурма й Shaurma на головних сторінках || add localized brand names and search metadata

## 2026-11-04 22:07
- [fix] Показано логотип Java зі збірки Devicon (MIT) під час перевірки Java || show the Java logo during runtime checks

## 2026-11-04 22:07
- [fix] Перевірено оновлення без токена через локальну відповідь сервера || test tokenless updates with a local signal server
- [fix] Додано виведення журналу невдалих кроків CI у новому потоці збірки || show failed CI step logs in the new build flow
