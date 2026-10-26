import { readFile, writeFile, rename } from 'node:fs/promises'
import { join } from 'node:path'
import { app, autoUpdater as nativeUpdater, dialog } from 'electron'
import pkg from 'electron-updater'
import type { UpdateDownloadedEvent } from 'electron-updater'

import { verifyUpdateAttestation } from './update-attestation.js'

const { autoUpdater } = pkg

const UPDATE_CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000

let timer: NodeJS.Timeout | undefined
let started = false
let stopping = false
let checking = false
let preparing = false
let readyVersion: string | undefined

// MacUpdater prepares a loopback feed before emitting update-downloaded, but
// does not ask Squirrel to fetch it when autoInstallOnAppQuit is false. Start
// that handoff explicitly after attestation verification, and wait for native
// readiness before promising that an ordinary quit will install the update.
function stageMacUpdate(): Promise<void> {
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      nativeUpdater.removeListener('update-downloaded', onDownloaded)
      nativeUpdater.removeListener('error', onError)
    }
    const onDownloaded = () => {
      cleanup()
      resolve()
    }
    const onError = (error: Error) => {
      cleanup()
      reject(error)
    }
    nativeUpdater.once('update-downloaded', onDownloaded)
    nativeUpdater.once('error', onError)
    try {
      nativeUpdater.checkForUpdates()
    } catch (error) {
      cleanup()
      reject(error)
    }
  })
}

async function wasNotified(version: string): Promise<boolean> {
  const path = join(app.getPath('userData'), 'update-notification.json')
  try {
    const saved = JSON.parse(await readFile(path, 'utf8')) as { version?: string }
    return saved?.version === version
  } catch {
    // A missing or invalid notification record must not block an update.
    return false
  }
}

async function rememberNotification(version: string): Promise<void> {
  const path = join(app.getPath('userData'), 'update-notification.json')
  try {
    const temporaryPath = `${path}.tmp`
    await writeFile(temporaryPath, JSON.stringify({ version }), 'utf8')
    await rename(temporaryPath, path)
  } catch (error) {
    console.error('[updater] could not persist update notification:', error)
  }
}

async function prepareUpdate(info: UpdateDownloadedEvent): Promise<void> {
  if (stopping || preparing || readyVersion) return
  preparing = true
  try {
    await verifyUpdateAttestation(info.downloadedFile, info.version)
    if (stopping) return
    console.info('[updater] attestation verified for', info.version)

    if (process.platform === 'darwin') await stageMacUpdate()
    if (stopping) return
    readyVersion = info.version
    clearUpdateTimer()
    console.info('[updater] update ready for install on quit:', info.version)

    if (await wasNotified(info.version) || stopping) return
    const choicePromise = dialog.showMessageBox({
      type: 'info',
      title: 'Tinfoil Proxy update ready',
      message: `Tinfoil Proxy ${info.version} is ready to install.`,
      detail:
        'The update will be applied the next time you quit Tinfoil Proxy, or you can restart now.',
      buttons: ['Restart now', 'Later'],
      defaultId: 0,
      cancelId: 1,
      noLink: true
    })
    const [choice] = await Promise.all([choicePromise, rememberNotification(info.version)])
    if (choice.response === 0 && !stopping) autoUpdater.quitAndInstall()
  } catch (error) {
    console.error('[updater] could not prepare update:', error)
  } finally {
    preparing = false
  }
}

export function startAutoUpdater(): void {
  if (!app.isPackaged || started) return
  started = true

  autoUpdater.autoDownload = true
  // Attestation authenticates the single downloaded installer, not a web
  // installer's separately downloaded payload. Releases use offline NSIS.
  autoUpdater.disableWebInstaller = true
  // Keep the library's automatic handoff disabled. Its event dispatch does
  // not await our verifier, so toggling this flag back on cannot resume it.
  autoUpdater.autoInstallOnAppQuit = false

  autoUpdater.on('update-downloaded', (info) => {
    void prepareUpdate(info)
  })

  app.once('quit', (_event, exitCode) => {
    // Squirrel installs a staged macOS update on ordinary quit itself.
    // BaseUpdater platforms need the same synchronous install used by its
    // own quit handler, gated here on completed attestation verification.
    if (exitCode === 0 && readyVersion && autoUpdater instanceof pkg.BaseUpdater) {
      autoUpdater.install(true, false)
    }
  })

  autoUpdater.on('error', (err) => {
    console.error('[updater]', err)
  })

  const run = async () => {
    if (stopping || checking || preparing || readyVersion) return
    checking = true
    try {
      const result = await autoUpdater.checkForUpdates()
      await result?.downloadPromise
    } catch (error) {
      console.error('[updater] update check failed:', error)
    } finally {
      checking = false
    }
  }

  void run()
  timer = setInterval(() => { void run() }, UPDATE_CHECK_INTERVAL_MS)
}

function clearUpdateTimer(): void {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}

export function stopAutoUpdater(): void {
  stopping = true
  clearUpdateTimer()
}
