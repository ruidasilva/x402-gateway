# x402 TypeScript SDK — Interface Design

Version: 1.0
Status: Design (pre-implementation)
Companion to: x402.md v1.0 (frozen)

---

## Design principles

1. **Correct by construction.** Types make invalid states unrepresentable.
2. **No footguns.** The SDK prevents binding mismatch, encoding errors, and field omission through the type system.
3. **Explicit boundaries.** Each protocol step is a separate function. No step is hidden.
4. **Stateless.** No hidden caches, no implicit state, no session tracking.
5. **Composable.** Wallet, broadcaster, and delegator are injected, not hardcoded.

---

## 1. Protocol types

These types mirror the wire format exactly. Field names use snake_case to match the JSON wire format. `v` and `scheme` are literal types — only the values defined by v1.0 are representable.

```typescript
// ---------------------------------------------------------------------------
// Wire-format types (match x402.md §4 and §5 exactly)
// ---------------------------------------------------------------------------

/** Nonce UTXO referenced in a challenge (spec §4). */
interface NonceRef {
  readonly txid: string              // 64-char lowercase hex
  readonly vout: number              // output index
  readonly satoshis: number          // must be > 0
  readonly locking_script_hex: string
}

/** Pre-signed template for Profile B (extension, not in base spec). */
interface TemplateRef {
  readonly rawtx_hex: string
  readonly price_sats: number
}

/** Decoded 402 challenge (spec §4). All fields required except template. */
interface Challenge {
  readonly v: 1                               // literal — only v1.0 defined
  readonly scheme: "bsv-tx-v1"                // literal — only bsv-tx-v1 defined
  readonly amount_sats: number
  readonly payee_locking_script_hex: string
  readonly expires_at: number                 // UNIX timestamp (seconds)
  readonly domain: string
  readonly method: string
  readonly path: string
  readonly query: string                      // empty string if none
  readonly req_headers_sha256: string
  readonly req_body_sha256: string
  readonly nonce_utxo: NonceRef
  readonly require_mempool_accept: boolean
  readonly template?: TemplateRef             // Profile B only
}

/** Request binding carried in the proof (spec §5). Five fields, no domain. */
interface RequestBinding {
  readonly method: string
  readonly path: string
  readonly query: string
  readonly req_headers_sha256: string
  readonly req_body_sha256: string
}

/** Payment data nested under "payment" in the proof (spec §5). */
interface Payment {
  readonly txid: string       // 64-char lowercase hex
  readonly rawtx_b64: string  // standard base64 (with padding)
}

/** Complete proof object (spec §5). */
interface Proof {
  readonly v: 1                       // literal 1, not string
  readonly scheme: "bsv-tx-v1"        // literal, not arbitrary string
  readonly challenge_sha256: string
  readonly request: RequestBinding
  readonly payment: Payment
}
```

**Design decision:** Both `Challenge` and `Proof` use literal types for `v` and `scheme` (`1` and `"bsv-tx-v1"`). This makes invalid protocol versions and unknown schemes unrepresentable at compile time. Runtime parsing (`parseChallenge`) must still validate these values and throw `ChallengeError` for unsupported versions — the literal types prevent construction of invalid objects, not reception of them.

**Design decision:** All fields are `readonly`. Challenge, proof, and binding objects are immutable once constructed. This prevents accidental mutation between construction and use.

---

## 2. Intermediate types (SDK-internal)

These types carry data between protocol steps. They are not wire-format objects.

```typescript
/** Result of parsing the X402-Challenge header. */
interface ParsedChallenge {
  readonly challenge: Challenge
  /** The canonical JSON bytes exactly as received (for hashing). */
  readonly canonicalBytes: Uint8Array
  /** SHA-256 hex digest of canonicalBytes. */
  readonly challengeHash: string
}

/** A completed transaction ready for broadcast and proof construction. */
interface CompletedTransaction {
  /** Transaction identifier (double SHA-256, byte-reversed, hex). */
  readonly txid: string
  /** Raw transaction bytes as hex. */
  readonly rawtxHex: string
}

/** The original request context, captured before the 402 is received. */
interface RequestContext {
  readonly url: URL
  readonly method: string
  readonly headers: Headers
  readonly body: string | Uint8Array | null
}
```

