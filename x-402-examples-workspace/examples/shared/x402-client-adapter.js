/**
 * x402 Client Adapter
 *
 * Wraps the real @merkleworks/x402-client (from /context/client-js)
 * and exposes its utility functions for use in the playground.
 *
 * ┌─────────────────────────────────────────────────────────────┐
 * │ PROTOCOL-ALIGNED: This adapter imports real x402-client     │
 * │ functions. The following are used directly from client-js:  │
 * │                                                             │
 * │   - parseChallenge()     Parse X402-Challenge header        │
 * │   - buildProofHeader()   Encode proof as base64url JSON     │
 * │   - hashHeaders()        SHA-256 of canonical request hdrs  │
 * │   - hashBody()           SHA-256 of request body            │
 * │   - sha256hex()          SHA-256 hex digest                 │
 * │   - canonicalize()       RFC 8785 canonical JSON            │
 * │   - CHALLENGE_HEADER     "x402-challenge"                   │
 * │   - PROOF_HEADER         "X402-Proof"                       │
 * │   - ACCEPT_HEADER        "x402-accept"                      │
 * │                                                             │
 * │ SIMULATED: The following require live infrastructure        │
 * │ and are replaced with local simulations:                    │
 * │                                                             │
 * │   - Delegator       Needs a funded key-signing service      │
 * │   - Broadcaster     Needs BSV network access                │
 * │   - Transaction     Needs real UTXO for nonce spend         │
 * │                                                             │
 * │ TO SWAP FOR PRODUCTION:                                     │
 * │   Replace SimulatedDelegator with HttpDelegator              │
 * │   Replace SimulatedBroadcaster with WoCBroadcaster           │
 * │   Both classes are exported from @merkleworks/x402-client   │
 * └─────────────────────────────────────────────────────────────┘
 */

const crypto = require("crypto");
const path = require("path");

// Resolved path to the built client-js package
const CLIENT_JS_PATH = path.resolve(
  __dirname,
  "..",
  "..",
  "context",
  "x402-gateway-reference-implementation",
  "client-js",
  "dist",
  "index.js"
);

// ─── Lazy-loaded real client-js exports ───────────────
// client-js is ESM; we use dynamic import() from CJS.

let _clientModule = null;

async function getClientModule() {
  if (!_clientModule) {
    _clientModule = await import(CLIENT_JS_PATH);
  }
  return _clientModule;
}

// ─── Re-exported real functions ───────────────────────

/** Parse an X402-Challenge header value. Returns { challenge, hash }. */
async function parseChallenge(headerValue) {
  const mod = await getClientModule();
  return mod.parseChallenge(headerValue);
}

/** SHA-256 hex digest of a string or Buffer. */
async function sha256hex(input) {
  const mod = await getClientModule();
  return mod.sha256hex(input);
}

/** Hash request headers per x402 canonical form. */
async function hashHeaders(headers) {
  const mod = await getClientModule();
  return mod.hashHeaders(headers);
}

/** Hash request body per x402 spec. */
async function hashBody(body) {
  const mod = await getClientModule();
  return mod.hashBody(body);
}

/** RFC 8785 canonical JSON encoding. */
async function canonicalize(obj) {
  const mod = await getClientModule();
  return mod.canonicalize(obj);
}

/** Build a base64url-encoded proof header value. */
async function buildProofHeader(opts) {
  const mod = await getClientModule();
  return mod.buildProofHeader(opts);
}

/** Get the canonical header name constants from client-js. */
async function getHeaderConstants() {
  const mod = await getClientModule();
  return {
    CHALLENGE_HEADER: mod.CHALLENGE_HEADER, // "x402-challenge"
    PROOF_HEADER: mod.PROOF_HEADER,         // "X402-Proof"
    ACCEPT_HEADER: mod.ACCEPT_HEADER,       // "x402-accept"
  };
}

// ─── Simulated components ─────────────────────────────
// These replace real infrastructure that is not available
// in local dev. Each documents the production replacement.

/**
 * SIMULATED Delegator
 *
 * In production, replace with:
 *   import { HttpDelegator } from "@merkleworks/x402-client"
 *   const delegator = new HttpDelegator("https://your-delegator.example.com/delegate")
 *
 * The real HttpDelegator sends a partial unsigned transaction to a
 * fee-funding service that adds fee inputs and signs its own inputs
 * using SIGHASH_ALL | ANYONECANPAY | FORKID.
 */
class SimulatedDelegator {
  async completeTransaction(request) {
    // [SIMULATED] In production, this sends the partial tx to a real
    // delegator service that funds fees and returns a signed transaction.
    const txid = crypto.randomBytes(32).toString("hex");
    const rawtxHex =
      request.partial_tx_hex + crypto.randomBytes(100).toString("hex");
    return {
      txid,
      rawtx_hex: rawtxHex,
      accepted: true,
    };
  }
}

/**
 * SIMULATED Broadcaster
 *
 * In production, replace with:
 *   import { WoCBroadcaster, WOC_MAINNET } from "@merkleworks/x402-client"
 *   const broadcaster = new WoCBroadcaster(WOC_MAINNET)
 *
 * The real WoCBroadcaster POSTs the raw transaction hex to
 * WhatsOnChain's broadcast API and returns the confirmed txid.
 */
class SimulatedBroadcaster {
  async broadcast(rawtxHex) {
    // [SIMULATED] In production, this broadcasts to the BSV network.
    // Simulated broadcast returns immediately.
    return {
      txid: crypto
        .createHash("sha256")
        .update(Buffer.from(rawtxHex, "hex"))
        .digest("hex"),
      broadcast_time_ms: Math.floor(Math.random() * 200) + 50,
    };
  }
}

/**
 * Build a simulated partial transaction hex.
 *
 * In production, replace with:
 *   import { buildPartialTransaction } from "@merkleworks/x402-client"
 *   const partialTxHex = buildPartialTransaction(challenge)
 *
 * The real function builds a valid unsigned BSV transaction that:
 *   - Spends the nonce UTXO as input[0]
 *   - Pays amount_sats to the payee locking script as output[0]
 */
function buildSimulatedPartialTx(challenge) {
  // [SIMULATED] Random bytes standing in for a real unsigned transaction.
  return crypto.randomBytes(250).toString("hex");
}

module.exports = {
  // Real client-js functions (protocol-aligned)
  parseChallenge,
  sha256hex,
  hashHeaders,
  hashBody,
  canonicalize,
  buildProofHeader,
  getHeaderConstants,
  getClientModule,

  // Simulated components (swap for production)
  SimulatedDelegator,
  SimulatedBroadcaster,
  buildSimulatedPartialTx,

  // Path for reference
  CLIENT_JS_PATH,
};
