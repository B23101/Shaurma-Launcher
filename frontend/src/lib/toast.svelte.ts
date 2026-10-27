// ── Toast + Confirm: заміна браузерних alert()/confirm() ──────────────
//
// Лаунчер НЕ використовує нативні alert/confirm/prompt — все через
// інтерфейс: тости (повідомлення) та модалка підтвердження. Це єдина
// точка входу, щоб жоден компонент не пробував викликати браузерні
// діалоги. Компоненти: Toasts.svelte / ConfirmModal.svelte.

export type ToastKind = 'success' | 'error' | 'info' | 'warn'

export interface ToastItem {
  id: number
  kind: ToastKind
  message: string
  // Дія-кнопка в тості (напр. «Відкрити теку» після збереження логу).
  action?: { label: string; onClick: () => void }
}

// Прямий експорт стану: компоненти імпортують toasts і Svelte 5 сам
// відстежує зміни (read of $state під час render).
export const toasts = $state<ToastItem[]>([])
let nextId = 1

// Де показувати тости: знизу справа (основний UI) чи зверху справа.
// Повноекранний майстер налаштування перемикає це на 'top' — інакше
// тости («Успішний вхід» після входу в акаунт) перекривають кнопку
// «Далі» у футері майстра (тости мають z-index 2100000, майстер —
// 99999, тож фізично лягали поверх кнопки).
//
// ВАЖЛИВО: Svelte 5 забороняє експортувати $state, який ПЕРЕПРИСВОЮЄТЬСЯ
// (state_invalid_export). Тому зберігаємо обʼєкт і мутуємо властивість.
export const toastAnchor = $state<{ pos: 'bottom' | 'top' }>({ pos: 'bottom' })

/** Перемкнути позицію тостів (див. toastAnchor). */
export function setToastAnchor(v: 'bottom' | 'top') {
  toastAnchor.pos = v
}

/** Показати toast-повідомлення. duration в мс (0 = не зникає сам).
 *  action — необов'язкова кнопка-дія (лаунчер-стиль). */
export function toast(message: string, kind: ToastKind = 'info', duration = 3500, action?: { label: string; onClick: () => void }): number {
  const id = nextId++
  toasts.push({ id, kind, message, action })
  if (duration > 0) {
    setTimeout(() => dismissToast(id), duration)
  }
  return id
}

export function dismissToast(id: number) {
  const i = toasts.findIndex((t) => t.id === id)
  if (i >= 0) toasts.splice(i, 1)
}

// ── Confirm modal ──

export interface ConfirmState {
  open: boolean
  title: string
  message: string
  confirmLabel: string
  cancelLabel: string
  danger: boolean
  resolve?: (v: boolean) => void
}

export const confirmState = $state<ConfirmState>({
  open: false, title: '', message: '', confirmLabel: '', cancelLabel: '', danger: false
})

/**
 * Показати модалку підтвердження і повернути Promise<boolean>.
 * Використання: `if (await confirmDialog(t('acc.deleteConfirm'), ...)) …`
 */
export function confirmDialog(
  title: string,
  message: string,
  opts: { confirmLabel?: string; cancelLabel?: string; danger?: boolean } = {}
): Promise<boolean> {
  return new Promise((resolve) => {
    confirmState.open = true
    confirmState.title = title
    confirmState.message = message
    confirmState.confirmLabel = opts.confirmLabel ?? 'OK'
    confirmState.cancelLabel = opts.cancelLabel ?? 'Cancel'
    confirmState.danger = opts.danger ?? true
    confirmState.resolve = resolve
  })
}

export function confirmAnswer(v: boolean) {
  const r = confirmState.resolve
  confirmState.open = false
  confirmState.resolve = undefined
  r?.(v)
}