**Design decision:** `RequestContext` captures the original request at the point of the 402 response. The proof builder uses this — not the retry request — ensuring binding always matches the challenge.

---

## 3. Extension interfaces

These allow injection of custom implementations.

```typescript
/** Input to the delegator — only the fields needed for fee completion. */
interface DelegationInput {
  readonly partialTxHex: string
  readonly nonceUtxo: { readonly txid: string; readonly vout: number }
  readonly challengeHash: string
}

/** Completes a partial transaction by adding fee inputs. */
interface Delegator {
  complete(input: DelegationInput): Promise<CompletedTransaction>
}

/** Broadcasts a raw transaction to the settlement network. */
interface Broadcaster {
  broadcast(rawtxHex: string): Promise<string>  // returns txid
}

/** Provides a partial transaction for the challenge's nonce UTXO. */
interface TransactionBuilder {
  buildPartial(challenge: Challenge): Promise<string>  // returns hex
}
```

**Design decision:** `Delegator.complete` takes a narrow `DelegationInput` containing only what the delegator needs: the partial transaction, the nonce outpoint for validation, and the challenge hash for correlation. It does NOT take the full `Challenge` — the delegator is a settlement-layer component and must not parse HTTP binding fields, payment amounts, or expiry. This enforces the architectural boundary (invariant A-3: delegator never parses HTTP).

**Design decision:** `TransactionBuilder` is a separate interface. The default implementation constructs a partial tx from the challenge template (Profile B) or builds an unsigned tx (Profile A). Custom wallets implement this interface to use their own signing logic.

---

## 4. Error types

Each error maps to a specific protocol failure. No generic errors.

```typescript
/** The X402-Challenge header is missing, malformed, or invalid. */
class ChallengeError extends Error {
  readonly name = "ChallengeError"
}

/** The challenge has expired (current time > expires_at). */
class ChallengeExpiredError extends ChallengeError {
  readonly name = "ChallengeExpiredError"
  readonly expiresAt: number
  readonly currentTime: number
}

/** The delegator rejected or failed to complete the transaction. */
class DelegationError extends Error {
  readonly name = "DelegationError"
  readonly status?: number
  readonly code?: string
}

/** The transaction could not be broadcast to the network. */
class BroadcastError extends Error {
  readonly name = "BroadcastError"
  readonly code?: string
}

/** The server rejected the proof on retry. */
class ProofRejectedError extends Error {
  readonly name = "ProofRejectedError"
  readonly status: number
  readonly serverCode?: string     // e.g., "invalid_binding", "expired_challenge"
  readonly serverMessage?: string
}

/** Request binding does not match the challenge. SDK-side pre-flight check. */
class BindingMismatchError extends Error {
  readonly name = "BindingMismatchError"
  readonly field: "method" | "path" | "query" | "domain"
  readonly expected: string
  readonly actual: string
}
```

**Design decision:** `BindingMismatchError` is thrown client-side before the proof is sent. If the retry request's method/path/query doesn't match the challenge, the SDK catches it immediately rather than letting the server reject it. This is a pre-flight safety check.

**Design decision:** `ProofRejectedError` includes the server's error code and message. This lets the caller distinguish between "expired challenge" (retry with new challenge) and "invalid binding" (bug in the caller).

---

## 5. Core functions

Each function maps to one protocol step. Functions are pure where possible.

