import { readFile, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { compress, uncompress } from 'snappyjs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  AllOf,
  GITHUB_OIDC_ISSUER,
  GitHubWorkflowRef,
  GitHubWorkflowRepository,
  OIDCIssuer,
  OIDCRunnerEnvironment
} from '@freedomofpress/sigstore-browser'

const { netFetchMock } = vi.hoisted(() => ({ netFetchMock: vi.fn() }))

vi.mock('electron', () => ({ net: { fetch: netFetchMock } }))

import { proxyPolicy, verifyUpdateAttestation } from './update-attestation.js'
import { verifyBundleWithPolicy } from './update-attestation-core.js'

const bundleFixture = new URL('./testdata/attestation/bundle.snappy', import.meta.url)
// Subject digest of the captured v0.19.9 linux/amd64 tinfoil-cli archive.
const verifiedDigest = '6c9bbaf8e7dc4cf1502cfa222cf8badf53386157dd8b433ac2855c4650babb77'

function bundlePolicy(ref = 'refs/tags/v0.19.9'): AllOf {
  return new AllOf([
    new OIDCIssuer(GITHUB_OIDC_ISSUER),
    new GitHubWorkflowRepository('tinfoilsh/tinfoil-cli'),
    new GitHubWorkflowRef(ref),
    new OIDCRunnerEnvironment('github-hosted')
  ])
}

function response(body: Uint8Array | string): Response {
  return new Response(body)
}

beforeEach(() => {
  netFetchMock.mockReset()
})

describe('verifyBundleWithPolicy', () => {
  it('verifies the captured release bundle', async () => {
    netFetchMock.mockResolvedValue(response(await readFile(bundleFixture)))

    await expect(
      verifyBundleWithPolicy('https://example.test/bundle', verifiedDigest, bundlePolicy())
    ).resolves.toBeUndefined()
  })

  it('rejects a bundle signed for a different tag', async () => {
    netFetchMock.mockResolvedValue(response(await readFile(bundleFixture)))

    await expect(
      verifyBundleWithPolicy(
        'https://example.test/bundle',
        verifiedDigest,
        bundlePolicy('refs/tags/v0.19.10')
      )
    ).rejects.toThrow()
  })

  it('rejects a bundle from a different repository under the proxy policy', async () => {
    netFetchMock.mockResolvedValue(response(await readFile(bundleFixture)))

    await expect(
      verifyBundleWithPolicy('https://example.test/bundle', verifiedDigest, proxyPolicy('0.19.9'))
    ).rejects.toThrow()
  })

  it('rejects a bundle that does not cover the digest', async () => {
    netFetchMock.mockResolvedValue(response(await readFile(bundleFixture)))

    await expect(
      verifyBundleWithPolicy('https://example.test/bundle', '0'.repeat(64), bundlePolicy())
    ).rejects.toThrow(/subjects do not include/)
  })

  it('rejects a tampered bundle', async () => {
    const decoded = uncompress(await readFile(bundleFixture), 4 << 20)
    const index = decoded.length - 8
    const current = decoded[index]
    if (current === undefined) throw new Error('bundle fixture is too short')
    decoded[index] = current ^ 1
    netFetchMock.mockResolvedValue(response(compress(decoded)))

    await expect(
      verifyBundleWithPolicy('https://example.test/bundle', verifiedDigest, bundlePolicy())
    ).rejects.toThrow()
  })

  it('rejects a compressed body over the size limit', async () => {
    netFetchMock.mockResolvedValue(response(new Uint8Array((4 << 20) + 1)))

    await expect(
      verifyBundleWithPolicy('https://example.test/bundle', verifiedDigest, bundlePolicy())
    ).rejects.toThrow(/exceeds size limit/)
  })

  it('rejects a bundle that expands over the size limit', async () => {
    netFetchMock.mockResolvedValue(response(compress(new Uint8Array(16 << 20).fill(0x20))))

    await expect(
      verifyBundleWithPolicy('https://example.test/bundle', verifiedDigest, bundlePolicy())
    ).rejects.toThrow(/snappy/)
  })
})

describe('verifyUpdateAttestation', () => {
  let dir: string

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), 'tinfoil-attestation-'))
  })

  afterEach(async () => {
    await rm(dir, { recursive: true, force: true })
  })

  it('refuses a digest with no attestations', async () => {
    const file = join(dir, 'update.zip')
    await writeFile(file, 'not the released archive')
    netFetchMock.mockResolvedValue(
      new Response(JSON.stringify({ attestations: [] }), { status: 200 })
    )

    await expect(verifyUpdateAttestation(file, '0.2.7')).rejects.toThrow(/no attestations/)
  })
})
