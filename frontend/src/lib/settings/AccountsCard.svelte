<script lang="ts">
  // AccountsCard.svelte — список акаунтів лаунчера. Порядок міняється
  // перетягуванням за значок ⠿ (pointer-drag з setPointerCapture: HTML5
  // draggable не спрацьовує через preventDefault на mousedown).
  import { flip } from 'svelte/animate'
  import { t } from '../i18n.svelte'
  import SCard from './SCard.svelte'
  import type { Account } from '../types'

  let {
    accounts, activeAccountId, skinAuthNick = '', headFor,
    dragAcc = $bindable<number | null>(null), dragOverRow = $bindable<number | null>(null),
    moveAccountTo, reloginAccount, switchAccount, removeAccount, openRenameAccount, onaddaccount,
  }: {
    accounts: Account[]
    activeAccountId: string
    skinAuthNick?: string
    headFor: (uuid?: string) => string
    dragAcc?: number | null
    dragOverRow?: number | null
    moveAccountTo: (from: number, to: number) => void
    reloginAccount: (acc: Account) => void
    switchAccount: (id: string) => void
    removeAccount: (id: string) => void
    openRenameAccount: (acc: Account) => void
    onaddaccount: () => void
  } = $props()

  // Локально перейменувати можна лише офлайн-акаунт, чий нік НЕ прив'язаний до
  // скін-сесії Shaurma: такий нік керується з гардероба (інакше нік у лаунчері
  // і на сервері скінів розійшлися б).
  const canRename = (a: Account) =>
    !a.isLicensed && !(skinAuthNick && skinAuthNick.toLowerCase() === (a.username ?? '').trim().toLowerCase())

  function gripDown(e: PointerEvent, i: number) {
    e.preventDefault()
    dragAcc = i
    dragOverRow = null
    try { (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId) } catch { /* ок */ }
  }
  function gripMove(e: PointerEvent) {
    if (dragAcc === null) return
    const row = document.elementFromPoint(e.clientX, e.clientY)?.closest?.('[data-acc-row]') as HTMLElement | null
    if (row) dragOverRow = Number(row.dataset.accRow)
  }
  function gripUp(e: PointerEvent) {
    const from = dragAcc, to = dragOverRow
    dragAcc = null; dragOverRow = null
    try { (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId) } catch { /* ок */ }
    if (from !== null && to !== null && from !== to) moveAccountTo(from, to)
  }
</script>

<SCard id="x:accounts" icon="ti-users" tone="violet" title={t('st.c.accounts')} desc={t('st.c.accounts.d')}>
  {#snippet actions()}
    <button class="btn btn-primary btn-sm" onclick={onaddaccount}><i class="ti ti-plus"></i> {t('st.accounts.add')}</button>
  {/snippet}
  {#each accounts as acc, i (acc.id)}
    <div class="st-item" data-acc-row={i} class:dragging={dragAcc === i}
         class:drop-before={dragAcc !== null && dragAcc !== i && dragOverRow === i && dragOverRow < dragAcc}
         class:drop-after={dragAcc !== null && dragAcc !== i && dragOverRow === i && dragOverRow > dragAcc}
         animate:flip={{ duration: 200 }}>
      <span class="st-grip" role="button" tabindex="-1" title={t('acc.dragHint')} aria-label={t('acc.dragHint')}
            onpointerdown={(e) => gripDown(e, i)} onpointermove={gripMove} onpointerup={gripUp}
            onpointercancel={() => { dragAcc = null; dragOverRow = null }}><i class="ti ti-grip-vertical"></i></span>
      <div class="st-avatar"><img src={headFor(acc.uuid)} alt="" /></div>
      <div class="st-item-body">
        <div class="st-item-name">{acc.username}
          {#if acc.id === activeAccountId}<span class="st-tag on">{t('acc.active')}</span>{/if}
        </div>
        <div class="st-item-sub">{acc.isLicensed ? t('st.accounts.licensed') : t('st.accounts.offline')}</div>
      </div>
      <div class="st-item-act">
        {#if acc.id !== activeAccountId}
          <button class="btn btn-ghost btn-sm" onclick={() => switchAccount(acc.id)}><i class="ti ti-user-check"></i> {t('acc.use')}</button>
        {/if}
        {#if acc.isLicensed}
          <button class="st-iconbtn" onclick={() => reloginAccount(acc)} title={t('acc.relogin')} aria-label={t('acc.relogin')}><i class="ti ti-refresh"></i></button>
        {/if}
        {#if canRename(acc)}
          <button class="st-iconbtn" onclick={() => openRenameAccount(acc)} title={t('acc.rename')} aria-label={t('acc.rename')}><i class="ti ti-edit"></i></button>
        {/if}
        <button class="st-iconbtn danger" onclick={() => removeAccount(acc.id)} title={t('st.delete')} aria-label={t('st.delete')}><i class="ti ti-trash"></i></button>
      </div>
    </div>
  {/each}
  {#if accounts.length === 0}
    <div class="st-empty"><i class="ti ti-user-off"></i><b>{t('st.accounts.empty')}</b></div>
  {/if}
</SCard>
