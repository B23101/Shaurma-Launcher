#!/usr/bin/env bash
# build-and-publish.sh — збирає com.shaurma.Launcher(Base).yml у спільний
# Flatpak-репозиторій ("shaurma-repo/") і за бажанням у переносний
# .flatpak-файл. Виконувати на машині, де вже встановлені flatpak та
# flatpak-builder (див. INSTALL нижче).
#
# ДВІ РЕДАКЦІЇ, ОКРЕМІ app-id:
#   shaurma -> com.shaurma.Launcher      (повна версія)
#   base    -> com.shaurma.LauncherBase  (базова версія)
# Обидві пишуться в ОДИН і той самий "shaurma-repo/" (--repo=), але під
# різними app-id — тобто різними OSTree-гілками всередині репозиторію,
# тому не перезаписують одна одну. Якщо раніше в тебе був лише один
# app-id на обидві редакції — це і була помилка: другий build стирав
# перший у тому самому репозиторії.
#
# ВИКОРИСТАННЯ:
#   ./build-and-publish.sh shaurma   # збирає лише повну версію
#   ./build-and-publish.sh base      # збирає лише base
#   ./build-and-publish.sh all       # збирає ОБИДВІ в один repo/ (за замовчуванням)
#
# ПЕРЕД ЗАПУСКОМ:
#   1. Поклади зібраний Linux-бінарник shell (той самий ELF, що зараз йде в
#      AppDir для AppImage — shell/linux/appimage/build.sh) у цю теку під
#      іменем "shaurma-shell" і зроби його виконуваним:
#        cp /шлях/до/ShaurmLauncher_shell_elf ./shaurma-shell
#        chmod +x ./shaurma-shell
#   2. Поклади запечений стартовий набір із dist/app-<edition>-linux/:
#        backend.so  updater.so  launcher-version.json
#        seed/frontend/  seed/updater-frontend/   <- РОЗПАКОВАНІ теки
#      (маніфест бере їх двома dir-джерелами з dest:, тож seed/frontend і
#      seed/updater-frontend лягають у корінь збірки окремими теками).
#      launcher-version.json — локальний маніфест тієї самої версії:
#      без нього перший запуск перекачує з CDN усе, що вже запечено.
#   3. Іконки вже лежать як shaurma-icon.png / shaurma-base-icon.png —
#      заміни на реальні, якщо потрібно окреме лого для base.
#
# INSTALL (Ubuntu/Debian, один раз):
#   sudo apt install flatpak flatpak-builder
#   flatpak remote-add --if-not-exists flathub https://flathub.org/repo/flathub.flatpakrepo
#   (org.gnome.Platform//50 і Sdk докачаються самі через --install-deps-from)
#
# ВАЖЛИВО: рантайм мусить бути org.gnome.Platform//50 — рівно те, що
# прописано в обох .yml і в pack-manager/internal/launcherbuild/wsl.go.
# org.freedesktop.Platform не має GTK4/WebKitGTK 6.0, під які зібраний
# shell, тож застосунок з ним просто не стартує.
#
# ПІДПИС РЕПОЗИТОРІЮ (необов'язково, але потрібно для звичайного
# `flatpak remote-add` БЕЗ --no-gpg-verify): задай ID GPG-ключа в
# оточенні — скрипт підпише ним репозиторій (--gpg-sign) і впише
# публічний ключ у GPGKey= файлу shaurma.flatpakrepo:
#   FLATPAK_GPG_KEY=<ID-ключа> ./build-and-publish.sh all

set -euo pipefail
cd "$(dirname "$0")"

EDITION="${1:-all}"
BUILD_DIR="build-dir"
REPO_DIR="shaurma-repo"
FLATHUB_REPO="https://dl.flathub.org/repo/flathub.flatpakrepo"

if [[ ! -f "shaurma-shell" ]]; then
  echo "ПОМИЛКА: немає файлу ./shaurma-shell — поклади сюди зібраний ELF-бінарник shell і зроби chmod +x." >&2
  exit 1
