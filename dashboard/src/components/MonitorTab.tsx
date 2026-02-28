import { useCallback } from 'react'
import type { GatewayEvent } from '../types'
import { getStatsSummary } from '../api'
import { useApi } from '../hooks/useApi'
import PoolStats from './PoolStats'
import EventLog from './EventLog'

interface Props {
  events: GatewayEvent[]
  connected: boolean
  clearEvents: () => void
}

function formatUptime(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}h ${m}m ${s}s`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

export default function MonitorTab({ events, connected, clearEvents }: Props) {
  const fetcher = useCallback(() => getStatsSummary(), [])
  const { data: stats } = useApi(fetcher, 5000)

  return (
    <div>
      <div className="tab-header">
        <h2 className="tab-title">Monitor</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span className={`status-dot ${connected ? 'connected' : 'disconnected'}`} />
          <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            {connected ? 'Live' : 'Reconnecting...'}
          </span>
        </div>
      </div>

      {/* Summary stats */}
      <div className="grid grid-4" style={{ marginBottom: 16 }}>
        <div className="card">
          <div className="stat-value">{stats?.totalRequests ?? '-'}</div>
          <div className="stat-label">Requests (1h)</div>
        </div>
        <div className="card">
          <div className="stat-value" style={{ color: 'var(--accent-green-text)' }}>
            {stats?.payments ?? '-'}
          </div>
          <div className="stat-label">Payments</div>
        </div>
        <div className="card">
          <div className="stat-value" style={{ color: 'var(--accent-yellow)' }}>
            {stats?.challenges ?? '-'}
          </div>
          <div className="stat-label">Challenges</div>
        </div>
        <div className="card">
          <div className="stat-value" style={{ color: 'var(--accent-red-text)' }}>
            {stats?.errors ?? '-'}
          </div>
          <div className="stat-label">Errors</div>
        </div>
      </div>

      {/* Server info + Pool stats */}
      <div className="grid grid-2">
        <div>
          {stats?.feePool && <PoolStats label="Fee" stats={stats.feePool} />}
          {stats?.paymentPool && <PoolStats label="Payment" stats={stats.paymentPool} />}
        </div>
        <div>
          <div className="card">
            <div className="card-header">
              <span className="card-title">Server Info</span>
            </div>
            <div className="config-row">
              <span className="config-key">Uptime</span>
              <span className="config-value">{stats ? formatUptime(stats.uptimeSeconds) : '-'}</span>
            </div>
            <div className="config-row">
              <span className="config-key">Avg Latency</span>
              <span className="config-value">{stats ? `${stats.avgDurationMs.toFixed(1)}ms` : '-'}</span>
            </div>
            <div className="config-row">
              <span className="config-key">Total Fee Sats</span>
              <span className="config-value">{stats?.totalFeeSats ?? '-'}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Event log */}
      <EventLog events={events} onClear={clearEvents} />
    </div>
  )
}
