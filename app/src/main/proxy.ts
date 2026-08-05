import { spawn, type ChildProcessByStdio } from 'node:child_process'
import type { Readable, Writable } from 'node:stream'
import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { app } from 'electron'
import { z } from 'zod'

import { PROXY_LISTEN_HOST } from './constants.js'
import { stateStore } from './state.js'

const PROXY_STOP_GRACE_MS = 3000
const STDERR_TAIL_LIMIT = 4096
const READY_TIMEOUT_MS = 30000
const PORT_IN_USE_PATTERN = /address already in use|EADDRINUSE/i

type CliProcess = ChildProcessByStdio<Writable, Readable, Readable>

const verificationStepSchema = z.object({
  status: z.enum(['success', 'skipped']),
  error: z.string().optional()
})

const proxyVerificationDocumentSchema = z.object({
  schemaVersion: z.literal(1),
  configRepo: z.string().min(1),
  enclaveHost: z.string().min(1),
  releaseTag: z.string().min(1).optional(),
  releaseDigest: z.string().min(1),
  codeMeasurement: z.object({ type: z.string().min(1), registers: z.array(z.string()).min(1) }),
  enclaveMeasurement: z.object({
    measurement: z.object({ type: z.string().min(1), registers: z.array(z.string()).min(1) })
  }),
  tlsPublicKey: z.string().min(1),
  hpkePublicKey: z.string().min(1),
  codeFingerprint: z.string().min(1),
  enclaveFingerprint: z.string().min(1),
  selectedRouterEndpoint: z.string().min(1),
  securityVerified: z.literal(true),
  verifier: z.object({ name: z.string().min(1), version: z.string().min(1) }),
  verifiedAt: z.string().min(1),
  steps: z.object({
    fetchDigest: verificationStepSchema,
    verifyCode: verificationStepSchema,
    verifyEnclave: verificationStepSchema,
    compareMeasurements: verificationStepSchema,
    verifyCertificate: verificationStepSchema
  }),
  runtime: z.object({
    instanceId: z.string().min(1),
    listener: z.string().min(1),
    software: z.object({
      name: z.literal('tinfoil-proxy'),
      version: z.string().min(1)
    })
  })
})

type ProxyVerificationDocument = z.infer<typeof proxyVerificationDocumentSchema>

interface ReadyMessage {
  event: 'ready'
  instanceId: string
  enclave: string
  repo: string
  listen: string
  verificationDocument: ProxyVerificationDocument
}

interface TokensMessage {
  event: 'tokens'
  upstreamed: number
  downstreamed: number
}

interface InvalidReadyMessage {
  event: 'invalid-ready'
  error: string
}

interface VerificationMessage {
  event: 'verification'
  verificationDocument: ProxyVerificationDocument
}

interface InvalidVerificationMessage {
  event: 'invalid-verification'
  error: string
}

type ProxyMessage = ReadyMessage | TokensMessage | InvalidReadyMessage | VerificationMessage | InvalidVerificationMessage

let child: CliProcess | undefined
let intentionalShutdown = false
let stopWaiter: Promise<void> | undefined

function binaryFileName(): string {
  return process.platform === 'win32' ? 'tinfoil-proxy.exe' : 'tinfoil-proxy'
}

function locateBinary(): string {
  const name = binaryFileName()
  const candidates: string[] = []
  if (app.isPackaged) {
    candidates.push(join(process.resourcesPath, 'bin', name))
  } else {
    candidates.push(join(app.getAppPath(), 'resources', 'bin', name))
    candidates.push(join(app.getAppPath(), '..', name))
  }
  for (const candidate of candidates) {
    if (existsSync(candidate)) return candidate
  }
  return candidates[0] ?? name
}

export function proxyEndpoint(port: number): string {
  return `http://${PROXY_LISTEN_HOST}:${port}/v1`
}

export function verificationDocumentEndpoint(port: number): string {
  return `http://${PROXY_LISTEN_HOST}:${port}/verification-document`
}

function setProxyState(partial: Partial<ReturnType<typeof stateStore.get>['proxy']>): void {
  const current = stateStore.get().proxy
  stateStore.set({ proxy: { ...current, ...partial } })
}

function attachLogging(
  proc: CliProcess,
  sink: { stderrTail: string },
  onStdoutLine: (line: string) => void
): void {
  proc.stdout.setEncoding('utf8')
  proc.stderr.setEncoding('utf8')
  let stdoutBuffer = ''
  proc.stdout.on('data', (chunk: string) => {
    stdoutBuffer += chunk
    let newlineIndex = stdoutBuffer.indexOf('\n')
    while (newlineIndex !== -1) {
      const line = stdoutBuffer.slice(0, newlineIndex).trim()
      stdoutBuffer = stdoutBuffer.slice(newlineIndex + 1)
      if (line.length > 0) {
        console.log('[tinfoil]', line)
        onStdoutLine(line)
      }
      newlineIndex = stdoutBuffer.indexOf('\n')
    }
  })
  proc.stderr.on('data', (chunk: string) => {
    sink.stderrTail = (sink.stderrTail + chunk).slice(-STDERR_TAIL_LIMIT)
    for (const line of chunk.split('\n')) {
      if (line.trim().length > 0) console.warn('[tinfoil]', line)
    }
  })
}

