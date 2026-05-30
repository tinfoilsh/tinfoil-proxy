import { useCallback, useEffect, useRef, useState } from 'react'
import { PiArrowsClockwise, PiSpinner } from 'react-icons/pi'

const REFRESH_MIN_SPIN_MS = 1000

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

function fleetDot(status: TrayState['status']): string {
  switch (status) {
    case 'verified':
      return 'router-dot router-verified'
    case 'failed':
      return 'router-dot router-failed'
    default:
      return 'router-dot'
  }
}

type LockState = 'verified' | 'failed' | 'off' | 'initializing'

function LockBadge({ state }: { state: LockState }) {
  const closed = state === 'verified'
  return (
    <span className={`lock lock-${state}`} aria-hidden="true">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
        {closed ? (
          <path d="M8 10V7a4 4 0 0 1 8 0v3" />
        ) : (
          <path d="M8 10V7a4 4 0 0 1 7.46-2" />
        )}
        <rect x="5" y="10" width="14" height="10" rx="2.2" />
        <circle cx="12" cy="15" r="1.4" fill="currentColor" stroke="none" />
      </svg>
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

  const [copied, setCopied] = useState(false)
  const onCopyEndpoint = useCallback(async () => {
    const value = await window.tinfoil.copyEndpoint()
    if (!value) return
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }, [])

  const [refreshing, setRefreshing] = useState(false)
  const onRefreshRouters = useCallback(async () => {
    if (refreshing) return
    setRefreshing(true)
    const minDuration = new Promise((resolve) => setTimeout(resolve, REFRESH_MIN_SPIN_MS))
    try {
      const [updated] = await Promise.all([window.tinfoil.refreshRouters(), minDuration])
      setState(updated)
    } finally {
      setRefreshing(false)
    }
  }, [refreshing])

  if (!state) {
    return (
      <div className={`shell compact ${isDark ? 'dark' : 'light'}`}>
        <div className="card">
          <div className="status-row">
            <LockBadge state="initializing" />
            <span className="status-text">Loading…</span>
          </div>
        </div>
      </div>
    )
  }

  const enabled = state.proxy.enabled
  const running = state.proxy.running
  const verifying = state.proxy.verifying
  const verified = state.proxy.verified
  const active = enabled && running && verified

  const statusTitle = !enabled
    ? 'Tinfoil Proxy is off'
    : state.proxy.lastError
      ? "Couldn't confirm your connection is private"
      : verifying
        ? 'Verifying Tinfoil enclave…'
        : verified && running
          ? "You're protected by Tinfoil"
          : 'Starting Tinfoil Proxy…'

  const statusSub = !enabled
    ? 'Turn this on to start Tinfoil Proxy and route API requests through attested enclaves.'
    : state.proxy.lastError
      ? state.proxy.lastError
      : verifying
        ? state.proxy.enclave
          ? `Independently verifying ${state.proxy.enclave} before routing traffic.`
          : 'Waiting for the proxy to report its upstream enclave.'
        : verified && running
          ? 'Every API request is routed through an attested enclave whose code and hardware are verified end-to-end.'
          : 'Tinfoil Proxy is starting on the configured port.'

  const lockState: LockState = !enabled
    ? 'off'
    : state.proxy.lastError
      ? 'failed'
      : verifying
        ? 'initializing'
        : verified && running
          ? 'verified'
          : 'initializing'

  return (
    <div className={`shell compact ${active ? 'active' : 'inactive'} ${isDark ? 'dark' : 'light'}`}>
      <div className="card" ref={cardRef}>
        <div className="status-row">
          <LockBadge state={lockState} />
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

        <div className="tabs">
          {state.routers.length === 0 ? (
            <div className="tabs-empty">
              {refreshing ? 'Refreshing routers…' : 'No routers reachable'}
            </div>
          ) : (
            <div className="routers-summary">
              <span className={fleetDot(state.status)} />
              <span className="routers-text">{state.statusMessage}</span>
            </div>
          )}
          <button
            type="button"
            className="refresh"
            onClick={() => {
              void onRefreshRouters()
            }}
            disabled={refreshing}
            title="Refresh router list from atc.tinfoil.sh"
            aria-label="Refresh routers"
          >
            {refreshing ? (
              <PiSpinner className="refresh-spin" size={14} aria-hidden="true" />
            ) : (
              <PiArrowsClockwise size={14} aria-hidden="true" />
            )}
          </button>
        </div>
      </div>
    </div>
  )
}
