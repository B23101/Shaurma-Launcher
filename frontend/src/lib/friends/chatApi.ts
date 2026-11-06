// chatApi.ts — джерело даних для ShaurmaChat.
//
// ShaurmaChat один на особисті й групові чати: увесь вигляд, анімації, меню, відповіді,
// реакції, закріплені й прокрутка — спільні. Різниться лише те, ЗВІДКИ приходять повідомлення
// і КУДИ йдуть дії. Цей інтерфейс повторює рівно ті виклики App.* і події chat:*, які потрібні
// чату, тож особистий чат працює як і раніше (privateChatApi — пряма обгортка), а групи
// підставляють свій адаптер (groups/groupChatApi.ts).
import { App, on } from '../wails'
import type { SocialChatPage } from '../types'

export interface ChatSendState { status: string; id?: number; ts?: number; code?: string }

export interface ChatApi {
  /** Курсор історії: 'ts' — час повідомлення (особисті), 'id' — номер повідомлення (групи). */
  cursor: 'ts' | 'id'
  /** Скільки повідомлень брати за один запит історії (за замовчуванням 50). */
  pageSize?: number
  /** Скільки непрочитаних було на момент відкриття (для роздільника «Нові»). */
  unread(peer: string): Promise<number>
  history(peer: string, before: number, limit: number): Promise<SocialChatPage>
  historyAfter(peer: string, after: number, limit: number): Promise<SocialChatPage>
  markRead(peer: string, upTo: number): Promise<void>
  /** Повертає clientMessageId; підтвердження — подією chat:ack або chat:failed. */
  send(peer: string, text: string, replyTo: number): Promise<string>
  retry(peer: string, text: string, clientMessageId: string): Promise<string>
  sendState(clientMessageId: string): Promise<ChatSendState>
  typing(peer: string): Promise<void>
  edit(peer: string, id: number, text: string): Promise<void>
  remove(peer: string, id: number): Promise<void>
  pin(peer: string, id: number, pin: boolean): Promise<void>
  react(peer: string, id: number, emoji: string): Promise<void>
  /** Переклад помилки (групи мають власні коди); без нього — social.err.*. */
  errText?(e: unknown): string
  /** Підписка на події у форматі chat:* (chatId у даних — той, що чат перевіряє). */
  on(event: string, cb: (data: any) => void): () => void
}

/** Груповий режим ShaurmaChat: що показати в шапці й що дозволено (права — лише підказка UI). */
export interface GroupChatInfo {
  id: string
  title: string
  icon: string
  color: string
  memberIds: string[]
  members: number
  adminName: string
  isAdmin: boolean
  canPin: boolean
  canUnpin: boolean
  /** Хто зараз друкує (імена). */
  typing: string[]
  maxLen: number
  nameOf(accountId: string): string
  headOf(accountId: string): string
  /** Текст системної події (вступ, вихід, нова адмінка…). */
  sysText(body: string): string
  onSettings(): void
  onMembers(): void
  readers(messageId: number): Promise<string[]>
  muted: boolean
  toggleMute(): void
  /** «Отримати збірку» з картки build_offer. */
  claim(messageId: number, from: string): void
}

export const privateChatApi: ChatApi = {
  cursor: 'ts',
  unread: async (peer) => (await App.GetFriendsState())?.unread?.[peer] ?? 0,
  history: (peer, before, limit) => App.GetChatHistory(peer, before, limit),
  historyAfter: (peer, after, limit) => App.GetChatHistoryAfter(peer, after, limit),
  markRead: (peer, upTo) => App.MarkChatRead(peer, upTo),
  send: (peer, text, replyTo) => (replyTo > 0 ? App.SendChatMessageReply(peer, text, replyTo) : App.SendChatMessage(peer, text)),
  retry: (peer, text, cid) => App.RetryChatMessage(peer, text, cid),
  sendState: (cid) => App.GetChatSendState(cid),
  typing: (peer) => App.SendChatTyping(peer),
  edit: (peer, id, text) => App.EditChatMessage(peer, id, text),
  remove: (peer, id) => App.DeleteChatMessage(peer, id),
  pin: (peer, id, pin) => (pin ? App.PinChatMessage(peer, id) : App.UnpinChatMessage(peer, id)),
  react: (peer, id, emoji) => App.ReactChatMessage(peer, id, emoji),
  on,
}
