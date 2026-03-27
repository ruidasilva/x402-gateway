// x402 SDK — X402Client: high-level fetch() + prepare() step chain.

import type {
  Delegator,
  Broadcaster,
  TransactionBuilder,
  PaymentSession,
  DebugEvent,
} from "./types.js"
import { parseChallenge, assertNotExpired, CHALLENGE_HEADER, PROOF_HEADER } from "./challenge.js"
import { buildPartialTransaction } from "./transaction.js"
import { buildProof } from "./proof.js"
import { createSession } from "./session.js"
import { ChallengeError, ProofRejectedError } from "./errors.js"

export interface X402ClientConfig {
  delegator: Delegator
  broadcaster: Broadcaster
  transactionBuilder?: TransactionBuilder
  defaultHeaders?: Record<string, string>
  fetch?: typeof globalThis.fetch
  /** Optional debug callback. Receives protocol events for logging/diagnostics. */
  debug?: (event: DebugEvent) => void
}

// Default TransactionBuilder using the built-in buildPartialTransaction.
class DefaultTransactionBuilder implements TransactionBuilder {
  async buildPartial(challenge: Parameters<TransactionBuilder["buildPartial"]>[0]): Promise<string> {
    return buildPartialTransaction(challenge)
  }
}

export class X402Client {
  private readonly delegator: Delegator
  private readonly broadcaster: Broadcaster
  private readonly txBuilder: TransactionBuilder
  private readonly defaultHeaders: Record<string, string>
  private readonly fetchFn: typeof globalThis.fetch
  private readonly debug: ((event: DebugEvent) => void) | undefined

  constructor(config: X402ClientConfig) {
    this.delegator = config.delegator
    this.broadcaster = config.broadcaster
    this.txBuilder = config.transactionBuilder ?? new DefaultTransactionBuilder()
    this.defaultHeaders = config.defaultHeaders ?? {}
    this.fetchFn = config.fetch ?? globalThis.fetch.bind(globalThis)
    this.debug = config.debug
  }

  async fetch(input: string | URL, init?: RequestInit): Promise<Response> {
    const mergedInit = this.mergeHeaders(init)
    const res = await this.fetchFn(input, mergedInit)

    if (res.status !== 402) return res

    const challengeHeader = res.headers.get(CHALLENGE_HEADER)
    if (!challengeHeader) {
      throw new ChallengeError(`Received 402 but no ${CHALLENGE_HEADER} header`)
    }

    const { challenge, challengeHash } = parseChallenge(challengeHeader)
    assertNotExpired(challenge)
    this.debug?.({ type: "challenge", challenge, challengeHash })

    const partialTxHex = await this.txBuilder.buildPartial(challenge)

    const completed = await this.delegator.complete({
      partialTxHex,
      nonceUtxo: { txid: challenge.nonce_utxo.txid, vout: challenge.nonce_utxo.vout },
      challengeHash,
    })

    this.debug?.({ type: "transaction", txid: completed.txid, rawtxHex: completed.rawtxHex })

    await this.broadcaster.broadcast(completed.rawtxHex)
    this.debug?.({ type: "broadcast", txid: completed.txid })

    const url = new URL(typeof input === "string" ? input : input.toString())
    const method = mergedInit?.method?.toUpperCase() ?? "GET"
    const headers = new Headers(mergedInit?.headers)
    const body = extractBody(mergedInit)

    const { proof, header: proofHeader } = buildProof({
      tx: completed,
      challengeHash,
      challenge,
      request: { url, method, headers, body },
    })
    this.debug?.({ type: "proof", proof })

    const retryHeaders = new Headers(mergedInit?.headers)
    retryHeaders.set(PROOF_HEADER, proofHeader)

    const retryRes = await this.fetchFn(input, { ...mergedInit, headers: retryHeaders })
    this.debug?.({ type: "retry", status: retryRes.status })

    if (retryRes.status !== 200) {
      let serverCode: string | undefined
      let serverMessage: string | undefined
      try {
        const body = await retryRes.clone().json()
        serverCode = body.code
        serverMessage = body.message
      } catch { /* non-JSON response */ }
      throw new ProofRejectedError(
        `Proof rejected with status ${retryRes.status}`,
        retryRes.status,
        serverCode,
        serverMessage,
      )
    }

    return retryRes
  }

  async prepare(input: string | URL, init?: RequestInit): Promise<PaymentSession | null> {
    const mergedInit = this.mergeHeaders(init)
    const res = await this.fetchFn(input, mergedInit)

    if (res.status !== 402) return null

    const challengeHeader = res.headers.get(CHALLENGE_HEADER)
    if (!challengeHeader) {
      throw new ChallengeError(`Received 402 but no ${CHALLENGE_HEADER} header`)
    }

    const { challenge, challengeHash } = parseChallenge(challengeHeader)
    assertNotExpired(challenge)

    const url = new URL(typeof input === "string" ? input : input.toString())
    const method = mergedInit?.method?.toUpperCase() ?? "GET"
    const headers = new Headers(mergedInit?.headers)
    const body = extractBody(mergedInit)

    return createSession(
      challenge,
      challengeHash,
      { url, method, headers, body },
      this.delegator,
      this.broadcaster,
      this.txBuilder,
      this.fetchFn,
    )
  }

  private mergeHeaders(init?: RequestInit): RequestInit | undefined {
    const keys = Object.keys(this.defaultHeaders)
    if (keys.length === 0) return init
    const merged = new Headers(init?.headers)
    for (const [k, v] of Object.entries(this.defaultHeaders)) {
      if (!merged.has(k)) merged.set(k, v)
    }
    return { ...init, headers: merged }
  }
}

function extractBody(init: RequestInit | undefined): string | null {
  if (!init?.body) return null
  if (typeof init.body === "string") return init.body
  return null
}
