<script lang="ts">
  // ── JvmArgsEditor: список JVM-аргументів (рядок = один аргумент) ────────
  // Спільний редактор для налаштувань лаунчера (SettingRow, тип jvmargs) і
  // збірки (PackPerfSection): кожен аргумент — ОКРЕМИЙ РЯДОК з кнопками
  // «+» (додати рядок) і «🗑» (видалити), плюс перемикач «Редагувати
  // текстом» для пакетної вставки.
  //
  // ВАЖЛИВО (баг, виправлений): раніше редактор будував рядки заново зі
  // значення (split по пробілах + filter порожніх) на кожному рендері.
  // Через це кнопка «Додати аргумент» НЕ працювала: порожній новий рядок
  // миттєво зникав зі значення (filter прибирав порожні рядки) і вводити
  // в нього було нікуди. Тепер редактор тримає ВЛАСНИЙ робочий список
  // (jvmList, може містити порожні рядки під час редагування) і комітить
  // у значення лише непусті аргументи — новий рядок живе, поки в нього
  // не введуть текст або його не видалять.

  import { t } from './i18n.svelte'

  let { value, onchange, placeholder = '-XX:+UseG1GC -XX:+ParallelRefProcEnabled' } = $props<{
    value: string
    onchange: (v: string) => void
    // Підказка в текстовому режимі. JVM і аргументи програми — різні набори
    // (у програми типовий «--nogui»), тому місце для підказки винесено
    // назовні, щоб редактор залишався одним і тим самим компонентом.
    placeholder?: string
  }>()

  // Режим: list (рядки) | text (textarea для пакетної вставки).
  let textMode = $state(false)
  // Робочий список у list-режимі. Може містити порожні рядки — вони
  // «живуть», доки користувач не введе текст або не видалить рядок.
  let jvmList = $state<string[]>([])
  // Останнє значення, яке ми віддали батьку — щоб не перезаписувати
  // робочий список власним комітом (див. $effect нижче).
  let lastCommitted = $state<string | null>(null)

  // Синхронізація робочого списку зі ЗНАЧЕННЯМ. Виконується лише в
  // LIST-режимі: у text-режимі джерело правди — сама textarea (вона читає
  // value напряму), а перезапис списку там нічого не давав. Раніше умова
  // була ІНВЕРТОВАНА (sync лише в text-режимі) — через це list-режим
  // стартував із порожнім списком і вже наявні аргументи НЕ відображалися,
  // а «＋» додавав рядок у порожній список.
  //
  // Перезаповнюємо лише якщо значення відрізняється від того, що ми самі
  // комітили — інакше власний коміт (editRow → onchange) затирав би
  // щойно доданий порожній рядок.
  $effect(() => {
    if (textMode) return
    const cur = String(value ?? '')
    if (cur !== lastCommitted) {
      jvmList = cur.split(/\s+/).filter(Boolean)
      lastCommitted = cur
    }
  })

  function commit() {
    const joined = jvmList.filter((a) => a.trim() !== '').join(' ')
    if (joined !== lastCommitted) {
      lastCommitted = joined
      onchange(joined)
    }
  }

  function addRow() {
    jvmList = [...jvmList, '']
    // Фокус на новий рядок.
    setTimeout(() => {
      const inputs = document.querySelectorAll<HTMLInputElement>('.jvm-arg-input')
      if (inputs.length) inputs[inputs.length - 1].focus()
    }, 30)
  }

  function deleteRow(i: number) {
    jvmList = jvmList.filter((_, idx) => idx !== i)
    commit()
  }

  function editRow(i: number, v: string) {
    const next = [...jvmList]
    next[i] = v
    jvmList = next
    commit()
  }
</script>

<div class="jvm-editor">
  <div class="jvm-header">
    <button class="btn btn-ghost jvm-edit-as-text" onclick={() => textMode = !textMode}>
      <i class="ti {textMode ? 'ti-list' : 'ti-pencil'}"></i>
      {textMode ? t('settings.javaArgs.list') : t('settings.javaArgs.editAsText')}
    </button>
  </div>

  {#if textMode}
    <textarea
      class="input mono jvm-textarea"
      rows="4"
      value={value ?? ''}
      {placeholder}
      onchange={(e: any) => onchange(e.target.value)}
    ></textarea>
  {:else}
    <div class="jvm-list">
      {#each jvmList as arg, i (i)}
        <div class="jvm-row">
          <span class="jvm-num">{String(i + 1).padStart(2, '0')}</span>
          <input
            class="input mono jvm-arg-input"
            value={arg}
            spellcheck={false}
            oninput={(e: any) => editRow(i, e.target.value)}
            onblur={(e: any) => editRow(i, e.target.value)}
          />
          <button class="btn-icon jvm-del" onclick={() => deleteRow(i)} title={t('delete')}>
            <i class="ti ti-trash"></i>
          </button>
        </div>
      {/each}
      <button class="btn btn-ghost jvm-add" onclick={addRow}>
        <i class="ti ti-plus"></i> {t('settings.javaArgs.add')}
      </button>
    </div>
  {/if}
</div>

<style>
  .jvm-editor { flex: 1; display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  .jvm-header { display: flex; justify-content: flex-end; }
  .jvm-edit-as-text {
    font-size: 12px; padding: 4px 10px; gap: 5px; color: var(--text-mute);
  }
  .jvm-list { display: flex; flex-direction: column; gap: 6px; }
  .jvm-row { display: flex; align-items: center; gap: 8px; }
  .jvm-num {
    font-size: 11px; font-family: var(--font-mono, monospace);
    color: var(--text-dim); min-width: 20px; text-align: right;
    user-select: none; flex-shrink: 0;
  }
  .jvm-arg-input { flex: 1; min-width: 0; font-size: 12px; padding: 6px 10px; }
  .jvm-del {
    flex-shrink: 0; width: 30px; height: 30px; border-radius: 8px; border: none;
    background: transparent; color: var(--text-dim); cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    transition: color .12s, background .12s; font-size: 14px;
  }
  .jvm-del:hover { color: var(--red, #ff6b6b); background: rgba(255,107,107,.1); }
  .jvm-add {
    margin-top: 2px; width: 100%; justify-content: center;
    border-style: dashed; font-size: 12px; gap: 6px; color: var(--text-mute);
  }
  .jvm-add:hover { color: var(--text); }
  .jvm-textarea { width: 100%; font-size: 12px; resize: vertical; min-height: 80px; }
</style>
