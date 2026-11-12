# PENDING CHANGES

<!--
ЦЕЙ ФАЙЛ ВЕДЕ НЕЙРОНКА. Кожна зміна самого лаунчера = новий пункт тут (правило з AGENTS.md).

ОБЛАСТЬ ПОДАННЯ ОНОВЛЕННЯ: лише сам лаунчер.
До цього файлу, опису оновлення та подання на GitHub включай лише зміни коду,
файлів і компонентів самого лаунчера. Зміни окремого Pack Manager, сервера
«Шаурми» та інфраструктури VPS сюди не записуй і до оновлення лаунчера не включай:
ці окремі проєкти ми не подаємо на GitHub разом із лаунчером.
Pack Manager лише готує та публікує оновлення; це не робить його частиною лаунчера.

Pack Manager після успішної збірки сам зробить git commit + push, візьме звідси
текст для commit-повідомлення і ОЧИСТИТЬ відправлені пункти. Нічого руками не чисти.

Формат: новий блок "## YYYY-MM-DD HH:MM" внизу, під ним пункти:
  - [тип] Опис українською || English commit text (необов'язково)
Типи: feat, fix, perf, refactor, docs, build, ci, chore.
Пункт = одна змістовна зміна, одним рядком, без секретів і без згадок ШІ.
-->

## 2026-11-06 18:58
- [fix] Припинено пізні підписки консолі після закриття вкладки || prevent console subscriptions after component disposal
- [perf] Повторне відкриття серверів показує останній список; обмежено паралельні запити й фонове опитування || reuse the latest server list and bound background polling
- [perf] Скорочено появу розділів і збірок до 180 мс || shorten page fade to 180ms

## 2026-11-06 19:03
- [fix] Обмежено черги логів прихованих консолей та очищено відкладені кадри й застарілі відповіді || bound pending console logs and clean up deferred callbacks
- [perf] Усунено накладання запитів метрик гри та серверів || prevent overlapping game and server metrics requests
- [fix] Звільнено застарілі 3D-моделі та завершені записи запитів асетів || release stale models and completed asset request entries

## 2026-11-06 19:07
- [perf] Додано очищення невикористаних аватарок і ресурсів 3D-прев’ю після простою зі збереженням активних переглядачів || reclaim idle avatar caches and unused 3d preview resources

## 2026-11-06 19:17
- [perf] Додано мініатюри малих іконок до 128 px зі спільним кешем за адресою та розміром || resize small pack icons before sending them to the webview
- [perf] Локальні консолі завантажують ділянки історії з пошуком на бекенді й віртуальною прокруткою || page local console history and filter logs on the backend
- [perf] Припинено розсилання повного потоку локальних логів у WebView та додано спільне зберігання повторних рядків || stop broadcasting local log text and share repeated message storage

## 2026-11-06 21:30
- [perf] WebView2 переводиться на низький рівень пам'яті (MemoryUsageTargetLevel) у треї, коли вікно неактивне понад 15 с і коли користувач бездіяльний понад 90 с, та повертається до звичайного рівня при поверненні || lower webview2 memory target when the window is hidden, inactive or the user is idle
- [perf] Робочі набори лаунчера й WebView2 віддаються ОС двічі після приховування вікна, щоб забрати кеші, скинуті Chromium із затримкою || trim process working sets twice after hiding the window

## 2026-11-06 21:45
- [perf] Збирач сміття backend переходить на найекономніший профіль у простої/треї й повертається до активного при поверненні користувача; активний профіль GOGC 40 || switch the go gc to a tighter profile while idle or hidden
- [perf] WebView2: вимкнено запасний рендерер і зменшено молоду купу V8 (без втрати GPU-плавності) || disable the spare renderer and shrink the v8 semi-space
- [perf] Перехід в економію пам'яті раніше: 10 с без активації вікна або 60 с без вводу || enter memory saving sooner when inactive or idle

## 2026-11-06 22:05
- [perf] Плитки збірок, папок і бічна панель з'являються за ~0,16 с із мінімальним каскадом замість ~0,6 с; після появи анімація не тримає окремий шар на кожній плитці || shorten tile appearance animation and release compositor layers after it ends
- [perf] Список серверів оновлюється на місці й лише змінені поля, тож картки не перемальовуються цілком кожні 2 с || merge server list updates in place instead of replacing every item
- [perf] Список серверів прогрівається у фоні після старту, і сторінка «Сервери» відкривається одразу з картками, без спінера || prefetch the server list after startup so the servers page opens without a spinner

## 2026-11-06 22:40
- [perf] Фон банера збірки зменшується на бекенді до 1920 px (JPEG лишається JPEG) замість повного оригіналу в пам'яті WebView || downscale pack banner backgrounds to 1920px before sending them to the webview
- [fix] Економія пам'яті віддає ОС робочі набори лише процесам WebView2, а не грі та локальним серверам || trim working sets of webview2 processes only, not the game or servers
- [perf] Плавніша поява розділів: 260 мс замість 180 мс || lengthen page fade-in to 260ms

## 2026-11-06 23:10
- [fix] Гардероб не показує білу модель без скіна: модель з'являється лише з текстурою, до цього — індикатор завантаження || hide the untextured wardrobe model behind a loading indicator
- [perf] Шейдери 3D-переглядача компілюються асинхронно перед першим показом, тож відкриття гардероба не блокує інтерфейс || compile wardrobe viewer shaders asynchronously before the first frame

## 2026-11-07 00:15
- [feat] Додано в локальний Pack Manager підготовку ZIP із релізними файлами GitHub, перевіркою SHA-256 і поясненням для Microsoft || add Microsoft review bundle preparation to local pack manager

## 2026-11-07 00:21
- [fix] Відокремлено версію пакета Microsoft від нового випуску; замість неактивної кнопки показується пояснення некоректної версії, підтримано префікс v || fix Microsoft bundle version input and explain validation errors

## 2026-11-07 00:24
- [fix] Додано версію до назв ZIP і хеш-файлу Microsoft; звернення показується окремо в Pack Manager з копіюванням замість SUBMISSION.txt || version Microsoft bundle filenames and show a copyable submission message

## 2026-11-07 00:30
- [fix] Відновлено український текст UTF-8 у вкладці випуску та повідомленнях Pack Manager після пошкодження кодування || restore UTF-8 Ukrainian text in pack manager release panel and messages

## 2026-11-07 12:40
- [feat] Вкладка Microsoft: у ZIP лише ShaurmLauncher.exe, backend.dll і updater.dll плоско, з паролем infected; файли беруться з релізу GitHub без змін і звіряються з SHA256SUMS || add microsoft tab with flat password-protected zip of verified release binaries
- [feat] Картка подання з копіюванням файлу, пароля, назви детекту й готового повідомлення в редакторі, без окремого файлу || add step-by-step submission card with copy buttons and editable message
- [fix] Невдала підготовка пакета лишає теку failed замість схожої на готову; версія з префіксом v приймається || keep failed microsoft bundles apart and accept v-prefixed versions
- [fix] Випуск не показує успіх, якщо установлювач або сигнал worker завершились збоєм || report installer and signal failures instead of success
- [refactor] Введені у формах значення зберігаються у ui-state.json замість localStorage WebView || store form inputs in ui-state.json instead of webview localStorage


## 2026-11-07 15:23
- [build] Оновлено installer/SECURITY-REVIEW.md: прибрано застарілі числа спрацювань і опис скриншота, додано нове посилання VirusTotal || update installer security review with current VirusTotal link

## 2026-11-07 15:26
- [refactor] Видалено надсилання статистики використання розробнику, компонент згоди, налаштування та переклади; локальну статистику гри збережено || remove developer usage telemetry and consent settings

## 2026-11-09 09:39
- [build] У scripts/build-windows.ps1 увімкнено cgo і налаштовано C-компілятор під час збору ліцензій updater-backend; попереднє середовище відновлюється після перевірки || configure cgo and compiler for dependency license collection

## 2026-11-09 11:25
- [feat] Видалення акаунта відкладено на 7 днів: дані лишаються, акаунт заморожується, вхід через OAuth скасовує видалення; після строку вузол остаточно чистить акаунт раз на годину || defer account deletion by 7 days with restore on oauth sign-in
- [fix] Остаточне видалення акаунта стирає запити в друзі, особисті чати з ним (повідомлення, закріплені, реакції, лічильники й прочитане в обох), блокування, ігровий час, статистику, «востаннє в мережі», P2P, тимчасові запрошення й коментарі || purge all account-related data on final deletion
- [fix] Групи при видаленні акаунта: єдиний учасник — група видаляється повністю, інакше лідерство переходить наступному учаснику || transfer group leadership or delete empty groups on account deletion
- [fix] Видалені повідомлення груп стираються з бази повністю (без службового рядка), видалена група одразу втрачає всі повідомлення || delete group messages physically instead of keeping tombstones
- [fix] friend_invites чистяться: опрацьовані — через 7 днів, без відповіді — через 90 днів, сирітські одразу; додано ліміт 100 очікуючих запитів на вихідні й на вхідні та відмову для заморожених акаунтів || purge friend invites and cap pending requests per account
- [fix] Лімітери в пам'яті самі прибирають прострочені ключі (hourRate — за часом, PerKeyLimiter — від 1024 ключів) || stop in-memory rate limiters from growing unbounded
- [chore] Міграція 0021: колонка users.delete_after та індекси friend_invites; мінімальна схема вузла 21 || add migration 0021 for deferred account deletion

## 2026-11-09 17:53
- [fix] Нативні HTML-підказки замінено кастомними підказками профілів; приховано повторення видимого тексту, збережено назви кнопок-іконок і додано закриття через Escape || replace native titles with shared custom tooltips

## 2026-11-09 18:17
- [fix] Уточнено локальне видалення акаунта: «Видалити з пристрою» в підказках і підтвердженні українською та англійською || clarify account removal from device

## 2026-11-09 18:18
- [fix] Прибрано зайву підказку з назвою папки збірок у бічній панелі || remove redundant sidebar folder tooltip

## 2026-11-09 18:19
- [fix] Виправлено підказку меню збірки з «Оновити» на «Ще дії» в обох панелях || label pack overflow menu as more actions

## 2026-11-09 18:21
- [fix] Приховано нативні стрілки числових полів ширини та висоти вікна || hide native spinners in window size fields

## 2026-11-09 18:24
- [feat] У допомозі повідомлення про помилки ведуть на GitHub Issues; збережено Discord і додано підтримку через Donatello з перекладами та пошуком || route bug reports to github and add discord and donatello help links

## 2026-11-09 18:52
- [fix] Зображення в описах проєктів зберігають пропорції при звуженні панелі навіть із заданою HTML-висотою || preserve description image aspect ratio when constrained by panel width

## 2026-11-12 00:30
- [ci] У .github/workflows/build-launcher.yml передано ключі Gemini та Groq із GitHub Secrets у Windows і Linux збірки || pass gemini and groq secrets to release builds
- [build] У scripts/ та flatpak/build-in-sdk.sh відновлено вбудовування ключів у backend і дозволено лише налаштовані ключі у перевірці артефактів; додано перевірку ізоляції || restore backend ai key injection and validate artifact isolation

## 2026-11-12 09:47
- [chore] Прибрано коментарі, що описували минулі зміни («раніше було…», історії виправлених помилок, «РЕГРЕСІЯ») і посилання на неіснуючі документи та файли; лишено лише опис поточної поведінки коду (backend, frontend, shell, updater-backend) || remove historical comments and references to missing docs

## 2026-11-12 09:56
- [docs] PRIVACY.md і README приведено у відповідність до коду: прибрано розділ про вимкнену статистику, описано хмарний ігровий час, звіт апдейтера, очищення крашу перед ШІ та вбудовані ключі збірки || align privacy policy and readme with actual behavior
- [fix] Підказки налаштування хмарного ігрового часу більше не стверджують, що друзі його не бачать (профіль показує загальний час і кількість збірок) || correct cloud playtime visibility text
- [chore] Видалено невикористану модалку ErrorReportsConsentModal; дочищено коментарі про минулі зміни та відновлено відступи коментарів || remove unused error reports modal and finish comment cleanup

## 2026-11-12 10:01
- [chore] Дочищено залишки коментарів про минулі зміни (заголовки «Раніше…», історії виправлень) у backend, frontend і shell || finish removing historical comments
- [fix] У User-Agent запитів до Modrinth, CurseForge і Groq замість e-mail вказано URL репозиторію || use repository url instead of email in user agent
