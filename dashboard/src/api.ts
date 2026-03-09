// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//

import type { ConfigResponse, StatsSummary, TimeseriesPoint, TreasuryInfo, TreasuryUTXOsResponse, FanoutHistoryEntry } from './types'

const BASE = ''

async function fetchJSON<T>(url: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(BASE + url, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`${res.status}: ${body}`)
  }
  return res.json()
}

// Config
export async function getConfig(): Promise<ConfigResponse> {
  return fetchJSON('/api/v1/config')
}

export async function updateConfig(updates: Record<string, unknown>): Promise<{ success: boolean; updated: Record<string, unknown>; restart_required?: boolean; restart_reason?: string }> {
  return fetchJSON('/api/v1/config', {
    method: 'PUT',
    body: JSON.stringify(updates),
  })
}

// Stats
export async function getStatsSummary(): Promise<StatsSummary> {
  return fetchJSON('/api/v1/stats/summary')
}

export async function getTimeseries(): Promise<{ points: TimeseriesPoint[]; bucketMs: number }> {
  return fetchJSON('/api/v1/stats/timeseries')
}

// Treasury
export async function getTreasuryInfo(): Promise<TreasuryInfo> {
  return fetchJSON('/api/v1/treasury/info')
}

export async function triggerFanout(req: {
  pool: string
  count: number
  fundingTxid: string
  fundingVout: number
  fundingScript: string
  fundingSatoshis: number
}): Promise<{ success: boolean; txid: string; utxoCount: number; pool: string }> {
  return fetchJSON('/api/v1/treasury/fanout', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function getTreasuryUTXOs(): Promise<TreasuryUTXOsResponse> {
  return fetchJSON('/api/v1/treasury/utxos')
}

export async function getFanoutHistory(): Promise<{ success: boolean; history: FanoutHistoryEntry[] }> {
  return fetchJSON('/api/v1/treasury/history')
}

// Health
export async function getHealth(): Promise<Record<string, unknown>> {
  return fetchJSON('/health')
}

// Testing: x402 flow
export async function testExpensiveEndpoint(proof?: string): Promise<{ status: number; body: unknown; headers: Record<string, string> }> {
  const headers: Record<string, string> = {}
  if (proof) {
    headers['X402-Proof'] = proof
  }
  const res = await fetch('/v1/expensive', { headers })
  const body = await res.json().catch(() => res.text())
  const respHeaders: Record<string, string> = {}
  res.headers.forEach((v, k) => { respHeaders[k] = v })
  return { status: res.status, body, headers: respHeaders }
}

export async function buildProof(challenge: string): Promise<{ proof_header: string; txid: string; challenge_hash: string }> {
  return fetchJSON('/demo/build-proof', {
    method: 'POST',
    body: JSON.stringify({ challenge }),
  })
}
