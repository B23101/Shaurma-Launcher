// settingsMeta.ts — зворотна сумісність: розділи налаштувань тепер описані в
// settings/registry.ts (єдине джерело правди для меню та пошуку).
import { settingsSections } from './settings/registry'

export { settingsSections, settingsSectionIds, settingsSectionById, normalizeSectionId } from './settings/registry'

/** ID розділу → i18n-ключ його назви (для глобального пошуку). */
export const grpTitleKeys: Record<string, string> = Object.fromEntries(settingsSections.map((s) => [s.id, s.titleKey]))
