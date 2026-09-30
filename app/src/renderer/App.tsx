import { useCallback, useEffect, useRef, useState } from 'react'
import { PiCaretRight, PiCircleFill, PiX } from 'react-icons/pi'

const TOKEN_COUNT_FORMATTER = new Intl.NumberFormat(undefined, {
  notation: 'compact',
  maximumFractionDigits: 1
})
const FULL_TOKEN_COUNT_FORMATTER = new Intl.NumberFormat()

type TrayState = Awaited<ReturnType<typeof window.tinfoil.getState>>

function useDarkMode(): boolean {
  const [dark, setDark] = useState(() =>
    window.matchMedia('(prefers-color-scheme: dark)').matches
  )
  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (event: MediaQueryListEvent) => setDark(event.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])
  return dark
}

function formatTokenCount(count: number): string {
  return TOKEN_COUNT_FORMATTER.format(count)
}

function formatFullTokenCount(count: number): string {
  return `${FULL_TOKEN_COUNT_FORMATTER.format(count)} tokens`
}

const HOSTNAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/i
const MAX_HOSTNAME_LENGTH = 253
const MAX_ALLOWED_HOSTS = 32

// Mirrors the main-process validation in config.ts so invalid entries are
// caught before the IPC round trip.
function normalizeAllowedHost(value: string): string | null {
  const trimmed = value.trim().toLowerCase()
  if (trimmed.length === 0) return null
  const colonIndex = trimmed.lastIndexOf(':')
  let host = trimmed
  let port: number | null = null
  if (colonIndex !== -1) {
    const portText = trimmed.slice(colonIndex + 1)
    if (!/^[0-9]+$/.test(portText)) return null
    port = Number(portText)
    if (!Number.isInteger(port) || port < 1 || port > 65535) return null
    host = trimmed.slice(0, colonIndex)
  }
  if (host.length > MAX_HOSTNAME_LENGTH) return null
  if (!HOSTNAME_PATTERN.test(host)) return null
  return port === null ? host : `${host}:${port}`
}

type ProxyStatus = 'ready' | 'failed' | 'off' | 'initializing'

function StatusBadge({ state }: { state: ProxyStatus }) {
  return (
    <span className={`status-badge status-badge-${state}`} aria-hidden="true">
      <PiCircleFill size={12} />
    </span>
  )
}

