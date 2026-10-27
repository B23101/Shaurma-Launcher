<script lang="ts">
  // AutoPacksCard.svelte — «Автоматичні збірки» (auto-vanilla / auto-snapshot):
  // самі стежать за новою версією Minecraft. Тумблер на ще не створеній збірці
  // створює її, на створеній — вмикає/вимикає (вимкнена ховається зі списків,
  // файли лишаються на диску).
  import { t } from '../i18n.svelte'
  import { packAsset } from '../packAssets.svelte'
  import SCard from './SCard.svelte'
  import type { AutoPackView } from '../types'

  let {
    autoPacks, busy = false, onToggle, onDelete, onOpen,
  }: {
    autoPacks: AutoPackView[]
    busy?: boolean
    onToggle: (kind: string, enabled: boolean) => Promise<void> | void
    onDelete: (view: AutoPackView) => void
    onOpen: (id: string) => void
  } = $props()

  let busyKind = $state('')
  async function toggle(kind: string, enabled: boolean) {
    if (busyKind) return
    busyKind = kind
    try { await onToggle(kind, enabled) } finally { busyKind = '' }
  }
  const title = (v: AutoPackView) => (v.kind === 'snapshot' ? t('st.autopacks.snapshot') : t('st.autopacks.release'))
  const loaderName = (l?: string) => (!l ? '' : l === 'vanilla' ? 'Vanilla' : l.charAt(0).toUpperCase() + l.slice(1))
</script>

<SCard id="x:autopacks" icon="ti-rocket" tone="violet" title={t('st.c.autopacks')} desc={t('st.c.autopacks.d')}>
  {#if busy}
    <div class="st-empty"><i class="ti ti-loader-2 spin"></i><span>{t('loading')}</span></div>
  {:else}
    {#each autoPacks as v (v.kind)}
      {@const icon = v.iconUrl ? packAsset(v.iconUrl) : ''}
      <div class="st-item" style="opacity:{v.created && !v.enabled ? .6 : 1}">
        <span class="st-item-ic" class:has-img={!!icon} class:dim={!v.created} style="--pcol:{v.color || 'var(--violet)'}">
          {#if icon}<img src={icon} alt="" />{:else}<i class="ti {v.icon || (v.kind === 'snapshot' ? 'ti-photo' : 'ti-box')}"></i>{/if}
        </span>
        <div class="st-item-body">
          <div class="st-item-name">{title(v)}
            {#if v.created && v.hasNewVersion}<span class="st-tag warn"><i class="ti ti-refresh"></i> {t('st.autopacks.newAvailable', { v: v.targetVersion ?? '' })}</span>
            {:else if v.created}<span class="st-tag ok"><i class="ti ti-check"></i> {t('st.autopacks.upToDate')}</span>{/if}
          </div>
          <div class="st-item-sub">
            {#if v.created}{v.mcVersion || '—'}{#if v.loader} · {loaderName(v.loader)}{/if}
            {:else}{t('st.autopacks.notCreated')}{/if}
          </div>
        </div>
        <div class="st-item-act">
          {#if v.created && v.id}
            <button class="st-iconbtn" onclick={() => onOpen(v.id!)} title={t('st.autopacks.open')} aria-label={t('st.autopacks.open')}><i class="ti ti-arrow-up-right"></i></button>
            <button class="st-iconbtn danger" onclick={() => onDelete(v)} title={t('st.delete')} aria-label={t('st.delete')}><i class="ti ti-trash"></i></button>
          {/if}
          {#if busyKind === v.kind}
            <span class="st-unit"><i class="ti ti-loader-2 spin"></i> {t('st.autopacks.creating')}</span>
          {:else}
            <button type="button" class="st-switch" class:on={v.enabled} role="switch" aria-checked={v.enabled}
                    aria-label={title(v)} onclick={() => toggle(v.kind, !v.enabled)}></button>
          {/if}
        </div>
      </div>
    {/each}
    <div class="st-pad"><div class="st-note"><i class="ti ti-info-circle"></i> {t('st.autopacks.footer')}</div></div>
  {/if}
</SCard>
