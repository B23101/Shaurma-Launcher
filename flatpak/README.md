# Shaurma Launcher — Flatpak-пакування

Цей комплект пакує лаунчер у Flatpak. Всередині образу: shell-бінарник
+ ЗАПЕЧЕНИЙ стартовий набір у `/app/shaurma-seed` — `backend.so`,
`updater.so`, розпаковані `frontend/` та `updater-frontend/` і
**локальний `launcher-version.json` тієї самої версії** — плюс .desktop,
іконка і wl-clipboard.

Завдяки запеченому набору **перший запуск працює без мережі** й
**нічого не докачує**: shell копіює ці файли у свою теку даних
(`shell/flatpak_seed_linux.go`), а updater бачить локальний маніфест,
порівнює sha256/розміри (`manifest.Diff`) і качає **лише те, що справді
новіше за образ**. Без цього маніфесту `manifest.Load()` повертав
порожній маніфест («нічого не встановлено»), і перший запуск тягнув з
CDN ~120 МБ тих самих файлів, які вже лежать у `/app`.

Сам shell у Flatpak не самоновлюється: він живе в read-only `/app/bin` і
оновлюється тільки разом із пакетом (`flatpak update`). Апдейтер це
враховує й не включає shell у список качки (`updater-backend/internal/core/flatpak.go`).

## Встановлення для користувача — одна команда

Найпростіше — `.flatpakref` (додає репозиторій, сам докачує рантайм
`org.gnome.Platform//50` з flathub і ставить застосунок):

```bash
flatpak install --user https://твій-домен/com.shaurma.Launcher.flatpakref
```

`.flatpakref` для кожної редакції пакувальник кладе поруч із
`shaurma.flatpakrepo`. Окремий `.flatpak`-файл теж сам підтягне рантайм
(у бандл прописано `--runtime-repo`).

```bash
./install.sh ./shaurma-launcher-shaurma.flatpak   # або URL .flatpakrepo
```

`install.sh` сам додає flathub, ставить рівно потрібний рантайм
`org.gnome.Platform//50` (GTK4 + WebKitGTK 6.0 — нічого системного не
треба), ставить вибрану редакцію і **перевіряє, що дозволи пісочниці
справді застосовані**: мережа, `--device=dri` (GPU), `--socket=pulseaudio`
(звук гри).

Усі варіанти встановлення (файл, репозиторій, DEB/RPM/AppImage) — у
`docs/INSTALL-LAUNCHER.md`.

**Дві редакції — два окремих app-id.** Повна версія (shaurma) і base —
це два різних Flatpak-застосунки з різними ідентифікаторами:

| Редакція | app-id                     | Файл                              |
|----------|-----------------------------|-----------------------------------|
| shaurma  | `com.shaurma.Launcher`      | `shaurma-launcher-shaurma.flatpak`|
| base     | `com.shaurma.LauncherBase`  | `shaurma-launcher-base.flatpak`   |

Обидва можуть стояти на одній машині одночасно, не заважаючи один
одному, і обидва живуть в ОДНОМУ спільному репозиторії
(`shaurma-repo/`) — користувач один раз підключає репозиторій, а тоді
сам обирає, яку редакцію ставити за app-id.

## Автоматична збірка (рекомендовано) — разом з AppImage, одним build.ps1

Крок Flatpak тепер вбудований у головний `build.ps1` поруч з AppImage —
не треба вручну копіювати бінарник і запускати окремий скрипт:

```powershell
.\build.ps1 -IncludeLinux -BuildAppImage -BuildFlatpak
```

(або `-LinuxOnly -BuildAppImage -BuildFlatpak` на CI/WSL2-машині, де
Windows-частина не потрібна). Перший запуск сам поставить у WSL2
`flatpak`, `flatpak-builder` і runtime `org.gnome.Platform//50` через
apt/flathub (потрібен `WSL_SUDO_PASSWORD`, як і для решти Linux-кроків). Результат — готові файли для кожної редакції, обидві
записані в ОДИН спільний репозиторій:

