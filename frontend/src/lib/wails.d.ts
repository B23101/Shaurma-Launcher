// ── Типи модуля Wails v3 runtime (/wails/runtime.js) ────────────────────
// Wails v3 віддає свій JS-runtime за адресою /wails/runtime.js (його
// роздає assetserver додатка). Цей файл дає TypeScript типи для нього.
// Шлях зіставляється через "paths" у tsconfig.json; Vite зовнішнює цей
// імпорт (build.rollupOptions.external), тому у збірці він лишається як є
// і браузер підвантажує його з ассет-сервера Wails.

export interface WailsEvent {
  name: string
  data: any
  sender?: string
}

export const Call: {
  ByName(methodName: string, ...args: any[]): Promise<any>
  ByID(methodID: number, ...args: any[]): Promise<any>
  Call(options: { methodName?: string; methodID?: number; args?: any[] }): Promise<any>
}

export const Events: {
  On(eventName: string, callback: (event: WailsEvent) => void): () => void
  Off(...eventNames: string[]): void
  OffAll(): void
  Once(eventName: string, callback: (event: WailsEvent) => void): () => void
  OnMultiple(eventName: string, callback: (event: WailsEvent) => void, maxCallbacks: number): () => void
  Emit(name: string, data?: any): void
}

export const Application: {
  Quit(): Promise<void>
  Hide(): Promise<void>
  Show(): Promise<void>
}

export const Window: {
  Hide(): Promise<void>
  Show(): Promise<void>
  Minimise(): Promise<void>
  UnMinimise(): Promise<void>
  Maximise(): Promise<void>
  UnMaximise(): Promise<void>
  ToggleMaximise(): Promise<void>
  Close(): Promise<void>
  Fullscreen(): Promise<void>
  UnFullscreen(): Promise<void>
  Get(name: string): Window
}
