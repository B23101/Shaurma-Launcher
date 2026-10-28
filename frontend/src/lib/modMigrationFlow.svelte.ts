// ── Спільний стан аналізу міграції/портування модів ─────────────────────
// Вибір джерела, аналіз (з живими рядками з mods:migrate-analyze), підсумки,
// вибір модів для перенесення і скасування — один клас для всіх місць:
// діалог міграції/портування (ModMigrationDialog), створення збірки
// (CreatePackPage) і створення сервера (ServerWizard). Раніше кожне місце
// тримало власну копію цієї логіки.
//
// Ціль-сервер (ID "srv:…", modTarget.ts) — бекенд додає кожному рядку
// side: клієнтські моди не переносяться, одразу вибрані лише серверні,
// а моди з невизначеною стороною вирішує користувач.
import { App, on } from './wails'
import { t } from './i18n.svelte'
import { toast } from './toast.svelte'
import { isServerModTarget } from './modTarget'
import type { MigrationItem, MigrationPack, ModMigrationReport, ModMigrationResult } from './types'

export interface ModMigrationFlowOptions {
  // ID цілі: збірка, "srv:<id>", або '' / 'srv:' — ціль ще створюється.
  target: () => string
  mcVersion: () => string
  loader: () => string
  // 'port' — AnalyzeModPort (ті самі jar, лише на існуючий сервер).
  mode?: () => 'migrate' | 'port'
}

type Counts = { found: number; notFound: number; already: number; update: number }

function countStatuses(items: MigrationItem[]): Counts {
  const c = { found: 0, notFound: 0, already: 0, update: 0 }
  for (const it of items) {
    if (it.status === 'found') c.found++
    else if (it.status === 'not-found') c.notFound++
    else if (it.status === 'already') c.already++
    else if (it.status === 'update') c.update++
  }
  return c
}

export class ModMigrationFlow {
  packs = $state<MigrationPack[]>([])
  packsLoaded = $state(false)
  source = $state('')
  analyzing = $state(false)
  cancelling = $state(false)
  report = $state<ModMigrationReport | null>(null)
  // Живі рядки під час аналізу (статуси приходять частинами).
  live = $state<MigrationItem[]>([])
  analyzeDone = $state(0)
  analyzeTotal = $state(0)
  // Вибрані для перенесення моди (ключ — sourceFileName).
  selected = $state<Set<string>>(new Set())

  #opts: ModMigrationFlowOptions

  constructor(opts: ModMigrationFlowOptions) {
    this.#opts = opts
  }

  get forServer(): boolean { return isServerModTarget(this.#opts.target()) }

  // Клієнтські моди на сервер не переносяться зовсім.
  isTransferable = (it: MigrationItem): boolean =>
    (it.status === 'found' || it.status === 'update') && !(this.forServer && it.side === 'client')

  transferable = $derived((this.report?.items ?? []).filter(this.isTransferable))
  selectedItems = $derived(this.transferable.filter(it => this.selected.has(it.sourceFileName)))
  liveCounts = $derived(countStatuses(this.live))
  counts = $derived(countStatuses(this.report?.items ?? []))
  sideCounts = $derived.by(() => {
    const c = { server: 0, client: 0, unknown: 0 }
    for (const it of this.report?.items ?? []) if (it.side) c[it.side]++
    return c
  })
  // Дедуп живого списку за sourceFileName ({#each} з ключем падає на дублікаті).
  liveUnique = $derived.by(() => {
    const seen = new Map<string, MigrationItem>()
    for (const it of this.live) seen.set(it.sourceFileName, it)
    return [...seen.values()]
  })

  async loadPacks(excludeId = '') {
    try {
      this.packs = (await App.GetMigrationPacks(excludeId)) ?? []
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      this.packsLoaded = true
    }
  }

  setSource(id: string) {
    this.source = id
    this.reset()
  }

  reset() {
    this.report = null
    this.live = []
    this.analyzeDone = 0
    this.analyzeTotal = 0
    this.selected = new Set()
  }

  async analyze() {
    const target = this.#opts.target()
    if (!this.source || this.analyzing) return
    if (this.source === target) { toast(t('mods.migrateSamePack'), 'error'); return }
    this.reset()
    this.analyzing = true
    try {
      const res = this.#opts.mode?.() === 'port'
        ? await App.AnalyzeModPort(this.source, target)
        : await App.AnalyzeModMigration(this.source, target, this.#opts.mcVersion(), this.#opts.loader())
      this.live = res.items ?? []
      this.report = res
      // Одразу вибрані всі, що можна перенести; на сервер — лише серверні.
      this.selected = new Set(
        (res.items ?? [])
          .filter(it => this.isTransferable(it) && (!this.forServer || it.side === 'server'))
          .map(it => it.sourceFileName),
      )
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      this.analyzing = false
    }
  }

  // Бекенд зупиняє розбір і повертає частковий звіт (report.cancelled).
  async cancel() {
    if (!this.analyzing) return
    this.cancelling = true
    try { await App.CancelModMigrationAnalysis(this.#opts.target()) }
    catch { /* аналіз міг завершитись між кліком і запитом */ }
    finally { this.cancelling = false }
  }

  toggle(key: string) {
    const next = new Set(this.selected)
    if (next.has(key)) next.delete(key); else next.add(key)
    this.selected = next
  }

  toggleAll() {
    this.selected = this.selected.size === this.transferable.length
      ? new Set()
      : new Set(this.transferable.map(it => it.sourceFileName))
  }

  // Синхронна міграція в уже існуючу ціль (діалог міграції/портування).
  migrate(targetId = this.#opts.target()): Promise<ModMigrationResult> {
    return App.MigrateMods(targetId, this.selectedItems)
  }

  // Живі рядки аналізу: бекенд шле mods:migrate-analyze по одному моду.
  // Повертає відписку — викликати з onMount.
  listen(): () => void {
    return on('mods:migrate-analyze', (p: any) => {
      if (p?.packId !== this.#opts.target()) return
      if (p.total) this.analyzeTotal = p.total
      if (p.phase !== 'item' || !p.sourceFileName) return
      // Події йдуть з паралельних горутин — лічильник лише монотонний.
      this.analyzeDone = Math.max(p.current ?? 0, this.analyzeDone + 1)
      const item: MigrationItem = {
        sourceName: p.sourceName ?? '',
        sourceFileName: p.sourceFileName,
        sourceSha1: '',
        status: p.status ?? 'not-found',
        projectId: '',
        projectSlug: '',
        resolvedName: p.resolvedName ?? '',
        resolvedVersion: '',
        resolvedFilename: '',
        resolvedUrl: '',
        resolvedSha1: '',
        source: p.source ?? '',
        iconUrl: p.iconUrl ?? '',
        willReplace: false,
      }
      const idx = this.live.findIndex(it => it.sourceFileName === p.sourceFileName)
      this.live = idx >= 0 ? this.live.map((it, i) => (i === idx ? item : it)) : [...this.live, item]
    })
  }
}
