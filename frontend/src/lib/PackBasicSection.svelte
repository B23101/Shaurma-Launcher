<script lang="ts">
  // ── Розділ «Основне» на сторінці збірки ────────────────────────────────
  // Офіційні збірки Shaurma: фіксована інформація (назва, версія гри,
  // лоадер, опис, дата оновлення) — значення приходять з маніфесту і на
  // бекенді не змінюються (read-only).
  //
  // Кастомні збірки: той самий опис + кнопка «Редагувати». У режимі
  // редагування можна змінити назву, опис, версію Minecraft, лоадер та
  // його версію, колір, іконку (пресет або PNG) і фон. Зміна версії
  // гри/лоадера на бекенді очищає кеш старої версії. Після збереження
  // сторінка/сайдбар оновлюються одразу (onUpdated → refreshBuilds).
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { packAsset } from './packAssets.svelte'
  import AppearanceEditor from './AppearanceEditor.svelte'
  import WhitelistPanel from './WhitelistPanel.svelte'
  import VersionPickerModal from './VersionPickerModal.svelte'
  import { sortVersionsDesc } from './flexver'
  import type { BuildKind, CustomLoader, CustomPack, MCVersionEntry, LoaderVersionEntry, PackIndexEntry } from './types'
  import vanillaIcon from '../assets/images/vanilla_icon.png'
  import fabricIcon from '../assets/images/fabric_icon.png'
  import forgeIcon from '../assets/images/forge_icon.png'
  import neoforgeIcon from '../assets/images/neoforge_icon.png'
  import quiltIcon from '../assets/images/quilt_icon.png'

  let {
    pack,
    kind = null,
    onUpdated = () => {},
    onPackRenamed = () => {}
  } = $props<{
    pack: PackIndexEntry
    kind: BuildKind | null
    onUpdated?: () => void
    // Після зміни ID збірки (назви теки) — переключити відкриту сторінку
    // на новий ID (старий ID зникає зі списку, сторінка не має «повиснути»).
    // Передаємо і старий, і новий ID: батько синхронно мігрує посилання
    // (папки/порядок/закріплення) зі старого на новий, щоб збірка лишилась
    // на місці (гонка з $effect очищення папок при builds:changed).
    onPackRenamed?: (oldId: string, newId: string) => void
  }>()

  // Іконки лоадерів (ті самі, що у формі створення збірки).
  const loaders: { id: CustomLoader; icon: string; color: string }[] = [
    { id: 'vanilla', icon: vanillaIcon, color: '#8b8b8b' },
    { id: 'fabric', icon: fabricIcon, color: '#ff8a00' },
    { id: 'forge', icon: forgeIcon, color: '#d08b5b' },
    { id: 'neoforge', icon: neoforgeIcon, color: '#ff6b35' },
    { id: 'quilt', icon: quiltIcon, color: '#9d7cff' },
  ]
  // Пресет-іконки — у спільному компоненті PresetIconPicker (категорії,
  // пошук, згортання). Тут лишаємо лише палітру кольорів.


  // ── Форма редагування ──
  let saving = $state(false)
  let loadingCfg = $state(false)
  // Повний опис кастомної збірки (iconPath/backgroundPath/RAM/Java), які
  // не потрапляють у PackIndexEntry сторінки — підтягується при відкритті
  // редагування і зберігається без змін при збереженні форми.
  let cp = $state<CustomPack | null>(null)

  let name = $state('')
  let description = $state('')
  let mcVersion = $state('')
  let loader = $state<CustomLoader>('vanilla')
  let loaderVersion = $state('')
  let color = $state('#ff8a00')
  let iconName = $state('')
  let iconSrcPath = $state('')
  let backgroundSrcPath = $state('')
  // true, поки видалення банера ще не збережено (кнопка «Видалити»)
  let backgroundRemovePending = $state(false)
  let showSnapshots = $state(false)

  // Авто-збірки (Vanilla Release / Snapshot, керовані з налаштувань) —
  // версію гри й лоадер їм виставляє ЛИШЕ фонова синхронізація за
  // останньою версією Mojang. Раніше цю форму це не хвилювало: юзер міг
  // тут-таки поставити довільну MCVersion/Loader, і бекенд це приймав —
  // наступна фонова перевірка version або мовчки повертала все як було
  // (плутанина «зберіг, а не змінилось»), або, навпаки, нова версія Mojang
  // просто не підхоплювалась при запуску, бо збережене значення вже не
  // збігалось із тим, що очікував трекер. Тому тут відповідні контроли
  // лише READ-ONLY — щоб зайвий раз не блокувати всю форму запитом,
  // перевіряємо ЛОКАЛЬНО по вже завантаженому списку авто-збірок
  // (App.GetAutoPacks кешується AutoPacksSettings/сайдбаром теж).
  let isAutoPack = $state(false)
  async function checkAutoPack() {
    try {
      const list = await App.GetAutoPacks()
      isAutoPack = (list ?? []).some(p => p.id === pack.id)
    } catch { isAutoPack = false }
  }

  // Редагування ID збірки (назви теки) — тепер частина єдиної форми:
  // поле лишається тут (окремий рядок, бо це фізичне перейменування теки
  // на диску, а не звичайне поле), але зберігається тією ж кнопкою
  // «Зберегти», що й назва/опис/версії (див. saveText).
  let editPackId = $state('')
  let packIdCheck = $state<{ id: string; exists: boolean; suggested: string } | null>(null)
  const dirtyPackId = $derived(editPackId !== pack.id && editPackId.trim() !== '')

  async function onPackIdInput() {
    packIdCheck = null
    if (!editPackId || editPackId === pack.id) return
    packIdCheck = await App.CheckCustomPackIDChange(pack.id, editPackId).catch(() => null)
  }

  let showOld = $state(false)

  let mcVersions = $state<MCVersionEntry[]>([])
  let loaderVersions = $state<LoaderVersionEntry[]>([])
  let mcVersionsLoading = $state(false)
  let loaderVersionsLoading = $state(false)
  // Лоадери без версій під обрану MC ховаються з сітки (як Prism) і при
  // зникненні поточного лоадера відбувається авто-перемикання на Vanilla.
  let availableLoaders = $state<CustomLoader[]>(['vanilla', 'fabric', 'forge', 'neoforge', 'quilt'])
  let availabilityChecked = $state(false)

  const filteredMCVersions = $derived(
    mcVersions.filter(v => {
      if (v.type === 'release') return true
      if (v.type === 'snapshot') return showSnapshots
      return showOld
    })
  )

  async function loadMCVersions() {
    if (mcVersions.length) return
    mcVersionsLoading = true
    try { mcVersions = (await App.GetCustomPackMCVersions()) ?? [] }
    catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
    finally { mcVersionsLoading = false }
  }

  async function loadLoaderVersions() {
    if (loader === 'vanilla' || !mcVersion) { loaderVersions = []; loaderVersion = ''; return }
    loaderVersionsLoading = true
    try {
      const raw = await App.GetCustomPackLoaderVersions(loader, mcVersion)
      // Сортування як у Prism VersionFilterModel (SortRole, DescendingOrder)
      // — найновіші перші за FlexVer.
      loaderVersions = sortVersionsDesc([...(raw ?? [])], v => v.version)
      if (!loaderVersion || !loaderVersions.some(v => v.version === loaderVersion)) {
        const recommended = loaderVersions.find(v => v.recommended) ?? loaderVersions[0]
        loaderVersion = recommended?.version ?? ''
      }
    } catch (e: any) {
      loaderVersions = []
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      loaderVersionsLoading = false
    }
  }

  // Лоадер без версій для цієї MC зникає з сітки; якщо зник поточний —
  // перемикаємось на вільний (Vanilla, як «None» у Prism CustomPage).
  async function loadLoaderAvailability() {
    if (!mcVersion) return
    try {
      const list = (await App.GetCustomPackLoaderAvailability(mcVersion)) as CustomLoader[]
      availabilityChecked = true
      const next: CustomLoader[] = list.length ? list : ['vanilla']
      availableLoaders = next.includes('vanilla') ? next : ['vanilla', ...next]
      if (!availableLoaders.includes(loader)) {
        loader = 'vanilla'
        loaderVersion = ''
        loaderVersions = []
      }
    } catch { /* мережева помилка — лишаємо всі лоадери */ }
  }

  let lastLoaderKey = ''
  $effect(() => {
    const key = `${loader}:${mcVersion}`
    if (key !== lastLoaderKey) {
      lastLoaderKey = key
      loadLoaderVersions()
    }
  })

  let lastAvailKey = ''
  $effect(() => {
    if (mcVersion && lastAvailKey !== mcVersion) {
      lastAvailKey = mcVersion
      loadLoaderAvailability()
    }
  })

  function selectLoader(id: CustomLoader) {
    if (availableLoaders.includes(id)) loader = id
  }

  // Відкриття редагування: підтягуємо повний опис збірки і заповнюємо
  // форму поточними значеннями (список версій/лоадерів качається лінькво).
  async function openEdit() {
    loadingCfg = true
    try {
      const full = await App.GetCustomPack(pack.id)
      cp = full
      name = full.name
      description = full.description ?? ''
      mcVersion = full.mcVersion
      loader = (full.loader as CustomLoader) || 'vanilla'
      loaderVersion = full.loaderVersion ?? ''
      color = full.color || '#ff8a00'
      iconName = full.icon ?? ''
      iconSrcPath = ''
      backgroundSrcPath = ''
      // «З чим порівнювати зміни»: персоналізація (назва/іконка/колір/фон)
      // зберігається АВТОМАТИЧНО, зміна ВЕРСІЙ гри/лоадера — лише кнопкою
      // «Зберегти версії» поряд із селектами (дорога операція: перекачування
      // компонентів). dirtyVersions рахується саме проти цих «оригіналів».
      origVersions = { mc: full.mcVersion, loader: (full.loader as CustomLoader) || 'vanilla', lv: full.loaderVersion ?? '' }
      origText = { name: full.name, description: full.description ?? '' }
      lastSavedAppearance = { color: full.color || '#ff8a00', iconName: full.icon ?? '', iconSrcPath: '', backgroundSrcPath: '' }
      editPackId = full.id
      packIdCheck = null
      await checkAutoPack()
      await loadMCVersions()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      loadFailed = true
    } finally {
      loadingCfg = false
    }
  }

  // ── Оригінали для трекінгу змін ──
  // origVersions — з чим порівнювати ВЕРСІЇ (mc/loader/loaderVersion).
  // origText — з чим порівнювати ТЕКСТОВІ поля (назва/опис). Обидва
  // оновлюються лише після реального збереження (кнопкою), НЕ авто.
  let origVersions = $state<{ mc: string; loader: CustomLoader; lv: string }>({ mc: '', loader: 'vanilla', lv: '' })
  let origText = $state<{ name: string; description: string }>({ name: '', description: '' })

  // Чи змінились ВЕРСІЇ (MC/лоадер/версія лоадера) порівняно зі збереженим
  // станом. Ці поля дорогі (перекачування компонентів) — тому не авто.
  const dirtyVersions = $derived(
    mcVersion !== origVersions.mc ||
    loader !== origVersions.loader ||
    loaderVersion !== origVersions.lv
  )
  // Чи змінились текстові поля (назва/опис) — теж НЕ авто-зберігаються,
  // юзер сам вирішує коли зберегти кнопкою (щоб форма не «блимала» при
  // кожному натисканні клавіші).
  const dirtyText = $derived(
    name !== origText.name ||
    description !== origText.description
  )
  // Об'єднаний прапорець для кнопки «Зберегти» — показуємо, якщо є
  // незбережені зміни (текст, версії і/або ID). Раніше ID мав окрему
  // кнопку «Зберегти ID» поруч із полем — дві незалежні точки збереження
  // в одній формі означали, що натискання лише однієї з двох кнопок
  // застосовувало ЛИШЕ ту частину форми (баг: «змінив назву й ID, а
  // застосувалось тільки щось одне»). Тепер ID — рівноправне поле форми:
  // та сама кнопка «Зберегти» застосовує назву/опис/версії/ID разом.
  const dirtyAny = $derived(dirtyVersions || dirtyText || dirtyPackId)

  // Авто-збереження ВИГЛЯДУ (колір, іконка, фон) — debounce 700 мс після
  // останньої зміни. Це «косметика», яка безпечно зберігається у фоні без
  // перезапиту форми (щоб не блимало): бекенд оновлюємо, але НЕ викликаємо
  // openEdit() — локальні iconSrcPath/backgroundSrcPath просто скидаємо
  // після успіху (вони одноразові, шлях до файлу все одно вже застосований
  // у прев'ю). Назва/опис/версії при цьому завжди йдуть як ПОТОЧНІ значення
  // origText/origVersions (ще не збережені юзером текстові правки бекенду
  // не надсилаємо — так вигляд і текст лишаються незалежними).
  let autoSaveTimer: ReturnType<typeof setTimeout> | undefined
  let autoSaving = $state(false)
  let autoSavedTick = $state(0)
  let lastSavedAppearance = { color: '', iconName: '', iconSrcPath: '', backgroundSrcPath: '' }

  async function saveAppearance() {
    if (saving || !cp) return
    saving = true
    try {
      await App.UpdateCustomPack(pack.id, {
        // Назва/опис — ОСТАННІ ЗБЕРЕЖЕНІ (origText), а не те, що зараз у
        // полях: авто-збереження вигляду ніколи не «підхоплює» незбережені
        // текстові правки за кулісами.
        name: origText.name, description: origText.description,
        iconSrcPath, backgroundSrcPath, backgroundRemove: backgroundRemovePending, icon: iconName,
        // iconChanged: true — це збереження ВИГЛЯДУ (авто-save з $effect
        // спрацьовує саме на реальну зміну iconName/iconSrcPath), тож тут
        // бекенду можна довіряти поточному значенню iconName.
        iconChanged: true,
        color,
        mcVersion: origVersions.mc, loader: origVersions.loader, loaderVersion: origVersions.lv,
        useCustomRam: cp?.useCustomRam ?? false,
        minRamMb: cp?.minRamMb ?? 0,
        maxRamMb: cp?.maxRamMb ?? 0,
        useSeparateJava: cp?.useSeparateJava ?? false,
        javaPath: cp?.javaPath ?? '',
      })
      onUpdated()
      // iconSrcPath/backgroundSrcPath одноразові (шлях до файлу для
      // імпорту) — після збереження бекенд скопіював файл, локально їх
      // більше тримати не треба. cp оновлюємо тихо (без loadingCfg), щоб
      // прев'ю продовжувало показувати актуальний шлях без «блимання».
      if (cp) {
        cp = { ...cp, color, icon: iconName, backgroundPath: backgroundRemovePending ? '' : cp.backgroundPath }
        if (iconSrcPath) iconSrcPath = ''
        if (backgroundSrcPath) backgroundSrcPath = ''
      }
      backgroundRemovePending = false
      lastSavedAppearance = { color, iconName, iconSrcPath: '', backgroundSrcPath: '' }
      autoSavedTick++
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      saving = false
    }
  }

  $effect(() => {
    void color; void iconName; void iconSrcPath; void backgroundSrcPath; void backgroundRemovePending
    if (!cp || loadingCfg || saving) { clearTimeout(autoSaveTimer); return }
    const changed = color !== lastSavedAppearance.color ||
      iconName !== lastSavedAppearance.iconName ||
      !!iconSrcPath || !!backgroundSrcPath || backgroundRemovePending
    if (!changed) { clearTimeout(autoSaveTimer); return }
    clearTimeout(autoSaveTimer)
    autoSaveTimer = setTimeout(() => {
      if (!saving) { autoSaving = true; saveAppearance().finally(() => { autoSaving = false }) }
    }, 700)
    return () => clearTimeout(autoSaveTimer)
  })

  // Зберегти НАЗВУ/ОПИС/ВЕРСІЇ/ID — єдина кнопка «Зберегти», активна лише
  // коли є незбережені зміни (dirtyAny). Вигляд (колір/іконка/фон) на цей
  // момент уже збережений автоматично, тож передаємо його поточні
  // значення разом з новим текстом/версіями.
  //
  // ID (назва теки) зберігається ПЕРШИМ окремим викликом
  // (ChangeCustomPackID — фізичне перейменування теки на диску, інша
  // семантика помилок і побічних мігрцій, ніж звичайний Update), а решта
  // полів — після нього, вже під актуальним ID. Раніше це були дві
  // окремі кнопки в одній формі, і натискання лише однієї з них залишало
  // другу частину незбереженою непомітно для користувача — тепер обидва
  // кроки виконує один клік і одна кнопка.
  async function saveText() {
    if (!dirtyAny || saving) return
    if (!name.trim()) { toast(t('createPack.error.nameRequired'), 'error'); return }
    if (!mcVersion) { toast(t('createPack.error.mcVersionRequired'), 'error'); return }
    if (loader !== 'vanilla' && !loaderVersion) {
      toast(t('createPack.error.loaderVersionRequired'), 'error')
      return
    }
    if (dirtyPackId && packIdCheck?.exists) {
      toast(t('pack.basic.idTaken', { suggested: packIdCheck.suggested }), 'error')
      return
    }
    saving = true
    let currentId = pack.id
    try {
      if (dirtyPackId) {
        const renamed = await App.ChangeCustomPackID(pack.id, editPackId)
        currentId = renamed.id || editPackId
        // Сторінка/сайдбар мають синхронно перейти на новий ID до того, як
        // прийде подія builds:changed (та сама причина, що й у savePackId
        // раніше) — інакше збірка на мить «губиться» зі своєї папки.
        onPackRenamed(pack.id, currentId)
      }
      await App.UpdateCustomPack(currentId, {
        name, description, iconSrcPath, backgroundSrcPath, backgroundRemove: backgroundRemovePending, icon: iconName,
        // iconChanged: false — це збереження НАЗВИ/ОПИСУ/ВЕРСІЙ. iconName
        // тут може бути «застарілим» значенням поля (форма завжди тримає
        // його в стані), а не свідомим вибором користувача в цьому
        // збереженні — вигляд уже збережений окремо через saveAppearance()
        // (авто-save). Без цього прапорця бекенд бачив би непорожній icon
        // без нового iconSrcPath і видаляв би робочий PNG при кожному
        // збереженні назви.
        iconChanged: false,
        color,
        mcVersion, loader, loaderVersion,
        useCustomRam: cp?.useCustomRam ?? false,
        minRamMb: cp?.minRamMb ?? 0,
        maxRamMb: cp?.maxRamMb ?? 0,
        useSeparateJava: cp?.useSeparateJava ?? false,
        javaPath: cp?.javaPath ?? '',
      })
      backgroundRemovePending = false
      toast(t('pack.basic.saved'), 'success')
      onUpdated()
      // Тихо підтягуємо свіжий опис (без loadingCfg-спінера — форма вже
      // показує щойно збережені значення, повторний повний openEdit()
      // лише миготів би нею). Оригінали для трекінгу змін фіксуємо тут же.
      try {
        const full = await App.GetCustomPack(currentId)
        cp = full
        origVersions = { mc: full.mcVersion, loader: (full.loader as CustomLoader) || 'vanilla', lv: full.loaderVersion ?? '' }
        origText = { name: full.name, description: full.description ?? '' }
        lastSavedAppearance = { color: full.color || '#ff8a00', iconName: full.icon ?? '', iconSrcPath: '', backgroundSrcPath: '' }
        iconSrcPath = ''
        backgroundSrcPath = ''
        editPackId = full.id
        packIdCheck = null
      } catch { /* збереження вже пройшло успішно, рефреш — best-effort */ }
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      saving = false
    }
  }

  // Кастомні збірки: «Основне» ОДРАЗУ в режимі редагування — візуал збірки
  // (назва, іконка, фон, колір) видно й можна міняти без кнопки
  // «Редагувати». Форма підтягує повний опис один раз (або після помилки).
  let loadFailed = $state(false)
  $effect(() => {
    if (kind === 'custom' && !cp && !loadingCfg && !loadFailed) openEdit()
  })

  function selectPresetIcon(n: string) { iconName = n; iconSrcPath = '' }
  async function browseIcon() {
    try { const p = await App.BrowseImageFile(); if (p) { iconSrcPath = p; iconName = '' } } catch { /* скасовано */ }
  }
  async function browseBackground() {
    try { const p = await App.BrowseImageFile(); if (p) { backgroundSrcPath = p; backgroundRemovePending = false } } catch { /* скасовано */ }
  }
  // Видалити банер: прапорець backgroundRemovePending змушує авто-збереження
  // (і saveText) передати backgroundRemove=true бекенду — той прибере файл
  // з диска і очистить шлях. Скидається після успішного збереження.
  function removeBackground() {
    backgroundSrcPath = ''
    backgroundRemovePending = true
  }

  const iconPreviewUrl = $derived(
    iconSrcPath
      ? packAsset('local-file://' + iconSrcPath)
      : (!iconName && cp?.iconPath ? packAsset('local-file://' + cp.iconPath) : '')
  )
  const backgroundPreviewUrl = $derived(
    backgroundSrcPath
      ? packAsset('local-file://' + backgroundSrcPath)
      : (cp?.backgroundPath ? packAsset('local-file://' + cp.backgroundPath) : '')
  )

