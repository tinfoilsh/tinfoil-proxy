import { join } from 'node:path'
import { app, nativeImage, type NativeImage } from 'electron'

import type { VerificationStatus } from './state.js'

export type TrayIconState = 'off' | 'verified' | 'initializing' | 'failed'

const LINUX_TRAY_ICON_SIZE = 22

function assetPath(name: string): string {
  if (app.isPackaged) {
    return join(process.resourcesPath, 'assets', name)
  }
  return join(app.getAppPath(), 'assets', name)
}

export function trayIconState(active: boolean, status: VerificationStatus): TrayIconState {
  if (!active) return 'off'
  if (status === 'failed') return 'failed'
  if (status === 'verified') return 'verified'
  return 'initializing'
}

function templateBaseName(state: TrayIconState): string {
  switch (state) {
    case 'failed':
      return 'icon-tray-error-Template'
    case 'verified':
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

  // Off macOS the panel renders the bitmap as-is rather than tinting a template
  // mask, so the monochrome menu-bar glyphs are nearly invisible on themed
  // panels. Use the full-colour brand mark at a panel-friendly size; the menu
  // header and tooltip carry the verification state instead of the icon.
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