```typescript
// ---------------------------------------------------------------------------
// Step 1: Parse challenge
// ---------------------------------------------------------------------------

/**
 * Parse the X402-Challenge header value into a typed Challenge.
 *
 * Decodes base64url, validates required fields, computes the canonical
 * hash. The returned canonicalBytes are the exact bytes to hash — do
 * not re-serialize the challenge object.
 *
 * Throws ChallengeError if the header is malformed or missing required fields.
 */
function parseChallenge(headerValue: string): ParsedChallenge

// ---------------------------------------------------------------------------
// Step 2: Build partial transaction
// ---------------------------------------------------------------------------

/**
 * Construct a partial transaction that spends the challenge's nonce UTXO
 * and pays the required amount to the payee.
 *
 * For Profile B (template mode): returns the pre-signed template hex.
 * For Profile A: constructs an unsigned transaction.
 *
 * Custom wallet implementations should implement TransactionBuilder instead.
 */
function buildPartialTransaction(challenge: Challenge): string  // hex

// ---------------------------------------------------------------------------
// Step 3: Complete via delegator
// ---------------------------------------------------------------------------

// Uses Delegator.complete() — no SDK function needed.
// The delegator adds fee inputs and signs only its own inputs.

// ---------------------------------------------------------------------------
// Step 4: Broadcast
// ---------------------------------------------------------------------------

// Uses Broadcaster.broadcast() — no SDK function needed.
// The CLIENT broadcasts. The server and delegator never broadcast.

// ---------------------------------------------------------------------------
// Step 5: Build proof
// ---------------------------------------------------------------------------

/**
 * Build the X402-Proof header value from a completed transaction and
 * the original request context.
 *
 * This function:
 * 1. Computes the request binding (method, path, query, header hash, body hash)
 * 2. Constructs the proof object (v=1, scheme, challenge hash, binding, payment)
 * 3. Encodes payment.rawtx_b64 as standard base64 (with padding)
 * 4. Encodes the full proof JSON as base64url (no padding)
 *
 * The request context MUST be the original request that triggered the 402,
 * NOT the retry request. The SDK enforces this by capturing the context
 * at the point of the 402 response.
 *
 * Throws BindingMismatchError if the request context does not match the challenge.
 */
function buildProof(params: {
  tx: CompletedTransaction
  challengeHash: string
  challenge: Challenge
  request: RequestContext
}): string  // base64url-encoded proof header value

// ---------------------------------------------------------------------------
// Hashing utilities (exported for custom implementations)
// ---------------------------------------------------------------------------

/** SHA-256 hex digest. */
function sha256hex(data: Uint8Array | string): string

/** Canonical JSON serialization (RFC 8785: sorted keys, no whitespace). */
function canonicalize(value: unknown): string

/** Hash request headers per spec §4 (allowlist, lowercase, sorted, name:value\n). */
function hashHeaders(headers: Headers | Record<string, string>): string

/** SHA-256 hex of body bytes. Empty body → SHA-256(""). */
function hashBody(body: string | Uint8Array | null): string

/** Double SHA-256, byte-reversed, hex — Bitcoin txid derivation. */
function computeTxid(rawtxHex: string): string

// ---------------------------------------------------------------------------
// Development / CI utility
// ---------------------------------------------------------------------------

/**
 * Verify that the SDK's canonical JSON, hashing, and encoding functions
 * reproduce the canonical test vectors exactly.
 *
 * Loads the vector file, runs all applicable checks (challenge hash,
 * base64url encoding, header binding hash, body hash, txid derivation),
 * and throws on any mismatch.
 *
 * NOT used at runtime. Intended for CI pipelines and development validation.
 *
 * @param vectorPath Path to x402-vectors-v1.json
 * @throws Error with details of the first mismatched vector
 */
function verifyVectorFile(vectorPath: string): Promise<void>
```

---

## 6. High-level client

The `X402Client` provides a `fetch()`-like API that executes the full flow.

