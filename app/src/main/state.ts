import { EventEmitter } from 'node:events'

export type VerificationStatus = 'initializing' | 'verified' | 'failed'

export interface RouterState {
  router: string
  status: VerificationStatus
  lastError?: string
}

export interface ProxyState {
  enabled: boolean
  running: boolean
  verifying: boolean
  verified: boolean
  port: number
  upstreamedTokens: number
  downstreamedTokens: number
  enclave?: string
  lastError?: string
}

export interface TrayState {
  status: VerificationStatus
  statusMessage: string
  routers: RouterState[]
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
  status: 'initializing',
  statusMessage: 'Starting…',
  routers: [],
  proxy: {
    enabled: false,
    running: false,
    verifying: false,
    verified: false,
    port: 0,
    upstreamedTokens: 0,
    downstreamedTokens: 0
  },
  launchAtLogin: true
})