</script>

{#if kind === 'custom'}
  <div class="create-grid basic-edit-grid">
    <!-- ЛІВА КОЛОНКА: назва/опис/версії/лоадер + кнопки Зберегти/Скинути -->
    <div class="basic-main-col">
      <div class="card">
        <div class="card-head">
          <div class="ci"><i class="ti ti-pencil"></i></div>
          <div class="ctw"><div class="ct">{t('pack.basic.editTitle')}</div><div class="cd">{t('pack.basic.editDesc')}</div></div>
        </div>

        {#if loadingCfg}
          <div class="row"><div class="rl-sub"><i class="ti ti-loader spin"></i> {t('pack.basic.loading')}</div></div>
        {:else}
          <div class="row col">
            <div class="rl-name">{t('createPack.name')}</div>
            <input class="input" bind:value={name} />
          </div>
          <div class="row col">
            <div class="rl-name">{t('createPack.description')} <span class="muted" style="font-weight:500">{t('createPack.descriptionOptional')}</span></div>
            <input class="input" bind:value={description} />
          </div>
          {#if dirtyText}
            <div class="field-hint" style="padding:0 4px;color:var(--yellow)"><i class="ti ti-alert-triangle"></i> {t('pack.basic.textChangedHint')}</div>
          {/if}

          <!-- ID збірки (назва теки на диску) — поле форми, зберігається
               РАЗОМ з рештою полів кнопкою «Зберегти» внизу (не власною
               кнопкою: раніше дві незалежні кнопки в одній формі означали,
               що застосовувалась лише частина зміненого). -->
          <div class="row col" style="margin-top:4px">
            <div class="rl-name">{t('pack.basic.idLabel')} <span class="muted" style="font-size:11px;font-weight:400">({t('pack.basic.idHint')})</span></div>
            <input class="input mono" bind:value={editPackId} oninput={onPackIdInput} spellcheck={false} disabled={isAutoPack} />
            {#if packIdCheck?.exists}
              <div class="field-hint" style="color:var(--red)">
                <i class="ti ti-alert-triangle"></i>
                {t('pack.basic.idTaken', { suggested: packIdCheck.suggested })}
              </div>
            {:else if dirtyPackId && packIdCheck && !packIdCheck.exists}
              <div class="field-hint" style="color:var(--green)">
                <i class="ti ti-check"></i> {t('pack.basic.idWillSaveHint')}
              </div>
            {/if}
          </div>

          <!-- Зміна версій/лоадера — ДОРОГА операція (перекачування
               компонентів), тому цей блок зберігається ЛИШЕ кнопкою
               «Зберегти» і скидається «Скинути». Вигляд (іконка/колір/
               фон) редагується в окремому віджеті праворуч і зберігається
               автоматично у фоні. -->
          {#if isAutoPack}
            <div class="field-hint" style="padding:8px 10px;margin:4px 0;border-radius:8px;background:color-mix(in srgb, var(--yellow) 12%, transparent)">
              <i class="ti ti-lock"></i> {t('pack.basic.autoPackLocked')}
            </div>
          {/if}
          <div class="row">
            <div class="row-label"><div class="rl-name">{t('createPack.mcVersion')}</div></div>
            <div class="flex gap8">
              <span class="chip" class:on={showSnapshots} onclick={() => !isAutoPack && (showSnapshots = !showSnapshots)} role="button" tabindex="0" aria-disabled={isAutoPack}>{t('createPack.mcVersion.snapshot')}</span>
              <span class="chip" class:on={showOld} onclick={() => !isAutoPack && (showOld = !showOld)} role="button" tabindex="0" aria-disabled={isAutoPack}>{t('createPack.mcVersion.old')}</span>
              <VersionPickerModal
                value={mcVersion}
                onselect={(v) => mcVersion = v}
                disabled={mcVersionsLoading || isAutoPack}
                placeholder={mcVersionsLoading ? '…' : ''}
                title={t('createPack.mcVersion')}
                icon="ti-versions"
                ariaLabel={t('createPack.mcVersion')}
                options={filteredMCVersions.map(v => ({ value: v.id, label: v.id, sub: v.type !== 'release' ? v.type : undefined }))}
              />
            </div>
          </div>
          {#if !isAutoPack && mcVersion && mcVersion !== pack.mc_version}
            <div class="field-hint" style="padding:0 4px;color:var(--yellow)"><i class="ti ti-alert-triangle"></i> {t('pack.basic.versionChangeHint')}</div>
          {/if}

          <div class="row col" class:disabled-block={isAutoPack}>
            <div class="rl-name">{t('createPack.loader')}</div>
            {#if availabilityChecked && availableLoaders.length < loaders.length}
              <div class="field-hint" style="margin-bottom:6px;color:var(--yellow)"><i class="ti ti-alert-triangle"></i> {t('createPack.loader.unavailableHint')}</div>
            {/if}
            <div class="loader-grid">
              {#each loaders.filter(l => availableLoaders.includes(l.id)) as l (l.id)}
                <div class="loader-opt" class:sel={loader === l.id} class:disabled={isAutoPack} onclick={() => !isAutoPack && selectLoader(l.id)} role="button" tabindex="0" aria-disabled={isAutoPack}>
                  <img class="loader-ic" src={l.icon} alt="" style="--lcol:{l.color}" />
                  <span class="ln">{t(`createPack.loader.${l.id}`)}</span>
                  <span class="lt">{t(`createPack.loader.${l.id}.sub`)}</span>
                </div>
              {/each}
            </div>
            {#if loader !== 'vanilla'}
              <div class="flex" style="justify-content:space-between;margin-top:4px">
                <div class="rl-sub">{t('createPack.loaderVersion')}</div>
                <VersionPickerModal
                  value={loaderVersion}
                  onselect={(v) => loaderVersion = v}
                  disabled={loaderVersionsLoading || loaderVersions.length === 0 || isAutoPack}
                  placeholder={loaderVersionsLoading ? '…' : loaderVersions.length === 0 ? t('createPack.loaderVersion.none') : ''}
                  title={t('createPack.loaderVersion')}
                  icon="ti-versions"
                  ariaLabel={t('createPack.loaderVersion')}
                  emptyText={t('createPack.loaderVersion.none')}
                  options={loaderVersions.map(v => ({
                    value: v.version,
                    label: v.version,
                    badgeIcon: v.recommended ? 'ti-star' : undefined,
                    badgeTitle: v.recommended ? t('createPack.loaderVersion.recommended') : undefined
                  }))}
                />
              </div>
            {/if}
            {#if !isAutoPack && dirtyVersions}
              <div class="ver-change-bar">
                <span class="vcb-note"><i class="ti ti-alert-triangle"></i> {t('pack.basic.versionsChangedHint')}</span>
              </div>
            {/if}
          </div>

          <div class="row" style="border-bottom:none;padding-top:6px">
            <div style="flex:1"></div>
            <button class="btn btn-ghost" onclick={openEdit} disabled={saving} title={t('pack.basic.reset')}><i class="ti ti-refresh"></i> {t('pack.basic.reset')}</button>
            <button class="btn btn-primary" onclick={saveText} disabled={saving || !dirtyAny || !!(dirtyPackId && packIdCheck?.exists)}>
              <i class="ti {saving ? 'ti-loader spin' : 'ti-device-floppy'}"></i>
              {saving ? t('pack.basic.saving') : t('pack.basic.save')}
            </button>
          </div>
        {/if}
      </div>
    </div>

    <!-- ПРАВА КОЛОНКА: ОКРЕМИЙ ВІДЖЕТ «Вигляд» — авто-збереження
         (без кнопки). Стиль повторює CreatePackPage. -->
    <div class="preview-col">
      {#if !loadingCfg}
        <!-- Вигляд — СПІЛЬНИЙ компонент зі серверами (AppearanceEditor):
             одна розмітка, одна палітра, один PresetIconPicker. -->
        <AppearanceEditor
          title={t('pack.basic.appearanceTitle')}
          desc={t('pack.basic.appearanceDesc')}
          ciClass="violet"
          {iconPreviewUrl}
          {iconName}
          {color}
          onBrowseIcon={browseIcon}
          onSelectPreset={selectPresetIcon}
          onColorChange={(c: string) => color = c}
          {backgroundPreviewUrl}
          {backgroundRemovePending}
          onBrowseBackground={browseBackground}
          onRemoveBackground={removeBackground}
          autoSaveState={autoSaving ? 'saving' : (autoSavedTick > 0 ? 'saved' : 'idle')}
        />
      {/if}
    </div>
  </div>
{:else if kind === 'shaurma'}
  <div class="card">
    <div class="card-head">
      <div class="ci"><i class="ti ti-info-circle"></i></div>
      <div class="ctw"><div class="ct">{t('pack.tab.basic')}</div></div>
    </div>
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.basic.name')}</div></div>
      <div class="rl-name">{pack.name}</div>
    </div>
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.basic.version')}</div></div>
      <div class="select field-locked"><i class="ti ti-versions lf"></i>{pack.mc_version}</div>
    </div>
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.basic.loader')}</div></div>
      <div class="select field-locked"><i class="ti ti-package lf"></i>{pack.loader_type} {pack.loader_version}</div>
    </div>
    {#if pack.description}
      <div class="row col">
        <div class="rl-name">{t('pack.basic.description')}</div>
        <div style="font-size:12.5px;color:var(--text-mute);line-height:1.55">{pack.description}</div>
      </div>
    {/if}
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.basic.updated')}</div></div>
      <div class="mono" style="font-size:11.5px;color:var(--text-mute)">{pack.updated_at}</div>
    </div>
  </div>
  {#if kind === 'shaurma'}
    <div class="field-hint" style="padding:0 4px"><i class="ti ti-lock"></i> {t('pack.locked.hint')}</div>
  {/if}
  <!-- Моди, дозволені автором збірки понад офіційний маніфест (Pack
       Manager → Білий список): раніше в лаунчері їх було не видно
       взагалі — синк просто мовчки не видаляв такі jar під час
       перевірки цілісності файлів, без жодного сліду в UI. Панель сама
       малює свою картку і сама ховається, якщо список порожній. -->
  <WhitelistPanel packId={pack.id} />
{/if}

<style>
  /* Двоколонковий макет «Основного» (як у CreatePackPage): зліва картка
     з назвою/версіями/лоадером (кнопки Зберегти/Скинути), праворуч —
     окремий віджет «Вигляд» з авто-збереженням. */
  :global(.basic-edit-grid) { margin-bottom:18px; }
  .basic-main-col { min-width:0; }

  /* Прев'ю банера й чіп авто-збереження переїхали в AppearanceEditor
     (спільний віджет) — тут вони більше не потрібні. */

  /* Рядок «версії змінені → Зберегти» під селектами лоадера. Показується
     лише коли dirtyVersions; персоналізація зберігається сама. */
  .ver-change-bar {
    display:flex; align-items:center; gap:10px; flex-wrap:wrap;
    margin-top:8px; padding:8px 12px;
    background:rgba(245,158,11,.08); border:1px solid rgba(245,158,11,.3);
    border-radius:var(--r-sm);
  }
  .vcb-note {
    flex:1; min-width:0; font-size:11.5px; color:var(--yellow);
    display:flex; align-items:center; gap:6px;
  }

  /* Компактніші іконки лоадерів у «Основному»: картки займали забагато
     місця порівняно з рештою розділу (великі PNG 30px + паддинги). */
  :global(.loader-opt) { padding:9px 6px; }
  :global(.loader-opt .loader-ic) { width:24px; height:24px; }
  :global(.loader-opt .ln) { font-size:10.5px; }
  :global(.loader-opt .lt) { font-size:8.5px; }

  .field-hint {
    font-size: 12px;
    padding: 2px 4px;
    display: flex;
    align-items: center;
    gap: 5px;
  }
</style>