```typescript
interface X402ClientConfig {
  /** Delegator implementation. Required. */
  delegator: Delegator

  /** Broadcaster implementation. Required. */
  broadcaster: Broadcaster

  /**
   * Transaction builder. Optional.
   * Default: uses challenge template (Profile B) or builds unsigned (Profile A).
   */
  transactionBuilder?: TransactionBuilder

  /** Extra headers sent with every request. */
  defaultHeaders?: Record<string, string>

  /** Override global fetch (for testing). */
  fetch?: typeof globalThis.fetch
}

class X402Client {
  constructor(config: X402ClientConfig)

  /**
   * Fetch a resource, automatically handling 402 payment challenges.
   *
   * Non-402 responses are returned as-is.
   * On 402: parses challenge → builds tx → delegates → broadcasts → builds proof → retries.
   *
   * Throws ChallengeError, ChallengeExpiredError, DelegationError,
   * BroadcastError, or ProofRejectedError on failure.
   *
   * Never retries more than once. Never retries on non-402 errors.
   */
  async fetch(input: string | URL, init?: RequestInit): Promise<Response>
}
```

**Design decision:** `delegator` and `broadcaster` are required constructor arguments, not optional with defaults. This forces the caller to make an explicit choice about which delegator and broadcaster to use. There is no silent default that might broadcast to mainnet unexpectedly.

**Design decision:** The client never retries more than once. If the proof is rejected, it throws `ProofRejectedError`. The caller decides whether to retry.

### PaymentSession (typed step chain)

For callers who need step-by-step control without the all-or-nothing `fetch()`, the client exposes `prepare()` which returns a typed step chain. Each step returns a different type representing the next valid state. This enforces correct execution order at compile time — calling `broadcast()` before `finalizeTransaction()` is a type error, not a runtime exception.

```typescript
class X402Client {
  // ... fetch() as above ...

  /**
   * Prepare a payment session for a request.
   *
   * Sends the initial request. If the response is not 402, returns null.
   * If 402, parses the challenge and returns a PaymentSession — the
   * entry point to the typed step chain.
   *
   * The session captures the request context at creation. All subsequent
   * steps use this frozen context. The request cannot be mutated.
   */
  async prepare(input: string | URL, init?: RequestInit): Promise<PaymentSession | null>
}

// ---------------------------------------------------------------------------
// Typed step chain — each step returns the next valid state
// ---------------------------------------------------------------------------

/**
 * Entry point: challenge parsed, request context frozen.
 * Only valid next step: build partial transaction.
 */
interface PaymentSession {
  readonly challenge: Challenge
  readonly challengeHash: string
  readonly request: RequestContext

  buildPartialTransaction(): Promise<PartialTxStep>
}

/**
 * Partial transaction built. Only valid next step: delegate for fee completion.
 */
interface PartialTxStep {
  readonly partialTxHex: string

  finalizeTransaction(): Promise<FinalizedTxStep>
}

/**
 * Transaction completed by delegator. Only valid next step: broadcast.
 */
interface FinalizedTxStep {
  readonly txid: string
  readonly rawtxHex: string

  broadcast(): Promise<BroadcastStep>
}

/**
 * Transaction broadcast to settlement network. Only valid next step: build proof.
 */
interface BroadcastStep {
  readonly txid: string
  readonly rawtxHex: string

  buildProof(): ProofStep
}

/**
 * Proof constructed. Only valid next step: retry with proof.
 */
interface ProofStep {
  readonly proof: Proof
  readonly header: string            // base64url-encoded proof for X402-Proof header

  retry(): Promise<Response>
}
```

**Design decision: compile-time order enforcement.** Each step type exposes only the method for the next valid step. `PaymentSession` has `buildPartialTransaction()` but not `broadcast()`. `PartialTxStep` has `finalizeTransaction()` but not `buildProof()`. Calling steps out of order is a TypeScript compilation error, not a runtime exception. This eliminates the entire class of "wrong order" bugs.

**Design decision: no back-references.** Step objects do not carry a reference to the `PaymentSession`. This prevents re-entering the flow (calling `buildPartialTransaction()` again from a later step), which would violate the one-challenge-one-payment-one-proof invariant. Callers who need the challenge or request context for logging should capture them from the session before entering the chain.

