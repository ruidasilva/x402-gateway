// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//

// Pool statistics from Go backend
export interface PoolStats {
  total: number
  available: number
  leased: number
  spent: number
  quarantined: number // UTXOs removed by integrity check
  utxo_value: number // denomination per UTXO (sats)
}

// Configuration response (safe — no secret keys)
export interface ConfigResponse {
  network: string
  port: number
  broadcaster: string
  feeRate: number
  poolReplenishThreshold: number
  poolOptimalSize: number
  redisEnabled: boolean
  poolSize: number
  leaseTTLSeconds: number
  payeeAddress: string
  keyMode: string // "xpriv" or "wif"
  nonceAddress: string
  feeAddress: string
  paymentAddress: string
  treasuryAddress: string
  templateMode: boolean
  templatePriceSats?: number
  feeUTXOSats: number // fee pool UTXO denomination (1–1000 sats)
  profile: string // "A (Open Nonce)" or "B (Gateway Template)"
  delegatorUrl: string
  delegatorEmbedded: boolean
  broadcasterUrl?: string
  mode: string // "mock" or "live" — runtime mode for pool namespace
}

// Challenge data decoded from X402-Challenge header (base64url JSON)
export interface ChallengeData {
  v: string
  scheme: string
  amount_sats: number
  payee_locking_script_hex: string
  expires_at: number
  domain: string
  method: string
  path: string
  query: string
  req_headers_sha256: string
  req_body_sha256: string
  nonce_utxo: {
    txid: string
    vout: number
    satoshis: number
    locking_script_hex: string
  }
  template?: {
    rawtx_hex: string
    price_sats: number
  }
}

// Stats summary from Go backend
export interface StatsSummary {
  totalRequests: number
  payments: number
  challenges: number
  errors: number
  avgDurationMs: number
  totalFeeSats: number
  uptimeSeconds: number
  noncePool: PoolStats
  feePool: PoolStats
  paymentPool: PoolStats
}

// Time-series data point
export interface TimeseriesPoint {
  timestamp: number
  requests: number
  payments: number
  errors: number
}

// Treasury info response
export interface TreasuryInfo {
  address: string
  network: string
  broadcaster: string
  keyMode: string
  derivationPath: string
  noncePool: PoolStats
  feePool: PoolStats
  paymentPool: PoolStats
}

// Treasury UTXO from the watcher
export interface TreasuryUTXO {
  txid: string
  vout: number
  script: string
  satoshis: number
  status?: 'confirmed' | 'mempool'
}

// Treasury UTXOs response
export interface TreasuryUTXOsResponse {
  utxos: TreasuryUTXO[]
  lastPoll?: string
  error?: string
}

// Fan-out history entry
export interface FanoutHistoryEntry {
  txid: string
  pool: string
  count: number
  timestamp: string
}

// SSE event from the event stream
export interface GatewayEvent {
  path: string
  method: string
  status: number
  duration_ms: number
  timestamp: string
  details?: Record<string, string>
}

// Dashboard tabs
export type TabId = 'monitor' | 'settings' | 'treasury' | 'testing' | 'analytics'
