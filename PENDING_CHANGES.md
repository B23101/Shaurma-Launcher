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

## 2026-10-31 12:51
- [fix] Усунуто нескінченний цикл реактивного завантаження дизайну, через який кнопки лаунчера переставали реагувати || prevent pack design reactivity from freezing launcher controls
- [fix] Ізольовано зворотний виклик макета від реактивних залежностей компонента дизайну || isolate pack layout callbacks from design effect dependencies
- [chore] Додано регресійну перевірку початкового та асинхронного завантаження дизайну й перемикання збірок || add regression coverage for stable pack design loading

## 2026-10-31 16:55
- [build] scripts/: блокувати production-збірку без ключів маніфесту та перевіряти це без RequireSecrets || reject missing production manifest keys in all Windows builds
- [build] scripts/: узгодити CompanyName DLL та перевірку видавця, прибрати Go build ID у DLL || align DLL company metadata and clear Go build IDs
- [fix] Надсилати install_ping лише за згодою на статистику || require statistics consent for installation reports
- [docs] installer/: уточнити згоду на статистику у політиці встановлення || document installation statistics consent in installer policy
- [ci] .signpath/: надати канонічний XML Base і уточнити обмеження Foundation || expose canonical Base signing configuration and clarify eligibility
- [docs] Уточнити README, приватність і повідомлення про authlib-injector || clarify signing status update verification and third-party notices
- [docs] scripts/RELEASING.md: додати результати аудиту й ручні перевірки || record signing audit limitations and manual release checks
- [docs] Оновити docs/dev/ANTIVIRUS_FALSE_POSITIVES.md за production-політикою || replace obsolete antivirus guidance with production security policy

## 2026-10-31 17:00
- [fix] Видалено install_ping і пов’язаний код обліку встановлень || remove installation ping and its tracking code
- [docs] installer/: прибрано згадки про install_ping з політик і коментарів || remove installation ping references from privacy and installer policies

## 2026-10-31 17:50
- [fix] Заблоковано вихід за межі каталогів та посилання під час імпорту shaurmpack і shaurmbundle || contain pack and bundle extraction within destination directories
- [fix] Перевіряти хеші завантажень паків до встановлення та повертати помилки імпорту || verify staged pack downloads before installation
- [fix] Обмежено завантаження паків HTTPS-хостами Modrinth і CurseForge та захищено перенаправлення || restrict pack download origins and protect redirect credentials
- [fix] Перевіряти хост CDN CurseForge замість пошуку підрядка в URL || validate CurseForge CDN host boundaries
- [fix] Захищено відновлення бекапів від zip-slip та перевірено ID експорту й видалення || contain backup extraction and validate backup identifiers
- [fix] Перевіряти Host локального проксі скінів || reject untrusted skin proxy host headers
- [chore] Додано регресійні перевірки небезпечних шляхів, URL та пошкоджених завантажень || cover unsafe archive paths download origins and integrity failures

## 2026-10-31 17:59
- [fix] Імпортувати з бандлів лише безпечні метадані без команд, Java-аргументів і чужих локальних шляхів || strip executable settings and local paths from imported bundle metadata
- [fix] Перевіряти та розпаковувати бекап до заміни світу, блокувати сторонні файли сервера й зберігати оригінал при помилці || stage world restoration and preserve original data on failure
- [fix] Заблоковано використання батьківської теки як назви світу || reject parent directory world names
- [fix] Обмежено кількість і розмір файлів паків та читання JSON з архівів || bound pack archive extraction and metadata allocation
- [chore] Додано перевірки безпечного імпорту метаданих і збереження світу при відхиленому бекапі || test safe bundle metadata and world preservation on rejected backups

## 2026-10-31 18:12
- [fix] Перевіряти маніфест і файли Java за SHA-1, розміром та дозволеними HTTPS-джерелами || verify Java manifests files and download origins
- [fix] Обмежити шляхи й посилання Java коренем встановлення та замінювати файли після перевірки || contain Java installation paths and publish verified downloads
- [fix] Перевіряти SHA-512, формат і джерела mrpack, включно з докачуванням і перенаправленнями || enforce mrpack format hashes origins and redirect policy
- [chore] Додано перевірки підміни Java, маніфестів і SHA-512 модпаків || test Java integrity and mrpack security policy
- [docs] scripts/RELEASING.md: описано захист завантажень і ручні вимоги модерації || document download safeguards and manual moderation requirements

## 2026-10-31 18:28
- [fix] Захищено токени запитів маніфестів при перенаправленні та заборонено перехід з HTTPS на HTTP || protect manifest credentials across redirects
- [fix] Обмежено читання маніфестів CurseForge з ZIP та відхилено завеликі маніфести під час перегляду імпорту || bound CurseForge manifest reads and reject oversized previews
- [fix] Обмежено запис, розпакування та очищення синхронізації коренем збірки через os.Root || contain sync filesystem mutations within the installation root
- [chore] Додано перевірки перенаправлень, посилань назовні, першого встановлення та докачування || test sync containment redirects and download compatibility
