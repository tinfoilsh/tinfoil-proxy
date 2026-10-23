import { net as electronNet } from 'electron'
import { uncompress } from 'snappyjs'
import { AllOf, SigstoreVerifier } from '@freedomofpress/sigstore-browser'
import sigstoreTrustedRoot from './sigstore-trusted-root.js'

const ATTESTATION_MAX_BYTES = 4 << 20
// A pending verification must not hold install-on-quit open.
const ATTESTATION_TIMEOUT_MS = 30_000

interface InTotoStatement {
  _type?: string
  subject?: Array<{ name: string; digest: Record<string, string> }>
}

let cachedVerifier: SigstoreVerifier | undefined = undefined

async function verifier(): Promise<SigstoreVerifier> {
  if (cachedVerifier) return cachedVerifier
  const v = new SigstoreVerifier()
  await v.loadSigstoreRoot(sigstoreTrustedRoot)
  cachedVerifier = v
  return v
}

// electronNet is Electron's fetch: it uses the app's configured network
// stack (proxy, custom certs) rather than Node's global fetch.
export async function netFetch(url: string, init?: RequestInit): Promise<Response> {
  return electronNet.fetch(url, { ...init, signal: AbortSignal.timeout(ATTESTATION_TIMEOUT_MS) })
}

// Read a response body incrementally, aborting as soon as it is known to
// exceed the size limit so an oversized bundle cannot be buffered first.
async function readBounded(response: Response, limit: number): Promise<Uint8Array> {
  if (!response.body) {
    throw new Error('attestation bundle has no body')
  }
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let total = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    total += value.byteLength
    if (total > limit) {
      await reader.cancel()
      throw new Error('attestation bundle exceeds size limit')
    }
    chunks.push(value)
  }
  const out = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    out.set(chunk, offset)
    offset += chunk.byteLength
  }
  return out
}

// Verify one bundle against an explicit policy. The production entry point in
// update-attestation.ts always supplies the fixed proxy policy; this export
// exists so tests can exercise the real verifier with a known bundle.
export async function verifyBundleWithPolicy(
  bundleURL: string,
  digest: string,
  policy: AllOf
): Promise<void> {
  const response = await netFetch(bundleURL)
  if (!response.ok) {
    throw new Error(`fetching attestation bundle failed: ${String(response.status)}`)
  }
  const compressed = await readBounded(response, ATTESTATION_MAX_BYTES)
  // snappyjs enforces the decoded-length bound itself: uncompress refuses
  // output larger than maxLength.
  let bundleJSON: Uint8Array
  try {
    bundleJSON = uncompress(compressed, ATTESTATION_MAX_BYTES)
  } catch {
    throw new Error('attestation bundle is not valid snappy data')
  }

  const bundle = JSON.parse(new TextDecoder().decode(bundleJSON))
  // Legacy x509CertificateChain layout can carry extra CA certificates
  // under the signing certificate; only v0.3 single-cert is accepted.
  if (bundle?.verificationMaterial?.x509CertificateChain) {
    throw new Error('legacy bundle format not supported')
  }
  const env = bundle?.dsseEnvelope
  if (!env || !Array.isArray(env.signatures) || env.signatures.length !== 1) {
    throw new Error('attestation bundle must carry exactly one DSSE signature')
  }

  const v = await verifier()
  const { payloadType, payload } = await v.verifyDsse(bundle, policy)
  if (payloadType !== 'application/vnd.in-toto+json') {
    throw new Error(`unsupported attestation payload type: ${payloadType}`)
  }

  const statement = JSON.parse(new TextDecoder().decode(payload)) as InTotoStatement
  if (statement._type !== 'https://in-toto.io/Statement/v1') {
    throw new Error('attestation is not an in-toto statement')
  }
  // The update archive's digest must appear among the attested subjects;
  // the release workflow attests every platform artifact in one statement.
  const subjects = statement.subject ?? []
  if (!subjects.some((s) => s.digest?.sha256?.toLowerCase() === digest.toLowerCase())) {
    throw new Error('attestation subjects do not include this update')
  }
}
