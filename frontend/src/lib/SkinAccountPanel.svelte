<script lang="ts">
  // ══════════════════════════════════════════════════════════════════════
  // Акаунт Шаурми АКТИВНОГО акаунта лаунчера (обидві редакції).
  // Кожен акаунт лаунчера має СВІЙ акаунт Шаурми (окремі друзі, чат, P2P, скін). Акаунт
  // «лише з ніком» його не має. Створення: лаунчер підставляє нік, користувач лише підтверджує
  // особу в Google/Microsoft (OAuth, scope openid; жодних токенів не зберігається).
  // Бекенд: shaurma_account_shaurma.go → VPS. Worker акаунтів не веде.
  // ══════════════════════════════════════════════════════════════════════
  import { App } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import MiniSkin3D from './MiniSkin3D.svelte'
  import { pirateNicknameError } from './validation'

  let { accountName = '' }: { accountName?: string } = $props()

  type Info = { hasAccount: boolean; accountId?: string; nickname?: string; launcherAccountId?: string; launcherNick?: string; isLicensed: boolean; reason?: string }
  let info = $state<Info | null>(null)
  let loading = $state(true)
  let waiting = $state(false)
  let busy = $state('')
  let nickDraft = $state('')
  let newNick = $state('')
  let model = $state<'classic' | 'slim'>('classic')
  let skinPreview = $state('')
  let skinB64 = $state('')
  let fileInput: HTMLInputElement | undefined = $state()

  async function load() {
    try {
      info = await App.GetShaurmaAccount()
      nickDraft = info?.nickname ?? ''
      newNick = (info?.launcherNick ?? accountName ?? '').trim()
    } catch { info = null }
    loading = false
  }
  $effect(() => { void accountName; load() })
  $effect(() => {
    const h = () => { load() }
    window.addEventListener('shaurma-account:changed', h)
    return () => window.removeEventListener('shaurma-account:changed', h)
  })
  function notifyChanged() { window.dispatchEvent(new CustomEvent('skin-auth:changed')) }

  async function create(provider: 'google' | 'microsoft') {
    const nick = newNick.trim()
    const err = pirateNicknameError(nick)
    if (err) { toast(err, 'error'); return }
    waiting = true
    try {
      await App.CreateShaurmaAccount(nick, provider)
      await load()
      notifyChanged()
      toast('Акаунт Шаурми створено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { waiting = false }
  }
  function cancelLogin() { App.CancelShaurmaLogin(); waiting = false }

  async function saveNickname() {
    const nick = nickDraft.trim()
    const err = pirateNicknameError(nick)
    if (err) { toast(err, 'error'); return }
    busy = 'nick'
    try {
      await App.SetShaurmaNickname(nick)
      await load()
      notifyChanged()
      toast('Нік збережено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = '' }
  }
  async function copyId() {
    try { await navigator.clipboard.writeText(info?.accountId ?? ''); toast('ID скопійовано', 'success') }
    catch { toast('Не вдалося скопіювати', 'error') }
  }
  async function remove() {
    const proceed = await confirmDialog(
      'Видалити акаунт Шаурми?',
      'Акаунт, його друзі, чат і скін буде видалено на сервері та тут. Це незворотно: ID не відновити. Сам акаунт лаунчера й гра не зачіпаються.',
      { confirmLabel: 'Видалити', cancelLabel: 'Скасувати', danger: true },
    )
    if (!proceed) return
    busy = 'delete'
    try {
      await App.DeleteShaurmaAccount()
      await load()
      notifyChanged()
      toast('Акаунт Шаурми видалено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = '' }
  }

  async function pickSkin(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    if (!file.type.includes('png')) {
      toast('Потрібен PNG-файл скіна (64×64)', 'error')
      input.value = ''
      return
    }
    try {
      const dataUrl = await new Promise<string>((resolve, reject) => {
        const fr = new FileReader()
        fr.onload = () => resolve(String(fr.result))
        fr.onerror = () => reject(new Error('Не вдалося прочитати файл'))
        fr.readAsDataURL(file)
      })
      skinPreview = dataUrl
      skinB64 = dataUrl.split(',')[1] || ''
    } catch (err: any) { toast(String(err), 'error') }
    finally { input.value = '' }
  }

  async function uploadSkin() {
    if (!skinB64) { toast('Спершу оберіть PNG-файл скіна', 'error'); return }
    busy = 'skin'
    try {
      await App.UploadSkin(skinB64, model)
      toast('Скін завантажено на сервер скінів', 'success')
      skinPreview = ''
      skinB64 = ''
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = '' }
  }
</script>

<div class="sap">
  <div class="sap-note">
    <i class="ti ti-info-circle"></i>
    <div>
      Акаунт Шаурми — <b>окремий для кожного акаунта лаунчера</b>: свої друзі, чат, P2P і скін.
      Акаунт лише з ніком його не має, тож доступу до друзів, P2P і скінів не отримує.
      Це не Minecraft-акаунт: жодні ліцензійні дані чи токени на сервер Шаурми не передаються.
    </div>
  </div>

  {#if loading}
    <div class="sap-state"><i class="ti ti-loader spin"></i> Перевірка…</div>
  {:else if info?.reason === 'no_launcher_account'}
    <div class="sap-warn"><i class="ti ti-alert-triangle"></i> Спершу додайте акаунт у лаунчері.</div>
  {:else if info?.reason === 'no_hwid'}
    <div class="sap-warn"><i class="ti ti-alert-triangle"></i> Не вдалося визначити цей комп'ютер — акаунт Шаурми тут недоступний.</div>
  {:else if !info?.hasAccount}
    <div class="sap-block">
      <div class="sap-title"><i class="ti ti-user-plus"></i> Створити акаунт Шаурми</div>
      <div class="sap-sub">
        Для акаунта лаунчера «{info?.launcherNick || accountName}». Нік — лише те, як вас бачать друзі й у грі, він не обов'язково
        унікальний. Унікальний — ID, який ви отримаєте: за ним вас додають у друзі.
      </div>
      <input class="input mono" bind:value={newNick} placeholder="Нік" maxlength="16" disabled={waiting} />
      {#if waiting}
        <div class="sap-wait">
          <i class="ti ti-loader spin"></i>
          <span>Очікування входу в браузері… завершіть вхід.</span>
          <button class="btn btn-ghost btn-sm" onclick={cancelLogin}>Скасувати</button>
        </div>
      {:else}
        <div class="sap-row-inline">
          <button class="btn btn-primary" onclick={() => create('google')}><i class="ti ti-brand-google"></i> Підтвердити через Google</button>
          <button class="btn btn-ghost" onclick={() => create('microsoft')}><i class="ti ti-brand-windows"></i> Через Microsoft</button>
        </div>
        <div class="sap-sub">Вхід лише підтверджує, що ви — жива людина (захист від ботів). Пароль, пошта й доступ до вашого акаунта серверу не передаються.</div>
      {/if}
    </div>
  {:else}
    <div class="sap-block">
      <div class="sap-title"><i class="ti ti-id-badge-2"></i> Ваш ID</div>
      <div class="sap-sub">За цим ID вас додають у друзі. Він унікальний і не змінюється; нік можна міняти.</div>
      <div class="sap-row-inline">
        <input class="input mono" value={info.accountId} readonly onfocus={(e) => (e.currentTarget as HTMLInputElement).select()} />
        <button class="btn btn-primary btn-sm" onclick={copyId}><i class="ti ti-copy"></i> Копіювати</button>
      </div>
    </div>

    <div class="sap-block">
      <div class="sap-title"><i class="ti ti-user"></i> Нік (відображення)</div>
      <div class="sap-sub">Його бачать друзі й гра. Можна міняти не частіше двох разів на тиждень.</div>
      <div class="sap-row-inline">
        <input class="input mono" bind:value={nickDraft} placeholder="Ваш нік" maxlength="16"
               onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') saveNickname() }} />
        <button class="btn btn-primary btn-sm" onclick={saveNickname} disabled={busy === 'nick' || !nickDraft.trim() || nickDraft.trim() === info.nickname}>
          {#if busy === 'nick'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if} Зберегти
        </button>
      </div>
    </div>

    <div class="sap-block">
      <div class="sap-title"><i class="ti ti-photo"></i> Власний скін</div>
      <div class="sap-sub">PNG 64×64 (формат Minecraft). Скіни можна міняти не частіше ніж раз на 10 секунд.</div>
      <div class="sap-skin">
        <div class="sap-skin-preview">
          {#if skinPreview}
            <MiniSkin3D slim={model === 'slim'} textureUrl={skinPreview} />
          {:else}
            <div class="sap-skin-empty"><i class="ti ti-photo-off"></i></div>
          {/if}
        </div>
        <div class="sap-skin-controls">
          <div class="sap-models">
            <button class="sap-model" class:on={model === 'classic'} onclick={() => model = 'classic'}>
              <i class="ti ti-user"></i> Класична (Steve)
            </button>
            <button class="sap-model" class:on={model === 'slim'} onclick={() => model = 'slim'}>
              <i class="ti ti-user"></i> Тонкі руки (Alex)
            </button>
          </div>
          <div class="sap-row-inline">
            <label class="btn btn-ghost btn-sm" for="sap-skin-file" style="cursor:pointer">
              <i class="ti ti-upload"></i> Обрати PNG
            </label>
            <input id="sap-skin-file" bind:this={fileInput} type="file" accept="image/png" hidden onchange={pickSkin} />
            <button class="btn btn-primary btn-sm" onclick={uploadSkin} disabled={busy === 'skin' || !skinB64}>
              {#if busy === 'skin'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-cloud-upload"></i>{/if} Завантажити скін
            </button>
          </div>
        </div>
      </div>
    </div>

    <div class="sap-block">
      <button class="btn btn-ghost btn-sm" onclick={remove} disabled={busy === 'delete'}>
        {#if busy === 'delete'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-trash"></i>{/if} Видалити акаунт Шаурми
      </button>
    </div>
  {/if}
</div>

<style>
  .sap { display: flex; flex-direction: column; gap: 12px; }
  .sap-note {
    display: flex; align-items: flex-start; gap: 8px;
    font: 400 11px/1.5 var(--font-ui); color: var(--text-mute);
    background: color-mix(in srgb, var(--orange) 7%, transparent);
    border: 1px solid color-mix(in srgb, var(--orange) 25%, transparent);
    border-radius: 10px; padding: 9px 11px;
  }
  .sap-note i { color: var(--orange); font-size: 14px; margin-top: 1px; }
  .sap-note b { color: var(--text); }
  .sap-state { display: flex; align-items: center; gap: 8px; color: var(--text-mute); font-size: 12px; padding: 14px 2px; }
  .sap-block {
    display: flex; flex-direction: column; gap: 8px;
    padding: 12px; border-radius: 12px;
    background: var(--bg-card); border: 1px solid var(--border);
  }
  .sap-title { display: flex; align-items: center; gap: 7px; font: 700 12.5px/1 var(--font-ui); color: var(--text); }
  .sap-title i { color: var(--orange); font-size: 15px; }
  .sap-sub { font: 400 var(--fs-min)/1.5 var(--font-ui); color: var(--text-mute); }
  .sap-warn { display: flex; align-items: center; gap: 6px; font: 500 var(--fs-min)/1.4 var(--font-ui); color: var(--yellow); }
  .sap-wait {
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
    font: 500 11px/1.4 var(--font-ui); color: var(--orange);
  }
  .sap-rows { display: flex; flex-direction: column; gap: 3px; }
  .sap-row { display: flex; gap: 8px; font: 400 11px/1.4 var(--font-ui); }
  .sap-row span { color: var(--text-dim); min-width: 84px; }
  .sap-row b { color: var(--text); font-family: var(--font-mono); font-weight: 600; word-break: break-all; }
  .sap-row-inline { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .sap-row-inline .input { flex: 1 1 180px; min-width: 0; }
  .sap-skin { display: flex; gap: 14px; align-items: flex-start; flex-wrap: wrap; }
  .sap-skin-preview {
    width: 96px; height: 128px; border-radius: 10px; overflow: hidden; flex-shrink: 0;
    background: var(--bg-input); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center;
  }
  .sap-skin-empty { color: var(--text-dim); font-size: 26px; }
  .sap-skin-controls { flex: 1 1 240px; min-width: 0; display: flex; flex-direction: column; gap: 10px; }
  .sap-models { display: flex; gap: 6px; flex-wrap: wrap; }
  .sap-model {
    display: inline-flex; align-items: center; gap: 6px; cursor: pointer;
    padding: 6px 11px; border-radius: 8px;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute);
    font: 600 var(--fs-min)/1 var(--font-ui); transition: border-color .14s, color .14s, background .14s;
  }
  .sap-model:hover { color: var(--text); border-color: var(--border-hi); }
  .sap-model.on { color: var(--orange); border-color: color-mix(in srgb, var(--orange) 45%, transparent); background: color-mix(in srgb, var(--orange) 10%, transparent); }
</style>