fi

# Швидка перевірка "чи це взагалі ELF", щоб не зловити ситуацію, коли сюди
# випадково потрапив не той бінарник (напр. з іншого Wails-проєкту типу
# pack-manager) — це раніше призводило до чужої іконки/назви (дефолтний
# Wails "W") і краху застосунку одразу після старту всередині пісочниці.
if ! file "shaurma-shell" | grep -q "ELF"; then
  echo "ПОПЕРЕДЖЕННЯ: ./shaurma-shell не виглядає як ELF-бінарник Linux ($(file -b "shaurma-shell")). Перевір, що це саме зібраний shell (shell/linux/appimage/build.sh), а не файл з іншої платформи чи іншого модуля (напр. pack-manager)." >&2
fi

# Запечений стартовий набір — рівно те, що робить лаунчер самодостатнім
# без мережі. launcher-version.json обов'язковий: з ним updater бачить, що
# версія вже стоїть, і не качає ті самі backend.so/updater.so/frontend.zip
# з CDN. Без нього перший запуск тягне ~120 МБ «поверх» образу.
missing_required=0
for f in backend.so updater.so launcher-version.json; do
  if [[ ! -f "$f" ]]; then
    echo "· немає $f" >&2
    missing_required=1
  fi
done
for d in seed/frontend seed/updater-frontend; do
  if [[ ! -f "$d/index.html" ]]; then
    echo "· немає $d/index.html (потрібна РОЗПАКОВАНА тека)" >&2
    missing_required=1
  fi
done
if [[ "$missing_required" -ne 0 ]]; then
  echo "ПОМИЛКА: неповний запечений набір — див. список вище. Скопіюй файли з dist/app-<edition>-linux/ у цю теку (розділ «Крок 1» у README) і запусти знову." >&2
  exit 1
fi

# Підпис репозиторію: без ключа flatpak-builder створює НЕпідписаний
# OSTree-репозиторій, і тоді remote-add працює лише з --no-gpg-verify
# (інакше: "GPG verification enabled, but no signatures found"). Раніше
# .flatpakrepo генерувався з порожнім GPGKey=, а README показував
# remote-add БЕЗ цього прапорця — тобто інструкція для користувача
# гарантовано падала.
GPG_KEY="${FLATPAK_GPG_KEY:-}"
if [[ -n "$GPG_KEY" ]] && ! gpg --list-secret-keys --with-colons "$GPG_KEY" >/dev/null 2>&1; then
  echo "ПОМИЛКА: FLATPAK_GPG_KEY=$GPG_KEY — такого секретного ключа немає в keyring (gpg --list-secret-keys --keyid-format=long)." >&2
  exit 1
fi

# Порожній масив під set -u: `${ARR[@]+...}` — інакше bash < 4.4 падає на
# unbound variable, коли ключа немає.
declare -a SIGN_ARGS=()
if [[ -n "$GPG_KEY" ]]; then
  SIGN_ARGS+=("--gpg-sign=$GPG_KEY")
fi

declare -a APP_IDS=()
case "$EDITION" in
  shaurma) APP_IDS=("com.shaurma.Launcher") ;;
  base)    APP_IDS=("com.shaurma.LauncherBase") ;;
  all)     APP_IDS=("com.shaurma.Launcher" "com.shaurma.LauncherBase") ;;
  *)
    echo "ПОМИЛКА: невідома редакція \"$EDITION\". Використай: shaurma / base / all" >&2
    exit 1
    ;;
esac

