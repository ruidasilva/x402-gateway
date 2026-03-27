// x402 SDK — Wire-format types
// Matches x402.md v1.0 §4 and §5 exactly.

export interface NonceRef {
  readonly txid: string
  readonly vout: number
  readonly satoshis: number
  readonly locking_script_hex: string
}

export interface TemplateRef {
  readonly rawtx_hex: string
  readonly price_sats: number
}

export interface Challenge {
  readonly v: 1
  readonly scheme: "bsv-tx-v1"
  readonly amount_sats: number
  readonly payee_locking_script_hex: string
  readonly expires_at: number
  readonly domain: string
  readonly method: string
  readonly path: string
  readonly query: string
  readonly req_headers_sha256: string
  readonly req_body_sha256: string
  readonly nonce_utxo: NonceRef
  readonly require_mempool_accept: boolean
  readonly template?: TemplateRef
}

export interface RequestBinding {
  readonly method: string
  readonly path: string
  readonly query: string
  readonly req_headers_sha256: string
  readonly req_body_sha256: string
}

export interface Payment {
  readonly txid: string
  readonly rawtx_b64: string
}

export interface Proof {
  readonly v: 1
  readonly scheme: "bsv-tx-v1"
  readonly challenge_sha256: string
  readonly request: RequestBinding
  readonly payment: Payment
}

export interface ParsedChallenge {
  readonly challenge: Challenge
  readonly canonicalBytes: Uint8Array
  readonly challengeHash: string
}

export interface CompletedTransaction {
  readonly txid: string
  readonly rawtxHex: string
}

export interface RequestContext {
  readonly url: URL
  readonly method: string
  readonly headers: Headers
  readonly body: string | Uint8Array | null
}

export interface DelegationInput {
  readonly partialTxHex: string
  readonly nonceUtxo: { readonly txid: string; readonly vout: number }
  readonly challengeHash: string
}

export interface Delegator {
  complete(input: DelegationInput): Promise<CompletedTransaction>
}

export interface Broadcaster {
  broadcast(rawtxHex: string): Promise<string>
}

export interface TransactionBuilder {
  buildPartial(challenge: Challenge): Promise<string>
}

// Step-chain types
export interface PaymentSession {
  readonly challenge: Challenge
  readonly challengeHash: string
  readonly request: RequestContext
  buildPartialTransaction(): Promise<PartialTxStep>
}

export interface PartialTxStep {
  readonly partialTxHex: string
  finalizeTransaction(): Promise<FinalizedTxStep>
}

export interface FinalizedTxStep {
  readonly txid: string
  readonly rawtxHex: string
  broadcast(): Promise<BroadcastStep>
}

export interface BroadcastStep {
  readonly txid: string
  readonly rawtxHex: string
  buildProof(): ProofStep
}

export interface ProofStep {
  readonly proof: Proof
  readonly header: string
  retry(): Promise<Response>
}

// Debug surface — optional, non-invasive visibility into protocol steps.
export type DebugEvent =
  | { readonly type: "challenge"; readonly challenge: Challenge; readonly challengeHash: string }
  | { readonly type: "binding"; readonly binding: RequestBinding }
  | { readonly type: "transaction"; readonly txid: string; readonly rawtxHex: string }
  | { readonly type: "broadcast"; readonly txid: string }
  | { readonly type: "proof"; readonly proof: Proof }
  | { readonly type: "retry"; readonly status: number }
