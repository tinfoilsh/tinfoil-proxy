import { useCallback, useEffect, useRef, useState } from 'react'
import { PiArrowsClockwise, PiSpinner } from 'react-icons/pi'

const REFRESH_MIN_SPIN_MS = 1000
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

function formatTokenCount(count: number): string {
  return TOKEN_COUNT_FORMATTER.format(count)
}

function formatFullTokenCount(count: number): string {
  return `${FULL_TOKEN_COUNT_FORMATTER.format(count)} tokens`
}

type LockState = 'verified' | 'failed' | 'off' | 'initializing'

function LockBadge({ state }: { state: LockState }) {
  const closed = state === 'verified'
  return (
    <span className={`lock lock-${state}`} aria-hidden="true">
      {closed ? (
        <svg width="16" height="16" viewBox="0 0 108 114" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path d="M92.0832 43.0673H15.0127C10.0627 43.0673 6.04999 47.08 6.04999 52.03V98.6138C6.04999 103.564 10.0627 107.577 15.0127 107.577H92.0832C97.0332 107.577 101.046 103.564 101.046 98.6138V52.03C101.046 47.08 97.0332 43.0673 92.0832 43.0673Z" stroke="currentColor" strokeWidth="12.0997" strokeMiterlimit="10" />
          <path d="M57.8054 86.7713H49.2908C48.7979 86.7713 48.3945 86.3949 48.3945 85.9288V64.7096C48.3945 64.2435 48.7979 63.8671 49.2908 63.8671H57.8054C58.2984 63.8671 58.7017 64.2435 58.7017 64.7096V85.9288C58.7017 86.3949 58.2984 86.7713 57.8054 86.7713Z" fill="currentColor" />
          <path d="M34.8697 2.05789C33.8972 7.09046 29.9626 11.0251 24.93 11.9976L34.8697 21.9372C35.8421 16.9047 39.7768 12.97 44.8093 11.9976L34.8697 2.05789Z" fill="currentColor" />
          <path d="M72.1412 2.05789C73.1137 7.09046 77.0483 11.0251 82.0809 11.9976L72.1412 21.9372C71.1687 16.9047 67.2341 12.97 62.2015 11.9976L72.1412 2.05789Z" fill="currentColor" />
          <path d="M69.4522 0H37.5449C36.0599 0 34.8561 1.20383 34.8561 2.68882V9.30779C34.8561 10.7928 36.0599 11.9966 37.5449 11.9966H69.4522C70.9372 11.9966 72.141 10.7928 72.141 9.30779V2.68882C72.141 1.20383 70.9372 0 69.4522 0Z" fill="currentColor" />
          <path d="M32.1674 11.9433H25.4453C23.9604 11.9433 22.7565 13.1472 22.7565 14.6321V40.3776C22.7565 41.8626 23.9604 43.0664 25.4453 43.0664H32.1674C33.6524 43.0664 34.8562 41.8626 34.8562 40.3776V14.6321C34.8562 13.1472 33.6524 11.9433 32.1674 11.9433Z" fill="currentColor" />
          <path d="M81.552 11.9402H74.8299C73.3449 11.9402 72.1411 13.144 72.1411 14.629V40.3744C72.1411 41.8594 73.3449 43.0632 74.8299 43.0632H81.552C83.037 43.0632 84.2408 41.8594 84.2408 40.3744V14.629C84.2408 13.144 83.037 11.9402 81.552 11.9402Z" fill="currentColor" />
        </svg>
      ) : (
        <svg width="16" height="16" viewBox="0 0 134 113" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path d="M84.0371 85.883H75.5579C75.067 85.883 74.6654 85.5082 74.6654 85.0441V63.9132C74.6654 63.4491 75.067 63.0742 75.5579 63.0742H84.0371C84.528 63.0742 84.9296 63.4491 84.9296 63.9132V85.0441C84.9296 85.5082 84.528 85.883 84.0371 85.883Z" fill="currentColor" />
          <path d="M49.1653 2.04633C50.1337 7.05793 54.052 10.9762 59.0636 11.9446L49.1653 21.8428C48.1969 16.8312 44.2787 12.913 39.2671 11.9446L49.1653 2.04633Z" fill="currentColor" />
          <path d="M12.0491 2.04633C11.0807 7.05793 7.16248 10.9762 2.15088 11.9446L12.0491 21.8428C13.0175 16.8312 16.9358 12.913 21.9474 11.9446L12.0491 2.04633Z" fill="currentColor" />
          <path d="M14.727 11.9473L46.5013 11.9473C47.9801 11.9473 49.179 10.7485 49.179 9.26968V2.67828C49.179 1.19947 47.9801 0.000664711 46.5013 0.000664711L14.727 0.000664711C13.2482 0.000664711 12.0493 1.19947 12.0493 2.67828V9.26968C12.0493 10.7485 13.2482 11.9473 14.727 11.9473Z" fill="currentColor" />
          <path d="M51.8517 42.8824H58.5458C60.0246 42.8824 61.2234 41.6835 61.2234 40.2047V14.5666C61.2234 13.0878 60.0246 11.8889 58.5458 11.8889H51.8517C50.3729 11.8889 49.1741 13.0878 49.1741 14.5666V40.2047C49.1741 41.6835 50.3729 42.8824 51.8517 42.8824Z" fill="currentColor" />
          <path d="M2.67754 30.6169H9.37158C10.8504 30.6169 12.0492 29.4181 12.0492 27.9393L12.0492 14.5646C12.0492 13.0858 10.8504 11.887 9.37158 11.887H2.67754C1.19873 11.887 -7.9155e-05 13.0858 -7.9155e-05 14.5646L-7.9155e-05 27.9393C-7.9155e-05 29.4181 1.19873 30.6169 2.67754 30.6169Z" fill="currentColor" />
          <path d="M118.266 42.3614H41.213C36.2836 42.3614 32.2876 46.3575 32.2876 51.2868V97.6765C32.2876 102.606 36.2836 106.602 41.213 106.602H118.266C123.195 106.602 127.191 102.606 127.191 97.6765V51.2868C127.191 46.3575 123.195 42.3614 118.266 42.3614Z" stroke="currentColor" strokeWidth="12.0493" strokeMiterlimit="10" />
        </svg>
      )}
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
      ? "Couldn't verify the Tinfoil enclave"
      : verifying
        ? 'Verifying Tinfoil enclave…'
        : verified && running
          ? "You're connected to Tinfoil"
          : 'Starting Tinfoil Proxy…'

  const statusSub = !enabled
    ? 'Turn this on to start Tinfoil Proxy and verify the enclave before any requests are sent.'
    : state.proxy.lastError
      ? state.proxy.lastError
      : verifying
        ? state.proxy.enclave
          ? `Independently verifying ${state.proxy.enclave} before any requests are sent.`
          : 'Waiting for the proxy to report its upstream enclave.'
        : verified && running
          ? 'Requests go directly to a Tinfoil enclave whose code and hardware have been attested, over a connection pinned to its verified key.'
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
