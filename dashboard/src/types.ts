// Pool statistics from Go backend
export interface PoolStats {
  total: number
  available: number
  leased: number
  spent: number
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