```
dist\Flatpak\shaurma\shaurma-launcher-shaurma.flatpak
dist\Flatpak\base\shaurma-launcher-base.flatpak
dist\Flatpak\shaurma-repo\        <- спільний репозиторій, обидва app-id
```

Користувач ставить потрібну редакцію однією командою (файл — окремо на
кожну, репозиторій — спільний):

```bash
flatpak install --user shaurma-launcher-shaurma.flatpak   # повна версія
flatpak run com.shaurma.Launcher

flatpak install --user shaurma-launcher-base.flatpak      # base
flatpak run com.shaurma.LauncherBase
```

Далі можна одразу перейти до "Крок 3 — план публікації" нижче, якщо
хочеш роздавати файл сам або виставити власний репозиторій з
`flatpak update`. Решта цього README (Кроки 0–2) описує ручний шлях
через `build-and-publish.sh` — лишений для збірки Flatpak окремо від
основного білда чи для налагодження маніфесту.

## Файли в архіві

- `com.shaurma.Launcher.yml` / `com.shaurma.LauncherBase.yml` —
  Flatpak-маніфести (що і як збирати), по одному на редакцію.
- `com.shaurma.Launcher.desktop` / `com.shaurma.LauncherBase.desktop` —
  ярлики застосунків (меню, панель задач).
- `com.shaurma.Launcher.metainfo.xml` /
  `com.shaurma.LauncherBase.metainfo.xml` — метадані для GNOME
  Software/KDE Discover.
- `shaurma-icon.png` / `shaurma-base-icon.png` — іконки (256×256).
  `shaurma-icon.png` взята з `frontend/public/appicon.png` проєкту;
  `shaurma-base-icon.png` наразі копія — заміни на реальне лого base,
  якщо воно відрізняється.
- `install.sh` — встановлення однією командою: flathub, рантайм,
  лаунчер, перевірка дозволів (звук/GPU/мережа).
- `build-and-publish.sh` — ручний скрипт (Крок 0-2 нижче), приймає
  редакцію аргументом (`shaurma` / `base` / `all`); `build.ps1
  -BuildFlatpak` вище робить те саме автоматично в одному проході для
  обох редакцій.

## Крок 0 — встановити інструменти (один раз, на своїй машині)

```bash
sudo apt install flatpak flatpak-builder
flatpak remote-add --if-not-exists flathub https://flathub.org/repo/flathub.flatpakrepo
flatpak install flathub org.gnome.Platform//50 org.gnome.Sdk//50
```

Рантайм — **тільки** `org.gnome.Platform//50` (не `org.freedesktop.Platform`):
shell зібраний під GTK4 + WebKitGTK 6.0 (`build-linux-shell.sh`), яких у
freedesktop-рантаймі немає. Ця сама версія прописана в обох `.yml` і в
`pack-manager/internal/launcherbuild/wsl.go` — якщо змінюєш, то всі три
місця одночасно.

## Крок 1 — розпакувати архів і додати бінарник

```bash
unzip shaurma-flatpak.zip -d shaurma-flatpak
cd shaurma-flatpak
```

Скопіюй сюди вже зібраний Linux ELF-бінарник shell (той самий, що зараз
кладеться в AppDir для AppImage — `shell/linux/appimage/build.sh`) під
іменем `shaurma-shell`, а також запечений стартовий набір із
`dist/app-<edition>-linux/` — `backend.so`, `updater.so`,
`launcher-version.json` і **розпаковані** теки `frontend/` та
`updater-frontend/` (у кожній мусить бути `index.html`):

```bash
cp /шлях/до/твого/зібраного/shell ./shaurma-shell
chmod +x ./shaurma-shell
cp /шлях/до/dist/app-shaurma-linux/backend.so  .
cp /шлях/до/dist/app-shaurma-linux/updater.so  .
cp /шлях/до/dist/app-shaurma-linux/launcher-version.json .
mkdir -p seed
cp -r /шлях/до/dist/app-shaurma-linux/frontend seed/
cp -r /шлях/до/dist/app-shaurma-linux/updater-frontend seed/
```

