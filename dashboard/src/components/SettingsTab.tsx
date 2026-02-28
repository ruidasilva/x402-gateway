import { useState, useCallback } from 'react'
import { getConfig, updateConfig } from '../api'
import { useApi } from '../hooks/useApi'

export default function SettingsTab() {
  const fetcher = useCallback(() => getConfig(), [])
  const { data: config, refresh } = useApi(fetcher)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  // Editable fields
  const [feeRate, setFeeRate] = useState<string>('')
  const [threshold, setThreshold] = useState<string>('')
  const [optimalSize, setOptimalSize] = useState<string>('')

  // Initialize editable fields when config loads
  if (config && !feeRate) {
    setFeeRate(String(config.feeRate))
    setThreshold(String(config.poolReplenishThreshold))
    setOptimalSize(String(config.poolOptimalSize))
  }

  async function handleSave() {
    setSaving(true)
    setMessage(null)
    try {
      const updates: Record<string, unknown> = {}
      if (config) {
        const newRate = parseFloat(feeRate)
        if (!isNaN(newRate) && newRate !== config.feeRate) updates.feeRate = newRate
        const newThreshold = parseInt(threshold)
        if (!isNaN(newThreshold) && newThreshold !== config.poolReplenishThreshold) updates.poolReplenishThreshold = newThreshold
        const newOptimal = parseInt(optimalSize)
        if (!isNaN(newOptimal) && newOptimal !== config.poolOptimalSize) updates.poolOptimalSize = newOptimal
      }
      if (Object.keys(updates).length === 0) {
        setMessage({ type: 'error', text: 'No changes to save' })
        return
      }
      await updateConfig(updates)
      setMessage({ type: 'success', text: 'Configuration updated successfully' })
      refresh()
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : String(err) })
    } finally {
      setSaving(false)
    }
  }

  function copyToClipboard(text: string) {
    navigator.clipboard.writeText(text)
  }

  if (!config) return <div className="spinner" />

  return (
    <div>
      <div className="tab-header">
        <h2 className="tab-title">Settings</h2>
      </div>

      {message && (
        <div className={`alert alert-${message.type}`}>{message.text}</div>
      )}

      {/* Read-only config */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Server Configuration</span>
          <span className="card-subtitle">Requires restart to change</span>
        </div>
        <div className="config-row">
          <span className="config-key">Network</span>
          <span className="config-value">{config.network}</span>
        </div>
        <div className="config-row">
          <span className="config-key">Broadcaster</span>
          <span className="config-value">{config.broadcaster}</span>
        </div>
        <div className="config-row">
          <span className="config-key">Port</span>
          <span className="config-value">{config.port}</span>
        </div>
        <div className="config-row">
          <span className="config-key">Redis</span>
          <span className="config-value">{config.redisEnabled ? 'Enabled' : 'Disabled (in-memory)'}</span>
        </div>
        <div className="config-row">
          <span className="config-key">Key Mode</span>
          <span className="config-value">{config.keyMode === 'xpriv' ? 'HD Wallet (xPriv)' : 'Single Key (WIF)'}</span>
        </div>
        <div className="config-row">
          <span className="config-key">Lease TTL</span>
          <span className="config-value">{config.leaseTTLSeconds}s</span>
        </div>
      </div>

      {/* Derived addresses */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Derived Addresses</span>
        </div>
        {[
          { label: 'Payee', addr: config.payeeAddress },
          { label: 'Payment Pool', addr: config.paymentAddress },
          { label: 'Fee Pool', addr: config.feeAddress },
          { label: 'Treasury', addr: config.treasuryAddress },
        ].map(({ label, addr }) => (
          <div key={label} className="config-row">
            <span className="config-key">{label}</span>
            <span className="copy-text" onClick={() => copyToClipboard(addr)} title="Click to copy">
              <span className="addr">{addr}</span>
            </span>
          </div>
        ))}
      </div>

      {/* Editable config */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Runtime Configuration</span>
          <span className="card-subtitle">Applied immediately without restart</span>
        </div>
        <div className="form-group">
          <label className="form-label">Fee Rate (sat/byte)</label>
          <input
            className="form-input"
            type="number"
            step="0.001"
            value={feeRate}
            onChange={(e) => setFeeRate(e.target.value)}
          />
        </div>
        <div className="form-group">
          <label className="form-label">Pool Replenish Threshold</label>
          <input
            className="form-input"
            type="number"
            value={threshold}
            onChange={(e) => setThreshold(e.target.value)}
          />
        </div>
        <div className="form-group">
          <label className="form-label">Pool Optimal Size</label>
          <input
            className="form-input"
            type="number"
            value={optimalSize}
            onChange={(e) => setOptimalSize(e.target.value)}
          />
        </div>
        <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
          {saving ? <span className="spinner" /> : null}
          Save Changes
        </button>
      </div>
    </div>
  )
}
