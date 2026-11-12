<script lang="ts">
  // ── Вікно запуску ярлика ─────────────────────────────────────────────
  // Живе в ОКРЕМОМУ вікні (frontend/shortcut.html), яке створює
  // backend/shortcut_window.go. Показує те саме мінімальне «картку запуску»
  // (іконка + назва + прогрес стадій).
  //
  // Дані: назву/іконку/колір збірки надсилає приховане головне вікно (подія
  // shortcut-window:info), прогрес — звичайна подія launch:progress.
  // «Скасувати» → App.ShortcutWindowCancel (зупиняє запуск, відкриває головний
  // екран). Закриває вікно backend, коли гра стартувала.
  import { onMount } from 'svelte'
  import { App, on } from './wails'
  import { setLanguage } from './i18n.svelte'
  import { syncAppearance } from './theme.svelte'
  import LaunchProgressOverlay from './LaunchProgressOverlay.svelte'
  import type { LaunchProgress } from './types'

  let packId = $state('')
  let name = $state('')
  let iconUrl = $state('')
  let presetIcon = $state('')
  let color = $state('')
  let progress = $state<LaunchProgress | null>(null)

  function applyInfo(info: any) {
    if (!info) return
    if (info.packId !== packId) progress = null // інша збірка — скидаємо прогрес
    packId = info.packId || ''
    name = info.name || ''
    iconUrl = info.iconUrl || ''
    presetIcon = info.presetIcon || ''
    color = info.color || ''
  }

  function cancel() {
    App.ShortcutWindowCancel().catch(() => {})
  }

  onMount(() => {
    let offAppearance: (() => void) | undefined
    syncAppearance().then(({ settings: s, off }) => {
      offAppearance = off
      if (s?.language) setLanguage(s.language)
    }).catch(() => {})

    const offs = [
      on('shortcut-window:info', applyInfo),
      on('launch:progress', (p: LaunchProgress) => {
        if (!p || (packId && p.instanceId !== packId)) return
        progress = p
      }),
    ]
    // Дані, що прийшли до підписки, лежать на бекенді.
    App.TakeShortcutLaunchInfo().then(applyInfo).catch(() => {})
    // Сторінка готова — backend показує вікно (без білого спалаху).
    App.ShortcutWindowReady().catch(() => {})
    return () => { offs.forEach(off => off()); offAppearance?.() }
  })
</script>

<LaunchProgressOverlay
  packName={name}
  packIcon={iconUrl}
  presetIcon={presetIcon}
  packColor={color}
  {progress}
  onCancel={cancel}
  windowMode={true}
/>