for APP_ID in "${APP_IDS[@]}"; do
  MANIFEST="$APP_ID.yml"
  if [[ ! -f "$MANIFEST" ]]; then
    echo "ПОМИЛКА: немає $MANIFEST поруч зі скриптом." >&2
    exit 1
  fi

  # Валідація метаданих ДО довгої збірки (пропускається, якщо утиліт немає).
  # Для власного репозиторію це лише попередження: appstreamcli повертає
  # ненульовий код навіть за відсутній <url type="homepage">, а під set -e
  # це раніше зупиняло всю збірку.
  if command -v desktop-file-validate >/dev/null 2>&1; then
    desktop-file-validate "$APP_ID.desktop" ||
      echo "ПОПЕРЕДЖЕННЯ: desktop-file-validate має зауваження до $APP_ID.desktop." >&2
  fi
  if command -v appstreamcli >/dev/null 2>&1; then
    appstreamcli validate --no-net "$APP_ID.metainfo.xml" ||
      echo "ПОПЕРЕДЖЕННЯ: appstreamcli має зауваження до $APP_ID.metainfo.xml (не критично для власного репо)." >&2
  fi

  echo "== [$APP_ID] 1. Збірка і публікація у спільний репозиторій ($REPO_DIR/) =="
  # Один прохід: --repo експортує результат у ТОЙ САМИЙ OSTree-репозиторій
  # для всіх редакцій (різні app-id не конфліктують). Окрема тека збірки на
  # app-id, щоб --force-clean однієї редакції не стирав кеш іншої.
  # --install-deps-from докачає org.gnome.Platform//50 і Sdk, якщо їх немає.
  flatpak-builder --force-clean --user --install-deps-from=flathub \
    --repo="$REPO_DIR" ${SIGN_ARGS[@]+"${SIGN_ARGS[@]}"} \
    "$BUILD_DIR/$APP_ID" "$MANIFEST"

  # Лінт маніфесту та репозиторію (якщо встановлено org.flatpak.Builder).
  if flatpak info org.flatpak.Builder >/dev/null 2>&1; then
    flatpak run --command=flatpak-builder-lint org.flatpak.Builder manifest "$MANIFEST" || \
      echo "ПОПЕРЕДЖЕННЯ: flatpak-builder-lint має зауваження (для власного репо не критично)." >&2
  fi

  echo "== [$APP_ID] 2. Переносний .flatpak-файл для прямого поширення =="
  if [[ "$APP_ID" == *Base ]]; then
    OUT_FLATPAK="shaurma-launcher-base.flatpak"
  else
    OUT_FLATPAK="shaurma-launcher-shaurma.flatpak"
  fi
  # --runtime-repo: інакше `flatpak install файл.flatpak` падає на машині, де
  # ще немає org.gnome.Platform//50 (бандл не містить рантайм).
  # FLATPAK_REPO_URL (публічна адреса $REPO_DIR) вшивається в бандл: після
  # встановлення з файлу `flatpak update` тягне оновлення звідти.
  declare -a URL_ARGS=()
  if [[ -n "${FLATPAK_REPO_URL:-}" ]]; then URL_ARGS+=("--repo-url=$FLATPAK_REPO_URL"); fi
  flatpak build-bundle --runtime-repo="$FLATHUB_REPO" ${URL_ARGS[@]+"${URL_ARGS[@]}"} "$REPO_DIR" "$OUT_FLATPAK" "$APP_ID"
  echo "   -> $OUT_FLATPAK"
done

echo "== 3. Генерація .flatpakrepo (файл, який користувачі імпортують) =="
# GPGKey — це base64 публічного ключа в armored-форматі (той самий формат,
# що у flathub.flatpakrepo). `base64 | tr -d '\n'` замість `base64 -w0`,
# щоб працювало і на GNU, і на BSD/macOS.
if [[ -n "$GPG_KEY" ]]; then
  GPG_KEY_B64="$(gpg --export --armor "$GPG_KEY" | base64 | tr -d '\n')"
  cat > "shaurma.flatpakrepo" <<EOF
[Flatpak Repo]
Title=Shaurma Launcher Repo
Url=file://$(pwd)/$REPO_DIR
# Заміни Url на реальний https://твій-домен/repo/, коли виставиш
# $REPO_DIR на публічний веб-сервер (nginx/CDN/GitHub Pages тощо).
Comment=Репозиторій Shaurma Launcher
RuntimeRepo=$FLATHUB_REPO
GPGKey=$GPG_KEY_B64
EOF
else
  cat > "shaurma.flatpakrepo" <<EOF
