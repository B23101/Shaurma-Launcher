// groupChatApi.ts — груповий чат через ТОЙ САМИЙ ShaurmaChat, що й особистий.
//
// Перекладає операції group_op і живі події group_* у формат, який чат уже вміє показувати
// (SocialChatPage, події chat:*). Права, ліміти й очищення тексту, як і раніше, на сервері.
import { App } from '../wails'
import { toast } from '../toast.svelte'
import type { ChatApi, ChatSendState } from '../friends/chatApi'
import type { SocialChatMessage, SocialChatPage } from '../types'
import { GROUP_PAGE, type GroupsStore } from './GroupsStore.svelte'
import type { GroupMessage } from './groupTypes'

export function toChatMessage(m: GroupMessage): SocialChatMessage {
  return {
    id: m.id,
    from: m.from,
    bodyType: m.kind === 'system' || m.kind === 'build_offer' ? m.kind : 'text',
    ciphertext: '',
    nonce: '',
    createdAt: m.ts,
    plaintext: m.body,
    replyToId: m.replyToId || 0,
    editedAt: m.editedAt || 0,
    reactions: (m.reactions ?? []).filter((r) => r && r.by && r.emoji),
  }
}

const codeOf = (e: unknown) => {
  const m = /social:([A-Za-z_ ]+)/.exec(String((e as any)?.message ?? e ?? ''))
  return m ? m[1].trim().replace(/ /g, '_') : 'unknown'
}

function newClientId(): string {
  const b = new Uint8Array(16)
  crypto.getRandomValues(b)
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
}

export function groupChatApi(store: GroupsStore, gid: string): ChatApi {
  const states = new Map<string, ChatSendState>()
  const local = new Map<string, Set<(d: any) => void>>()
  const emit = (ev: string, d: any) => local.get(ev)?.forEach((cb) => { try { cb(d) } catch { /* слухач чату */ } })

  async function page(fields: Record<string, unknown>): Promise<SocialChatPage> {
    return toPage(await App.GroupRequest('history', { groupId: gid, ...fields }))
  }
  function toPage(r: any): SocialChatPage {
    const h = r?.history ?? {}
    return {
      messages: (Array.isArray(h.messages) ? h.messages : []).map(toChatMessage),
      hasMore: !!h.hasMore,
      pinned: Array.isArray(h.pinned) ? h.pinned.map(toChatMessage) : undefined,
      reads: Array.isArray(h.reads) ? h.reads : undefined,
    }
  }

  function dispatch(text: string, replyTo: number, cid: string): string {
    states.set(cid, { status: 'pending' })
    App.GroupRequest('send', { groupId: gid, body: text, replyToId: replyTo > 0 ? replyTo : undefined, clientMessageId: cid })
      .then((r: any) => {
        const m: GroupMessage | undefined = r?.message
        if (!m?.id) throw new Error('social:unknown')
        store.addOwn(m)
        states.set(cid, { status: 'acked', id: m.id, ts: m.ts })
        emit('chat:ack', { chatId: gid, clientMessageId: cid, id: m.id, ts: m.ts })
      })
      .catch((e: unknown) => {
        const code = codeOf(e)
        states.set(cid, { status: 'failed', code })
        emit('chat:failed', { clientMessageId: cid, code })
        if (code !== 'not_connected') toast(store.errText(e), 'error')
      })
    return cid
  }

  // Живі події стору → події чату (лише цієї групи).
  function fromStore(event: string, cb: (d: any) => void): () => void {
    return store.listen((type, d) => {
      const m: GroupMessage | undefined = d?.message
      const id = String(m?.groupId ?? d?.groupId ?? '')
      if (id !== gid) return
      if (event === 'chat:message' && type === 'group_message' && m) {
        cb({ chatId: gid, id: m.id, from: m.from, ts: m.ts, bodyType: toChatMessage(m).bodyType, plaintext: m.body, replyToId: m.replyToId || 0 })
      } else if (event === 'chat:edited' && type === 'group_message_edited' && m) {
        cb({ chatId: gid, messageId: m.id, plaintext: m.body, editedAt: m.editedAt || Date.now(), by: m.from })
      } else if (event === 'chat:deleted' && type === 'group_message_deleted') {
        cb({ chatId: gid, messageId: Number(d.messageId) })
      } else if (event === 'chat:pins' && type === 'group_pins') {
        cb({ chatId: gid, pinned: (Array.isArray(d.pinned) ? d.pinned : []).map(toChatMessage) })
      } else if (event === 'chat:reactions' && type === 'group_reactions') {
        cb({ chatId: gid, messageId: Number(d.messageId), reactions: Array.isArray(d.reactions) ? d.reactions : [] })
      }
    })
  }

  const op = (name: string, fields: Record<string, unknown>) => App.GroupRequest(name, { groupId: gid, ...fields }).then(() => {})

  return {
    cursor: 'id',
    pageSize: GROUP_PAGE,
    unread: async () => store.openedUnread[gid] ?? 0,
    // Найновіша сторінка вже могла завантажитись при наведенні/кліку на групу — забираємо її без нового запиту.
    history: async (_p, before, limit) => (before === 0 ? toPage(await store.takeFirstPage(gid, limit)) : page({ before, limit })),
    historyAfter: (_p, after, limit) => page({ after: Math.max(0, after), limit }),
    markRead: (_p, upTo) => store.markRead(gid, upTo),
    send: async (_p, text, replyTo) => dispatch(text, replyTo, newClientId()),
    // Той самий clientMessageId: сервер відкидає дубль, якщо перша спроба таки дійшла.
    retry: async (_p, text, cid) => dispatch(text, 0, cid || newClientId()),
    sendState: async (cid) => states.get(cid) ?? { status: 'unknown' },
    typing: async () => store.sendTyping(gid),
    edit: (_p, id, text) => op('edit', { messageId: id, body: text }),
    remove: (_p, id) => op('delete_message', { messageId: id }),
    pin: (_p, id, pin) => op('pin', { messageId: id, pin }),
    react: (_p, id, emoji) => op('react', { messageId: id, emoji }),
    errText: (e) => store.errText(e),
    on(event, cb) {
      if (event === 'chat:ack' || event === 'chat:failed') {
        const set = local.get(event) ?? new Set()
        set.add(cb)
        local.set(event, set)
        return () => { set.delete(cb) }
      }
      if (['chat:message', 'chat:edited', 'chat:deleted', 'chat:pins', 'chat:reactions'].includes(event)) return fromStore(event, cb)
      return () => {}  // прочитання, запрошення на сервер, тимчасові доступи — лише в особистих чатах
    },
  }
}