Теки — саме в `seed/frontend` і `seed/updater-frontend`: маніфест бере
кожну окремим dir-джерелом з `dest:` (`frontend`, `updater-frontend`), тож
вони лягають у корінь збірки під власними іменами й не зливаються. Перед
`install` маніфест сам перевіряє розкладку і, якщо чогось бракує, падає з
переліком файлів, а не з загадковим `cp: cannot stat 'frontend/.'`.

`build.ps1 -BuildFlatpak` / Pack Manager роблять це самі — цей крок лише
для ручної збірки та налагодження маніфесту.

Якщо в тебе досі лише AppImage (а не окремий ELF-бінарник) — можеш
дістати бінарник прямо з нього:

```bash
./ShaurmLauncher.AppImage --appimage-extract
cp squashfs-root/usr/bin/* ./shaurma-shell   # знайди точну назву файлу всередині usr/bin/
chmod +x ./shaurma-shell
rm -rf squashfs-root
```

## Крок 2 — зібрати і протестувати локально

```bash
chmod +x build-and-publish.sh
./build-and-publish.sh all
```

Скрипт: збере ОБИДВІ редакції (`shaurma` / `base`, різні app-id) →
покладе результат у СПІЛЬНИЙ `shaurma-repo/` → згенерує
`shaurma.flatpakrepo` → зробить два переносних файли
`shaurma-launcher-shaurma.flatpak` і `shaurma-launcher-base.flatpak`.
Можна також збирати по одній (`./build-and-publish.sh shaurma` або
`./build-and-publish.sh base`) — обидва варіанти пишуть у той самий
`shaurma-repo/`, не стираючи одна одну.

Перевірка на своїй машині:

```bash
flatpak install --user shaurma-launcher-shaurma.flatpak
flatpak run com.shaurma.Launcher

flatpak install --user shaurma-launcher-base.flatpak
flatpak run com.shaurma.LauncherBase
```

Якщо все ок — застосунок(и) з'являться в меню з правильною іконкою і
стартують одразу, без мережі (компоненти вже запечені в образ).

## Крок 3 — план публікації для користувачів

Обери один із двох варіантів (можна обидва одразу):

### Варіант A — простий: файл `.flatpak` на редакцію, як зараз AppImage

Просто виклади `shaurma-launcher-shaurma.flatpak` і/або
`shaurma-launcher-base.flatpak` (ті самі файли, що і зараз роздаєш
AppImage) на свій сайт/Discord/CDN. Користувач робить:

```bash
flatpak install --user shaurma-launcher-shaurma.flatpak   # повна версія
flatpak install --user shaurma-launcher-base.flatpak      # base
```

Плюс: нічого додатково налаштовувати не треба, файл один на редакцію.
Мінус: користувач не отримує майбутні *самі* Flatpak-оновлення (оновлення
контенту лаунчера — backend/frontend — все одно йдуть як завжди через
твій CDN і apdater.so, це не залежить від Flatpak).

### Варіант B — правильний: власний Flatpak-репозиторій з автооновленнями shell

1. Виклади всю теку `shaurma-repo/` (згенеровану скриптом, з ОБОМА
   редакціями всередині) на будь-який http(s)-хостинг зі статичними
   файлами (nginx, GitHub Pages, S3+CloudFront, Cloudflare Pages тощо).
   Структура теки лишається як є — просто скопіюй її на сервер.
2. Відкоригуй `shaurma.flatpakrepo` — заміни `Url=file://...` на реальну
   публічну адресу, наприклад:
   ```
   Url=https://cdn.shaurma.example/flatpak-repo
   ```
3. Виклади сам файл `shaurma.flatpakrepo` теж на сайт (наприклад
   `https://shaurma.example/shaurma.flatpakrepo`), щоб на нього могли
   послатись користувачі чи посилання "Встановити" на сайті.
