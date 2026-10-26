const assert = require('node:assert/strict')
const { createHash } = require('node:crypto')
const { EventEmitter } = require('node:events')
const fs = require('node:fs/promises')
const { readFileSync } = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const { setTimeout: delay } = require('node:timers/promises')
const { test } = require('node:test')
const vm = require('node:vm')
const ts = require('typescript')
const { CancellationToken } = require('builder-util-runtime')
const { MacUpdater } = require('electron-updater/out/MacUpdater')
const { NsisUpdater } = require('electron-updater/out/NsisUpdater')
const { DebUpdater } = require('electron-updater/out/DebUpdater')
const { BaseUpdater } = require('electron-updater/out/BaseUpdater')
const { DownloadedUpdateHelper } = require('electron-updater/out/DownloadedUpdateHelper')

// Run the production controller against the installed electron-updater classes.
// Only Electron UI/native APIs, attestation, HTTP bytes and OS installer execution
// are substituted. No real application is quit or installed by this suite.
const filename = path.resolve(__dirname, '../src/main/updater.ts')
const source = ts.transpileModule(readFileSync(filename, 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true }
}).outputText
const payload = Buffer.from('fixture installer')
const sha512 = createHash('sha512').update(payload).digest('base64')

function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

async function until(predicate) {
  const deadline = Date.now() + 3000
  while (!(await predicate())) {
    if (Date.now() > deadline) throw new Error('Timed out waiting for updater state')
    await delay(5)
  }
}

async function fixture(t, platform, options = {}) {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'proxy-updater-'))
  const app = new EventEmitter()
  app.isPackaged = true
  const native = new EventEmitter()
  const calls = { checks: 0, nativeChecks: 0, nativeRestarts: 0, downloads: 0, verifications: 0, installs: [], dialogs: [], logs: [], timers: new Set() }
  native.setFeedURL = value => { calls.feed = value }
  native.checkForUpdates = () => { calls.nativeChecks++ }
  native.quitAndInstall = () => { calls.nativeRestarts++ }
  const electron = { app, autoUpdater: native, dialog: {
    showMessageBox: async value => { calls.dialogs.push(value); return { response: options.restart ? 0 : 1 } }
  } }
  const electronPath = require.resolve('electron')
  const previousElectron = require.cache[electronPath]
  require.cache[electronPath] = { id: electronPath, filename: electronPath, loaded: true, exports: electron }
  const adapter = {
    version: '0.2.7',
    onQuit: callback => app.on('quit', (_event, code) => callback(code)),
    quit: () => normalQuit(),
    relaunch: () => { calls.relaunch = true }
  }
  const Class = platform === 'darwin' ? MacUpdater : platform === 'win32' ? NsisUpdater : DebUpdater
  const updater = new Class(undefined, adapter)
  const log = (...args) => calls.logs.push(args)
  const logger = { info: log, warn: log, error: log, debug: log }
  updater.logger = logger
  const helper = new DownloadedUpdateHelper(path.join(directory, 'cache'))
  updater.downloadedUpdateHelper = helper
  updater.getOrCreateDownloadHelper = async () => helper
  updater.configOnDisk = { value: Promise.resolve({}) }
  updater.httpExecutor = {
    download: async (_url, destination) => { calls.downloads++; await fs.writeFile(destination, payload) }
  }
  updater.checkForUpdates = async () => {
    calls.checks++
    return null
  }
  // Keep the real NSIS/Deb installation logic; replace only process execution.
  updater.spawnLog = async (command, args) => { calls.installs.push({ command, args }) }
  updater.hasCommand = () => true
  updater.detectPackageManager = () => 'dpkg'
  updater.runCommandWithSudoIfNeeded = args => { calls.installs.push({ args }) }

  const module = { exports: {} }
  const fakeRequire = id => {
    if (id === 'electron') return electron
    if (id === 'electron-updater') return { autoUpdater: updater, BaseUpdater }
    if (id === './update-attestation.js') return { verifyUpdateAttestation: async (...args) => {
      calls.verifications++
      return options.verify?.(...args)
    } }
    return require(id)
  }
  vm.runInNewContext(source, {
    module, exports: module.exports, require: fakeRequire,
    process: { platform }, console: logger,
    setInterval: callback => { calls.timers.add(callback); return callback },
    clearInterval: callback => calls.timers.delete(callback)
  }, { filename })
  const controller = module.exports
  controller.startAutoUpdater()
  function normalQuit(code = 0) {
    controller.stopAutoUpdater()
    app.emit('quit', {}, code)
  }
  async function offer(version = '0.2.8') {
    const extension = platform === 'darwin' ? 'zip' : platform === 'win32' ? 'exe' : 'deb'
    const file = { url: new URL(`https://example.invalid/update.${extension}`), info: { url: `update.${extension}`, sha512, size: payload.length } }
    const info = { version, files: [file.info] }
    return updater.doDownloadUpdate({
      updateInfoAndProvider: { info, provider: { resolveFiles: () => [file] } },
      requestHeaders: {}, cancellationToken: new CancellationToken(),
      disableDifferentialDownload: true, disableWebInstaller: updater.disableWebInstaller
    })
  }
  t.after(async () => {
    controller.stopAutoUpdater()
    if (updater.server?.listening) await new Promise(resolve => updater.server.close(resolve))
    if (previousElectron) require.cache[electronPath] = previousElectron
    else delete require.cache[electronPath]
    await fs.rm(directory, { recursive: true, force: true })
  })
  return { calls, native, updater, normalQuit, offer }
}

