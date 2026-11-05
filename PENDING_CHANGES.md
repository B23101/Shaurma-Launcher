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

## 2026-11-05 18:26
- [feat] Додано перевірку сервера та стану збірки перед входом із запрошення || check server and build readiness before joining friends
- [feat] Додано оборотні профілі графіки у розділ продуктивності || add reversible per-pack graphics presets
- [feat] Додано копії одиночних світів перед оновленням і відновлення окремими світами || protect local worlds with snapshots and restore independent copies

## 2026-11-05 18:37
- [fix] Вирівняно кнопку перевірки P2P у картці налаштувань || align the p2p connection check button within its settings card
- [fix] Запрошення на сервер надсилаються в обрані групові чати без розсилки учасникам у приватні || send server invitations to selected group chats instead of member direct messages
- [fix] Повторне групове запрошення до світу не блокується лише через уже наданий доступ || allow world invitations to groups whose members already have access

## 2026-11-05 18:40
- [fix] Захищено текстури гардероба від очищення кешу та фонового видалення, додано перенесення старих шляхів у постійне сховище || preserve wardrobe textures during cache cleanup and migrate legacy references
- [feat] Додано кнопку відкриття теки компонентів у розділі про лаунчер || add an open components folder button to launcher information

## 2026-11-05 18:43
- [fix] Скасовано перенесення текстур гардероба; залишено лише захист папки skins від очищення || keep skins protected from cleanup without migrating textures

## 2026-11-05 18:52
- [fix] Кількість серверів завантажується одразу при відкритті сторінки збірки без переходу у вкладку серверів || load the server count when opening a modpack page

## 2026-11-05 18:56
- [fix] Виправлено Ctrl+K для української розкладки та усунено конфлікт із пошуком сервера || fix global search shortcut across keyboard layouts and server pages

## 2026-11-05 19:10
- [fix] Кнопка «Тека компонентів» у розділі про лаунчер тепер відкриває теку на Windows || open the components folder with explorer on windows
- [refactor] Прибрано велику картку-заголовок у серверах збірки, пошук і фільтри зведено в один рядок як у вкладці контенту || compact pack servers toolbar to match the content tab
- [fix] Під-вкладки модів, ресурспаків, датапаків і шейдерів вирівняно по центру || center content sub-tabs

## 2026-11-05 19:40
- [perf] Моди CurseForge-модпаків резолвляться пакетними запитами замість окремого запиту на кожен мод || resolve curseforge modpack files in batches instead of one request per mod
- [fix] Етап отримання даних про моди більше не показується як окреме коло завантаження || show curseforge resolve as a manifest stage instead of a fake download pass
- [fix] Прогрес байтів при повторних спробах завантаження не рахується двічі || avoid double counting bytes when a mod download is retried

## 2026-11-05 20:15
- [refactor] Вікно приватності друзів переведено на спільні картки, рядки й перемикачі налаштувань лаунчера || use shared settings cards and switches in the friends privacy dialog
- [feat] Панель міграції модів оновлено в стилі лаунчера: єдиний список, помаранчеві акценти || restyle the mod migration panel to match the launcher
- [fix] Чат піднімається нагору одразу після надсилання, а початковий порядок застосовується одним кроком || reorder chats instantly on send and apply loaded activity in one step
- [fix] Клік у списку чатів не відкриває інший чат, якщо список переставився під час натискання || ignore chat list clicks when rows move between press and release
- [feat] Додано пружний відгук і хвилю на кліки та анімацію перемикання вкладок з вимкненням у режимі продуктивності || add spring press feedback, ripples and tab switch animations

## 2026-11-05 21:10
- [refactor] Вибір збірки в міграції модів використовує спільний список лаунчера з папками || use the shared pack list with folders in the mod migration picker
- [fix] Виправлено зсунутий радар аналізу в міграції модів, діалог міграції оновлено в стилі лаунчера || fix the offset migration radar and restyle the migration dialog
- [feat] У дереві залежностей моди можна перетягувати, дерево центрується на полотні, додано скидання розкладки || allow dragging mods in the dependency graph and center its layout
- [fix] Картки статистики в профілі більше не перекриваються іконкою || stop profile stat icons overlapping values
- [fix] Лічильник у вікні запрошення не наїжджає на заголовок || keep the invite counter on one line with its heading
- [refactor] Усі повзунки лаунчера отримали єдиний сучасний стиль із заповненням доріжки || unify all range sliders into one modern style
- [feat] Глобальний пошук: фільтри розділів, швидкі дії, недавні запити, керування клавіатурою і пошук збірок за лоадером, версією та папкою || add scopes, quick actions, recent queries and keyboard navigation to global search
- [feat] Бібліотека збірок: ковзний таббар, вирівняні картки й анімації при наведенні || polish the pack library tabs and tiles

## 2026-11-05 21:45
- [feat] У статусі Discord додано кнопку з запрошенням пограти, що веде на сайт лаунчера || add a website invite button to discord rich presence
- [feat] Розділ «Сховище» оновлено: анімоване кільце, зрозумілі назви тек, частки й смужки розміру збірок || redesign the storage settings section
- [feat] Дерево вибору файлів оновлено: власні галочки, кольорові іконки тек і файлів, плавне розкриття || modernize the file selection tree
- [feat] Вікно експорту збірки оновлено в стилі лаунчера || restyle the pack export dialog
- [feat] Вікно допомоги з P2P показує перевірки списком зі статусами замість суцільного тексту || show p2p network checks as a status list

## 2026-11-05 22:20
- [feat] Знову ввімкнено доктора крашів: після вильоту гри пропонує конкретні виправлення, без повторних вікон і без загальних порад для довгих сесій || re-enable the crash doctor with smarter gating
- [feat] Вікно перевірки збірки перед запуском оновлено: спокійні поради замість помилок, пам'ять налаштовується повзунком прямо у вікні з безпечним значенням || redesign the pre-launch pack check with inline memory adjustment

## 2026-11-05 22:50
- [feat] Доктор крашів перероблено: головна причина, одна кнопка головного виправлення, інші варіанти згорнуто, «Грати знову» після виправлення || redesign the crash doctor around one main fix
- [fix] Вікно доктора крашів завжди закривається й не перекриває лаунчер, одночасно не показується друге вікно про краш || make the crash doctor always closable and avoid stacking crash windows
- [fix] Вікно доктора знову вмикається у фронтенді (раніше лишалось заблокованим окремим прапорцем) || re-enable the crash doctor in the frontend
- [fix] Кнопка «Зупинити» під час встановлення більше не роздувається й не переносить тулбар збірки || keep the stop button compact on the pack toolbar

## 2026-11-05 23:20
- [fix] У банері збірки показується її опис, пресет-іконка має повноцінну рамку || show pack description and a framed preset icon in the pack banner
- [fix] Підпис у порожньому полі вибору іконки збірки більше не вилазить за рамку || keep the empty icon picker label inside its box
- [fix] Іконки й фони кастомних збірок оновлюються одразу після зміни чи видалення, без перезапуску || refresh custom pack icons immediately after they change
- [fix] У режимі великого банера авто-збірки більше не показують розділ «Основне», додано вкладку «Вікі» для збірок Шаурми || match big banner tabs with the normal pack page
- [fix] Великий банер сторінки проєкту в браузері контенту більше не розтягує бейдж типу й не тулить статистику під іконку || fix the full-width banner layout on content project pages