**Design decision: each step is immutable.** Step objects are created by the previous step's method and cannot be constructed directly. All fields are `readonly`. There is no way to inject a fake intermediate state.

**Design decision:** `prepare()` returns `null` (not a session) for non-402 responses. This avoids the antipattern of constructing a payment session for a request that doesn't require payment.

---

## 7. Usage examples

### Minimal (high-level client)

```typescript
import { X402Client, HttpDelegator, WoCBroadcaster } from "@merkleworks/x402-client"

const client = new X402Client({
  delegator: new HttpDelegator("https://gateway.example.com"),
  broadcaster: new WoCBroadcaster("mainnet"),
})

const res = await client.fetch("https://gateway.example.com/v1/expensive")
console.log(res.status) // 200
console.log(await res.json())
```

### Advanced (typed step chain)

```typescript
import { X402Client, HttpDelegator, WoCBroadcaster } from "@merkleworks/x402-client"

const client = new X402Client({
  delegator: new HttpDelegator("https://gateway.example.com"),
  broadcaster: new WoCBroadcaster("mainnet"),
})

// Each step returns a typed object. Only the next valid method is available.
const session = await client.prepare("https://gateway.example.com/v1/expensive")
if (!session) { /* response was not 402 */ }

console.log("Challenge:", session.challengeHash)
console.log("Amount:", session.challenge.amount_sats, "sats")

const partial = await session.buildPartialTransaction()
// partial.broadcast()     — compile error: PartialTxStep has no broadcast()
// partial.buildProof()    — compile error: PartialTxStep has no buildProof()

const completed = await partial.finalizeTransaction()
console.log("TxID:", completed.txid)

const broadcast = await completed.broadcast()

const proof = broadcast.buildProof()
console.log("Proof header:", proof.header.slice(0, 40) + "...")

const paid = await proof.retry()
console.log(paid.status) // 200
```

The type system prevents misordering. At each step, only one method exists. This is enforced by the TypeScript compiler, not runtime checks.

The chain also composes with `.then()` for a single-expression flow:

```typescript
const res = await client.prepare(url)
  .then(s => s!.buildPartialTransaction())
  .then(s => s.finalizeTransaction())
  .then(s => s.broadcast())
  .then(s => s.buildProof())
  .then(s => s.retry())
```

### Manual (no client, bare functions)

```typescript
import {
  parseChallenge,
  buildPartialTransaction,
  buildProof,
  HttpDelegator,
  WoCBroadcaster,
} from "@merkleworks/x402-client"

// Step 1: Make request, get 402
const res = await fetch("https://gateway.example.com/v1/expensive")
if (res.status !== 402) { /* handle non-payment response */ }

// Step 2: Parse challenge
const challengeHeader = res.headers.get("x402-challenge")!
const { challenge, challengeHash } = parseChallenge(challengeHeader)

// Step 3: Build partial transaction
const partialTxHex = buildPartialTransaction(challenge)

// Step 4: Delegate (add fee inputs)
const delegator = new HttpDelegator("https://gateway.example.com")
const completedTx = await delegator.complete({
  partialTxHex,
  nonceUtxo: { txid: challenge.nonce_utxo.txid, vout: challenge.nonce_utxo.vout },
  challengeHash,
})

// Step 5: Broadcast (client responsibility)
const broadcaster = new WoCBroadcaster("mainnet")
await broadcaster.broadcast(completedTx.rawtxHex)

// Step 6: Build proof and retry
const proofHeader = buildProof({
  tx: completedTx,
  challengeHash,
  challenge,
  request: {
    url: new URL("https://gateway.example.com/v1/expensive"),
    method: "GET",
    headers: new Headers(),
    body: null,
  },
})

const paid = await fetch("https://gateway.example.com/v1/expensive", {
  headers: { "X402-Proof": proofHeader },
})

console.log(paid.status) // 200
```

