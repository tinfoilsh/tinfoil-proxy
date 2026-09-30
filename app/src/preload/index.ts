import { contextBridge, ipcRenderer } from 'electron'

export interface ProxySnapshot {
  enabled: boolean
  running: boolean
  port: number
  allowedHosts: string[]
  upstreamedTokens: number
  downstreamedTokens: number
  gateway?: string
  attestationError?: string
  lastError?: string
}

export interface TrayStateSnapshot {
  endpoint?: string
  proxy: ProxySnapshot
  launchAtLogin: boolean
  launchAtLoginSupported: boolean
  lastError?: string
}

const api = {
  getState: (): Promise<TrayStateSnapshot> => ipcRenderer.invoke('tray:getState'),
  openVerifications: (): Promise<boolean> => ipcRenderer.invoke('tray:openVerifications'),
  copyEndpoint: (): Promise<string | null> => ipcRenderer.invoke('tray:copyEndpoint'),
  setProxyEnabled: (enabled: boolean): Promise<TrayStateSnapshot> =>
    ipcRenderer.invoke('tray:setProxyEnabled', enabled),
  setProxyPort: (port: number): Promise<TrayStateSnapshot> =>
    ipcRenderer.invoke('tray:setProxyPort', port),
  setAllowedHosts: (hosts: string[]): Promise<TrayStateSnapshot> =>
    ipcRenderer.invoke('tray:setAllowedHosts', hosts),
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
