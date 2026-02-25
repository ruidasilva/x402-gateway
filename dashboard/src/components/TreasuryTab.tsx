import { useState, useCallback } from 'react'
import { getTreasuryInfo, triggerFanout, getFanoutHistory } from '../api'
import { useApi } from '../hooks/useApi'
import PoolStats from './PoolStats'

export default function TreasuryTab() {
  const infoFetcher = useCallback(() => getTreasuryInfo(), [])
  const historyFetcher = useCallback(() => getFanoutHistory(), [])
  const { data: info, refresh: refreshInfo } = useApi(infoFetcher, 10000)
  const { data: historyData, refresh: refreshHistory } = useApi(historyFetcher, 10000)

  const [pool, setPool] = useState('nonce')
  const [count, setCount] = useState('100')
  const [fundingTxid, setFundingTxid] = useState('')
  const [fundingVout, setFundingVout] = useState('0')
  const [fundingScript, setFundingScript] = useState('')
  const [fundingSatoshis, setFundingSatoshis] = useState('')
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  async function handleFanout() {
    setRunning(true)
    setResult(null)
    try {
      const res = await triggerFanout({
        pool,
        count: parseInt(count),
        fundingTxid,
        fundingVout: parseInt(fundingVout),
        fundingScript,
        fundingSatoshis: parseInt(fundingSatoshis),
      })
      setResult({ type: 'success', text: `Fan-out complete: ${res.utxoCount} UTXOs created (txid: ${res.txid.slice(0, 16)}...)` })
      refreshInfo()
      refreshHistory()
    } catch (err) {
      setResult({ type: 'error', text: err instanceof Error ? err.message : String(err) })
    } finally {
      setRunning(false)
    }
  }

  function copyToClipboard(text: string) {
    navigator.clipboard.writeText(text)
  }

  if (!info) return <div className="spinner" />

  return (
    <div>
      <div className="tab-header">
        <h2 className="tab-title">Treasury</h2>
      </div>

      {/* Treasury address */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Treasury Address</span>
          <span className="card-subtitle">{info.network} | {info.keyMode === 'xpriv' ? info.derivationPath : 'single key'}</span>
        </div>
        <div style={{ marginBottom: 12 }}>
          <span
            className="copy-text"
            onClick={() => copyToClipboard(info.address)}
            title="Click to copy"
            style={{ fontSize: 15 }}
          >
            <span className="addr">{info.address}</span>
          </span>
        </div>
        <div className="alert alert-info">
          Fund this address to create new UTXOs via fan-out. Each fan-out splits one funding UTXO into many 1-sat UTXOs for pool replenishment.
        </div>
      </div>

      {/* Pool status */}
      <div className="grid grid-2">
        {info.noncePool && <PoolStats label="Nonce" stats={info.noncePool} />}
        {info.feePool && <PoolStats label="Fee" stats={info.feePool} />}
      </div>

      {/* Fan-out form */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Create Fan-Out</span>
          <span className="card-subtitle">Split a funding UTXO into pool UTXOs</span>
        </div>

        {result && <div className={`alert alert-${result.type}`}>{result.text}</div>}

        <div className="grid grid-2">
          <div className="form-group">
            <label className="form-label">Target Pool</label>
            <select className="form-input" value={pool} onChange={(e) => setPool(e.target.value)}>
              <option value="nonce">Nonce Pool</option>
              <option value="fee">Fee Pool</option>
            </select>
          </div>
          <div className="form-group">
            <label className="form-label">Output Count</label>
            <input className="form-input" type="number" value={count} onChange={(e) => setCount(e.target.value)} />
          </div>
        </div>

        <div className="form-group">
          <label className="form-label">Funding TXID</label>
          <input className="form-input" placeholder="64-char hex txid" value={fundingTxid} onChange={(e) => setFundingTxid(e.target.value)} />
        </div>
        <div className="grid grid-2">
          <div className="form-group">
            <label className="form-label">Funding Vout</label>
            <input className="form-input" type="number" value={fundingVout} onChange={(e) => setFundingVout(e.target.value)} />
          </div>
          <div className="form-group">
            <label className="form-label">Funding Satoshis</label>
            <input className="form-input" type="number" value={fundingSatoshis} onChange={(e) => setFundingSatoshis(e.target.value)} />
          </div>
        </div>
        <div className="form-group">
          <label className="form-label">Funding Locking Script (hex)</label>
          <input className="form-input" placeholder="76a914...88ac" value={fundingScript} onChange={(e) => setFundingScript(e.target.value)} />
        </div>

        <button className="btn btn-primary" onClick={handleFanout} disabled={running || !fundingTxid || !fundingScript || !fundingSatoshis}>
          {running ? <span className="spinner" /> : null}
          Execute Fan-Out
        </button>
      </div>

      {/* History */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Fan-Out History</span>
        </div>
        {historyData?.history && historyData.history.length > 0 ? (
          <table className="table">
            <thead>
              <tr>
                <th>TxID</th>
                <th>Pool</th>
                <th>Count</th>
                <th>Time</th>
              </tr>
            </thead>
            <tbody>
              {historyData.history.map((entry, i) => (
                <tr key={i}>
                  <td style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>{entry.txid.slice(0, 16)}...</td>
                  <td>{entry.pool}</td>
                  <td>{entry.count}</td>
                  <td>{new Date(entry.timestamp).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div style={{ padding: 16, color: 'var(--text-muted)', textAlign: 'center', fontSize: 13 }}>
            No fan-out transactions yet
          </div>
        )}
      </div>
    </div>
  )
}
