import { app, dialog } from 'electron'
import pkg from 'electron-updater'

import { verifyUpdateAttestation } from './update-attestation.js'

const { autoUpdater } = pkg

const UPDATE_CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000

let timer: NodeJS.Timeout | undefined

// Updates are authenticated before they can be installed. Install-on-quit
// is disabled for a download until its attestation verifies, and a failed
// verification leaves the update on disk but never offered.
export function startAutoUpdater(): void {
  if (!app.isPackaged) return

  autoUpdater.autoDownload = true
  autoUpdater.autoInstallOnAppQuit = true

  autoUpdater.on('update-downloaded', async (info) => {
    // Fail closed from the start: quitting during verification must not
    // install the unverified download, so install-on-quit is off until the
    // attestation verifies.
    autoUpdater.autoInstallOnAppQuit = false
    try {
      await verifyUpdateAttestation(info.downloadedFile, info.version)
    } catch (err) {
      console.error('[updater] update attestation verification failed; not installing:', err)
      return
    }
    autoUpdater.autoInstallOnAppQuit = true

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
    if (choice.response === 0) {
      autoUpdater.quitAndInstall()
    }
  })

  autoUpdater.on('error', (err) => {
    console.error('[updater]', err)
  })

  const run = () => {
    autoUpdater.checkForUpdates().catch(() => undefined)
  }

  run()
  timer = setInterval(run, UPDATE_CHECK_INTERVAL_MS)
}

export function stopAutoUpdater(): void {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}
