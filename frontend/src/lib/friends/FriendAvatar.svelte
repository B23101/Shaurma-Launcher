<script lang="ts">
  // FriendAvatar — голова + колір профілю + крапка «в мережі». Один і той самий вигляд друга
  // скрізь: вибір друзів, списки доступу P2P, «від кого адреса», запрошення тощо.
  import { friendHead, friendColor } from './friendHead'
  import type { SocialStatus } from '../types'

  let {
    nickname = '',
    color = '',
    status = undefined as SocialStatus | undefined,
    size = 32,
    // Якщо голова вже відома (напр. з чату) — не робимо зайвий запит.
    head = '',
    // ring='status' — замість крапки статусу обведення кольору статусу (для вузьких місць:
    // кнопка «Друзі» у sidebar). Крапка там «спала» і не читалась.
    ring: ringMode = 'color',
  }: { nickname?: string; color?: string; status?: SocialStatus; size?: number; head?: string; ring?: 'color' | 'status' } = $props()

  let loaded = $state('')
  $effect(() => {
    const nick = nickname
    const given = head
    if (given) { loaded = given; return }
    loaded = ''
    let dead = false
    friendHead(nick).then((h) => { if (!dead) loaded = h })
    return () => { dead = true }
  })

  const ring = $derived(friendColor(color))
  // in_game — скрізь той самий помаранчевий (--orange), що й крапка статусу в чаті
  // та бейдж профілю: раніше тут був синій і колір розходився з рештою лаунчера.
  const statusRing = $derived(status === 'online' ? '#22c55e' : status === 'in_game' ? '#ff8a00' : status === 'away' ? '#f59e0b' : ring)
  const initial = $derived((nickname || '?').trim().charAt(0).toUpperCase() || '?')
</script>

<span class="fav" class:ring-status={ringMode === 'status'} style="--sz:{size}px; --ring:{ringMode === 'status' ? statusRing : ring}">
  <span class="fav-face">
    {#if loaded}<img src={loaded} alt="" loading="lazy" />{:else}<b>{initial}</b>{/if}
  </span>
  {#if status && ringMode !== 'status'}<i class="fav-dot {status}" aria-hidden="true"></i>{/if}
</span>

<style>
  .fav { position: relative; display: inline-flex; width: var(--sz); height: var(--sz); flex-shrink: 0; }
  .fav-face {
    width: 100%; height: 100%; border-radius: calc(var(--sz) * .3); overflow: hidden;
    display: grid; place-items: center;
    background: color-mix(in srgb, var(--ring) 16%, var(--bg-input));
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--ring) 70%, transparent);
  }
  /* Режим «кільце статусу» (кнопка «Друзі»): голова — квадрат із заокругленням, а
     обведення кольору стану мережі повторює ЦЮ Ж форму (власна рамка контейнера з
     тонким зазором). Раніше голова робилась круглою всередині квадратного контуру —
     кути «фарбувались» і виходило криво. Крапки статусу тут немає: стан = колір рамки. */
  .fav.ring-status {
    box-sizing: border-box;
    padding: 1.5px;
    border: 2px solid var(--ring);
    border-radius: calc(var(--sz) * .34);
    background: transparent;
    transition: border-color .25s;
  }
  .fav.ring-status .fav-face {
    border-radius: calc(var(--sz) * .2);
    box-shadow: none;
    background: var(--bg-input);
  }
  .fav-face img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; display: block; }
  .fav-face b { font: 800 calc(var(--sz) * .42)/1 var(--font-ui); color: var(--ring); }
  .fav-dot {
    position: absolute; right: -2px; bottom: -2px; width: calc(var(--sz) * .34); height: calc(var(--sz) * .34);
    min-width: 8px; min-height: 8px; border-radius: 50%; background: var(--text-dim);
    border: 2px solid var(--bg-panel);
  }
  .fav-dot.online { background: #22c55e; }
  .fav-dot.in_game { background: #ff8a00; }
  .fav-dot.away { background: #f59e0b; }
</style>
