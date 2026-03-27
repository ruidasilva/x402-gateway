// x402 SDK — Typed step-chain (PaymentSession).
// Each step returns the next valid state. Order enforced at compile time.

import type {
  Challenge,
  RequestContext,
  Delegator,
  Broadcaster,
  TransactionBuilder,
  PaymentSession,
  PartialTxStep,
  FinalizedTxStep,
  BroadcastStep,
  ProofStep,
  Proof,
} from "./types.js"
import { buildProof } from "./proof.js"

export function createSession(
  challenge: Challenge,
  challengeHash: string,
  request: RequestContext,
  delegator: Delegator,
  broadcaster: Broadcaster,
  txBuilder: TransactionBuilder,
  fetchFn: typeof globalThis.fetch,
): PaymentSession {
  return Object.freeze({
    challenge,
    challengeHash,
    request,

    async buildPartialTransaction(): Promise<PartialTxStep> {
      const partialTxHex = await txBuilder.buildPartial(challenge)
      return createPartialTxStep(
        partialTxHex, challenge, challengeHash, request, delegator, broadcaster, fetchFn,
      )
    },
  })
}

function createPartialTxStep(
  partialTxHex: string,
  challenge: Challenge,
  challengeHash: string,
  request: RequestContext,
  delegator: Delegator,
  broadcaster: Broadcaster,
  fetchFn: typeof globalThis.fetch,
): PartialTxStep {
  return Object.freeze({
    partialTxHex,

    async finalizeTransaction(): Promise<FinalizedTxStep> {
      const completed = await delegator.complete({
        partialTxHex,
        nonceUtxo: { txid: challenge.nonce_utxo.txid, vout: challenge.nonce_utxo.vout },
        challengeHash,
      })
      return createFinalizedTxStep(
        completed.txid, completed.rawtxHex,
        challenge, challengeHash, request, broadcaster, fetchFn,
      )
    },
  })
}

function createFinalizedTxStep(
  txid: string,
  rawtxHex: string,
  challenge: Challenge,
  challengeHash: string,
  request: RequestContext,
  broadcaster: Broadcaster,
  fetchFn: typeof globalThis.fetch,
): FinalizedTxStep {
  return Object.freeze({
    txid,
    rawtxHex,

    async broadcast(): Promise<BroadcastStep> {
      await broadcaster.broadcast(rawtxHex)
      return createBroadcastStep(
        txid, rawtxHex, challenge, challengeHash, request, fetchFn,
      )
    },
  })
}

function createBroadcastStep(
  txid: string,
  rawtxHex: string,
  challenge: Challenge,
  challengeHash: string,
  request: RequestContext,
  fetchFn: typeof globalThis.fetch,
): BroadcastStep {
  return Object.freeze({
    txid,
    rawtxHex,

    buildProof(): ProofStep {
      const { proof, header } = buildProof({
        tx: { txid, rawtxHex },
        challengeHash,
        challenge,
        request,
      })
      return createProofStep(proof, header, request, fetchFn)
    },
  })
}

function createProofStep(
  proof: Proof,
  header: string,
  request: RequestContext,
  fetchFn: typeof globalThis.fetch,
): ProofStep {
  return Object.freeze({
    proof,
    header,

    async retry(): Promise<Response> {
      const retryHeaders = new Headers(request.headers)
      retryHeaders.set("X402-Proof", header)

      return fetchFn(request.url.toString(), {
        method: request.method,
        headers: retryHeaders,
        body: request.body as BodyInit | null,
      })
    },
  })
}
