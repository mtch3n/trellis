export type Gate = { nominations: number; unverified: number; review: string | null }

declare module 'claude-code' {
  interface PluginState {
    trellis: { gate: Gate | null }
  }
}