test('macOS: verified bytes reach Squirrel before ready is shown; ordinary quit needs no restart call', async t => {
  const verification = deferred()
  const f = await fixture(t, 'darwin', { verify: () => verification.promise })
  await f.offer()
  assert.equal(f.calls.nativeChecks, 0)
  assert.equal(f.calls.dialogs.length, 0)
  verification.resolve()
  await until(() => f.calls.nativeChecks === 1)
  assert.ok(f.calls.feed.url.startsWith('http://127.0.0.1:'))
  assert.equal(f.calls.dialogs.length, 0)
  f.native.emit('update-downloaded')
  await until(() => f.calls.dialogs.length === 1)
  assert.equal(f.updater.squirrelDownloadedUpdate, true)
  f.normalQuit()
  assert.equal(f.calls.nativeRestarts, 0)
})

test('macOS: Restart now calls the real MacUpdater path after native readiness', async t => {
  const f = await fixture(t, 'darwin', { restart: true })
  await f.offer()
  await until(() => f.calls.nativeChecks === 1)
  assert.equal(f.calls.nativeRestarts, 0)
  f.native.emit('update-downloaded')
  await until(() => f.calls.nativeRestarts === 1)
})

test('macOS: native staging failure leaves the update unannounced and permits retry', async t => {
  const f = await fixture(t, 'darwin')
  await f.offer()
  await until(() => f.calls.nativeChecks === 1)
  f.native.emit('error', new Error('native staging failed'))
  await until(() => f.calls.logs.some(args => args[0] === '[updater] could not prepare update:'))
  assert.equal(f.calls.dialogs.length, 0)
  await f.offer()
  await until(() => f.calls.nativeChecks === 2)
  f.native.emit('update-downloaded')
  await until(() => f.calls.dialogs.length === 1)
  assert.equal(f.calls.downloads, 1, 'retry exercises cached completion')
})

for (const platform of ['darwin', 'win32', 'linux']) {
  test(`${platform}: rejected verification blocks both installation and the ready prompt, including cache hits`, async t => {
    const f = await fixture(t, platform, { verify: async () => { throw new Error('invalid attestation') } })
    await f.offer()
    await until(() => f.calls.logs.some(args => args[0] === '[updater] could not prepare update:'))
    await f.offer()
    await until(() => f.calls.verifications === 2)
    f.normalQuit()
    assert.equal(f.calls.downloads, 1)
    assert.equal(f.calls.nativeChecks, 0)
    assert.equal(f.calls.installs.length, 0)
    assert.equal(f.calls.dialogs.length, 0)
    assert.equal(f.updater.autoInstallOnAppQuit, false)
  })

  test(`${platform}: quitting during verification does not stage or install afterward`, async t => {
    const verification = deferred()
    const f = await fixture(t, platform, { verify: () => verification.promise })
    await f.offer()
    const checks = f.calls.checks
    for (const run of f.calls.timers) run()
    assert.equal(f.calls.checks, checks, 'do not replace the download during verification')
    f.normalQuit()
    verification.resolve()
    await delay(10)
    assert.equal(f.calls.nativeChecks, 0)
    assert.equal(f.calls.installs.length, 0)
    assert.equal(f.calls.dialogs.length, 0)
  })

  test(`${platform}: a cached update is verified and prepared again on the next check`, async t => {
    const f = await fixture(t, platform)
    const scheduledCheck = [...f.calls.timers][0]
    for (let attempt = 1; attempt <= 2; attempt++) {
      await f.offer()
      if (platform === 'darwin') {
        await until(() => f.calls.nativeChecks === attempt)
        f.native.emit('update-downloaded')
      }
      await until(() => f.calls.dialogs.length === attempt)
      const checks = f.calls.checks
      scheduledCheck()
      assert.equal(f.calls.checks, checks + 1, 'periodic checks continue after Later')
    }
    assert.equal(f.calls.downloads, 1)
    assert.equal(f.calls.verifications, 2)
    f.normalQuit()
    if (platform !== 'darwin') assert.equal(f.calls.installs.length, 1)
  })
}

for (const platform of ['win32', 'linux']) {
  test(`${platform}: Later and ordinary quit install a verified update without forcing relaunch`, async t => {
    const f = await fixture(t, platform)
    if (platform === 'win32') assert.equal(f.updater.disableWebInstaller, false)
    await f.offer()
    await until(() => f.calls.dialogs.length === 1)
    assert.equal(f.calls.installs.length, 0)
    f.normalQuit()
    assert.equal(f.calls.installs.length, 1)
    if (platform === 'win32') {
      assert.ok(f.calls.installs[0].args.includes('/S'))
      assert.ok(!f.calls.installs[0].args.includes('--force-run'))
    } else {
      assert.equal(f.calls.installs[0].args[0], 'dpkg')
      assert.equal(f.calls.relaunch, undefined)
    }
  })

  test(`${platform}: Restart now installs once even when the quit hook also runs`, async t => {
    const f = await fixture(t, platform, { restart: true })
    await f.offer()
    await until(() => f.calls.installs.length === 1)
    await delay(10)
    assert.equal(f.calls.installs.length, 1)
    if (platform === 'win32') assert.ok(f.calls.installs[0].args.includes('--force-run'))
    else assert.equal(f.calls.relaunch, true)
  })

  test(`${platform}: abnormal exit does not launch an installer`, async t => {
    const f = await fixture(t, platform)
    await f.offer()
    await until(() => f.calls.dialogs.length === 1)
    f.normalQuit(1)
    assert.equal(f.calls.installs.length, 0)
  })
}
