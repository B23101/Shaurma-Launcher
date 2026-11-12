import type { ConsoleLine } from './types'
export interface ConsolePage {
 lines: ConsoleLine[]
 offset: number
 total: number
 count: number
 errors: number
 warnings: number
 lastId: number
}
export type ConsolePageLoader = (offset: number, limit: number, level: string, query: string, after: number) => Promise<ConsolePage>