### Custom wallet

```typescript
import type { TransactionBuilder, Challenge } from "@merkleworks/x402-client"

class MyWallet implements TransactionBuilder {
  async buildPartial(challenge: Challenge): Promise<string> {
    // Sign the nonce input with your own key management
    // Return the partial transaction as hex
    return mySigningLogic(challenge.nonce_utxo, challenge.payee_locking_script_hex)
  }
}

const client = new X402Client({
  delegator: new HttpDelegator("https://gateway.example.com"),
  broadcaster: new WoCBroadcaster("mainnet"),
  transactionBuilder: new MyWallet(),
})
```

### Custom broadcaster

```typescript
import type { Broadcaster } from "@merkleworks/x402-client"

class ArcBroadcaster implements Broadcaster {
  async broadcast(rawtxHex: string): Promise<string> {
    const res = await fetch("https://arc.gorillapool.io/v1/tx", {
      method: "POST",
      headers: { "Content-Type": "application/octet-stream" },
      body: Buffer.from(rawtxHex, "hex"),
    })
    const data = await res.json()
    return data.txid
  }
}
```

---

## 8. Misuse prevention

| Misuse case | Prevention mechanism |
|---|---|
| Proof with wrong path | `buildProof` takes the original `RequestContext` captured at the 402 response, not the retry URL. The SDK throws `BindingMismatchError` if the context doesn't match the challenge. |
| Proof with `v: "1"` (string) | `Proof.v` is typed as literal `1` (number). TypeScript rejects `v: "1"` at compile time. |
| Proof with flat `txid`/`rawtx_b64` | `Proof` type nests these under `payment: Payment`. There is no top-level `txid` field on the `Proof` type. |
| Missing `payment.rawtx_b64` | `Payment` requires both `txid` and `rawtx_b64`. TypeScript enforces this. |
| Hex used instead of base64 for `rawtx_b64` | `buildProof` performs the hex-to-base64 conversion internally. The caller provides `rawtxHex`; the SDK encodes it. |
| Replay of proof across endpoints | Request binding includes path, method, query, body hash, and header hash. Different endpoints produce different bindings. |
| `domain` in request binding | `RequestBinding` has 5 fields. There is no `domain` field. TypeScript rejects it. |
| Proof sent without broadcasting | The typed step chain enforces order: `buildProof()` only exists on `BroadcastStep`, which is only returned by `broadcast()`. Skipping broadcast is a compile-time error. |
| Steps called out of order | Each step type exposes only the next valid method. `PartialTxStep` has `finalizeTransaction()` but not `broadcast()`. `FinalizedTxStep` has `broadcast()` but not `buildProof()`. Misordering does not compile. |
| Challenge mutation after parsing | All `Challenge` fields are `readonly`. TypeScript prevents mutation. |
| Wrong challenge hash | `parseChallenge` computes the hash from the canonical bytes. The caller cannot provide an arbitrary hash. |
| Expired challenge used | The high-level client checks `expires_at` before proceeding. `ChallengeExpiredError` is thrown with both timestamps for diagnosis. |
| Delegator receives HTTP context | `DelegationInput` contains only `partialTxHex`, `nonceUtxo`, and `challengeHash`. No HTTP fields, no amount, no expiry. Enforces invariant A-3. |
| Delegator broadcasts | `Delegator.complete` returns a `CompletedTransaction`. It has no broadcast method. The type system separates these concerns. |
| Implementation drift from vectors | `verifyVectorFile()` runs in CI and validates canonical JSON, hashing, and encoding against frozen vectors. Catches regressions after refactors. |

---

## 9. Design decisions

**Why `delegator` and `broadcaster` are required, not optional.**
The previous SDK defaulted to `WoCBroadcaster` and `HttpDelegator` with derived URLs. This is convenient but dangerous — a test client could accidentally broadcast to mainnet. Making both required forces the developer to consciously choose their infrastructure.

