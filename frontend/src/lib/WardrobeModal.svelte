<script lang="ts">
  // WardrobeModal — тонка обгортка над спільним UiModal для гардероба.
  // Лишає звичний для Wardrobe.svelte API (snippets body()/foot(), accent,
  // size, bodyNoScroll), а шапка, анімації, Esc, фокус і перетягування за
  // шапку — від UiModal, тож вікна гардероба виглядають як решта лаунчера.
  import UiModal from './UiModal.svelte'

  let {
    open = false,
    onClose = () => {},
    title = '',
    icon = 'ti-box',
    accent = 'orange',
    size = 'md',
    bodyNoScroll = false,
    body,
    foot,
  }: {
    open?: boolean
    onClose?: () => void
    title?: string
    icon?: string
    accent?: 'orange' | 'violet'
    size?: 'md' | 'lg' | 'xl'
    // Коли вміст сам керує скролом (напр. редактор пресету: 3D-картка
    // розтягується на доступне місце, права колонка скролиться сама) —
    // тіло модалки НЕ скролиться і не має власних відступів.
    bodyNoScroll?: boolean
    body?: import('svelte').Snippet
    foot?: import('svelte').Snippet
  } = $props()

  const widths = { md: '520px', lg: '680px', xl: '940px' }

  // Блокуємо скрол сторінки під гардеробом, поки вікно відкрите.
  $effect(() => {
    if (!open) return
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = '' }
  })
</script>

<UiModal {open} onclose={onClose} {title} {icon} draggable
         color={accent === 'violet' ? 'var(--violet-l, #9d7cff)' : 'var(--orange)'}
         width="min({widths[size]}, calc(100vw - 40px))" maxHeight="min(88vh, 900px)"
         bodyClass={bodyNoScroll ? 'no-scroll' : ''} footer={foot ?? null}>
  {#if body}{@render body()}{/if}
</UiModal>
