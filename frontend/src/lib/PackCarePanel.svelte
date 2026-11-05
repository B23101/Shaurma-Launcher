<script lang="ts">
  import { App } from './wails'
  import { confirmDialog, toast } from './toast.svelte'
  let { packId }: { packId: string } = $props()
  let busy = $state(false)
  let error = $state('')
  let reversible = $state(false)
  let snapshots = $state<string[]>([])
  let selected = $state('')
  let preset = $state('balanced')
  async function refresh(id: string) {
    try {
      const [active, list] = await Promise.all([App.GetPerformancePreset(id), App.ListPackWorldSnapshots(id)])
      if (id !== packId) return
      reversible = active; snapshots = list; selected = list[0] || ''; error = ''
    } catch (e) { if (id === packId) error = String(e) }
  }
  $effect(() => { void refresh(packId) })
  async function run(action: () => Promise<unknown>, message: string) {
    if (busy) return
    busy = true; error = ''
    try { await action(); toast(message, 'success'); await refresh(packId) }
    catch (e) { error = String(e) }
    finally { busy = false }
  }
  async function restore() {
    if (!selected || busy) return
    if (!await confirmDialog('Відновити світи окремими копіями?', 'Поточні світи залишаться на місці. Копії з’являться в Minecraft в окремих папках із позначкою restored.', {confirmLabel: 'Відновити копії', cancelLabel: 'Скасувати', danger: false})) return
    await run(() => App.RestorePackWorldSnapshot(packId, selected), 'Копії світів відновлено')
  }
</script>

<section class="care" aria-labelledby="care-title" aria-busy={busy}>
  <h3 id="care-title"><i class="ti ti-gauge" aria-hidden="true"></i> Зручний старт</h3>
  <p>Застосовуйте зміни після виходу з гри. Моди, Java та параметри автора збірки залишаються під вашим окремим контролем.</p>
  <label for="care-preset">Профіль графіки</label>
  <select id="care-preset" bind:value={preset} disabled={busy}>
    <option value="balanced">Баланс</option><option value="low">Слабкий ПК</option>
  </select>
  <p>{preset === 'low' ? 'Дальність: 6 чанків · симуляція: 5 · швидка графіка · мінімум частинок · дальність сутностей: 75%.' : 'Дальність: 10 чанків · симуляція: 6 · детальна графіка · менше частинок · дальність сутностей: 100%.'}</p>
  <p>Початкові значення зберігаються до першого застосування. Приріст FPS залежить від комп’ютера та збірки.</p>
  <div class="actions">
    <button class="btn btn-primary" disabled={busy} onclick={() => run(() => App.ApplyPerformancePreset(packId, preset), 'Профіль графіки застосовано')}>Застосувати профіль</button>
    <button class="btn btn-ghost" disabled={busy || !reversible} onclick={() => run(() => App.ApplyPerformancePreset(packId, 'restore'), 'Початкову графіку відновлено')}>Повернути початкові</button>
  </div>
  <h3><i class="ti ti-shield-check" aria-hidden="true"></i> Захист одиночних світів</h3>
  <p>Копії зберігаються на цьому диску й займають приблизно стільки ж місця, скільки світи. Відновлення створює окремі світи.</p>
  <div class="actions">
    <button class="btn btn-ghost" disabled={busy} onclick={() => run(async () => { const name = await App.CreatePackWorldSnapshot(packId); if (!name) throw new Error('Немає одиночних світів для копіювання') }, 'Світи збережено')}>Зберегти світи</button>
    <button class="btn btn-ghost" disabled={busy} onclick={() => refresh(packId)}>Оновити список</button>
  </div>
  {#if snapshots.length}
    <label for="care-snapshot">Збереження (UTC)</label>
    <select id="care-snapshot" bind:value={selected} disabled={busy}>{#each snapshots as item}<option value={item}>{item}</option>{/each}</select>
    <button class="btn btn-ghost" disabled={busy || !selected} onclick={restore}>Відновити окремими світами</button>
  {:else}<p>Резервних копій ще немає.</p>{/if}
  {#if busy}<p role="status">Виконуємо дію. Дочекайтеся завершення…</p>{/if}
  {#if error}<p role="alert">{error}. Закрийте гру, перевірте вільне місце й повторіть.</p>{/if}
</section>

<style>
  .care { background:var(--bg-card); border:1px solid var(--border-hi); border-radius:14px; padding:20px; margin-bottom:16px; display:flex; flex-direction:column; gap:12px; }
  h3 { margin:0; font-size:15px; color:var(--text); display:flex; align-items:center; gap:8px; }
  h3:not(:first-child) { margin-top:12px; padding-top:20px; border-top:1px solid var(--border-hi); }
  h3 i { color:var(--orange); }
  p { margin:0; color:var(--text-mute); font-size:13px; line-height:1.55; overflow-wrap:anywhere; }
  label { color:var(--text); font-size:13px; }
  select { width:100%; min-width:0; padding:10px; border-radius:8px; border:1px solid var(--border-hi); color:var(--text); background:var(--bg-input); }
  .actions { display:flex; gap:8px; flex-wrap:wrap; }
  button { white-space:normal; min-height:40px; }
  button:focus-visible, select:focus-visible { outline:2px solid var(--orange); outline-offset:3px; }
  [role=alert] { color:var(--red); }
</style>
