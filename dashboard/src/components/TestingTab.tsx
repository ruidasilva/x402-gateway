// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//

import { useState } from 'react'
import { testExpensiveEndpoint, buildProof } from '../api'

type StepStatus = 'pending' | 'active' | 'success' | 'error'

interface FlowStep {
  title: string
  status: StepStatus
  detail?: string
}

export default function TestingTab() {
  const [steps, setSteps] = useState<FlowStep[]>([
    { title: 'GET /v1/expensive (no proof)', status: 'pending' },
    { title: 'Build proof from challenge', status: 'pending' },
    { title: 'GET /v1/expensive (with proof)', status: 'pending' },
  ])
  const [running, setRunning] = useState(false)
  const [responses, setResponses] = useState<Record<number, object>>({})

  function updateStep(idx: number, update: Partial<FlowStep>) {
    setSteps((prev) => prev.map((s, i) => i === idx ? { ...s, ...update } : s))
  }

  async function runFlow() {
    setRunning(true)
    setResponses({})
    setSteps([
      { title: 'GET /v1/expensive (no proof)', status: 'pending' },
      { title: 'Build proof from challenge', status: 'pending' },
      { title: 'GET /v1/expensive (with proof)', status: 'pending' },
    ])

    try {
      // Step 1: GET without proof -> 402
      updateStep(0, { status: 'active' })
      const step1 = await testExpensiveEndpoint()
      setResponses((prev) => ({ ...prev, 0: step1 }))

      if (step1.status !== 402) {
        updateStep(0, { status: 'error', detail: `Expected 402, got ${step1.status}` })
        return
      }

      const challenge = step1.headers['x402-challenge']
      if (!challenge) {
        updateStep(0, { status: 'error', detail: 'No X402-Challenge header in response' })
        return
      }
      updateStep(0, { status: 'success', detail: `402 received, challenge: ${challenge.slice(0, 40)}...` })

      // Step 2: Build proof
      updateStep(1, { status: 'active' })
      const proofResult = await buildProof(challenge)
      setResponses((prev) => ({ ...prev, 1: proofResult }))
      updateStep(1, { status: 'success', detail: `Proof built, txid: ${proofResult.txid.slice(0, 16)}...` })

      // Step 3: GET with proof -> 200
      updateStep(2, { status: 'active' })
      const step3 = await testExpensiveEndpoint(proofResult.proof_header)
      setResponses((prev) => ({ ...prev, 2: step3 }))

      if (step3.status === 200) {
        updateStep(2, { status: 'success', detail: `200 OK - Payment accepted!` })
      } else {
        updateStep(2, { status: 'error', detail: `Expected 200, got ${step3.status}` })
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      // Mark current active step as error
      setSteps((prev) => prev.map((s) => s.status === 'active' ? { ...s, status: 'error', detail: msg } : s))
    } finally {
      setRunning(false)
    }
  }

  // Custom request builder state
  const [customMethod, setCustomMethod] = useState('GET')
  const [customPath, setCustomPath] = useState('/v1/expensive')
  const [customHeaders, setCustomHeaders] = useState('')
  const [customBody, setCustomBody] = useState('')
  const [customResponse, setCustomResponse] = useState<object | null>(null)
  const [customRunning, setCustomRunning] = useState(false)

  async function runCustom() {
    setCustomRunning(true)
    setCustomResponse(null)
    try {
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      if (customHeaders.trim()) {
        for (const line of customHeaders.split('\n')) {
          const idx = line.indexOf(':')
          if (idx > 0) {
            headers[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
          }
        }
      }
      const res = await fetch(customPath, {
        method: customMethod,
        headers,
        body: customMethod !== 'GET' && customBody ? customBody : undefined,
      })
      const body = await res.json().catch(() => res.text())
      const respHeaders: Record<string, string> = {}
      res.headers.forEach((v, k) => { respHeaders[k] = v })
      setCustomResponse({ status: res.status, headers: respHeaders, body })
    } catch (err) {
      setCustomResponse({ error: err instanceof Error ? err.message : String(err) })
    } finally {
      setCustomRunning(false)
    }
  }

  return (
    <div>
      <div className="tab-header">
        <h2 className="tab-title">Testing</h2>
      </div>

      {/* x402 Flow Tester */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">x402 Payment Flow</span>
          <button className="btn btn-primary" onClick={runFlow} disabled={running}>
            {running ? <><span className="spinner" /> Running...</> : 'Run x402 Flow'}
          </button>
        </div>

        <div className="flow-steps">
          {steps.map((step, i) => (
            <div key={i} className={`flow-step ${step.status}`}>
              <div className="step-number">{i + 1}</div>
              <div className="step-content">
                <div className="step-title">{step.title}</div>
                {step.detail && <div className="step-detail">{step.detail}</div>}
                {responses[i] && (
                  <details style={{ marginTop: 8 }}>
                    <summary style={{ fontSize: 12, color: 'var(--accent-blue)', cursor: 'pointer' }}>
                      View Response
                    </summary>
                    <div className="json-view" style={{ marginTop: 4 }}>
                      {JSON.stringify(responses[i], null, 2)}
                    </div>
                  </details>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Custom Request Builder */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Custom Request</span>
        </div>

        <div className="grid grid-2">
          <div className="form-group">
            <label className="form-label">Method</label>
            <select className="form-input" value={customMethod} onChange={(e) => setCustomMethod(e.target.value)}>
              <option>GET</option>
              <option>POST</option>
              <option>PUT</option>
              <option>DELETE</option>
            </select>
          </div>
          <div className="form-group">
            <label className="form-label">Path</label>
            <input className="form-input" value={customPath} onChange={(e) => setCustomPath(e.target.value)} />
          </div>
        </div>
        <div className="form-group">
          <label className="form-label">Headers (one per line, key: value)</label>
          <textarea
            className="form-input"
            rows={3}
            value={customHeaders}
            onChange={(e) => setCustomHeaders(e.target.value)}
            placeholder="X-Custom-Header: value"
            style={{ fontFamily: 'var(--font-mono)', resize: 'vertical' }}
          />
        </div>
        {customMethod !== 'GET' && (
          <div className="form-group">
            <label className="form-label">Body (JSON)</label>
            <textarea
              className="form-input"
              rows={4}
              value={customBody}
              onChange={(e) => setCustomBody(e.target.value)}
              placeholder='{"key": "value"}'
              style={{ fontFamily: 'var(--font-mono)', resize: 'vertical' }}
            />
          </div>
        )}
        <button className="btn btn-primary" onClick={runCustom} disabled={customRunning}>
          {customRunning ? <span className="spinner" /> : null}
          Send Request
        </button>

        {customResponse && (
          <div className="json-view" style={{ marginTop: 12 }}>
            {JSON.stringify(customResponse, null, 2)}
          </div>
        )}
      </div>
    </div>
  )
}
