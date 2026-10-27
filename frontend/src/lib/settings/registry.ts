// registry.ts — ЄДИНЕ джерело правди про структуру налаштувань:
//   • які є розділи і як вони згруповані в меню (як у Prism: заголовок групи →
//     розділи);
//   • індекс для пошуку: КОЖЕН параметр (і зі схеми бекенду, і написаний
//     руками) має запис тут. Запис = якір `data-setting-key` у розмітці, тож
//     пошук завжди може доскролити саме до потрібного рядка.
//
// Тексти живуть в i18n під префіксом `st.`:
//   st.s.<розділ>          назва розділу        st.s.<розділ>.d   опис розділу
//   st.c.<картка>          назва картки
//   st.k.<ключ>            назва параметра зі схеми (+ `.d` — коротка підказка)
//   st.x.<назва>           назва «ручного» елемента (не зі схеми)

export type SectionId =
  | 'general' | 'appearance' | 'notifications' | 'account'
  | 'game' | 'java' | 'defaults' | 'console'
  | 'downloads' | 'privacy'
  | 'folders' | 'storage' | 'about'

export interface SectionDef {
  id: SectionId
  icon: string
  titleKey: string
  descKey: string
  groupId: string
}

export interface NavGroup {
  id: string
  titleKey: string
  sections: SectionDef[]
}

const sec = (id: SectionId, icon: string, groupId: string): SectionDef => ({
  id, icon, groupId, titleKey: `st.s.${id}`, descKey: `st.s.${id}.d`,
})

export const navGroups: NavGroup[] = [
  {
    id: 'launcher', titleKey: 'st.nav.launcher',
    sections: [
      sec('general', 'ti-adjustments', 'launcher'),
      sec('appearance', 'ti-palette', 'launcher'),
      sec('notifications', 'ti-bell', 'launcher'),
      sec('account', 'ti-user-circle', 'launcher'),
    ],
  },
  {
    id: 'minecraft', titleKey: 'st.nav.minecraft',
    sections: [
      sec('game', 'ti-device-gamepad-2', 'minecraft'),
      sec('java', 'ti-coffee', 'minecraft'),
      sec('defaults', 'ti-sparkles', 'minecraft'),
      sec('console', 'ti-terminal-2', 'minecraft'),
    ],
  },
  {
    id: 'online', titleKey: 'st.nav.online',
    sections: [
      sec('downloads', 'ti-download', 'online'),
      sec('privacy', 'ti-shield-lock', 'online'),
    ],
  },
  {
    id: 'data', titleKey: 'st.nav.data',
    sections: [
      sec('folders', 'ti-folder', 'data'),
      sec('storage', 'ti-database', 'data'),
      sec('about', 'ti-info-circle', 'data'),
    ],
  },
]

export const settingsSections: SectionDef[] = navGroups.flatMap((g) => g.sections)
export const settingsSectionIds: string[] = settingsSections.map((s) => s.id)
export const settingsSectionById: Record<string, SectionDef> =
  Object.fromEntries(settingsSections.map((s) => [s.id, s]))

// Старі ID розділів (збережені в uiState / посилання з інших місць) → нові.
const legacySections: Record<string, SectionId> = {
  window: 'general',
  autopacks: 'defaults',
  performance: 'java',
  shortcuts: 'folders',
  curseforge: 'downloads',
}
export function normalizeSectionId(id: string | undefined | null): SectionId {
  if (!id) return 'general'
  if (settingsSectionById[id]) return id as SectionId
  return legacySections[id] ?? 'general'
}

// ── Індекс пошуку ────────────────────────────────────────────────────────
export interface SearchItem {
  /** Якір: значення `data-setting-key` у розмітці. Для параметра зі схеми — його ключ. */
  id: string
  section: SectionId
  /** ID картки (ключ st.c.<card>) — показується у «хлібних крихтах». */
  card: string
  /** Додаткові слова для пошуку (синоніми, англійські назви) — не показуються. */
  kw?: string
}

const I = (section: SectionId, card: string, entries: Array<string | [string, string]>): SearchItem[] =>
  entries.map((e) => (typeof e === 'string' ? { id: e, section, card } : { id: e[0], section, card, kw: e[1] }))