4. Користувач підключає репозиторій і сам обирає редакцію за app-id:
   ```bash
   flatpak remote-add --user shaurma https://shaurma.example/shaurma.flatpakrepo
   flatpak install --user shaurma com.shaurma.Launcher       # повна
   flatpak install --user shaurma com.shaurma.LauncherBase   # base
   ```

   **Підпис репозиторію.** `flatpak remote-add` без `--no-gpg-verify`
   працює ЛИШЕ для підписаного репозиторію. Щоб підписати, один раз
   створи ключ і передай його ID скрипту:

   ```bash
   gpg --quick-generate-key "Shaurma Flatpak <flatpak@shaurma.local>" rsa4096 sign never
   gpg --list-secret-keys --keyid-format=long     # скопіюй ID
   FLATPAK_GPG_KEY=<ID> ./build-and-publish.sh all
   gpg --export --armor <ID> > shaurma-key.asc    # роздати користувачам
   ```

   Скрипт підпише OSTree-репозиторій (`--gpg-sign`) і впише публічний
   ключ у `GPGKey=` файлу `shaurma.flatpakrepo` — після цього команда
   вище працює як є. Якщо ключ НЕ заданий, репозиторій лишається без
   підпису, і тоді обов'язково треба `--no-gpg-verify` (інакше flatpak
   відмовить з "GPG verification enabled, but no signatures found").

5. Коли випускаєш нову ВЕРСІЮ САМОГО SHELL-бінарника (не контенту
   лаунчера — той оновлюється через власний CDN як завжди) — просто
   заново запускаєш `./build-and-publish.sh all` на новому
   `shaurma-shell` і перезаливаєш оновлену теку `shaurma-repo/` на
   хостинг. Користувачі обох редакцій отримають оновлення звичайною
   командою:
   ```bash
   flatpak update
   ```

### Про Flathub

Публікувати саме в офіційний Flathub (щоб застосунок шукався в GNOME
Software без додавання стороннього репозиторію) — окремий, довший шлях:
потрібен публічний Git-репозиторій з маніфестом (окремий на кожен
app-id — Flathub вимагає один репозиторій маніфесту на один app-id), і
модератори Flathub зазвичай не пропускають застосунки, що самі
докачують виконуваний код зі стороннього CDN після встановлення (саме
так зараз працює твій bootstrap.go/updater.so). Якщо колись захочеш
туди — треба або узгодити це окремо з модераторами, або переробити так,
щоб весь контент запікався в Flatpak-образ при кожному релізі (без
власного CDN-апдейтера всередині пісочниці). Для старту раджу Варіант A
або B вище — вони повністю під твоїм контролем і не потребують нічийого
схвалення.

## Дозволи пісочниці (finish-args у маніфесті)

Вже прописані в обох `com.shaurma.Launcher.yml` /
`com.shaurma.LauncherBase.yml` (ідентичні набори):

- `--share=network` — updater.so перевіряє CDN і тягне новіші компоненти
  (перший запуск працює й без мережі — див. запечений набір вище).
- `--device=dri` — доступ до GPU для WebKitGTK (без цього може повернутись
  чорний екран, тільки через брак дозволу пісочниці, а не через баг
  розпакування zip, який вже виправлено в коді).
- `--socket=wayland`, `--socket=x11`, `--share=ipc` — дисплей. Саме
  `x11`, а НЕ `fallback-x11`: лаунчер (GTK4) малюється через Wayland, а
  гра (Minecraft/GLFW) — через X11/XWayland. `fallback-x11` віддає X11-сокет
  лише коли Wayland-сокета нема, тож на Wayland-сесії гра не мала б дисплея.
- D-Bus: `--own-name=org.wails.*`, `--own-name=org.kde.*`,
  `--talk-name=org.kde.StatusNotifierWatcher`,
  `--talk-name=org.freedesktop.Notifications` — трей-іконка, нотифікації і
  single-instance; повний `--socket=session-bus` НЕ потрібен.
