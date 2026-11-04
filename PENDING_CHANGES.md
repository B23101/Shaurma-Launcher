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

## 2026-11-05 14:00
- [ci] Збірка Linux на GitHub: job linux-flatpak збирає ShaurmaLauncher.flatpak усередині org.gnome.Sdk//50, пакет і install-linux.sh ідуть у реліз і attestation || build the linux flatpak on github and attach it to the release
- [ci] Випуск більше не чекає повторних перевірок ci.yml: вони й так ідуть на кожен push у main || stop gating release builds on the ci workflow
- [build] Новий маніфест Flatpak: shell, backend.so і updater.so компілюються в SDK, додано xrandr для старих версій Minecraft і дозвіл на самооновлення через flatpak || build the flatpak from source inside the gnome sdk
- [build] scripts/build-linux-flatpak.sh: однакова збірка Flatpak на GitHub і в WSL || add a single flatpak build script for ci and wsl
- [feat] install-linux.sh: встановлення однією командою, сам ставить flatpak на будь-якому дистрибутиві й звіряє SHA-256 || add a one-line linux installer that sets up flatpak when missing
- [feat] Лаунчер у Flatpak оновлюється цілим пакетом: качає підписаний .flatpak з релізу й перевстановлюється через flatpak на хості || update the flatpak launcher by reinstalling the signed bundle
- [fix] Після оновлення пакета Flatpak shell розкладає новий запечений набір замість старого || reseed flatpak data when the bundled version is newer
- [chore] Прибрано старе пакування Flatpak (build-and-publish.sh, редакція Base, власний репозиторій) — його замінила збірка на GitHub || remove the old self-hosted flatpak packaging
- [docs] flatpak/README.md описує нове встановлення, збірку й оновлення || document the new flatpak install, build and update flow
- [fix] Flatpak: дозвіл на ім'я D-Bus для «одного екземпляра» Wails — без нього лаунчер у пісочниці закривався одразу після старту || allow the wails single-instance dbus name so the flatpak starts
- [fix] Сайт бачить нову версію одразу: access-worker звіряє список релізів із сигналом оновлень і перечитує GitHub, коли сигнал новіший (кеш 2 хв замість ~15) || refresh site releases as soon as the update signal is newer
- [fix] Сайт на телефоні: меню-кнопка з усіма розділами, мовою й виглядом, шапка вміщається, довгі блоки більше не лишаються невидимими || add a mobile menu and fix hidden blocks on phones
- [fix] Голова зі скіна на сторінці запрошення береться через VPS (Mojang блокує запити з Cloudflare), прямий маршрут /v1/skin прибрано || load invite skin heads through the vps
- [feat] Pack Manager: «Тестовий білд» збирає лаунчер, переносить попередні файли в резервну копію, ставить нові й запускає; кнопка повернення копії || add a local test build with backup and restore to pack manager
- [refactor] Pack Manager: прибрано стару локальну збірку з перевірками, «Випуск на GitHub» стартує без check-release.ps1 і додає Linux-пакет у підписаний маніфест || drop the old local build and local release checks from pack manager

## 2026-11-05 18:00
- [feat] Новий майстер першого запуску: велика картка з анімованими векторними сценами для кожного кроку, плавні переходи і підсумок наприкінці, де кожен рядок веде назад до свого налаштування || redesign the first-run wizard with animated scenes and a final summary
- [feat] Майстер налаштовує тему, анімації, режим для слабкого ПК, поведінку під час і після гри, сповіщення, Discord, гру через інтернет, ігровий час у хмарі, автозапуск, хрестик вікна й автооновлення || let the first-run wizard set theme, notifications, friends, game and window behavior
- [feat] Крок «Гра» радить обсяг пам'яті під ПК і попереджає, коли її забагато || recommend game memory based on installed ram in the wizard
- [fix] Вхід у майстрі йде тим самим шляхом, що й модалка входу: Google, вибір ніка й ліміт пристроїв тепер працюють і там || route wizard sign-in through the shared login flow
- [feat] Справжні 2D-голови гравців зі скінів (PlayerHead) у майстрі й банері входу замість намальованих квадратів || show real skin heads in the wizard and login banner
- [fix] Модалка входу: коротший текст банера і підказок, прибрано зайве пояснення під кнопками || shorten the login modal copy
- [fix] access-worker: запрошення працюють на Cloudflare (redirect manual замість непідтримуваного error, адреса VPS у wrangler.toml, щоб деплой її не стирав) || fix invite proxy on cloudflare workers
- [fix] Сайт: іконки з міткою версії (кеш Cloudflare не віддає старий спрайт без іконки меню), без горизонтального зсуву на телефоні, нижня плашка й картки модів вміщаються || version the icon sprite and fix remaining mobile overflow
- [feat] Сторінка завантаження: розділ Linux з командою встановлення, ручним способом, вимогами, оновленням і видаленням || fill the linux section on the download page

