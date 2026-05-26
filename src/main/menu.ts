import { app, clipboard, Menu, type MenuItemConstructorOptions, Tray, type Rectangle } from 'electron'

import { loadConfig, saveConfig } from './config.js'
import { trayIcon, trayIconState } from './icons.js'
import { applyLaunchAtLogin, isLaunchAtLoginSupported } from './login-item.js'
import { hidePopup, togglePopup } from './popup.js'
import { proxyEndpoint, startProxy, stopProxy } from './proxy.js'
import { refreshRouters } from './secure-client.js'
import { stateStore, type TrayState } from './state.js'

let tray: Tray | undefined
let contextMenu: Menu | undefined

function reportClickError(promise: Promise<void>, context: string): void {
  promise.catch((err) => {
    const message = err instanceof Error ? err.message : String(err)
    console.error(`[tray] ${context}:`, err)
    stateStore.set({ lastError: `${context}: ${message}` })
  })
}

function isActive(state: TrayState): boolean {
  return state.proxy.enabled && state.proxy.running && state.proxy.verified
}

function headerLabel(state: TrayState): string {
  const { proxy } = state
  if (!proxy.enabled) return '● Tinfoil — Off'
  if (proxy.lastError) return '● Tinfoil — Attestation failed'
  if (proxy.verifying) return '○ Tinfoil — Verifying enclave…'
  if (proxy.running && proxy.verified) return '● Tinfoil — Proxy on'
  return '○ Tinfoil — Starting…'
}

function buildMenu(state: TrayState, openDetails: () => void): Menu {
  const enabled = state.proxy.enabled
  const items: MenuItemConstructorOptions[] = [
    { label: headerLabel(state), enabled: false },
    { type: 'separator' },
    {
      label: enabled ? 'Stop proxy' : 'Start proxy',
      click: () => {
        reportClickError(toggle(!enabled), 'Failed to toggle proxy')
      }
    },
    {
      label: 'Show verification details…',
      click: openDetails
    }
  ]

  if (state.proxy.running && state.proxy.port > 0) {
    const endpoint = proxyEndpoint(state.proxy.port)
    items.push({
      label: `Copy ${endpoint}`,
      click: () => {
        clipboard.writeText(endpoint)
      }
    })
  }

  items.push(
    { type: 'separator' },
    {
      label: 'Refresh routers',
      click: () => {
        reportClickError(refreshRouters(), 'Failed to refresh routers')
      }
    }
  )

  const note = state.proxy.lastError ?? state.lastError
  if (note && !(state.proxy.enabled && state.proxy.running)) {
    items.push({ type: 'separator' }, { label: `Note: ${note}`, enabled: false })
  }

  if (isLaunchAtLoginSupported()) {
    items.push(
      { type: 'separator' },
      {
        label: 'Open at Login',
        type: 'checkbox',
        checked: state.launchAtLogin,
        click: () => {
          reportClickError(toggleLaunchAtLogin(!state.launchAtLogin), 'Failed to update Open at Login')
        }
      }
    )
  }

  items.push(
    { type: 'separator' },
    {
      label: 'Quit Tinfoil',
      click: () => {
        app.quit()
      }
    }
  )

  return Menu.buildFromTemplate(items)
}

async function toggleLaunchAtLogin(enable: boolean): Promise<void> {
  const cfg = await loadConfig()
  await saveConfig({ ...cfg, launchAtLogin: enable })
  applyLaunchAtLogin(enable)
  stateStore.set({ launchAtLogin: enable })
}

async function toggle(enable: boolean): Promise<void> {
  const cfg = await loadConfig()
  if (enable) {
    await startProxy(cfg.port)
  } else {
    await stopProxy()
    const current = stateStore.get().proxy
    stateStore.set({ proxy: { ...current, enabled: false, running: false, lastError: undefined } })
  }
  await saveConfig({ ...cfg, proxyEnabled: enable })
}

export function createTray(): Tray {
  if (tray) return tray
  const initial = trayIcon(trayIconState(false, 'initializing'))
  tray = new Tray(initial)
  tray.setToolTip('Tinfoil')

  const refresh = () => {
    if (!tray) return
    const state = stateStore.get()
    const active = isActive(state)
    const verificationStatus = state.proxy.lastError
      ? 'failed'
      : state.proxy.verifying
        ? 'initializing'
        : state.proxy.verified
          ? 'verified'
          : 'initializing'
    tray.setImage(trayIcon(trayIconState(active, verificationStatus)))
    tray.setToolTip(active ? `Tinfoil — Proxy on (${state.proxy.enclave ?? 'enclave'})` : 'Tinfoil — Off')
    contextMenu = buildMenu(state, () => {
      if (!tray) return
      togglePopup(tray.getBounds())
    })
  }

  refresh()
  stateStore.onChange(refresh)

  tray.on('click', (_event: Electron.KeyboardEvent, bounds: Rectangle) => {
    togglePopup(bounds)
  })

  tray.on('right-click', () => {
    if (!tray || !contextMenu) return
    hidePopup()
    tray.popUpContextMenu(contextMenu)
  })

  return tray
}
