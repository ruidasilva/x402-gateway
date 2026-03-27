// x402 SDK v1.0 — Public API

// Client
export { X402Client } from "./client.js"
export type { X402ClientConfig } from "./client.js"

// Challenge
export {
  parseChallenge,
  assertNotExpired,
  canonicalize,
  sha256hex,
  hashHeaders,
  hashBody,
  CHALLENGE_HEADER,
  PROOF_HEADER,
} from "./challenge.js"

// Transaction
export { buildPartialTransaction, isTemplateMode, computeTxid } from "./transaction.js"

// Proof
export { buildProof, buildRequestBinding, encodeProofHeader } from "./proof.js"

// Session (step chain)
export { createSession } from "./session.js"

// Adapters
export { HttpDelegator } from "./adapters/delegator.js"
export { WoCBroadcaster, WOC_MAINNET, WOC_TESTNET } from "./adapters/broadcaster.js"
export { DefaultWallet } from "./adapters/wallet.js"

// Errors
export {
  ChallengeError,
  ChallengeExpiredError,
  DelegationError,
  BroadcastError,
  ProofRejectedError,
  BindingMismatchError,
} from "./errors.js"

// Dev utilities
export { verifyVectorFile } from "./dev/verifyVectors.js"

// Types
export type {
  Challenge,
  NonceRef,
  TemplateRef,
  ParsedChallenge,
  CompletedTransaction,
  RequestContext,
  RequestBinding,
  Payment,
  Proof,
  DelegationInput,
  Delegator,
  Broadcaster,
  TransactionBuilder,
  PaymentSession,
  PartialTxStep,
  FinalizedTxStep,
  BroadcastStep,
  ProofStep,
  DebugEvent,
} from "./types.js"
