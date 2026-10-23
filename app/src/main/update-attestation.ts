import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'

import {
  GITHUB_OIDC_ISSUER,
  AllOf,
  OIDCIssuer,
  GitHubWorkflowRepository,
  GitHubWorkflowRef,
  OIDCRunnerEnvironment,
} from '@freedomofpress/sigstore-browser'
import { netFetch, verifyBundleWithPolicy } from './update-attestation-core.js'

// Updates are verified against Sigstore attestations fetched from
// api.github.com (independent of the release CDN) before install; the
// checksums in latest*.yml share the update's TLS connection and only
// rule out corruption.

const ATTESTATIONS_URL = 'https://api.github.com/repos/tinfoilsh/tinfoil-proxy/attestations'

interface AttestationEnvelope {
  attestations?: Array<{ bundle_url?: string }>
}

// The update signer policy is fixed here and is not caller-controlled.
export function proxyPolicy(version: string): AllOf {
  return new AllOf([
    new OIDCIssuer(GITHUB_OIDC_ISSUER),
    new GitHubWorkflowRepository('tinfoilsh/tinfoil-proxy'),
    new GitHubWorkflowRef(`refs/tags/v${version}`),
    new OIDCRunnerEnvironment('github-hosted'),
  ])
}

async function fetchJSON(url: string): Promise<unknown> {
  const response = await netFetch(url, {
    headers: { Accept: 'application/vnd.github+json', 'User-Agent': 'tinfoil-proxy-updater' },
  })
  // GitHub answers 404 for a digest with no attestations; everything else
  // unexpected is a transport failure. Both refuse the update.
  if (response.status === 404) {
    return { attestations: [] }
  }
  if (!response.ok) {
    throw new Error(`attestation request to ${url} failed: ${String(response.status)}`)
  }
  return response.json()
}

export async function verifyUpdateAttestation(downloadedFile: string, version: string): Promise<void> {
  const data = await readFile(downloadedFile)
  const digest = createHash('sha256').update(data).digest('hex')

  const envelope = (await fetchJSON(`${ATTESTATIONS_URL}/sha256:${digest}`)) as AttestationEnvelope
  const bundleURLs = (envelope.attestations ?? [])
    .map((a) => a.bundle_url)
    .filter((u): u is string => typeof u === 'string' && u.startsWith('https://'))
  if (bundleURLs.length === 0) {
    throw new Error(`no attestations for update digest sha256:${digest}`)
  }

  // GitHub serves attestation bundles snappy-compressed from short-lived
  // blob URLs.
  let lastError: Error | undefined = undefined
  for (const bundleURL of bundleURLs) {
    try {
      await verifyBundleWithPolicy(bundleURL, digest, proxyPolicy(version))
      return
    } catch (err) {
      lastError = err instanceof Error ? err : new Error(String(err))
    }
  }
  throw new Error(`no valid attestation authenticates this update: ${lastError?.message ?? 'none listed'}`)
}
