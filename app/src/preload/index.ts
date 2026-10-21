import { contextBridge, ipcRenderer } from 'electron'

export interface RouterSnapshot {
  router: string
  label: string
  status: 'initializing' | 'verified' | 'failed'
  lastError?: string
}

export interface ProxySnapshot {
  enabled: boolean
  running: boolean
  verifying: boolean
  verified: boolean
  port: number
  allowedHosts: string[]
  upstreamedTokens: number
  downstreamedTokens: number
  enclave?: string
  verifiedAt?: string
  lastError?: string
}

export interface TrayStateSnapshot {
  status: 'initializing' | 'verified' | 'failed'
  statusMessage: string
  routers: RouterSnapshot[]
  endpoint?: string
  proxy: ProxySnapshot
  launchAtLogin: boolean
  launchAtLoginSupported: boolean
  lastError?: string
}

const api = {
  getState: (): Promise<TrayStateSnapshot> => ipcRenderer.invoke('tray:getState'),
  copyEndpoint: (): Promise<string | null> => ipcRenderer.invoke('tray:copyEndpoint'),
  openVerificationDocument: (): Promise<boolean> =>
    ipcRenderer.invoke('tray:openVerificationDocument'),
  setProxyEnabled: (enabled: boolean): Promise<TrayStateSnapshot> =>
    ipcRenderer.invoke('tray:setProxyEnabled', enabled),
  setProxyPort: (port: number): Promise<TrayStateSnapshot> =>
    ipcRenderer.invoke('tray:setProxyPort', port),
  setAllowedHosts: (hosts: string[]): Promise<TrayStateSnapshot> =>
    ipcRenderer.invoke('tray:setAllowedHosts', hosts),
  refreshRouters: (): Promise<TrayStateSnapshot> =>
    ipcRenderer.invoke('tray:refreshRouters'),
  setLaunchAtLogin: (enabled: boolean): Promise<TrayStateSnapshot> =>
    ipcRenderer.invoke('tray:setLaunchAtLogin', enabled),
  setCompactHeight: (height: number): Promise<void> =>
    ipcRenderer.invoke('tray:setCompactHeight', height),
  onStateChanged: (handler: (state: TrayStateSnapshot) => void): (() => void) => {
    const listener = (_event: Electron.IpcRendererEvent, state: TrayStateSnapshot) =>
      handler(state)
    ipcRenderer.on('tray:stateChanged', listener)
    return () => ipcRenderer.off('tray:stateChanged', listener)
  }
}

contextBridge.exposeInMainWorld('tinfoil', api)

export type TinfoilApi = typeof api