## 2026-11-05 21:30
- [feat] Майстер: крок «Акаунт» розводить ліцензію (Microsoft) і гру без ліцензії (Google, радимо), нік нового акаунта Шаурми обирається прямо на сторінці || split licensed and unlicensed sign-in in the wizard and pick the nickname inline
- [feat] Майстер: для акаунта без ліцензії — вибір скіна з живою 3D-моделлю (стандартні скіни, свій PNG, скін за ніком), для всіх акаунтів Шаурми — профіль (хендл, біо, колір картки) || add skin picker with 3d preview and profile setup to the wizard
- [feat] Майстер: окремий крок «Файли» з папкою лаунчера на видному місці і перенесенням збірок з Prism і Modrinth App || add a files step with data folder and import from other launchers
- [feat] Майстер наприкінці пропонує створити першу збірку з покроковими підказками (CoachTour) на сторінці створення || offer a guided first modpack at the end of the wizard
- [feat] Вимкнений P2P: гра пояснює причину замість «Connection refused», а лаунчер пропонує увімкнути P2P одним кліком || explain disabled p2p in game and offer to enable it in the launcher
- [fix] Майстер: прибрано перемикач «Друзі Xbox у мережі», якого в лаунчері немає; вимкнення гри через інтернет показує, що саме перестане працювати || drop the unused xbox online toggle and warn when turning p2p off
- [fix] Сайт: простіший текст розділу Linux і акуратний підвал на телефоні (без обрізаного напису, рівні колонки) || simplify the linux copy and tidy the mobile footer

## 2026-11-05 22:10
- [fix] Майстер: картка Microsoft пояснює, що вхід працює і без ліцензії, а Google позначено як найкращий варіант саме без ліцензії || clarify microsoft sign-in works without a license in the wizard

## 2026-11-05 22:40
- [fix] Майстер: JVM-аргументи редагуються блоками (JvmArgsEditor), як у налаштуваннях збірки, а не одним рядком || edit wizard jvm arguments as blocks
- [fix] Майстер: прибрано 3D-куби зі сцен, у кроці «Файли» в теку падають пласкі картки збірок, картка першої збірки — одна іконка замість фігур || remove isometric cubes from wizard scenes

## 2026-11-05 23:10
- [fix] Майстер: на вітанні й у шапці справжня іконка лаунчера замість полум'я на плашці || use the real launcher icon in the wizard
- [fix] Майстер: картки Microsoft і Google рівноцінні — у Microsoft теж показано друзів, чат і гру через інтернет, обидві з власним бейджем і основною кнопкою || give the licensed sign-in card the same perks and weight as google
- [feat] Сповіщення у трей і тости лаунчера в новому стилі: окремі заокруглені картки, голова гравця зі значком типу (чат, запрошення), іконка лаунчера для службових, виїзд справа || restyle tray notifications and toasts as separate cards with player heads

## 2026-11-05 23:40
- [fix] Майстер, крок «Вигляд»: замість пензлика курсор клацає по кольорах, і макет лаунчера перефарбовується після кожного кліку || replace the brush with a cursor clicking accent colors in the wizard
- [fix] Майстер, крок «Лаунчер»: нова сцена — вікно з іконкою лаунчера ховається в трей і повертається, поруч перемикач автозапуску й смуга оновлення || redraw the wizard launcher behavior scene
