// pendingConfigFlush.ts — Shared store for pending instance config saves.
//
// Problem: PackInstancePage.update() debounces saves (350ms), but
// launchWithHealthGuard in App.svelte calls RunPackHealthCheck which reads
// config from disk on the backend. If the user changes RAM and immediately
// clicks Play, the health check sees the OLD value.
//
// Solution: PackInstancePage.update() registers the pending config here.
// launchWithHealthGuard flushes all pending configs before the health check.

import { App } from './wails'
import type { InstanceConfig } from './types'

const pending = new Map<string, InstanceConfig>()

/** Register a config that needs saving before the next launch. */
export function registerPendingConfig(packId: string, cfg: InstanceConfig) {
  pending.set(packId, { ...cfg })
}

/** Remove a pack from pending (e.g. after it's been saved). */
export function clearPendingConfig(packId: string) {
  pending.delete(packId)
}

/** Flush all pending configs to disk immediately. Called before RunPackHealthCheck. */
export async function flushAllPendingConfigs(): Promise<void> {
  const entries = [...pending.entries()]
  pending.clear()
  for (const [id, cfg] of entries) {
    try {
      await App.SaveInstanceConfig(cfg)
    } catch {
      // best-effort — health check will use whatever is on disk
    }
  }
}
