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
- [ci] Установлювач публікується окремим релізом installer-vX.Y.Z з власною контрольною сумою й attestation; реліз компонентів лишається Latest і посилається на нього || publish installer as a separate release
- [fix] Друзі, лобі й скіни знову працюють у лаунчері 2.8: сервер приймає єдиний лаунчер без токена каналу, друзі як і раніше вимагають входу в акаунт || social gateway accepts single launcher without channel token
- [docs] План сторінки завантаження на сайті: установлювачі з релізів GitHub через api.shaurma-launcher.org || plan site downloads from GitHub releases
- [chore] Скрипт резервної копії ключів, підписів, сертифіката й токенів на офлайн-носій (за бажанням — зашифрований архів) || add offline secrets backup script

## 2026-11-04 18:13
- [ci] Додано посилання на політику підпису до описів обох релізів у .github/workflows/build-launcher.yml || link the code signing policy in launcher and installer release notes

## 2026-11-04 18:58
- [fix] Імпорт бандла резервує окрему теку для кожної збірки при збігу назв || reserve distinct instance directories during bundle import
- [fix] Обмежено бандл до 1024 збірок і згруповано файли архіву за збірками || limit bundle pack counts and index archive entries by pack
- [docs] Політика підпису коду чесно описує вшиті публічні ідентифікатори API; релізи посилаються на Code signing policy і політику приватності || accurate code signing policy and links on release pages

## 2026-11-04 20:19
- [fix] Виправлено версію в маніфесті запуску Windows || fix invalid Windows activation manifest version

## 2026-11-04 20:35
- [feat] Новий сайт за макетом: головна, завантаження, версії, документація, правила, запрошення; без рекламних посилань і стороннього вмісту || rebuild site from new design without third-party content
- [feat] Сайт качає установлювач прямо з релізів GitHub і показує версію, розмір, дату, SHA-256 та спосіб перевірки файла || download installer directly from github releases on site
- [feat] Сторінка «Версії»: останні 5 версій з описом змін, установлювачем і SHA-256 || add changelog page with last five versions
- [feat] Новий worker віддає сайту список релізів GitHub (/v1/releases) і редирект на установлювач (/v1/download/windows) з кешем і CORS лише для сайту || add releases and installer redirect routes to access worker
- [feat] SEO сайту: canonical, Open Graph, JSON-LD (команда Shaurma, «Шаурма Лаунчер»), sitemap і robots || add seo metadata and structured data to site
- [fix] Заголовки безпеки сайту: CSP без unsafe-inline, frame-ancestors none, security.txt || harden site security headers

## 2026-11-04 21:10
- [fix] Іконки сайту — SVG-спрайт Tabler замість шрифту іконок: рівно стоять у кнопках і на всіх екранах || replace icon font with svg sprite on site
- [feat] Головна сайту: жива демонстрація вікна лаунчера (створення збірки, встановлення, запуск) замість розмитого скріншота || add animated launcher demo to site hero
- [feat] Розділ функцій на головній: скріншот закріплюється й змінюється під час прокрутки; скріншоти відкриваються на весь екран || add sticky screenshot story and lightbox to site
- [refactor] Прибрано декоративні ефекти (розмиті плями, сітку, стрілки, відблиск), натискання й переходи за єдиними значеннями, повага до зменшеного руху || simplify site visuals and unify motion

## 2026-11-04 21:40
- [fix] Кнопки сайту: більші відступи, натискання з «вдавлюванням», без зайвого підпису «з GitHub» || increase site button spacing and remove redundant label
- [feat] Під кнопкою завантаження на головній: картки «Відкритий код» і «Перевірити файл» та цифри версії, розміру, завантажувачів і джерел модів || add action cards and stats strip to site hero
- [feat] Розділ «Питання» на головній з розміткою FAQPage для пошуку || add faq section with structured data
- [feat] Плаваюча панель завантаження на головній після першого екрана || add floating download bar on home page
- [feat] Новий підвал сайту: заклик до завантаження, колонки розділів, посилання на GitHub і Discord || redesign site footer

## 2026-11-04 22:10
- [feat] Лічильник завантажень у новому worker: POST /v1/stats/download і GET /v1/stats, людина рахується раз на добу, без збереження IP || add privacy-preserving download counter to access worker
- [feat] Сайт надсилає натискання «Завантажити» в лічильник і показує кількість завантажень на головній || send download clicks to counter and show total on site
- [feat] Розділ функцій: рамка скріншота з лічильником розділів, блоковою тінню й карткою-фактом || restyle feature screenshot stage
- [feat] Розділ каталогу на головній: картки модів, що пливуть, і демонстрація пошуку || add catalog wall and search demo to home page
- [feat] Посилання на Telegram-канал у підвалі сайту || add telegram channel link to site footer

