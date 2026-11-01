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

## 2026-11-01 12:40
- [fix] Пресет-іконка збірки більше не «злітає»: записи збірки в instance.json тепер змінюються атомарно (Store.Update), а застарілий знімок не відкочує вигляд (ревізія Rev) || fix(custompack): make pack record updates atomic so preset icons are never reverted
- [fix] Автозбереження вигляду збірки фіксує реально надіслані значення; зміна кольору чи фону не видаляє завантажений PNG-іконку, а вибір іконки під час іншого збереження не губиться || fix(frontend): snapshot appearance on autosave and stop color changes from dropping the PNG icon
- [fix] Імпорт і оновлення збірки, тік прогресу, позначки заблокованих модів та синхронізація авто-збірок змінюють лише свої поля й не записують стару копію збірки || fix(backend): replace read-modify-write pack saves with atomic updates
- [perf] Завантаження модів .shaurmpack/.shaurmbundle: SHA-1 рахується під час завантаження, без повторного читання й копіювання через temp; перевірка розміру і хешу лишається повною || perf(shaurmpack): hash downloads while streaming and publish with an atomic rename
- [perf] Імпорт .shaurmpack: завантаження стартують паралельно з розпаковуванням overrides, великі файли качаються першими, спільний HTTP-клієнт з перевикористанням з'єднань || perf(shaurmpack): overlap downloads with extraction and reuse HTTP connections
- [perf] Store.Save більше не читає всі instance.json на кожен запис || perf(custompack): read only the target pack record on save
