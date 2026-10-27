import { t } from './i18n.svelte'

// Minecraft-правила офлайн-нікнейму (ті самі, що в backend pirate.go):
// 3–16 символів, лише a-zA-Z0-9_. Кирилиця/пробіли/довгі ніки гра не
// прийме — перевіряємо одразу тут, щоб не створювати марний акаунт.
// Спільний модуль: використовується і в модалці входу (App.svelte),
// і у віззі (Wizard.svelte), щоб правила не роз'їжджалися.
export function pirateNicknameError(name: string): string {
  const n = name.length
  if (n < 3 || n > 16) return t('pirate.nick.length')
  if (!/^[a-zA-Z0-9_]+$/.test(name)) return t('pirate.nick.chars')
  return ''
}

// Фільтр вводу: дозволяє ЛИШЕ a-zA-Z0-9_ (кирилиця/пробіли/спецсимволи
// відкидаються на льоту) і обрізає до 16 — юзер фізично не введе
// заборонений символ, а не побачить його згодом у тості.
export function sanitizeNick(v: string): string {
  return v.replace(/[^a-zA-Z0-9_]/g, '').slice(0, 16)
}
