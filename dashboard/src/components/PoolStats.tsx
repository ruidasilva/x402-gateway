import type { PoolStats as PoolStatsType } from '../types'

interface Props {
  label: string
  stats: PoolStatsType
}

export default function PoolStats({ label, stats }: Props) {
  const total = stats.total || 1 // avoid div by zero
  const availPct = (stats.available / total) * 100
  const leasedPct = (stats.leased / total) * 100
  const spentPct = (stats.spent / total) * 100

  return (
    <div className="card">
      <div className="card-header">
        <span className="card-title">{label} Pool</span>
        <span className="card-subtitle">{stats.available} / {stats.total} available</span>
      </div>
      <div className="pool-bar-container">
        <div className="pool-bar">
          <div className="pool-bar-segment available" style={{ width: `${availPct}%` }} />
          <div className="pool-bar-segment leased" style={{ width: `${leasedPct}%` }} />
          <div className="pool-bar-segment spent" style={{ width: `${spentPct}%` }} />
        </div>
        <div className="pool-bar-legend">
          <span><span className="legend-dot" style={{ background: 'var(--accent-green-text)' }} /> Available: {stats.available}</span>
          <span><span className="legend-dot" style={{ background: 'var(--accent-yellow)' }} /> Leased: {stats.leased}</span>
          <span><span className="legend-dot" style={{ background: 'var(--accent-red-text)' }} /> Spent: {stats.spent}</span>
        </div>
      </div>
    </div>
  )
}