- `--socket=pulseaudio` — звук у грі: Minecraft/Java запускається ДОЧІРНІМ
  процесом у цій самій пісочниці. Flatpak передає звук лише через
  PulseAudio-сумісний сокет (у PipeWire-системах це `pipewire-pulse`),
  тому прямого `/dev/snd` у пісочниці немає — і не потрібно.
  Бібліотеки, які цей сокет використовують (`libpulse.so.0`,
  `libasound.so.2`, `libpipewire-0.3.so.0`), уже є в
  `org.gnome.Platform//50`, тож LWJGL/OpenAL у грі знаходять їх без
  жодних додаткових модулів. Перевірка дозволу: `flatpak info
  --show-permissions com.shaurma.Launcher | grep pulseaudio`.
- `--filesystem=xdg-config/autostart:create` — перемикач «Запускати при
  старті» пише справжній `~/.config/autostart` хоста. Без цього дозволу
  файл лягав у `~/.var/app/<app-id>/config/autostart`, який стільниця не
  читає (перемикач нічого не робив). Усередині Flatpak автозапуск пишеться
  як `Exec=flatpak run <app-id>`, бо `/app/bin/...` на хості не існує
  (`backend/internal/autostart/autostart_linux.go`).
- `--filesystem=xdg-run/discord-ipc-0`,
  `--filesystem=xdg-run/app/com.discordapp.Discord` — Discord RPC: бекенд
  шукає Unix-сокет `discord-ipc-0..9` у `$XDG_RUNTIME_DIR`, а він у пісочниці
  приватний. Перший шлях — нативний Discord, другий — Discord із Flatpak.

Утиліти, яких немає в GNOME-рантаймі, збираються в сам образ: модуль
`wl-clipboard` дає `wl-copy`/`wl-paste`, які бекенд викликає для
копіювання в буфер обміну (`backend/clipboard_other.go`). На X11-only
сесіях `wl-copy` не працює — текст тоді копіюється через WebKitGTK
(`navigator.clipboard`, фолбек у фронтенді), а копіювання ЗОБРАЖЕНЬ на
чистому X11 недоступне (для нього потрібен `xclip`).

Якщо після встановлення побачиш чорний екран знову — спочатку перевір,
що всі ці finish-args реально застосувались (підстав потрібний app-id):

```bash
flatpak info --show-permissions com.shaurma.Launcher
flatpak info --show-permissions com.shaurma.LauncherBase
```

## Видалення: чому докачані компоненти лишаються після uninstall

`flatpak uninstall` за замовчуванням прибирає лише сам застосунок
(бінарник + .desktop + іконку — те, що реально запаковане в Flatpak-образ).
Робочі дані (налаштування, збірки, скопійований туди запечений набір
backend.so/updater.so/frontend/ та подальші оновлення) лежать у
контейнеризованому `$XDG_DATA_HOME` — фізично це
`~/.var/app/<app-id>/data/ShaurmLauncher` (повна редакція) і
`.../data/ShaurmLauncherBase` (base). Ім'я теки = ім'я бінарника в
`/app/bin`, тобто те саме, що в DEB/RPM/AppImage. Це навмисна, стандартна
поведінка Flatpak (як і для будь-якого іншого Flatpak-застосунку): дані
свідомо переживають uninstall, щоб не губити їх при переустановці або
випадковому видаленні. Щоб прибрати їх разом із застосунком, потрібен
явний прапор:

```bash
flatpak uninstall --delete-data com.shaurma.Launcher
flatpak uninstall --delete-data com.shaurma.LauncherBase
```

(DEB/RPM-пакети, на відміну від Flatpak, тепер самі чистять
`~/.local/share/<бінарник>` при `apt purge`/`rpm -e` — див. postrm.template
і `%postun` у shaurma-launcher.spec.template.)