export const searchItems: SearchItem[] = [
  // ── Загальні ──
  ...I('general', 'language', [['language', 'language мова інтерфейсу english українська'], ['x:importLang', 'переклад json своя мова']]),
  ...I('general', 'updates', [['autoUpdate', 'update оновлення'], ['updateChannel', 'beta бета стабільний stable канал']]),
  ...I('general', 'startup', [['startupMode', 'автозапуск автозавантаження startup windows система трей tray']]),
  ...I('general', 'window', [['closeAction', 'закрити хрестик close tray трей вихід'], ['minimizeAction', 'згорнути minimize трей панель завдань']]),

  // ── Вигляд ──
  ...I('appearance', 'theme', [
    ['x:theme', 'тема theme темна світла dark light oled система'],
    ['x:accent', 'акцент колір accent color градієнт'],
  ]),
  ...I('appearance', 'customTheme', ['themeCustomBg', 'themeCustomCard', 'themeCustomText']),
  ...I('appearance', 'font', [['font', 'шрифт font typeface'], ['fontPath', 'ttf otf файл шрифту'], ['x:fontsFolder', 'тека шрифтів fonts folder']]),
  ...I('appearance', 'sidebar', [
    ['sidebarOnlyPinned', 'закріплені pinned бічна панель сайдбар'],
    ['x:sidebarScale', 'розмір масштаб scale сайдбар'],
    ['x:sidebarWidth', 'ширина width сайдбар'],
    ['sidebarAnimAlways', 'gif іконки анімація сайдбар'],
    ['pinnedServersCompact', 'сервери закріплені компактні'],
  ]),
  ...I('appearance', 'pages', [['packBannerFull', 'банер збірки на всю ширину'], ['x:contentView', 'вигляд карток каталог список сітка grid']]),
  ...I('appearance', 'motion', [
    ['animationsEnabled', 'анімації animations ефекти'],
    ['performanceMode', 'слабкий пк продуктивність розмиття blur'],
    ['lowMemoryMode', 'економія пам\'яті оперативна пам\'ять low memory'],
    ['hardwareAcceleration', 'gpu апаратне прискорення'],
  ]),

  // ── Сповіщення ──
  ...I('notifications', 'notify', [
    ['notifyEnabled', 'повідомлення чат друзі трей toast'],
    ['notifyDurationSec', 'тривалість секунди час показу'],
    ['notifyDnd', 'dnd не турбувати тиша'],
    ['chatMuteAll', 'чат mute вимкнути чат'],
  ]),

  // ── Акаунти ──
  ...I('account', 'accounts', [['x:accounts', 'акаунти microsoft офлайн нік вхід вийти account']]),
  ...I('account', 'profile', [['x:profile', 'профіль шаурми shaurma id нік статистика']]),

  // ── Гра ──
  ...I('game', 'window', [
    ['fullscreen', 'повний екран fullscreen'],
    ['windowWidth', 'ширина вікна width resolution роздільна'],
    ['windowHeight', 'висота вікна height resolution роздільна'],
  ]),
  ...I('game', 'whilePlaying', [
    ['gameLaunchAction', 'згорнути лаунчер під час гри'],
    ['gameLaunchDelaySec', 'затримка згортання'],
    ['gameExitAction', 'після закриття гри показати лаунчер'],
    ['restoreOnCrash', 'краш крах впала показати'],
  ]),
  ...I('game', 'confirm', [
    ['confirmOnStop', 'зупинка гри підтвердження kill'],
    ['showRamWarnings', 'ram оперативна пам\'ять попередження'],
    ['tempModsDeleteByDefault', 'тимчасові моди видалення'],
  ]),
  ...I('game', 'serverChecks', [
    ['showServerWarnings', 'сервер запуск перевірка порт eula'],
    ['serverWarnIgnoreRam', 'сервер ram брак пам\'яті'],
    ['serverWarnIgnoreJava', 'сервер java версія'],
    ['serverWarnIgnoreDisk', 'сервер диск місце'],
    ['serverWarnIgnoreCrash', 'сервер краш минулий запуск'],
  ]),
  ...I('game', 'playtime', [
    ['recordGameTime', 'playtime ігровий час рахувати'],
    ['showGameTime', 'час під збірками'],
    ['showGameTimeOnPage', 'час на сторінці збірки'],
    ['showSessionTime', 'сесія поточна'],
    ['showTotalGameTime', 'загальний час'],
    ['gameTimeInHours', 'години hours'],
  ]),

  // ── Java та запуск ──
  ...I('java', 'memory', [['maxRAM', 'ram оперативка оперативна пам\'ять memory xmx гб mb']]),
  ...I('java', 'jvm', [['javaArgs', 'jvm аргументи java args xmx флаги']]),
  ...I('java', 'gpu', [['useDedicatedGPU', 'gpu відеокарта nvidia amd ноутбук дискретна']]),
  ...I('java', 'commands', [
    ['preLaunchCommand', 'команда перед запуском pre-launch'],
    ['wrapperCommand', 'wrapper обгортка optirun gamemode'],
    ['postExitCommand', 'команда після виходу post-exit'],
    ['envVars', 'змінні середовища env environment'],
  ]),

  // ── Нові збірки ──
  ...I('defaults', 'newPacks', [['autoDownloadAfterCreate', 'завантажувати одразу створення імпорт']]),
  ...I('defaults', 'mcOptions', [['x:mcOptions', 'options.txt мова звук клавіші керування fov fps sensitivity чутливість гучність']]),
  ...I('defaults', 'autopacks', [['x:autopacks', 'auto-vanilla auto-snapshot снапшот реліз автоматичні']]),
  ...I('defaults', 'import', [['x:import', 'prism modrinth міграція імпорт інші лаунчери']]),

  // ── Консоль і логи ──
  ...I('console', 'show', [
    ['showConsoleOnLaunch', 'консоль при запуску'],
    ['showConsoleOnCrash', 'консоль при краху'],
    ['showConsoleOnClose', 'консоль після закриття'],
  ]),
  ...I('console', 'look', [
    ['consoleFontFamily', 'шрифт консолі mono'],
    ['consoleFontMode', 'шрифт info warn error'],
    ['consoleFontSize', 'розмір тексту консолі'],
    ['consoleWrap', 'перенесення рядків wrap'],
    ['consoleMaxLines', 'максимум рядків буфер'],
    ['consoleInfoColor', 'колір звичайних повідомлень info'],
    ['consoleWarnColor', 'колір попереджень warn'],
    ['consoleErrorColor', 'колір помилок error'],
  ]),
  ...I('console', 'logs', [['saveLogs', 'журнал логи latest.log']]),
  ...I('console', 'crash', [
    ['disableAutoRepair', 'діагностика крах автоаналіз auto repair'],
    ['consoleAI', 'ші ai gemini аналіз збою'],
    ['consoleAIKey', 'gemini api ключ key'],
  ]),

  // ── Завантаження та моди ──
  ...I('downloads', 'speed', [
    ['maxConcurrentDownloads', 'одночасні завантаження швидкість потоки'],
    ['maxRetries', 'повтори помилка retry'],
    ['httpTimeoutSec', 'таймаут timeout очікування'],
  ]),
  ...I('downloads', 'mods', [
    ['allowAlphaBetaModUpdates', 'alpha beta оновлення модів'],
    ['useModrinthForBlocked', 'заблоковані моди curseforge modrinth'],
    ['disableModSuggestions', 'рекомендації модів підказки'],
  ]),
  ...I('downloads', 'curseforge', [['curseForgeApiKey', 'curseforge api ключ key']]),

  // ── Приватність і сервіси ──
  ...I('privacy', 'skins', [['disableSkins', 'скіни офлайн skins'], ['disableMojangSkinFallback', 'скіни mojang']]),
  ...I('privacy', 'p2p', [['disableP2P', 'p2p lan через інтернет мережа гра з друзями']]),
  ...I('privacy', 'discord', [
    ['discordRpcEnabled', 'discord rich presence присутність'],
    ['discordRPCDetails', 'discord перший рядок'],
    ['discordRPCState', 'discord другий рядок'],
  ]),
  ...I('privacy', 'cloud', [['disableCloudPlaytime', 'хмара ігровий час резервна копія cloud']]),

  // ── Теки та ярлики ──
  ...I('folders', 'folders', [['x:folders', 'теки шляхи папка збірок кешу логів java каталог']]),
  ...I('folders', 'shortcuts', [['x:shortcuts', 'ярлики робочий стіл пуск shortcut']]),

  // ── Сховище ──
  ...I('storage', 'usage', [['x:storageUsage', 'місце на диску кеш очистити розмір']]),
  ...I('storage', 'storagePacks', [['x:storagePacks', 'встановлені збірки розмір видалити']]),
  ...I('storage', 'cleanup', [
    ['x:cleanup', 'очищення зайві версії java бібліотеки асети сироти'],
    ['cleanupAfterPackDelete', 'видалення збірки авто очищення версії'],
  ]),

  // ── Про лаунчер ──
  ...I('about', 'app', [['x:version', 'версія version система архітектура'], ['x:updatesCheck', 'перевірити оновлення update']]),
  ...I('about', 'help', [['x:bug', 'баг помилка discord підтримка'], ['x:errorLog', 'журнал помилок mclo.gs логи лаунчера']]),
]

