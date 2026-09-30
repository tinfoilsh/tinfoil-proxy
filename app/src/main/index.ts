import { app } from 'electron'

import { loadConfig } from './config.js'
import { registerIpc } from './ipc.js'
import { applyLaunchAtLogin } from './login-item.js'
import { createTray, getTrayBounds } from './menu.js'
import { showDetailsWindow, showPopupIfReady } from './popup.js'
import { startProxy, stopProxy } from './proxy.js'
import { stateStore } from './state.js'
import { startAutoUpdater, stopAutoUpdater } from './updater.js'

app.commandLine.appendSwitch('password-store', 'basic')

let cleanupCompleted = false

if (process.platform === 'darwin') {
  app.dock?.hide()
}

const singleInstance = app.requestSingleInstanceLock()
if (!singleInstance) {
  app.exit(0)
}

app.on('second-instance', () => {
  if (process.platform === 'linux') {
    showDetailsWindow()
    return
  }
  const bounds = getTrayBounds()
  if (bounds) showPopupIfReady()
})

async function bootstrap(): Promise<void> {
  registerIpc()
  createTray()

  const cfg = await loadConfig()
  stateStore.set({
    proxy: {
      enabled: cfg.proxyEnabled,
      running: false,
      port: cfg.port,
      allowedHosts: cfg.allowedHosts,
      upstreamedTokens: 0,
      downstreamedTokens: 0,
      gateway: undefined,
      lastError: undefined
    },
    launchAtLogin: cfg.launchAtLogin
  })

  applyLaunchAtLogin(cfg.launchAtLogin)

  if (cfg.proxyEnabled) {
    await startProxy(cfg.port, cfg.allowedHosts)
  }

  startAutoUpdater()
}

app.whenReady().then(() => {
  bootstrap().catch((err) => {
    console.error('Tray bootstrap failed:', err)
    stateStore.set({
      lastError: err instanceof Error ? err.message : String(err)
    })
  })
})

app.on('window-all-closed', () => {
  // Tray app: keep alive when the popup is closed.
})

app.on('before-quit', (event) => {
  if (cleanupCompleted) return
  event.preventDefault()
  void (async () => {
    try {
      stopAutoUpdater()
      await stopProxy()
    } catch (err) {
      console.error('cleanup failed during quit:', err)
    } finally {
      cleanupCompleted = true
      app.quit()
    }
  })()
})
