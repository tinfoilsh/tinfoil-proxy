import { EventEmitter } from 'node:events'

export interface ProxyState {
  enabled: boolean
  running: boolean
  verifying: boolean
  verified: boolean
  port: number
  allowedHosts: string[]
  upstreamedTokens: number
  downstreamedTokens: number
  enclave?: string
  releaseTag?: string
  platform?: string
  verifiedAt?: string
  lastError?: string
}

export interface TrayState {
  proxy: ProxyState
  launchAtLogin: boolean
  lastError?: string
}

type Listener = (state: TrayState) => void

class StateStore extends EventEmitter {
  private state: TrayState

  constructor(initial: TrayState) {
    super()
    this.state = initial
  }

  get(): TrayState {
    return structuredClone(this.state)
  }

  set(partial: Partial<TrayState>): void {
    this.state = { ...this.state, ...partial }
    this.emit('change', this.state)
  }

  onChange(listener: Listener): () => void {
    this.on('change', listener)
    return () => this.off('change', listener)
  }
}

export const stateStore = new StateStore({
  proxy: {
    enabled: false,
    running: false,
    verifying: false,
    verified: false,
    port: 0,
    allowedHosts: [],
    upstreamedTokens: 0,
    downstreamedTokens: 0
  },
  launchAtLogin: true
})
