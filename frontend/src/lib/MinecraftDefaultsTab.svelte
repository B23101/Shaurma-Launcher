<script lang="ts">
  import UiModal from './UiModal.svelte'
  // ── Вкладка «Гра за умовч.» у налаштуваннях лаунчера ──────────────────
  // Порт старої фічі «Налаштування гри за замовчуванням» (OptionsFileManager +
  // MinecraftDefaultsConfig старого Java-лаунчера): користувач задає дефолтні
  // опції options.txt, які застосовуються до нових збірок (скачування Shaurma,
  // імпорт mrpack/zip, перший запуск). Зберігається JSON-блобом у SQLite.
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { confirmDialog, toast } from './toast.svelte'
  import SelectField from './SelectField.svelte'
  import { packAsset } from './packAssets.svelte'
  import type { BuildView, MinecraftDefaults } from './types'

  // Стартові значення = дефолти бекенду (model.DefaultMinecraftDefaults):
  // поки GetMinecraftDefaults не відповів, d уже типобезпечний і не може бути
  // null — жодних «possibly null» у шаблоні/обробниках (попередній
  // MinecraftDefaults|null змушував svelte-check жалітись на кожен d.* в
  // onclick/oninput, бо let d перепризначається).
  function defaultMCDefaults(): MinecraftDefaults {
    return {
      enabled: false,
      language: 'uk_ua',
      guiScale: 0,
      soundLevels: {
        master: 1.0, music: 0.0, record: 1.0, weather: 1.0, block: 1.0,
        hostile: 1.0, neutral: 1.0, player: 1.0, ambient: 1.0, voice: 1.0,
      },
      keyBindings: {
        'key.attack': 'key.mouse.left', 'key.use': 'key.mouse.right',
        'key.forward': 'key.keyboard.w', 'key.back': 'key.keyboard.s',
        'key.left': 'key.keyboard.a', 'key.right': 'key.keyboard.d',
        'key.jump': 'key.keyboard.space', 'key.sneak': 'key.keyboard.left.shift',
        'key.sprint': 'key.keyboard.left.control', 'key.inventory': 'key.keyboard.e',
        'key.swapOffhand': 'key.keyboard.f', 'key.drop': 'key.keyboard.q',
        'key.chat': 'key.keyboard.t', 'key.command': 'key.keyboard.slash',
        'key.playerlist': 'key.keyboard.tab', 'key.pickItem': 'key.mouse.middle',
        'key.screenshot': 'key.keyboard.f2', 'key.togglePerspective': 'key.keyboard.f5',
        'key.fullscreen': 'key.keyboard.f11', 'key.spectatorOutlines': 'key.keyboard.unknown',
        'key.smoothCamera': 'key.keyboard.unknown', 'key.advancements': 'key.keyboard.l',
        'key.saveToolbarActivator': 'key.keyboard.c', 'key.loadToolbarActivator': 'key.keyboard.x',
        'key.hotbar.1': 'key.keyboard.1', 'key.hotbar.2': 'key.keyboard.2',
        'key.hotbar.3': 'key.keyboard.3', 'key.hotbar.4': 'key.keyboard.4',
        'key.hotbar.5': 'key.keyboard.5', 'key.hotbar.6': 'key.keyboard.6',
        'key.hotbar.7': 'key.keyboard.7', 'key.hotbar.8': 'key.keyboard.8',
        'key.hotbar.9': 'key.keyboard.9',
      },
      fullscreen: false,
      enableVsync: true,
      maxFps: 120,
      autoJump: false,
      mouseSensitivity: 0.5,
      invertYMouse: false,
      fov: 0,
      darkMojangStudiosBackground: false,
    }
  }

  let d = $state<MinecraftDefaults>(defaultMCDefaults())
  // true — коли бекенд відповів реальними даними (екран-заглушка зникає).
  let loaded = $state(false)

  // ── Завантаження ──
  $effect(() => {
    App.GetMinecraftDefaults().then((v) => { d = v; loaded = true })
  })

  // Збереження з дебаунсом (слайдери/бінди швидко змінюються).
  let saveTimer: ReturnType<typeof setTimeout> | undefined
  function markChanged() {
    clearTimeout(saveTimer)
    saveTimer = setTimeout(async () => {
      try {
        await App.SaveMinecraftDefaults(d)
      } catch (e) {
        toast(String(e), 'error')
      }
    }, 400)
  }

  // ── Мови гри ──
  // Fallback-список, поки не завантажиться ПОВНИЙ набір з оригінальних
  // json-мов Mojang (App.GetGameLanguages: asset index версії останньої
  // збірки → minecraft/lang/*.json, назви з самих файлів мов). Якщо
  // бекенд відповів списком — використовуємо ЙОГО, а не цей.
  const MC_LANGS: [string, string][] = [
    ['en_us', 'English (US)'], ['en_gb', 'English (UK)'], ['uk_ua', 'Українська'],
    ['ru_ru', 'Русский'], ['de_de', 'Deutsch'], ['fr_fr', 'Français'],
    ['es_es', 'Español'], ['es_mx', 'Español (MX)'], ['pt_br', 'Português (BR)'],
    ['it_it', 'Italiano'], ['pl_pl', 'Polski'], ['tr_tr', 'Türkçe'],
    ['ja_jp', '日本語'], ['ko_kr', '한국어'], ['zh_cn', '简体中文'],
    ['zh_tw', '繁體中文'], ['ar_sa', 'العربية'],
  ]
  // Актуальний список: з Mojang, fallback — MC_LANGS.
  let mcLangs = $state<[string, string][]>(MC_LANGS)
  $effect(() => {
    let cancelled = false
    // Порожній рядок = «версія за умовч.». Без аргументу Wails падає
    // «очікує 1 аргумент(ів), отримано 0» (і помилка йшла в журнал).
    App.GetGameLanguages('')
      .then((list) => {
        if (cancelled || !Array.isArray(list)) return
        // Бекенд повертає [] (не помилку), коли версія гри ще не
        // встановлена локально й немає мережі, щоб узяти asset index
        // (loadGameLanguages → os.IsNotExist). Раніше це затирало
        // 17-мовний fallback ПОРОЖНІМ списком, і селект лишався майже
        // пустим, поки гру не завантажать. Порожню відповідь ігноруємо
        // і лишаємо fallback — оновлюємо лише коли бекенд реально
        // прислав мови.
        if (list.length === 0) return
        const mapped: [string, string][] = list.map(l => [l.code, l.name || l.code])
        if (d.language && !mapped.some(([c]) => c === d.language)) {
          mapped.push([d.language, d.language])
        }
        mcLangs = mapped
      })
      .catch(() => { /* офлайн/немає версій — лишаємо fallback-список */ })
    return () => { cancelled = true }
  })
  // ── GUI-масштаб: динамічно x1..xN за розміром екрана (як у старого
  //    лаунчера: maxScale = min(ширина/320, висота/240)). ──
  const guiScaleMax = $derived.by(() => {
    const w = window.screen?.width || 1920
    const h = window.screen?.height || 1080
    return Math.max(1, Math.min(Math.floor(w / 320), Math.floor(h / 240)))
  })
  const guiScaleOptions = $derived(['auto', ...Array.from({ length: guiScaleMax }, (_, i) => String(i + 1))])

  // ── Мова гри та GUI-масштаб: обидва селекти — через SelectField (той
  //    самий компонент, що й у решті лаунчера), тож випадаюче меню завжди
  //    у єдиному стилі UI і не «провалюється» під сусідні картки. ──

  // ── Звукові категорії ──
  const SOUND_CATS = ['master', 'music', 'record', 'weather', 'block', 'hostile', 'neutral', 'player', 'ambient', 'voice']

  // ── Бінди клавіш ──
  const BIND_KEYS = [
    'key.attack', 'key.use', 'key.forward', 'key.back', 'key.left', 'key.right',
    'key.jump', 'key.sneak', 'key.sprint', 'key.inventory', 'key.swapOffhand',
    'key.drop', 'key.chat', 'key.command', 'key.playerlist', 'key.pickItem',
    'key.screenshot', 'key.togglePerspective', 'key.fullscreen',
    'key.spectatorOutlines', 'key.smoothCamera', 'key.advancements',
    'key.saveToolbarActivator', 'key.loadToolbarActivator',
    'key.hotbar.1', 'key.hotbar.2', 'key.hotbar.3', 'key.hotbar.4',
    'key.hotbar.5', 'key.hotbar.6', 'key.hotbar.7', 'key.hotbar.8', 'key.hotbar.9',
  ]
  const BIND_LABELS: Record<string, string> = {
    'key.attack': 'Атака', 'key.use': 'Використати', 'key.forward': 'Вперед',
    'key.back': 'Назад', 'key.left': 'Вліво', 'key.right': 'Вправо',
    'key.jump': 'Стрибок', 'key.sneak': 'Крастись', 'key.sprint': 'Біг',
    'key.inventory': 'Інвентар', 'key.swapOffhand': 'Змінити руку',
    'key.drop': 'Викинути', 'key.chat': 'Чат', 'key.command': 'Команда',
    'key.playerlist': 'Список гравців', 'key.pickItem': 'Взяти блок',
    'key.screenshot': 'Скріншот', 'key.togglePerspective': 'Камера',
    'key.fullscreen': 'Повний екран', 'key.spectatorOutlines': 'Контури',
    'key.smoothCamera': 'Плавна камера', 'key.advancements': 'Досягнення',
    'key.saveToolbarActivator': 'Зберегти панель', 'key.loadToolbarActivator': 'Завантажити панель',
    'key.hotbar.1': 'Слот 1', 'key.hotbar.2': 'Слот 2', 'key.hotbar.3': 'Слот 3',
    'key.hotbar.4': 'Слот 4', 'key.hotbar.5': 'Слот 5', 'key.hotbar.6': 'Слот 6',
    'key.hotbar.7': 'Слот 7', 'key.hotbar.8': 'Слот 8', 'key.hotbar.9': 'Слот 9',
  }

  function formatKeyName(mcKey?: string): string {
    if (!mcKey || mcKey === 'key.keyboard.unknown') return t('defaults.key.none')
    if (mcKey.startsWith('key.mouse.')) {
      const m: Record<string, string> = { 'key.mouse.left': 'LMB', 'key.mouse.right': 'RMB', 'key.mouse.middle': 'MMB' }
      return m[mcKey] ?? mcKey
    }
    // Символьні клавіші — людяна назва замість дослівного "Grave.Accent" тощо.
    const symbolNames: Record<string, string> = {
      'key.keyboard.grave.accent': '`', 'key.keyboard.minus': '-', 'key.keyboard.equal': '=',
      'key.keyboard.left.bracket': '[', 'key.keyboard.right.bracket': ']',
      'key.keyboard.semicolon': ';', 'key.keyboard.apostrophe': "'",
      'key.keyboard.comma': ',', 'key.keyboard.period': '.', 'key.keyboard.slash': '/',
      'key.keyboard.backslash': '\\',
      'key.keyboard.keypad.add': 'Num +', 'key.keyboard.keypad.subtract': 'Num -',
      'key.keyboard.keypad.multiply': 'Num *', 'key.keyboard.keypad.divide': 'Num /',
      'key.keyboard.keypad.decimal': 'Num .', 'key.keyboard.keypad.enter': 'Num Enter',
    }
    if (symbolNames[mcKey]) return symbolNames[mcKey]
    return mcKey.replace('key.keyboard.', '').split('.').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ')
  }

  // Людяна назва бінда МОДА (напр. "key.iris.reload" → "Iris: Reload") —
  // options.txt не містить перекладу, лише технічний ідентифікатор з
  // lang-файлу мода; орієнтовна назва, а не точний переклад.
  function formatModBindLabel(bind: string): string {
    const parts = bind.split('.')
    const mod = parts[1] ?? ''
    const rest = parts.slice(2).join(' ') || mod
    const words = rest
      .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
      .replace(/[._-]+/g, ' ')
      .trim()
    const label = words
      .split(/\s+/)
      .filter(Boolean)
      .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
      .join(' ')
    if (!mod || mod === rest) return label || bind
    const modName = mod.charAt(0).toUpperCase() + mod.slice(1)
    return `${modName}: ${label}`
  }

  // Бінди модів, знайдені в d.keyBindings (з імпорту збірки), яких немає
  // у ванільному BIND_KEYS — напр. Iris/Sodium/JEI, збережені в тому ж
  // options.txt як key_<mod.key.name>.
  const modBindKeys = $derived.by(() => {
    return Object.keys(d.keyBindings)
      .filter((k) => !BIND_KEYS.includes(k))
      .sort((a, b) => formatModBindLabel(a).localeCompare(formatModBindLabel(b)))
  })

  // ── Захоплення клавіші (як у Minecraft) ──
  let capturingKey = $state<string | null>(null)

  function startCapture(bind: string) {
    if (capturingKey) capturingKey = null
    capturingKey = bind
  }
  function cancelCapture() {
    capturingKey = null
  }

  function keyEventToMc(e: KeyboardEvent): string | null {
    const code = e.code
    if (/^Key[A-Z]$/.test(code)) return 'key.keyboard.' + code.slice(3).toLowerCase()
    if (/^Digit[0-9]$/.test(code)) return 'key.keyboard.' + code.slice(5)
    if (/^F([1-9]|1[0-2])$/.test(code)) return 'key.keyboard.' + code.toLowerCase()
    const map: Record<string, string> = {
      Space: 'key.keyboard.space', Tab: 'key.keyboard.tab', Enter: 'key.keyboard.enter',
      Backspace: 'key.keyboard.backspace', Delete: 'key.keyboard.delete',
      Insert: 'key.keyboard.insert', Home: 'key.keyboard.home', End: 'key.keyboard.end',
      PageUp: 'key.keyboard.page.up', PageDown: 'key.keyboard.page.down',
      ArrowUp: 'key.keyboard.up', ArrowDown: 'key.keyboard.down',
      ArrowLeft: 'key.keyboard.left', ArrowRight: 'key.keyboard.right',
      ShiftLeft: 'key.keyboard.left.shift', ShiftRight: 'key.keyboard.right.shift',
      ControlLeft: 'key.keyboard.left.control', ControlRight: 'key.keyboard.right.control',
      AltLeft: 'key.keyboard.left.alt', AltRight: 'key.keyboard.right.alt',
      // Клавіші пунктуації/символів — раніше відсутні в мапі, тож
      // натискання «`» (Backquote) та сусідніх клавіш нічого не робило
      // (map[code] ?? null повертав null, бінд просто не встановлювався).
      Backquote: 'key.keyboard.grave.accent',
      Minus: 'key.keyboard.minus', Equal: 'key.keyboard.equal',
      BracketLeft: 'key.keyboard.left.bracket', BracketRight: 'key.keyboard.right.bracket',
      Semicolon: 'key.keyboard.semicolon', Quote: 'key.keyboard.apostrophe',
      Comma: 'key.keyboard.comma', Period: 'key.keyboard.period', Slash: 'key.keyboard.slash',
      Backslash: 'key.keyboard.backslash',
      CapsLock: 'key.keyboard.caps.lock',
      NumpadAdd: 'key.keyboard.keypad.add', NumpadSubtract: 'key.keyboard.keypad.subtract',
      NumpadMultiply: 'key.keyboard.keypad.multiply', NumpadDivide: 'key.keyboard.keypad.divide',
      NumpadDecimal: 'key.keyboard.keypad.decimal', NumpadEnter: 'key.keyboard.keypad.enter',
      Numpad0: 'key.keyboard.keypad.0', Numpad1: 'key.keyboard.keypad.1', Numpad2: 'key.keyboard.keypad.2',
      Numpad3: 'key.keyboard.keypad.3', Numpad4: 'key.keyboard.keypad.4', Numpad5: 'key.keyboard.keypad.5',
      Numpad6: 'key.keyboard.keypad.6', Numpad7: 'key.keyboard.keypad.7', Numpad8: 'key.keyboard.keypad.8',
      Numpad9: 'key.keyboard.keypad.9',
    }
    return map[code] ?? null
  }

  $effect(() => {
    if (!capturingKey) return
    const onKey = (e: KeyboardEvent) => {
      e.preventDefault()
      e.stopPropagation()
      if (e.key === 'Escape') {
        d.keyBindings[capturingKey!] = 'key.keyboard.unknown'
      } else {
        const name = keyEventToMc(e)
        if (name) d.keyBindings[capturingKey!] = name
      }
      cancelCapture()
      markChanged()
    }
    const onMouse = (e: MouseEvent) => {
      if (e.target instanceof Element && (e.target as HTMLElement).closest('.kb-capture')) return
      const name = e.button === 0 ? 'key.mouse.left' : e.button === 1 ? 'key.mouse.middle' : e.button === 2 ? 'key.mouse.right' : null
      if (name) d.keyBindings[capturingKey!] = name
      cancelCapture()
      markChanged()
    }
    window.addEventListener('keydown', onKey, true)
    window.addEventListener('mousedown', onMouse, true)
    return () => {
      window.removeEventListener('keydown', onKey, true)
      window.removeEventListener('mousedown', onMouse, true)
    }
  })

  // ── Імпорт з збірки ──
  let importModal = $state(false)
  let importPacks = $state<BuildView[]>([])
  let importing = $state(false)

  async function openImportModal() {
    try {
      // ?? [] — захист від null від бекенду: без цього, якщо GetBuilds
      // поверне null (порожній реєстр збірок), рядок `importPacks.length`
      // у шаблоні впав би з «Cannot read properties of null (reading
      // 'length')» і модалка «Імпортувати з збірки» не відкривалась би.
      importPacks = ((await App.GetBuilds()) ?? []).filter((b) => b.installed)
    } catch (e) {
      toast(String(e), 'error')
      return
    }
    importModal = true
  }
  async function importFromPack(packID: string) {
    importing = true
    try {
      const imported = await App.ImportMinecraftDefaultsFromPack(packID)
      imported.enabled = d.enabled
      d = imported
      importModal = false
      await App.SaveMinecraftDefaults(imported)
      toast(t('settings.saved'), 'success')
    } catch (e) {
      toast(String(e), 'error')
    } finally {
      importing = false
    }
  }

  // ── Скидання ──
  async function resetDefaults() {
    const ok = await confirmDialog(t('defaults.reset.confirm'), '', { danger: true })
    if (!ok) return
    try {
      const cur = d.enabled
      await App.ResetMinecraftDefaults()
      const fresh = await App.GetMinecraftDefaults()
      fresh.enabled = cur
      d = fresh
      toast(t('settings.saved'), 'success')
    } catch (e) {
      toast(String(e), 'error')
    }
  }
