/**
 * x402 Payment Simulator
 *
 * ┌─────────────────────────────────────────────────────────────┐
 * │ This file simulates the client-side x402 payment flow for  │
 * │ the developer playground. It uses REAL functions from       │
 * │ @merkleworks/x402-client where possible and SIMULATES the  │
 * │ parts that require live infrastructure.                     │
 * │                                                             │
 * │ REAL (from client-js):                                      │
 * │   parseChallenge()  — decodes X402-Challenge header         │
 * │   sha256hex()       — SHA-256 hashing                       │
 * │   hashHeaders()     — canonical request header hash         │
 * │   hashBody()        — request body hash                     │
 * │                                                             │
 * │ SIMULATED (no live infrastructure):                         │
 * │   Transaction building — random bytes, not real BSV tx      │
 * │   Delegation — no real fee-funding service                  │
 * │   Broadcasting — no real network submission                 │
 * │                                                             │
 * │ TO USE REAL CLIENT:                                         │
 * │   import { X402Client, HttpDelegator, WoCBroadcaster }      │
 * │   const client = new X402Client({ delegator, broadcaster }) │
 * │   const response = await client.fetch(url)                  │
 * │   // The real client handles the entire flow automatically  │
 * └─────────────────────────────────────────────────────────────┘
 */

const crypto = require("crypto");
const {
  parseChallenge,
  sha256hex,
  hashHeaders,
  hashBody,
  SimulatedDelegator,
  SimulatedBroadcaster,
  buildSimulatedPartialTx,
} = require("./x402-client-adapter");

const delegator = new SimulatedDelegator();
const broadcaster = new SimulatedBroadcaster();

/**
 * Simulates the full x402 client payment flow.
 * Returns step-by-step events for the payment flow inspector.
 *
 * Uses real parseChallenge() from client-js to decode the challenge.
 * Uses real hashHeaders()/hashBody() for request binding fields.
 * Simulates transaction building, delegation, and broadcast.
 *
 * @param {string} challengeHeader - Base64url-encoded challenge from 402 response
 * @param {Object} requestInfo - Original request details
 * @returns {Promise<Object>} { proof, proofEncoded, steps, txid, challengeHash }
 */
async function simulatePayment(challengeHeader, requestInfo = {}) {
  const steps = [];

  // ── Step 1: Parse challenge using REAL client-js function ──
  const { challenge, hash: challengeHash } =
    await parseChallenge(challengeHeader);

  steps.push({
    step: "challenge_received",
    label: "402 Payment Required",
    source: "protocol-aligned (parseChallenge from client-js)",
    data: {
      amount_sats: challenge.amount_sats,
      scheme: challenge.scheme,
      expires_at: challenge.expires_at,
      nonce_txid: challenge.nonce_utxo?.txid,
    },
  });

  // ── Step 2: Build partial transaction ──
  // [SIMULATED] In production: buildPartialTransaction(challenge) from client-js
  const partialTxHex = buildSimulatedPartialTx(challenge);

  steps.push({
    step: "tx_built",
    label: "Transaction Built",
    source: "simulated (random bytes, not real BSV transaction)",
    data: {
      inputs: 1,
      outputs: 1,
      amount_sats: challenge.amount_sats,
      nonce_spent: challenge.nonce_utxo
        ? `${challenge.nonce_utxo.txid}:${challenge.nonce_utxo.vout}`
        : "n/a",
    },
  });

  // ── Step 3: Delegate (fee funding + signing) ──
  // [SIMULATED] In production: HttpDelegator.completeTransaction(request)
  const delegationResult = await delegator.completeTransaction({
    partial_tx_hex: partialTxHex,
    challenge_hash: challengeHash,
    payee_locking_script_hex: challenge.payee_locking_script_hex,
    amount_sats: challenge.amount_sats,
    nonce_outpoint: challenge.nonce_utxo
      ? {
          txid: challenge.nonce_utxo.txid,
          vout: challenge.nonce_utxo.vout,
          satoshis: challenge.nonce_utxo.satoshis,
        }
      : null,
  });

  const txid = delegationResult.txid;

  steps.push({
    step: "tx_delegated",
    label: "Transaction Signed",
    source: "simulated (SimulatedDelegator, no real fee-funding service)",
    data: {
      txid,
      size_bytes: delegationResult.rawtx_hex.length / 2,
    },
  });

  // ── Step 4: Broadcast ──
  // [SIMULATED] In production: WoCBroadcaster.broadcast(rawtxHex)
  const broadcastResult = await broadcaster.broadcast(delegationResult.rawtx_hex);

  steps.push({
    step: "tx_broadcast",
    label: "Payment Broadcast",
    source: "simulated (SimulatedBroadcaster, no real network submission)",
    data: {
      txid,
      network: "simulated",
      broadcast_time_ms: broadcastResult.broadcast_time_ms,
    },
  });

  // ── Step 5: Build proof ──
  // Request binding uses REAL hashHeaders()/hashBody() from client-js
  const reqHeadersHash = await hashHeaders(requestInfo.headers || {});
  const reqBodyHash = await hashBody(requestInfo.body || null);

  const rawtxB64 = Buffer.from(delegationResult.rawtx_hex, "hex").toString(
    "base64"
  );

  // Proof object per x402.md §5: v is integer, payment is nested.
  // request contains 5 fields (no domain).
  const proof = {
    v: 1,
    scheme: "bsv-tx-v1",
    challenge_sha256: challengeHash,
    request: {
      method: (requestInfo.method || "GET").toUpperCase(),
      path: requestInfo.path || challenge.path || "/",
      query: requestInfo.query || "",
      req_headers_sha256: reqHeadersHash,
      req_body_sha256: reqBodyHash,
    },
    payment: {
      txid,
      rawtx_b64: rawtxB64,
    },
  };

  const proofEncoded = Buffer.from(JSON.stringify(proof)).toString("base64url");

  steps.push({
    step: "proof_built",
    label: "Proof Attached",
    source: "protocol-aligned (proof structure matches client-js Proof type)",
    data: {
      txid,
      challenge_hash: challengeHash,
    },
  });

  return { proof, proofEncoded, steps, txid, challengeHash };
}

module.exports = { simulatePayment };