export default function App() {
  const [state, setState] = useState<TrayState | null>(null)
  const [busy, setBusy] = useState(false)
  const [portInput, setPortInput] = useState<string>('')
  const cardRef = useRef<HTMLDivElement | null>(null)
  const portInputRef = useRef<HTMLInputElement | null>(null)
  const isDark = useDarkMode()

  useEffect(() => {
    const node = cardRef.current
    if (!node) return
    const report = () => {
      void window.tinfoil.setCompactHeight(Math.ceil(node.getBoundingClientRect().height))
    }
    report()
    const ro = new ResizeObserver(() => report())
    ro.observe(node)
    return () => ro.disconnect()
  }, [state])

  useEffect(() => {
    void window.tinfoil.getState().then(setState)
    return window.tinfoil.onStateChanged(setState)
  }, [])

  useEffect(() => {
    if (!state) return
    if (portInputRef.current && portInputRef.current === document.activeElement) return
    setPortInput(String(state.proxy.port))
  }, [state?.proxy.port])

  const onToggleActive = useCallback(async () => {
    if (!state) return
    setBusy(true)
    try {
      const next = !state.proxy.enabled
      const updated = await window.tinfoil.setProxyEnabled(next)
      setState(updated)
    } finally {
      setBusy(false)
    }
  }, [state])

  const onCommitPort = useCallback(async () => {
    if (!state) return
    const next = Number(portInput)
    if (!Number.isFinite(next) || !Number.isInteger(next) || next < 1 || next > 65535) {
      setPortInput(String(state.proxy.port))
      return
    }
    if (next === state.proxy.port) return
    const updated = await window.tinfoil.setProxyPort(next)
    setState(updated)
  }, [portInput, state])

  const onToggleLaunchAtLogin = useCallback(async () => {
    if (!state) return
    const updated = await window.tinfoil.setLaunchAtLogin(!state.launchAtLogin)
    setState(updated)
  }, [state])

  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [hostInput, setHostInput] = useState('')
  const [hostError, setHostError] = useState<string | null>(null)
  const [hostsBusy, setHostsBusy] = useState(false)

  const onAddAllowedHost = useCallback(async () => {
    if (!state || hostsBusy) return
    const normalized = normalizeAllowedHost(hostInput)
    if (normalized === null) {
      if (hostInput.trim().length > 0) {
        setHostError('Enter a valid hostname, optionally with a port.')
      }
      return
    }
    if (state.proxy.allowedHosts.length >= MAX_ALLOWED_HOSTS) {
      setHostError(`At most ${MAX_ALLOWED_HOSTS} allowed hosts are supported.`)
      return
    }
    setHostError(null)
    if (state.proxy.allowedHosts.includes(normalized)) {
      setHostInput('')
      return
    }
    setHostsBusy(true)
    try {
      const updated = await window.tinfoil.setAllowedHosts([
        ...state.proxy.allowedHosts,
        normalized
      ])
      setState(updated)
      // The save can take a while (it restarts the proxy), so only clear the
      // field if the user hasn't started typing the next entry meanwhile.
      setHostInput((current) => (current === hostInput ? '' : current))
    } catch {
      setHostError('Could not save the allowed host. Try again.')
    } finally {
      setHostsBusy(false)
    }
  }, [hostInput, hostsBusy, state])

  const onRemoveAllowedHost = useCallback(
    async (host: string) => {
      if (!state || hostsBusy) return
      setHostError(null)
      setHostsBusy(true)
      try {
        const updated = await window.tinfoil.setAllowedHosts(
          state.proxy.allowedHosts.filter((h) => h !== host)
        )
        setState(updated)
      } catch {
        setHostError(`Could not remove ${host}. Try again.`)
      } finally {
        setHostsBusy(false)
      }
    },
    [hostsBusy, state]
  )

  const [verificationError, setVerificationError] = useState('')
  const onViewVerifications = async () => {
    const opened = await window.tinfoil.openVerifications()
    setVerificationError(opened ? '' : 'Could not open verification data.')
  }

  const [copied, setCopied] = useState(false)
  const onCopyEndpoint = useCallback(async () => {
    const value = await window.tinfoil.copyEndpoint()
    if (!value) return
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }, [])

  if (!state) {
    return (
      <div className={`shell compact ${isDark ? 'dark' : 'light'}`}>
        <div className="card">
          <div className="status-row">
            <StatusBadge state="initializing" />
            <span className="status-text">Loading…</span>
          </div>
        </div>
      </div>
    )
  }

  const enabled = state.proxy.enabled
  const running = state.proxy.running
  const active = enabled && running

  const statusTitle = !enabled
    ? 'Tinfoil Proxy is off'
    : state.proxy.lastError
      ? "Couldn't start Tinfoil Proxy"
      : running ? 'Proxy ready' : 'Starting Tinfoil Proxy…'

  const statusSub = !enabled
    ? 'Turn this on to start Tinfoil Proxy.'
    : state.proxy.lastError
      ? state.proxy.lastError
      : running
        ? 'Enclaves are verified before inference requests are sent.'
        : 'Tinfoil Proxy is starting on the configured port.'

  const proxyStatus: ProxyStatus = !enabled
    ? 'off'
    : state.proxy.lastError
      ? 'failed'
      : running ? 'ready' : 'initializing'

  return (
    <div className={`shell compact ${active ? 'active' : 'inactive'} ${isDark ? 'dark' : 'light'}`}>
      <div className="card" ref={cardRef}>
        <div className="status-row">
          <StatusBadge state={proxyStatus} />
          <div className="status-text">
            <div className="status-title">{statusTitle}</div>
            <div className="status-sub">{statusSub}</div>
          </div>
          <button
            type="button"
            className={`toggle ${enabled ? 'on' : 'off'}`}
            onClick={onToggleActive}
            disabled={busy}
            aria-pressed={enabled}
            title={enabled ? 'Stop proxy' : 'Start proxy'}
          >
            <span className="knob" />
          </button>
        </div>

        <div className="port-row">
          <label className="port-label" htmlFor="proxy-port">
            Port
          </label>
          <input
            id="proxy-port"
            ref={portInputRef}
            className="port-input"
            type="number"
            min={1}
            max={65535}
            value={portInput}
            onChange={(e) => setPortInput(e.target.value)}
            onBlur={() => {
              void onCommitPort()
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.currentTarget.blur()
              }
            }}
          />
          {state.launchAtLoginSupported && (
            <label className="launch-row" title="Start Tinfoil Proxy when you log in">
              <input
                type="checkbox"
                checked={state.launchAtLogin}
                onChange={() => {
                  void onToggleLaunchAtLogin()
                }}
              />
              <span>Open at Login</span>
            </label>
          )}
        </div>

        {state.endpoint && (
          <button
            type="button"
            className={`endpoint ${copied ? 'endpoint-copied' : ''}`}
            onClick={() => {
              void onCopyEndpoint()
            }}
            title="Copy endpoint URL"
          >
            <span className="endpoint-host">{state.endpoint}</span>
            <span className="endpoint-action">{copied ? 'Copied' : 'Copy'}</span>
          </button>
        )}

        {enabled && (
          <div className="token-row" aria-label="Live proxy token usage">
            <div className="token-stat" title={formatFullTokenCount(state.proxy.upstreamedTokens)}>
              <span className="token-label">Input tokens</span>
              <span className="token-value">{formatTokenCount(state.proxy.upstreamedTokens)}</span>
            </div>
            <div className="token-stat" title={formatFullTokenCount(state.proxy.downstreamedTokens)}>
              <span className="token-label">Output tokens</span>
              <span className="token-value">{formatTokenCount(state.proxy.downstreamedTokens)}</span>
            </div>
          </div>
        )}

        {active && (
          <div className="verification-row">
            <button type="button" className="advanced-toggle" onClick={() => { void onViewVerifications() }}>
              View verification data
            </button>
            {verificationError && <p className="verification-error">{verificationError}</p>}
            {state.proxy.attestationError && (
              <p className="verification-error">Last blocked request: {state.proxy.attestationError}</p>
            )}
          </div>
        )}

        <div className="advanced">
          <button
            type="button"
            className="advanced-toggle"
            onClick={() => setAdvancedOpen((open) => !open)}
            aria-expanded={advancedOpen}
          >
            <span>Advanced settings</span>
            <PiCaretRight
              size={12}
              className={`advanced-caret ${advancedOpen ? 'advanced-caret-open' : ''}`}
              aria-hidden="true"
            />
          </button>
          {advancedOpen && (
            <div className="advanced-body">
              <div className="allowed-hosts-header">
                <span className="allowed-hosts-label">Allowed hosts</span>
                <span className="allowed-hosts-hint">
                  If you reach this proxy through another address, such as a reverse proxy in
                  front of it, add that hostname here so its requests are accepted.
                </span>
              </div>
              {state.proxy.allowedHosts.length > 0 && (
                <ul className="allowed-hosts-list">
                  {state.proxy.allowedHosts.map((host) => (
                    <li key={host} className="allowed-host-item">
                      <span className="allowed-host-name">{host}</span>
                      <button
                        type="button"
                        className="allowed-host-remove"
                        onClick={() => {
                          void onRemoveAllowedHost(host)
                        }}
                        disabled={hostsBusy}
                        title={`Remove ${host}`}
                        aria-label={`Remove ${host}`}
                      >
                        <PiX size={11} aria-hidden="true" />
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              <div className="allowed-host-add">
                <input
                  className={`allowed-host-input ${hostError ? 'allowed-host-input-error' : ''}`}
                  type="text"
                  placeholder="hostname or hostname:port"
                  value={hostInput}
                  onChange={(e) => {
                    setHostInput(e.target.value)
                    setHostError(null)
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      void onAddAllowedHost()
                    }
                  }}
                  spellCheck={false}
                  autoCapitalize="off"
                  autoCorrect="off"
                />
                <button
                  type="button"
                  className="allowed-host-add-button"
                  onClick={() => {
                    void onAddAllowedHost()
                  }}
                  disabled={hostsBusy || hostInput.trim().length === 0}
                >
                  Add
                </button>
              </div>
              {hostError && <div className="allowed-host-error">{hostError}</div>}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