</script>

{#if loaded}
  <div class="card">
    <div class="card-head">
      <div class="ci violet"><i class="ti ti-settings"></i></div>
      <div class="ctw"><div class="ct">{t('settings.tab.defaults')}</div><div class="cd">{t('defaults.enableHint')}</div></div>
    </div>
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('defaults.enable')}</div><div class="rl-sub">{t('defaults.enableHint')}</div></div>
      <div class="toggle" class:on={d.enabled} onclick={() => { d.enabled = !d.enabled; markChanged() }}></div>
    </div>
  </div>

  <div class:disabled={!d.enabled}>
    <!-- Мова та GUI -->
    <div class="card">
      <div class="card-head"><div class="ci"><i class="ti ti-language"></i></div>
        <div class="ctw"><div class="ct">{t('defaults.section.lang')}</div></div>
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.language')}</div></div>
        <SelectField
          value={d.language}
          options={mcLangs.map(([code, name]) => ({ value: code, label: name }))}
          onchange={(v) => { d.language = v; markChanged() }}
          minWidth="220px"
          ariaLabel={t('defaults.language')}
          placeholder={d.language}
        />
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.guiscale')}</div><div class="rl-sub">{t('defaults.guiscaleHint')}</div></div>
        <SelectField
          value={String(d.guiScale)}
          options={guiScaleOptions.map(opt => ({
            value: opt === 'auto' ? '0' : opt,
            label: opt === 'auto' ? t('defaults.guiscale.auto') : 'x' + opt
          }))}
          onchange={(v) => { d.guiScale = v === '0' ? 0 : Number(v); markChanged() }}
          minWidth="120px"
          ariaLabel={t('defaults.guiscale')}
        />
      </div>
    </div>

    <!-- Звук -->
    <div class="card">
      <div class="card-head"><div class="ci green"><i class="ti ti-volume"></i></div>
        <div class="ctw"><div class="ct">{t('defaults.section.sound')}</div></div>
      </div>
      {#each SOUND_CATS as cat}
        <div class="row">
          <div class="row-label"><div class="rl-name">{t('defaults.sound.' + cat)}</div></div>
          <div class="snd-ctl">
            <input class="snd-slider" type="range" min="0" max="100" step="1"
              value={Math.round((d.soundLevels[cat] ?? 1) * 100)}
              oninput={(e: any) => { d.soundLevels[cat] = Number(e.target.value) / 100; markChanged() }} />
            <span class="snd-pct">{Math.round((d.soundLevels[cat] ?? 1) * 100)}%</span>
          </div>
        </div>
      {/each}
    </div>

    <!-- Клавіші -->
    <div class="card">
      <div class="card-head"><div class="ci orange"><i class="ti ti-keyboard"></i></div>
        <div class="ctw"><div class="ct">{t('defaults.section.keybinds')}</div></div>
      </div>
      <div class="field-hint" style="padding:4px 20px;color:var(--text-mute)"><i class="ti ti-info-circle"></i> {t('defaults.key.pressHint')}</div>
      <div class="kb-grid">
        {#each BIND_KEYS as bind}
          <button class="kb-row {capturingKey === bind ? 'capturing' : ''}" onclick={() => startCapture(bind)}>
            <span class="kb-label">{BIND_LABELS[bind] ?? bind}</span>
            <span class="kb-key mono">{capturingKey === bind ? t('defaults.key.waiting') : formatKeyName(d.keyBindings[bind])}</span>
          </button>
        {/each}
      </div>
    </div>

    <!-- Клавіші модів — з'являється, лише якщо є хоч один імпортований
         бінд поза ванільним списком (напр. після «Імпортувати з збірки»
         зі збіркою, де встановлені Iris/Sodium/JEI тощо). -->
    {#if modBindKeys.length > 0}
      <div class="card">
        <div class="card-head"><div class="ci orange"><i class="ti ti-puzzle"></i></div>
          <div class="ctw"><div class="ct">{t('defaults.section.modKeybinds')}</div></div>
        </div>
        <div class="field-hint" style="padding:4px 20px;color:var(--text-mute)"><i class="ti ti-info-circle"></i> {t('defaults.key.pressHint')}</div>
        <div class="kb-grid">
          {#each modBindKeys as bind}
            <button class="kb-row {capturingKey === bind ? 'capturing' : ''}" onclick={() => startCapture(bind)} title={bind}>
              <span class="kb-label">{formatModBindLabel(bind)}</span>
              <span class="kb-key mono">{capturingKey === bind ? t('defaults.key.waiting') : formatKeyName(d.keyBindings[bind])}</span>
            </button>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Екран -->
    <div class="card">
      <div class="card-head"><div class="ci"><i class="ti ti-device-desktop"></i></div>
        <div class="ctw"><div class="ct">{t('defaults.section.display')}</div></div>
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.fullscreen')}</div></div>
        <div class="toggle" class:on={d.fullscreen} onclick={() => { d.fullscreen = !d.fullscreen; markChanged() }}></div>
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.vsync')}</div></div>
        <div class="toggle" class:on={d.enableVsync} onclick={() => { d.enableVsync = !d.enableVsync; markChanged() }}></div>
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.maxfps')}</div></div>
        <div class="snd-ctl">
          <input class="snd-slider" type="range" min="10" max="260" step="10"
            value={d.maxFps}
            oninput={(e: any) => { d.maxFps = Number(e.target.value); markChanged() }} />
          <span class="snd-pct" style="min-width:96px">{d.maxFps >= 260 ? t('defaults.maxfps.unlimited') : d.maxFps + ' FPS'}</span>
        </div>
      </div>
    </div>

    <!-- Геймплей -->
    <div class="card">
      <div class="card-head"><div class="ci violet"><i class="ti ti-device-gamepad-2"></i></div>
        <div class="ctw"><div class="ct">{t('defaults.section.gameplay')}</div></div>
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.autojump')}</div></div>
        <div class="toggle" class:on={d.autoJump} onclick={() => { d.autoJump = !d.autoJump; markChanged() }}></div>
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.invertyMouse')}</div></div>
        <div class="toggle" class:on={d.invertYMouse} onclick={() => { d.invertYMouse = !d.invertYMouse; markChanged() }}></div>
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.sensitivity')}</div></div>
        <div class="snd-ctl">
          <input class="snd-slider" type="range" min="0" max="200" step="1"
            value={Math.round(d.mouseSensitivity * 200)}
            oninput={(e: any) => { d.mouseSensitivity = Number(e.target.value) / 200; markChanged() }} />
          <span class="snd-pct" style="min-width:56px">{Math.round(d.mouseSensitivity * 200)}%</span>
        </div>
      </div>
    </div>

    <!-- Відео -->
    <div class="card">
      <div class="card-head"><div class="ci blue"><i class="ti ti-photo"></i></div>
        <div class="ctw"><div class="ct">{t('defaults.section.visual')}</div></div>
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.fov')}</div></div>
        <div class="snd-ctl">
          <input class="snd-slider" type="range" min="70" max="110" step="1"
            value={Math.round(d.fov * 40 + 70)}
            oninput={(e: any) => { d.fov = (Number(e.target.value) - 70) / 40; markChanged() }} />
          <span class="snd-pct" style="min-width:56px">{Math.round(d.fov * 40 + 70)}°</span>
        </div>
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('defaults.darkMojang')}</div></div>
        <div class="toggle" class:on={d.darkMojangStudiosBackground} onclick={() => { d.darkMojangStudiosBackground = !d.darkMojangStudiosBackground; markChanged() }}></div>
      </div>
    </div>
  </div>

  <div class="card" style="border:none;background:transparent;box-shadow:none;padding:0">
    <div style="display:flex;gap:10px;flex-wrap:wrap">
      <button class="btn btn-ghost btn-sm" onclick={openImportModal}><i class="ti ti-download"></i> {t('defaults.import.from.pack')}</button>
      <button class="btn btn-ghost btn-sm btn-danger" onclick={resetDefaults}><i class="ti ti-rotate"></i> {t('defaults.reset')}</button>
    </div>
  </div>

  <!-- Імпорт з збірки -->
  {#if importModal}
    <UiModal
      open={true}
      size="md"
      title={t('defaults.import.title')}
      icon="ti-download"
      color="var(--orange)"
      zIndex="2100000"
      locked={importing}
      onclose={() => { if (!importing) importModal = false }}
    >
          <div class="rl-sub" style="margin-bottom:10px">{t('pack.options.copyPick')}</div>
          {#if (importPacks ?? []).length === 0}
            <div class="rl-sub">{t('defaults.import.noPacks')}</div>
          {:else}
            <div class="cp-list">
              {#each importPacks as cp (cp.build.id)}
                {@const cpIcon = packAsset(cp.build.iconUrl)}
                <button class="cp-item" style="--pcol:{cp.build.color || 'var(--orange)'}" disabled={importing} onclick={() => importFromPack(cp.build.id)}>
                  <div class="cp-ic" class:has-icon={!!cpIcon}>
                    {#if cpIcon}<img src={cpIcon} alt="" loading="lazy" />{:else}<i class="ti {cp.build.icon || 'ti-puzzle'}"></i>{/if}
                  </div>
                  <div class="cp-name">{cp.build.name}</div>
                  <div class="cp-meta mono muted">{cp.build.mcVersion} · {cp.build.loaderType}</div>
                </button>
              {/each}
            </div>
          {/if}
    </UiModal>
  {/if}
{/if}

<style>
  .disabled { opacity: .5; pointer-events: none; user-select: none; }

  .snd-ctl { display: flex; align-items: center; gap: 10px; min-width: 260px; }
  .snd-pct { font-family: var(--font-mono); font-size: 11.5px; font-weight: 700; color: var(--text-mute); min-width: 44px; text-align: right; }
  .snd-slider { -webkit-appearance: none; appearance: none; flex: 1; height: 6px; border-radius: 3px; background: var(--hover-strong, rgba(255,255,255,.09)); outline: none; cursor: pointer; }
  .snd-slider::-webkit-slider-thumb { -webkit-appearance: none; appearance: none; width: 14px; height: 14px; border-radius: 50%; background: var(--orange); border: 2px solid #fff; box-shadow: 0 2px 6px rgba(0,0,0,.35); cursor: pointer; }
  .snd-slider::-moz-range-thumb { width: 14px; height: 14px; border-radius: 50%; background: var(--orange); border: 2px solid #fff; cursor: pointer; }

  .kb-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; padding: 4px 20px 18px; }
  @media (max-width: 900px) { .kb-grid { grid-template-columns: 1fr; } }
  .kb-row {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    padding: 8px 12px; border-radius: var(--r-sm); border: 1px solid var(--border);
    background: var(--bg-panel); cursor: pointer; transition: .15s; font-family: inherit;
  }
  .kb-row:hover { border-color: var(--border-hi); }
  .kb-row.capturing { border-color: var(--orange); box-shadow: 0 0 0 2px rgba(var(--orange-rgb), .25); }
  .kb-label { font-size: 12px; font-weight: 600; color: var(--text); }
  .kb-key { font-size: 11.5px; font-weight: 700; color: var(--orange); background: rgba(var(--orange-rgb), .1); border: 1px solid rgba(var(--orange-rgb), .25); padding: 3px 8px; border-radius: 6px; white-space: nowrap; }
  .kb-row.capturing .kb-key { color: #fff; background: var(--orange); }

  .cp-list { display: flex; flex-direction: column; gap: 6px; max-height: 320px; overflow-y: auto; }
  .cp-item {
    display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 9px 12px; border-radius: var(--r-sm); border: 1px solid var(--border);
    background: var(--bg-panel); cursor: pointer; transition: .15s; color: var(--text); font-family: inherit;
  }
  .cp-item:hover { border-color: var(--border-hi); }
  .cp-ic {
    width: 28px; height: 28px; border-radius: 7px; flex-shrink: 0; overflow: hidden;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 16%, var(--bg-input-hi));
    color: var(--pcol, var(--orange)); display: flex; align-items: center; justify-content: center; font-size: 15px;
  }
  .cp-ic.has-icon { background: transparent; }
  .cp-ic img { width: 100%; height: 100%; object-fit: cover; border-radius: 7px; }
  .cp-name { font-size: 12.5px; font-weight: 700; flex: 1; text-align: left; }
  .cp-meta { font-size: 10.5px; }
</style>
