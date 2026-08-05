import { promises as fs } from 'node:fs'
import { join } from 'node:path'
import { app } from 'electron'

import { PROXY_DEFAULT_PORT } from './constants.js'

export interface PersistedConfig {
  port: number
  proxyEnabled: boolean
  launchAtLogin: boolean
  allowedHosts: string[]
}

const FILE_NAME = 'config.json'
const MIN_PORT = 1
const MAX_PORT = 65535
const MAX_ALLOWED_HOSTS = 32
const MAX_HOSTNAME_LENGTH = 253
const HOSTNAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/i

function configPath(): string {
  return join(app.getPath('userData'), FILE_NAME)
}

function isValidPort(value: unknown): value is number {
  return (
    typeof value === 'number' &&
    Number.isInteger(value) &&
    value >= MIN_PORT &&
    value <= MAX_PORT
  )
}

// Accepts a hostname with an optional :port suffix, mirroring what the
// proxy binary's --allowed-host flag understands.
export function normalizeAllowedHost(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim().toLowerCase()
  if (trimmed.length === 0) return null
  const colonIndex = trimmed.lastIndexOf(':')
  let host = trimmed
  let port: number | null = null
  if (colonIndex !== -1) {
    const portText = trimmed.slice(colonIndex + 1)
    if (!/^[0-9]+$/.test(portText) || !isValidPort(Number(portText))) return null
    port = Number(portText)
    host = trimmed.slice(0, colonIndex)
  }
  if (host.length > MAX_HOSTNAME_LENGTH) return null
  if (!HOSTNAME_PATTERN.test(host)) return null
  return port === null ? host : `${host}:${port}`
}

export function sanitizeAllowedHosts(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  const hosts: string[] = []
  for (const entry of value.slice(0, MAX_ALLOWED_HOSTS)) {
    const normalized = normalizeAllowedHost(entry)
    if (normalized === null || hosts.includes(normalized)) continue
    hosts.push(normalized)
    if (hosts.length >= MAX_ALLOWED_HOSTS) break
  }
  return hosts
}

export async function loadConfig(): Promise<PersistedConfig> {
  try {
    const raw = await fs.readFile(configPath(), 'utf8')
    const parsed = JSON.parse(raw) as Partial<PersistedConfig> & { systemProxyEnabled?: boolean }
    const port = isValidPort(parsed.port) ? parsed.port : PROXY_DEFAULT_PORT
    const proxyEnabled =
      typeof parsed.proxyEnabled === 'boolean'
        ? parsed.proxyEnabled
        : typeof parsed.systemProxyEnabled === 'boolean'
          ? parsed.systemProxyEnabled
          : true
    const launchAtLogin = typeof parsed.launchAtLogin === 'boolean' ? parsed.launchAtLogin : false
    const allowedHosts = sanitizeAllowedHosts(parsed.allowedHosts)
    return { port, proxyEnabled, launchAtLogin, allowedHosts }
  } catch {
    return { port: PROXY_DEFAULT_PORT, proxyEnabled: true, launchAtLogin: false, allowedHosts: [] }
  }
}

export async function saveConfig(cfg: PersistedConfig): Promise<void> {
  const path = configPath()
  await fs.mkdir(join(path, '..'), { recursive: true })
  await fs.writeFile(path, JSON.stringify(cfg, null, 2), 'utf8')
}
