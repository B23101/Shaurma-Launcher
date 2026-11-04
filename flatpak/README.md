# Shaurma Launcher — Flatpak

Linux-версія лаунчера — один пакет Flatpak `ShaurmaLauncher.flatpak`, який лежить у кожному релізі на GitHub.

## Встановлення

```bash
curl -fsSL https://github.com/B23101/Shaurma-Launcher/releases/latest/download/install-linux.sh | bash
```

`install.sh` (у релізі — `install-linux.sh`):

1. якщо flatpak немає — ставить його пакетним менеджером дистрибутива (apt, dnf, zypper, pacman, apk, xbps, eopkg, emerge);
2. качає `ShaurmaLauncher.flatpak` з останнього релізу й звіряє SHA-256 із `SHA256SUMS`;
3. ставить лаунчер для поточного користувача. Рантайм `org.gnome.Platform//50` (GTK4, WebKitGTK 6.0) і драйвери GL flatpak бере з flathub сам.

Вручну: завантажте `ShaurmaLauncher.flatpak` і виконайте `flatpak install --user ShaurmaLauncher.flatpak` (або відкрийте файл у «Програмах»/Discover).

## Збірка

```bash
scripts/build-linux-flatpak.sh <версія> <тека з frontend.zip і updater-frontend.zip> <тека результату>
```

Скрипт однаковий для GitHub (job `linux-flatpak` у `.github/workflows/build-launcher.yml`) і для WSL/Linux. Потрібні `flatpak`, `flatpak-builder`, `unzip`. У WSL задайте `FLATPAK_WORK_DIR=$HOME/...`, щоб проміжні файли лежали на диску Linux, а не в `/mnt/c`.

- `com.shaurma.Launcher.yml` — маніфест. Код компілюється всередині `org.gnome.Sdk//50`, тож бінарники гарантовано сумісні з рантаймом на будь-якому дистрибутиві.
- `build-in-sdk.sh` — збирає `ShaurmLauncher`, `backend.so`, `updater.so` і ставить їх у `/app` разом із запеченим набором `/app/shaurma-seed` (фронтенд і `launcher-version.json`).
- У пакеті також `wl-clipboard` (буфер обміну на Wayland) і `xrandr` (потрібен старим версіям Minecraft на LWJGL 2).

## Оновлення

Shell і бібліотеки лежать у read-only `/app`, тому нова версія = новий пакет. Pack Manager вносить SHA-256 `ShaurmaLauncher.flatpak`
у підписаний маніфест релізу (платформа `linux-amd64`). Updater у Flatpak качає пакет, перевіряє підпис і хеш, виконує
`flatpak-spawn --host flatpak install --reinstall --bundle …` (дозвіл `--talk-name=org.freedesktop.Flatpak`) і перезапускає лаунчер.
Дані користувача (`~/.var/app/com.shaurma.Launcher`) лишаються; нова версія при першому старті розкладає свій запечений набір.

## Дозволи пісочниці

Мережа, GPU (`--device=dri`), Wayland і X11 (гра працює через X11/XWayland), звук (`--socket=pulseaudio`), трей і сповіщення,
автозапуск (`~/.config/autostart`), Discord Rich Presence, доступ до flatpak на хості для самооновлення.
