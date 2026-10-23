import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

interface UpdateInfo {
  downloadedFile: string
  version: string
}

const mocks = vi.hoisted(() => {
  const listeners = new Map<string, (info: UpdateInfo) => Promise<void>>()
  const showMessageBox = vi.fn()
  const checkForUpdates = vi.fn(() => Promise.resolve(null))
  const quitAndInstall = vi.fn()
  const verifyUpdateAttestation = vi.fn()
  let autoInstallOnAppQuit = false

  const autoUpdater = {
    autoDownload: false,
    get autoInstallOnAppQuit(): boolean {
      return autoInstallOnAppQuit
    },
    set autoInstallOnAppQuit(value: boolean) {
      autoInstallOnAppQuit = value
    },
    on: vi.fn((event: string, handler: (info: UpdateInfo) => Promise<void>) => {
      listeners.set(event, handler)
    }),
    checkForUpdates,
    quitAndInstall
  }

  return { autoUpdater, listeners, showMessageBox, verifyUpdateAttestation }
})

vi.mock('electron', () => ({
  app: { isPackaged: true },
  dialog: { showMessageBox: mocks.showMessageBox }
}))

vi.mock('electron-updater', () => ({ default: { autoUpdater: mocks.autoUpdater } }))

vi.mock('./update-attestation.js', () => ({
  verifyUpdateAttestation: mocks.verifyUpdateAttestation
}))

import { startAutoUpdater, stopAutoUpdater } from './updater.js'

function downloadedHandler(): (info: UpdateInfo) => Promise<void> {
  const handler = mocks.listeners.get('update-downloaded')
  if (!handler) throw new Error('update-downloaded handler was not registered')
  return handler
}

beforeEach(() => {
  vi.useFakeTimers()
  mocks.listeners.clear()
  mocks.showMessageBox.mockReset()
  mocks.showMessageBox.mockResolvedValue({ response: 1 })
  mocks.verifyUpdateAttestation.mockReset()
  mocks.autoUpdater.checkForUpdates.mockClear()
  mocks.autoUpdater.quitAndInstall.mockClear()
  mocks.autoUpdater.autoDownload = false
  mocks.autoUpdater.autoInstallOnAppQuit = true
})

afterEach(() => {
  stopAutoUpdater()
  vi.useRealTimers()
})

describe('startAutoUpdater', () => {
  it('keeps install-on-quit disabled while verification runs, then enables it', async () => {
    let resolveVerification!: () => void
    const verification = new Promise<void>((resolve) => {
      resolveVerification = resolve
    })
    mocks.verifyUpdateAttestation.mockReturnValue(verification)

    startAutoUpdater()
    const run = downloadedHandler()({
      downloadedFile: '/tmp/update.zip',
      version: '0.2.7'
    })

    expect(mocks.autoUpdater.autoInstallOnAppQuit).toBe(false)
    expect(mocks.showMessageBox).not.toHaveBeenCalled()

    resolveVerification()
    await run

    expect(mocks.verifyUpdateAttestation).toHaveBeenCalledWith('/tmp/update.zip', '0.2.7')
    expect(mocks.autoUpdater.autoInstallOnAppQuit).toBe(true)
    expect(mocks.showMessageBox).toHaveBeenCalledTimes(1)
  })

  it('leaves install-on-quit disabled and offers nothing when verification fails', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    mocks.verifyUpdateAttestation.mockRejectedValue(new Error('invalid attestation'))

    startAutoUpdater()
    await downloadedHandler()({
      downloadedFile: '/tmp/update.zip',
      version: '0.2.7'
    })

    expect(mocks.verifyUpdateAttestation).toHaveBeenCalledWith('/tmp/update.zip', '0.2.7')
    expect(mocks.autoUpdater.autoInstallOnAppQuit).toBe(false)
    expect(mocks.showMessageBox).not.toHaveBeenCalled()
    expect(mocks.autoUpdater.quitAndInstall).not.toHaveBeenCalled()
    errorSpy.mockRestore()
  })
})