**Why `buildProof` takes `RequestContext`, not individual fields.**
The previous `buildProofHeader` took 7 separate parameters (`txid`, `rawtxHex`, `challengeHash`, `url`, `method`, `headers`, `body`). This makes it easy to pass the wrong URL or forget to include headers. `RequestContext` bundles the original request as a single unit, captured at the 402 response boundary.

**Why `v` and `scheme` are literal types on both `Challenge` and `Proof`.**
The spec defines exactly one protocol version (1) and one scheme (`bsv-tx-v1`). Literal types make unsupported values unrepresentable at compile time. Runtime parsing still validates and rejects unknown values — the literal types prevent *construction* of invalid objects, not *reception* of them.

**Why `Delegator.complete` takes `DelegationInput`, not the full `Challenge`.**
The delegator is a settlement-layer component (invariant A-3: delegator never parses HTTP). It needs the partial transaction hex, the nonce outpoint for validation, and the challenge hash for correlation. It does not need the payment amount, expiry, binding hashes, or domain. The narrow interface enforces this architectural boundary.

**Why `PaymentSession` uses a typed step chain instead of a flat interface.**
The previous design exposed all five methods on a single `PaymentSession` interface. Nothing prevented calling `broadcast()` before `finalizeTransaction()` except documentation. The typed step chain makes each step return a new type with only the next valid method. Misordering is a compile-time error, not a runtime exception. Each step carries a `session` back-reference for inspection without exposing mutation or out-of-order methods.

**Why no `verifyProof` function.**
Proof verification is the server's responsibility, not the client's. Including a `verifyProof` function in the client SDK would imply the client should verify its own proofs, which is architecturally wrong. The server performs verification per spec Section 7.

**Why `verifyVectorFile` exists but is not a runtime function.**
The SDK must stay aligned with the canonical test vectors. `verifyVectorFile` is a CI/development tool that validates the SDK's canonicalization, hashing, and encoding against frozen vectors. It prevents implementation drift after refactors.

**Why `CompletedTransaction` is a separate type from `DelegationResult`.**
The delegator may return additional metadata (fee amount, mode, etc.). `CompletedTransaction` contains only what the proof builder needs: `txid` and `rawtxHex`. This keeps the proof builder independent of the delegator's wire format.

**Why hashing utilities are exported.**
Custom implementations need `hashHeaders`, `hashBody`, and `sha256hex` to construct their own request bindings or verify challenge hashes. These are pure functions with no side effects.

---

## 10. What changed from v0.2.0

| Aspect | v0.2.0 | This design |
|---|---|---|
| `Proof.v` | `string` | literal `1` |
| `Proof.scheme` | `string` | literal `"bsv-tx-v1"` |
| `Proof.txid` | top-level | nested under `payment` |
| `Proof.rawtx_b64` | top-level | nested under `payment` |
| `RequestBinding.domain` | present | removed (spec §5) |
| `Challenge.v` | `string` | literal `1` |
| `Challenge.scheme` | `string` | literal `"bsv-tx-v1"` |
| `Challenge.confirmations_required` | present | removed (not in spec) |
| Delegator interface | `complete(hex, Challenge)` | `complete(DelegationInput)` (narrow) |
| Delegator/broadcaster | optional with defaults | required constructor args |
| `buildProofHeader` params | 7 separate args | `{tx, challengeHash, challenge, request}` |
| Binding validation | none (server-side only) | client-side pre-flight via `BindingMismatchError` |
| `ProofRejectedError` | status only | status + server code + message |
| Step-by-step flow | manual function calls | Typed step chain (`PaymentSession` → `PartialTxStep` → `FinalizedTxStep` → `BroadcastStep` → `ProofStep`) |
| Vector verification | none | `verifyVectorFile()` (CI/dev utility) |

All changes align the SDK with x402.md v1.0 as patched through the compliance audit.
