import { useState, useCallback } from 'react'
import { getTreasuryInfo, triggerFanout, getFanoutHistory, getTreasuryUTXOs } from '../api'
import { useApi } from '../hooks/useApi'
import type { TreasuryUTXO } from '../types'
import PoolStats from './PoolStats'

export default function TreasuryTab() {
  const infoFetcher = useCallback(() => getTreasuryInfo(), [])
  const historyFetcher = useCallback(() => getFanoutHistory(), [])
  const utxoFetcher = useCallback(() => getTreasuryUTXOs(), [])
  const { data: info, refresh: refreshInfo } = useApi(infoFetcher, 10000)
  const { data: historyData, refresh: refreshHistory } = useApi(historyFetcher, 10000)
  const { data: utxoData, refresh: refreshUTXOs } = useApi(utxoFetcher, 10000)

  const [pool, setPool] = useState('fee')
  const [count, setCount] = useState('100')
  const [fundingTxid, setFundingTxid] = useState('')
  const [fundingVout, setFundingVout] = useState('0')
  const [fundingScript, setFundingScript] = useState('')
  const [fundingSatoshis, setFundingSatoshis] = useState('')
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [inputMode, setInputMode] = useState<'select' | 'manual'>('select')

  function selectUTXO(utxo: TreasuryUTXO) {
    setFundingTxid(utxo.txid)
    setFundingVout(String(utxo.vout))
    setFundingScript(utxo.script)
    setFundingSatoshis(String(utxo.satoshis))
  }

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
      refreshUTXOs()
      // Clear selection after successful fan-out
      setFundingTxid('')
      setFundingVout('0')
      setFundingScript('')
      setFundingSatoshis('')
    } catch (err) {
      setResult({ type: 'error', text: err instanceof Error ? err.message : String(err) })
    } finally {
      setRunning(false)
    }
  }

  function copyToClipboard(text: string) {
    navigator.clipboard.writeText(text)
  }

  function formatSats(sats: number): string {
    if (sats >= 100_000_000) return `${(sats / 100_000_000).toFixed(8)} BSV`
    if (sats >= 100_000) return `${(sats / 100_000).toFixed(0)}k sats`
    return `${sats.toLocaleString()} sats`
  }

  function wocBase(): string {
    return info?.network === 'testnet'
      ? 'https://test.whatsonchain.com'
      : 'https://whatsonchain.com'
  }

  function txUrl(txid: string): string {
    return `${wocBase()}/tx/${txid}`
  }

  function addressUrl(addr: string): string {
    return `${wocBase()}/address/${addr}`
  }

  if (!info) return <div className="spinner" />

  const demoMode = info.broadcaster === 'mock'
  const utxos = utxoData?.utxos ?? []
  const hasUTXOs = utxos.length > 0

  return (
    <div>
      <div className="tab-header">
        <h2 className="tab-title">Treasury</h2>
      </div>

      {/* Treasury address */}
      <div className="card">
        <div className="card-header">
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span className="card-title">Treasury Address</span>
            {demoMode && (
              <span
                style={{
                  fontSize: 10,
                  fontWeight: 600,
                  textTransform: 'uppercase',
                  letterSpacing: '0.05em',
                  padding: '2px 6px',
                  borderRadius: 4,
                  background: 'rgba(245, 158, 11, 0.15)',
                  color: '#f59e0b',
                  border: '1px solid rgba(245, 158, 11, 0.3)',
                }}
                title="Transactions are not broadcast to the network. Switch broadcaster in Settings."
              >
                Demo Mode
              </span>
            )}
          </div>
          <span className="card-subtitle">{info.network} | {info.keyMode === 'xpriv' ? info.derivationPath : 'single key'}</span>
        </div>
        <div style={{ marginBottom: 12, display: 'flex', alignItems: 'center', gap: 10 }}>
          <a
            href={addressUrl(info.address)}
            target="_blank"
            rel="noopener noreferrer"
            className="addr"
            style={{ fontSize: 15, color: 'var(--accent-blue)', textDecoration: 'none' }}
            title="View on WhatsonChain"
          >
            {info.address}
          </a>
          <button
            className="btn btn-sm"
            style={{ fontSize: 11, padding: '2px 8px' }}
            onClick={() => copyToClipboard(info.address)}
            title="Copy to clipboard"
          >
            Copy
          </button>
        </div>
        <div className="alert alert-info">
          Fund this address to create new UTXOs via fan-out. Each fan-out splits one funding UTXO into many small UTXOs for pool replenishment.
        </div>
      </div>

      {/* Pool status */}
      <div className="grid grid-3">
        {info.noncePool && <PoolStats label="Nonce" stats={info.noncePool} />}
        {info.feePool && <PoolStats label="Fee" stats={info.feePool} />}
        {info.paymentPool && <PoolStats label="Payment" stats={info.paymentPool} />}
      </div>

      {/* Available Treasury UTXOs */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Available Treasury UTXOs</span>
          <span className="card-subtitle">
            {utxoData?.lastPoll
              ? `Last polled: ${new Date(utxoData.lastPoll).toLocaleTimeString()}`
              : 'Polling...'}
          </span>
        </div>
        {utxoData?.error && (
          <div className="alert alert-error" style={{ marginBottom: 12 }}>{utxoData.error}</div>
        )}
        {hasUTXOs ? (
          <table className="table">
            <thead>
              <tr>
                <th>TxID</th>
                <th>Vout</th>
                <th>Amount</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              {utxos.map((utxo, i) => {
                const isSelected = fundingTxid === utxo.txid && fundingVout === String(utxo.vout)
                return (
                  <tr
                    key={`${utxo.txid}:${utxo.vout}`}
                    style={{
                      cursor: 'pointer',
                      background: isSelected ? 'var(--accent-bg, rgba(99,102,241,0.08))' : undefined,
                    }}
                    onClick={() => selectUTXO(utxo)}
                  >
                    <td style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                      <a
                        href={txUrl(utxo.txid)}
                        target="_blank"
                        rel="noopener noreferrer"
                        onClick={(e) => e.stopPropagation()}
                        style={{ color: 'var(--accent-blue)', textDecoration: 'none' }}
                        title={demoMode ? `${utxo.txid} (demo — may not exist on-chain)` : utxo.txid}
                      >
                        {utxo.txid.slice(0, 16)}...
                      </a>
                    </td>
                    <td>{utxo.vout}</td>
                    <td>{formatSats(utxo.satoshis)}</td>
                    <td>
                      <button
                        className="btn btn-sm"
                        style={{ fontSize: 11, padding: '2px 8px' }}
                        onClick={(e) => {
                          e.stopPropagation()
                          selectUTXO(utxo)
                        }}
                      >
                        {isSelected ? '✓ Selected' : 'Use'}
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        ) : (
          <div style={{ padding: 16, color: 'var(--text-muted)', textAlign: 'center', fontSize: 13 }}>
            No UTXOs found at treasury address. Fund the address above to get started.
          </div>
        )}
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
              <option value="nonce">Nonce Pool (1 sat)</option>
              <option value="fee">Fee Pool (1 sat)</option>
              <option value="payment">Payment Pool (100 sat)</option>
            </select>
          </div>
          <div className="form-group">
            <label className="form-label">Output Count</label>
            <input className="form-input" type="number" value={count} onChange={(e) => setCount(e.target.value)} />
          </div>
        </div>

        {/* Funding source toggle */}
        <div className="form-group">
          <label className="form-label">Funding Source</label>
          <div style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
            <button
              className={`btn btn-sm ${inputMode === 'select' ? 'btn-primary' : ''}`}
              style={{ fontSize: 12 }}
              onClick={() => setInputMode('select')}
            >
              Select from Wallet
            </button>
            <button
              className={`btn btn-sm ${inputMode === 'manual' ? 'btn-primary' : ''}`}
              style={{ fontSize: 12 }}
              onClick={() => setInputMode('manual')}
            >
              Manual Entry
            </button>
          </div>
        </div>

        {inputMode === 'select' ? (
          <div className="form-group">
            <label className="form-label">Select UTXO</label>
            {hasUTXOs ? (
              <select
                className="form-input"
                value={fundingTxid ? `${fundingTxid}:${fundingVout}` : ''}
                onChange={(e) => {
                  const selected = utxos.find(
                    (u) => `${u.txid}:${u.vout}` === e.target.value
                  )
                  if (selected) selectUTXO(selected)
                }}
              >
                <option value="">-- Select a UTXO --</option>
                {utxos.map((utxo) => (
                  <option key={`${utxo.txid}:${utxo.vout}`} value={`${utxo.txid}:${utxo.vout}`}>
                    {utxo.txid.slice(0, 16)}... : {utxo.vout} ({formatSats(utxo.satoshis)})
                  </option>
                ))}
              </select>
            ) : (
              <div className="alert alert-info" style={{ fontSize: 13 }}>
                No UTXOs available. Fund the treasury address or switch to Manual Entry.
              </div>
            )}
            {fundingTxid && inputMode === 'select' && (
              <div style={{ marginTop: 8, fontSize: 12, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>
                TXID: {fundingTxid}<br />
                Vout: {fundingVout} | Sats: {fundingSatoshis} | Script: {fundingScript.slice(0, 20)}...
              </div>
            )}
          </div>
        ) : (
          <>
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
          </>
        )}

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
                  <td style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                    <a
                      href={txUrl(entry.txid)}
                      target="_blank"
                      rel="noopener noreferrer"
                      style={{ color: 'var(--accent-blue)', textDecoration: 'none' }}
                      title={demoMode ? `${entry.txid} (demo — may not exist on-chain)` : entry.txid}
                    >
                      {entry.txid.slice(0, 16)}...
                    </a>
                  </td>
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
