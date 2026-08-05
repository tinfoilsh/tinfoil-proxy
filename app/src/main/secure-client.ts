import {
  AttestationError,
  FetchError,
  Verifier,
  type VerificationDocument
} from '@tinfoilsh/verifier'

import { REVERIFY_INTERVAL_MS, ROUTER_VERIFY_RETRY_DELAY_MS } from './constants.js'
import { fetchRouters } from './routers.js'
import { stateStore, type RouterState, type VerificationStatus } from './state.js'

interface ClientEntry {
  router: string
  status: VerificationStatus
  document?: VerificationDocument
  lastError?: string
  verification?: Promise<void>
}

const clients = new Map<string, ClientEntry>()
let reverifyTimer: NodeJS.Timeout | undefined
let reverifyInFlight = false
const DEFAULT_CONFIG_REPO = 'tinfoilsh/confidential-model-router'

function describeError(err: unknown): string {
  if (err instanceof Error) return err.message
  try {
    return JSON.stringify(err)
  } catch {
    return String(err)
  }
}

function snapshotRouters(): RouterState[] {
  return Array.from(clients.values()).map((entry) => ({
    router: entry.router,
    status: entry.status,
    lastError: entry.lastError
  }))
}

function updateGlobalStatus(): void {
  const entries = Array.from(clients.values())
  if (entries.length === 0) {
    stateStore.set({
      status: 'initializing',
      statusMessage: 'Waiting for routers…',
      routers: snapshotRouters()
    })
    return
  }
  const verified = entries.filter((e) => e.status === 'verified')
  const failed = entries.filter((e) => e.status === 'failed')
  let status: VerificationStatus = 'initializing'
  let statusMessage = 'Verifying enclave attestation…'
  let lastError: string | undefined
  if (verified.length === entries.length) {
    status = 'verified'
    statusMessage =
      entries.length === 1
        ? 'Attestation verified and key pinned'
        : `All ${entries.length} routers verified`
  } else if (failed.length > 0 && verified.length === 0) {
    status = 'failed'
    statusMessage = 'Attestation failed'
    lastError = failed[0]?.lastError
  } else if (verified.length > 0 && failed.length > 0) {
    status = 'verified'
    statusMessage = `${verified.length} of ${entries.length} routers verified`
    lastError = failed[0]?.lastError
  }
  stateStore.set({
    status,
    statusMessage,
    routers: snapshotRouters(),
    lastError
  })
}

function applyEntryFromDoc(entry: ClientEntry, doc: VerificationDocument): void {
  entry.document = doc
  if (doc.securityVerified) {
    entry.status = 'verified'
    entry.lastError = undefined
  } else {
    const failedStep = Object.entries(doc.steps).find(([, step]) => step?.status === 'failed')
    entry.status = 'failed'
    entry.lastError = failedStep?.[1]?.error ?? 'Attestation incomplete'
  }
  updateGlobalStatus()
}

async function verifyEntry(entry: ClientEntry): Promise<void> {
  if (entry.verification) return entry.verification
  entry.verification = (async () => {
    const verify = async (): Promise<VerificationDocument> => {
      const verifier = new Verifier({
        serverURL: `https://${entry.router}`,
        configRepo: DEFAULT_CONFIG_REPO
      })
      await verifier.verify()
      const document = verifier.getVerificationDocument()
      if (!document) throw new Error('Verifier returned no verification document')
      return document
    }

    try {
      applyEntryFromDoc(entry, await verify())
    } catch (err) {
      if (!(err instanceof FetchError || err instanceof AttestationError)) throw err
      await new Promise((resolve) => setTimeout(resolve, ROUTER_VERIFY_RETRY_DELAY_MS))
      applyEntryFromDoc(entry, await verify())
    }
  })().finally(() => {
    entry.verification = undefined
  })
  return entry.verification
}

export async function activateRouters(routers: string[]): Promise<void> {
  for (const host of Array.from(clients.keys())) {
    if (!routers.includes(host)) {
      clients.delete(host)
    }
  }

  for (const router of routers) {
    if (!clients.has(router)) {
      const entry: ClientEntry = {
        router,
        status: 'initializing'
      }
      clients.set(router, entry)
    }
  }

  updateGlobalStatus()

  await Promise.all(
    routers.map(async (router) => {
      const entry = clients.get(router)
      if (!entry) return
      entry.status = 'initializing'
      entry.lastError = undefined
      try {
        await verifyEntry(entry)
      } catch (err) {
        entry.status = 'failed'
        entry.lastError = describeError(err)
        entry.document = undefined
        updateGlobalStatus()
      }
    })
  )

  scheduleReverify()
}

function scheduleReverify(): void {
  if (reverifyTimer) return
  reverifyTimer = setInterval(() => {
    if (reverifyInFlight) return
    reverifyInFlight = true
    reverifyAll().finally(() => {
      reverifyInFlight = false
    })
  }, REVERIFY_INTERVAL_MS)
}

async function reverifyAll(): Promise<void> {
  await Promise.all(
    Array.from(clients.values()).map(async (entry) => {
      try {
        await verifyEntry(entry)
      } catch (err) {
        entry.status = 'failed'
        entry.lastError = describeError(err)
        entry.document = undefined
        updateGlobalStatus()
      }
    })
  )
}

export function disposeSecureClients(): void {
  if (reverifyTimer) {
    clearInterval(reverifyTimer)
    reverifyTimer = undefined
  }
  clients.clear()
}

export async function refreshRouters(): Promise<void> {
  try {
    const routers = await fetchRouters()
    stateStore.set({ lastError: undefined })
    await activateRouters(routers)
  } catch (err) {
    stateStore.set({ lastError: `Could not fetch routers: ${describeError(err)}` })
  }
}
