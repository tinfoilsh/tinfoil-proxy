import { join } from 'node:path'
import { app, nativeImage, type NativeImage } from 'electron'

import type { ProxyStatus } from './state.js'

export type TrayIconState = 'off' | 'ready' | 'initializing' | 'failed'

const LINUX_TRAY_ICON_SIZE = 22

function assetPath(name: string): string {
  if (app.isPackaged) {
    return join(process.resourcesPath, 'assets', name)
  }
  return join(app.getAppPath(), 'assets', name)
}

export function trayIconState(active: boolean, status: ProxyStatus): TrayIconState {
  if (!active) return 'off'
  if (status === 'failed') return 'failed'
  if (status === 'ready') return 'ready'
  return 'initializing'
}

function templateBaseName(state: TrayIconState): string {
  switch (state) {
    case 'failed':
      return 'icon-tray-error-Template'
    case 'ready':
    case 'initializing':
      return 'icon-tray-on-Template'
    case 'off':
    default:
      return 'icon-tray-off-Template'
  }
}

export function trayIcon(state: TrayIconState): NativeImage {
  if (process.platform === 'darwin') {
    const image = nativeImage.createFromPath(assetPath(`${templateBaseName(state)}.png`))
    image.setTemplateImage(true)
    return image
  }

  // Use the color icon where template masks are unsupported.
  const colored = nativeImage.createFromPath(assetPath('icon-tray.png'))
  if (colored.isEmpty()) {
    return nativeImage.createFromPath(assetPath(`${templateBaseName(state)}.png`))
  }
  return colored.resize({
    width: LINUX_TRAY_ICON_SIZE,
    height: LINUX_TRAY_ICON_SIZE,
    quality: 'best'
  })
}