function isTokenCount(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

function parseProxyLine(line: string): ProxyMessage | null {
  if (!line.startsWith('{')) return null
  try {
    const parsed = JSON.parse(line) as Record<string, unknown>
    if (parsed.event === 'ready') {
      if (
        typeof parsed.instance_id !== 'string' ||
        typeof parsed.enclave !== 'string' ||
        typeof parsed.repo !== 'string' ||
        typeof parsed.listen !== 'string' ||
        typeof parsed.verification_document !== 'object' ||
        parsed.verification_document === null
      ) {
        return { event: 'invalid-ready', error: 'proxy ready message is missing required fields' }
      }
      const document = proxyVerificationDocumentSchema.safeParse(parsed.verification_document)
      if (!document.success) {
        const issue = document.error.issues[0]
        const field = issue?.path.join('.') || 'verification_document'
        return {
          event: 'invalid-ready',
          error: `verification document schema mismatch at ${field}: ${issue?.message ?? 'invalid value'}`
        }
      }
      return {
        event: 'ready',
        instanceId: parsed.instance_id,
        enclave: parsed.enclave,
        repo: parsed.repo,
        listen: parsed.listen,
        verificationDocument: document.data
      }
    }
    if (
      parsed.event === 'tokens' &&
      isTokenCount(parsed.upstreamed) &&
      isTokenCount(parsed.downstreamed)
    ) {
      return {
        event: 'tokens',
        upstreamed: parsed.upstreamed,
        downstreamed: parsed.downstreamed
      }
    }
    if (parsed.event === 'verification') {
      const document = proxyVerificationDocumentSchema.safeParse(parsed.verification_document)
      if (!document.success) {
        const issue = document.error.issues[0]
        const field = issue?.path.join('.') || 'verification_document'
        return {
          event: 'invalid-verification',
          error: `updated verification document schema mismatch at ${field}: ${issue?.message ?? 'invalid value'}`
        }
      }
      return { event: 'verification', verificationDocument: document.data }
    }
  } catch {
    // Not a JSON line; ignore.
  }
  return null
}

async function waitForExit(proc: CliProcess): Promise<void> {
  return new Promise<void>((resolve) => {
    if (proc.exitCode !== null) {
      resolve()
      return
    }
    proc.once('exit', () => resolve())
  })
}

export async function startProxy(
  port: number,
  allowedHosts: string[] = []
): Promise<{ port: number; endpoint: string } | null> {
  if (child) {
    await stopProxy()
  }

  const binary = locateBinary()
  if (!existsSync(binary)) {
    const message = `Tinfoil proxy binary not found at ${binary}`
    setProxyState({
      enabled: true,
      running: false,
      verifying: false,
      verified: false,
      port,
      allowedHosts,
      upstreamedTokens: 0,
      downstreamedTokens: 0,
      enclave: undefined,
      lastError: message
    })
    return null
  }

  intentionalShutdown = false
  const args = ['-p', String(port), '-b', PROXY_LISTEN_HOST, '--handshake']
  for (const host of allowedHosts) {
    args.push('--allowed-host', host)
  }
  const proc = spawn(binary, args, {
    stdio: ['pipe', 'pipe', 'pipe'],
    env: { ...process.env }
  }) as CliProcess
  const logSink = { stderrTail: '' }
  let acceptedInstanceID: string | undefined
  let acceptedRepo: string | undefined

  setProxyState({
    enabled: true,
    running: false,
    verifying: true,
    verified: false,
    port,
    allowedHosts,
    upstreamedTokens: 0,
    downstreamedTokens: 0,
    enclave: undefined,
    lastError: undefined
  })

  let readyResolved = false
  let readyTimer: NodeJS.Timeout | undefined
  const settleReady = (
    fn: () => void,
    cleanup: () => void
  ): void => {
    if (readyResolved) return
    readyResolved = true
    if (readyTimer) clearTimeout(readyTimer)
    cleanup()
    fn()
  }

  const readyPromise = new Promise<ReadyMessage>((resolve, reject) => {
    const onEarlyExit = (code: number | null, signal: NodeJS.Signals | null): void => {
      settleReady(
        () => {
          const portInUse = PORT_IN_USE_PATTERN.test(logSink.stderrTail)
          const reason = portInUse
            ? `Port ${port} is already in use. Stop the other process or choose a different port.`
            : `Tinfoil proxy exited before reporting ready (${signal ?? `code ${code ?? 0}`})`
          reject(new Error(reason))
        },
        () => {}
      )
    }

    readyTimer = setTimeout(() => {
      settleReady(
        () => reject(new Error('proxy did not report ready within timeout')),
        () => proc.off('close', onEarlyExit)
      )
    }, READY_TIMEOUT_MS)

    proc.once('close', onEarlyExit)

    attachLogging(proc, logSink, (line) => {
      const message = parseProxyLine(line)
      if (!message) return
      if (message.event === 'invalid-verification') {
        if (child === proc && proc.exitCode === null) {
          setProxyState({ verified: false, lastError: message.error })
          proc.kill('SIGTERM')
        }
        return
      }
      if (message.event === 'verification') {
        if (child !== proc || proc.exitCode !== null || !acceptedInstanceID || !acceptedRepo) return
        const document = message.verificationDocument
        if (
          document.runtime.instanceId !== acceptedInstanceID ||
          document.runtime.listener !== `${PROXY_LISTEN_HOST}:${port}` ||
          document.configRepo !== acceptedRepo
        ) {
          setProxyState({
            verified: false,
            lastError: 'Updated verification document does not match the running proxy instance'
          })
          proc.kill('SIGTERM')
          return
        }
        setProxyState({ enclave: document.enclaveHost, verifiedAt: document.verifiedAt })
        return
      }
      if (message.event === 'invalid-ready') {
        settleReady(
          () => reject(new Error(message.error)),
          () => proc.off('close', onEarlyExit)
        )
        return
      }
      if (message.event === 'tokens') {
        if (child !== proc || proc.exitCode !== null) return
        setProxyState({
          upstreamedTokens: message.upstreamed,
          downstreamedTokens: message.downstreamed
        })
        return
      }
      settleReady(
        () => resolve(message),
        () => proc.off('close', onEarlyExit)
      )
    })
  })

  proc.on('exit', () => {
    if (child === proc) child = undefined
  })

  proc.on('close', (code, signal) => {
    if (child !== undefined && child !== proc) return
    const wasIntentional = intentionalShutdown
    if (wasIntentional) {
      setProxyState({ running: false, verifying: false, verified: false, lastError: undefined })
      return
    }
    const portInUse = PORT_IN_USE_PATTERN.test(logSink.stderrTail)
    const message = portInUse
      ? `Port ${port} is already in use. Stop the other process or choose a different port.`
      : `Tinfoil proxy exited unexpectedly (${signal ?? `code ${code ?? 0}`})`
    const existingError = stateStore.get().proxy.lastError
    setProxyState({
      running: false,
      verifying: false,
      verified: false,
      lastError: existingError ?? message
    })
  })

  proc.on('error', (err) => {
    if (child !== undefined && child !== proc) return
    child = undefined
    setProxyState({ running: false, verifying: false, verified: false, lastError: err.message })
  })

  child = proc

  let ready: ReadyMessage
  try {
    ready = await readyPromise
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    const existingError = stateStore.get().proxy.lastError
    setProxyState({
      running: false,
      verifying: false,
      verified: false,
      lastError: existingError ?? message
    })
    sendSignal(proc, 'abort')
    return null
  }

  if (child !== proc || proc.exitCode !== null) {
    return null
  }

  setProxyState({ enclave: ready.enclave })

  const document = ready.verificationDocument
  if (
    document.runtime.instanceId !== ready.instanceId ||
    document.runtime.listener !== ready.listen ||
    document.enclaveHost !== ready.enclave ||
    document.configRepo !== ready.repo ||
    ready.listen !== `${PROXY_LISTEN_HOST}:${port}`
  ) {
    setProxyState({
      running: false,
      verifying: false,
      verified: false,
      lastError: 'Proxy verification document does not match the running proxy instance'
    })
    sendSignal(proc, 'abort')
    return null
  }

  // The desktop app ships and trusts this proxy binary as its active verifier.
  // Binding the document to this process and listener avoids a second verifier
  // with independently changing release selection rather than adding a new
  // trust root.
  acceptedInstanceID = ready.instanceId
  acceptedRepo = ready.repo
  sendSignal(proc, 'go')
  setProxyState({
    running: true,
    verifying: false,
    verified: true,
    verifiedAt: document.verifiedAt,
    lastError: undefined
  })
  return { port, endpoint: proxyEndpoint(port) }
}

function sendSignal(proc: CliProcess, signal: 'go' | 'abort'): void {
  try {
    if (!proc.stdin.destroyed && proc.stdin.writable) {
      proc.stdin.write(`${signal}\n`)
      proc.stdin.end()
    }
  } catch (err) {
    console.warn('[tinfoil] failed to send handshake signal:', err)
  }
}

export async function stopProxy(): Promise<void> {
  const proc = child
  if (!proc) return
  if (stopWaiter) return stopWaiter
  intentionalShutdown = true
  stopWaiter = (async () => {
    try {
      proc.kill('SIGTERM')
      const settled = await Promise.race([
        waitForExit(proc),
        new Promise<'timeout'>((resolve) => setTimeout(() => resolve('timeout'), PROXY_STOP_GRACE_MS))
      ])
      if (settled === 'timeout' && proc.exitCode === null) {
        proc.kill('SIGKILL')
        await waitForExit(proc)
      }
    } finally {
      stopWaiter = undefined
    }
  })()
  return stopWaiter
}

export function isProxyRunning(): boolean {
  return child !== undefined && child.exitCode === null
}