## 2026-11-04 22:40
- [fix] Демонстрація на головній відтворює справжній інтерфейс лаунчера: бічна панель, панель інструментів, вкладки, екран запуску з тими самими етапами й кольорами || match home demo to real launcher ui and launch overlay
- [feat] Каталог на головній показує справжні іконки модів і кількість завантажень із відкритого API Modrinth || load real mod icons and downloads from modrinth api
- [fix] Мітки Modrinth і CurseForge у демо-пошуку з назвами || label sources in search demo

## 2026-11-04 23:30
- [feat] Англійська версія сайту (/en/): головна, завантаження, версії, документація, правила; перемикач мов і hreflang || add english version of the site
- [build] Сторінки сайту збираються з src/ скриптом build.py, деплой запускає збирання || build site pages from templates
- [fix] Демонстрація на головній проходить справжній шлях лаунчера: форма «Нова збірка», вибір завантажувача, створення, запуск || follow real create-pack flow in home demo
- [feat] Ефекти лаунчера на сайті: блиск карток при наведенні, пружна поява активних пунктів, каскадна поява списків || port launcher shine, spring and cascade effects to site
- [fix] Прибрано рамку навколо логотипа в підвалі || remove frame around footer logo

## 2026-11-04 23:55
- [feat] Вибір мови на сайті: панель із пошуком і картками мов з прапорами || add language picker panel to site
- [fix] Посилання-запрошення /friend/@нік більше не перенаправляється на /friend і не губить нік || keep handle in friend invite links on pages
- [fix] Відступи в шапці сайту й компактні пункти меню на вузьких екранах || fix site header spacing on narrow screens

## 2026-11-05 00:20
- [fix] Контраст тексту сайту за WCAG 4.5:1: приглушений текст і акцентний текст у світлій темі || fix site text contrast to wcag aa
- [fix] На сенсорних екранах кнопки шапки 44 px і більші відступи між посиланнями || enlarge touch targets on coarse pointers
- [feat] Глибина на головній: вікно лаунчера нахиляється за курсором, навколо плавають блоки завантажувачів, єдина шкала тіней || add pointer tilt, floating tiles and elevation scale
- [build] Збирання сайту додає до адрес CSS і JS мітку версії з хешу, щоб кеш Cloudflare не віддавав старі стилі після деплою || add content-hash versions to site asset urls

## 2026-11-05 00:50
- [feat] Плавні переходи між сторінками сайту (View Transitions), шапка лишається на місці || add cross-page view transitions
- [feat] Зміна мови без стрибків: та сама позиція на сторінці й той самий розділ документації, м’яке перетікання || keep scroll position and fade on language switch
- [feat] Поява елементів сторінок при прокрутці — лише м’яке проявлення, без зсуву й розмиття || add opacity-only scroll reveal
- [fix] Зміст документації на вузьких екранах більше не відкидає сторінку вгору й не показує смугу прокрутки || fix docs toc scroll jump and scrollbar
- [fix] Демо-пошук каталогу вміщається в блок карток || fit catalog search demo inside wall

## 2026-11-05 01:20
- [fix] Лаунчер під час запуску відновлює схему shaurma:// у HKCU, якщо її немає (оновлення без установлювача), щоб браузер пропонував відкрити лаунчер із запрошення || restore shaurma url protocol registration on startup
- [feat] Новий worker віддає скін гравця для сторінки запрошення (/v1/skin/<нік>) з Mojang із перевіркою адреси текстури || add skin endpoint for invite page
- [feat] Сторінка запрошення в друзі показує голову гравця зі скіна Minecraft замість літери || show minecraft head on friend invite page

## 2026-11-04 22:10
- [feat] Кнопки лаунчера «блоком»: нижня грань і вдавлення при натисканні, основна кнопка з градієнтом акценту, як на сайті || block-style launcher buttons with pressed state and accent gradient
- [feat] Шкала тіней --lift і --elev-1..4 у стилях лаунчера || add shadow scale tokens to launcher styles
- [fix] Контраст тексту: --text-dim не нижче 4.5:1 у всіх темах, колір тексту на акцентних кнопках обирається за контрастом || raise dim text contrast and pick accent button ink by contrast

## 2026-11-05 02:00
- [fix] Демо на головній запускає збірку з картки, як у лаунчері: стадії, іконки, світіння, смужка, стан «Зупинити» || match demo launch to real pack tile launch
- [fix] Пошук скіна у worker: спершу Minecraft Services, потім Mojang, з журналом відмов || add minecraft services fallback for skin lookup

## 2026-11-04 21:58
- [fix] Прибрано нижню грань і вдавлення кнопок лаунчера || flatten launcher buttons
- [feat] Оновлено вісім акцентних палітр для читабельного білого тексту || refresh launcher accents for white text contrast
