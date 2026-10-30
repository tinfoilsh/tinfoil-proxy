import { promises as fs } from 'node:fs'
import { join } from 'node:path'
import { app, autoUpdater as nativeUpdater, dialog } from 'electron'
import pkg from 'electron-updater'

import { verifyUpdateAttestation } from './update-attestation.js'

const { autoUpdater } = pkg

const UPDATE_CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000
const DISMISSED_UPDATE_VERSION_FILE = 'dismissed-update-version'

let timer: NodeJS.Timeout | undefined
let stopping = false
let preparing = false
let verified = false
let dismissedVersion: string | undefined

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

export function startAutoUpdater(): void {
  if (!app.isPackaged) return

  autoUpdater.autoDownload = true
  // Keep the library's automatic handoff disabled. Its event dispatch does
  // not await our verifier, so toggling this flag back on cannot resume it.
  autoUpdater.autoInstallOnAppQuit = false

  autoUpdater.on('update-downloaded', async (info) => {
    if (stopping || preparing) return
    preparing = true
    verified = false
    try {
      await verifyUpdateAttestation(info.downloadedFile, info.version)
      if (stopping) return
      console.info('[updater] attestation verified for', info.version)

      if (process.platform === 'darwin') await stageMacUpdate()
      if (stopping) return
      verified = true

      const userDataPath = app.getPath('userData')
      const dismissalPath = join(userDataPath, DISMISSED_UPDATE_VERSION_FILE)
      if (dismissedVersion === undefined) {
        try {
          dismissedVersion = await fs.readFile(dismissalPath, 'utf8')
        } catch (error) {
          if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
            autoUpdater.logger?.warn(`[updater] could not read dismissed update version: ${error}`)
          }
        }
      }
      if (stopping || dismissedVersion === info.version) return

      const choice = await dialog.showMessageBox({
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
      if (choice.response === 0 && !stopping) autoUpdater.quitAndInstall()
      if (choice.response === 1) {
        dismissedVersion = info.version
        try {
          await fs.mkdir(userDataPath, { recursive: true })
          await fs.writeFile(dismissalPath, dismissedVersion, 'utf8')
        } catch (error) {
          autoUpdater.logger?.warn(`[updater] could not save dismissed update version: ${error}`)
        }
      }
    } catch (error) {
      console.error('[updater] could not prepare update:', error)
    } finally {
      preparing = false
    }
  })

  app.once('quit', (_event, exitCode) => {
    // Squirrel installs a staged macOS update on ordinary quit itself.
    // BaseUpdater platforms need the same synchronous install used by its
    // own quit handler, gated here on completed attestation verification.
    if (exitCode === 0 && verified && autoUpdater instanceof pkg.BaseUpdater) {
      autoUpdater.install(true, false)
    }
  })

  autoUpdater.on('error', (err) => {
    console.error('[updater]', err)
  })

  const run = () => {
    if (stopping || preparing) return
    autoUpdater.checkForUpdates().catch(() => undefined)
  }

  run()
  timer = setInterval(run, UPDATE_CHECK_INTERVAL_MS)
}

export function stopAutoUpdater(): void {
  stopping = true
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}