// ── Назви ───────────────────────────────────────────────────────────────
/** i18n-ключ назви елемента. */
export const labelKeyOf = (id: string) => (id.startsWith('x:') ? `st.x.${id.slice(2)}` : `st.k.${id}`)
/** i18n-ключ підказки (може не існувати — перевіряйте через hasText). */
export const hintKeyOf = (id: string) => (id.startsWith('x:') ? `st.x.${id.slice(2)}.d` : `st.k.${id}.d`)
export const cardKeyOf = (card: string) => `st.c.${card}`

// ── Пошук ───────────────────────────────────────────────────────────────
const norm = (s: string) =>
  s.toLowerCase().replace(/[’ʼ'`]/g, '').replace(/ё/g, 'е').replace(/\s+/g, ' ').trim()

export interface SearchHit extends SearchItem {
  score: number
}

/**
 * Шукає налаштування за назвою, підказкою, назвою картки/розділу та
 * синонімами. Кожне слово запиту має знайтись десь у тексті (І, а не АБО);
 * збіг у назві важить більше, ніж у підказці чи синонімах.
 */
export function searchSettings(query: string, t: (k: string) => string, limit = 12): SearchHit[] {
  const tokens = norm(query).split(' ').filter(Boolean)
  if (!tokens.length) return []
  const out: SearchHit[] = []
  searchItems.forEach((it, idx) => {
    const label = norm(t(labelKeyOf(it.id)))
    const hintRaw = t(hintKeyOf(it.id))
    const hint = hintRaw === hintKeyOf(it.id) ? '' : norm(hintRaw)
    const where = norm(`${t(cardKeyOf(it.card))} ${t(`st.s.${it.section}`)}`)
    const kw = norm(`${it.kw ?? ''} ${it.id}`)
    let score = 0
    for (const tok of tokens) {
      let s = 0
      if (label.startsWith(tok)) s = 10
      else if (label.split(' ').some((w) => w.startsWith(tok))) s = 8
      else if (label.includes(tok)) s = 5
      else if (kw.split(' ').some((w) => w.startsWith(tok))) s = 4
      else if (kw.includes(tok)) s = 2.5
      else if (hint.includes(tok)) s = 2
      else if (where.includes(tok)) s = 1
      if (s === 0) return // слово ніде не знайдено → цей запис не підходить
      score += s
    }
    out.push({ ...it, score: score - idx / 10000 })
  })
  return out.sort((a, b) => b.score - a.score).slice(0, limit)
}

// ── Перехід до параметра ────────────────────────────────────────────────
/**
 * Прокручує сторінку до елемента з `data-setting-key=id` і коротко підсвічує
 * його. Елемент може з'явитись у DOM не одразу (розділ ремаунтиться після
 * перемикання вкладки), тому опитуємо DOM кілька десятків кадрів.
 * Якір обов'язково має бути РЕАЛЬНИМ блоком (не display:contents) — інакше
 * scrollIntoView нічого не робить, саме це раніше ламало пошук.
 */
export function revealSetting(id: string, tries = 60): void {
  const el = document.querySelector<HTMLElement>(`[data-setting-key="${CSS.escape(id)}"]`)
  if (el && el.getClientRects().length > 0) {
    el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    el.classList.remove('st-flash')
    // перезапуск CSS-анімації
    void el.offsetWidth
    el.classList.add('st-flash')
    window.setTimeout(() => el.classList.remove('st-flash'), 2400)
    return
  }
  if (tries <= 0) return
  requestAnimationFrame(() => revealSetting(id, tries - 1))
}