[Flatpak Repo]
Title=Shaurma Launcher Repo
Url=file://$(pwd)/$REPO_DIR
# Заміни Url на реальний https://твій-домен/repo/, коли виставиш
# $REPO_DIR на публічний веб-сервер (nginx/CDN/GitHub Pages тощо).
Comment=Репозиторій Shaurma Launcher
RuntimeRepo=$FLATHUB_REPO
# Репозиторій НЕ підписаний (FLATPAK_GPG_KEY не заданий) — підключати
# тільки з --no-gpg-verify, інакше flatpak відмовиться:
# "GPG verification enabled, but no signatures found".
EOF
fi

# .flatpakref на кожну редакцію: `flatpak install <url>.flatpakref` сам
# додає репозиторій, докачує org.gnome.Platform//50 (RuntimeRepo) і ставить
# застосунок — найпростіше встановлення для користувача.
for APP_ID in "${APP_IDS[@]}"; do
  cat > "$APP_ID.flatpakref" <<EOF
[Flatpak Ref]
Title=Shaurma Launcher
Name=$APP_ID
Branch=master
Url=file://$(pwd)/$REPO_DIR
# Заміни Url на публічну адресу репозиторію перед роздачею.
SuggestRemoteName=shaurma
IsRuntime=false
RuntimeRepo=$FLATHUB_REPO
EOF
done

cat <<EOF

Готово.

Або прямий файл (без репозиторію, як AppImage — один файл користувачу):
  flatpak install --user shaurma-launcher-shaurma.flatpak
  flatpak install --user shaurma-launcher-base.flatpak
  flatpak run com.shaurma.Launcher
  flatpak run com.shaurma.LauncherBase
EOF

if [[ -n "$GPG_KEY" ]]; then
  cat <<EOF

Локальна перевірка через репозиторій (репозиторій підписаний, ключ уже
вписаний у shaurma.flatpakrepo — --no-gpg-verify НЕ потрібен):
  flatpak remote-add --user shaurma-local file://$(pwd)/shaurma.flatpakrepo
  flatpak install --user shaurma-local com.shaurma.Launcher       # повна
  flatpak install --user shaurma-local com.shaurma.LauncherBase   # base
EOF
else
  cat <<EOF

Локальна перевірка через репозиторій (репозиторій БЕЗ підпису — обов'язково
з --no-gpg-verify, бо GPGKey у shaurma.flatpakrepo немає):
  flatpak remote-add --user --no-gpg-verify shaurma-local file://$(pwd)/shaurma.flatpakrepo
  flatpak install --user shaurma-local com.shaurma.Launcher       # повна
  flatpak install --user shaurma-local com.shaurma.LauncherBase   # base

Щоб репозиторій був підписаний (і remote-add працював без прапорця):
  gpg --quick-generate-key "Shaurma Flatpak <flatpak@shaurma.local>" rsa4096 sign never
  gpg --list-secret-keys --keyid-format=long     # скопіюй ID ключа
  FLATPAK_GPG_KEY=<ID> ./build-and-publish.sh all
  gpg --export --armor <ID> > shaurma-key.asc    # роздати користувачам
EOF
fi

cat <<EOF

Публікація для інших користувачів:
  - Виклади теку "$REPO_DIR/" на будь-який http(s)-хостинг (nginx, GitHub
    Pages, S3+CDN) — ОБИДВІ редакції лежать у ній разом.
  - Виправ Url у shaurma.flatpakrepo на реальну публічну адресу.
  - Користувачі роблять:
      flatpak remote-add --user shaurma https://твій-домен/repo/shaurma.flatpakrepo
      flatpak install --user shaurma com.shaurma.Launcher       # повна
      flatpak install --user shaurma com.shaurma.LauncherBase   # base
    (якщо репозиторій без підпису — додай --no-gpg-verify до remote-add)
EOF
