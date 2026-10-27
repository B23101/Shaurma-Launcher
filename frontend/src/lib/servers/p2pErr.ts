import { t } from '../i18n.svelte'

// p2pErr — людський текст помилки P2P. Бекенд додає до відмови p2p_limit_reached машинну мітку
// "[[limit:<scope>:<n>]]" (scope: account | device | network) — замінюємо її власним перекладом.
export function p2pErr(e: unknown): string {
  const raw = String((e as any)?.message ?? e ?? '')
  const m = raw.match(/\s*\[\[limit:([a-z]*):(\d+)\]\]/)
  if (!m) return raw
  const scope = ['account', 'device', 'network'].includes(m[1]) ? m[1] : 'account'
  return t(`servers.p2p.limit.${scope}`, { n: m[2] })
}
