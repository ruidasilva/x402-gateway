/**
 * Minimal x402 client — performs the protocol loop automatically.
 *
 * Usage:
 *   const { x402Fetch } = require("./x402Fetch");
 *   const response = await x402Fetch("http://localhost:3000/api/weather/london");
 *   console.log(await response.json());
 *
 * The client is responsible for ONE thing only:
 *   request → detect 402 → pay → attach proof → retry
 *
 * It does NOT implement wallets, UTXO management, balances, or accounting.
 *
 * Modes (set via X402_MODE environment variable):
 *   "mock" (default) — uses the server's /api/x402/simulate-payment endpoint
 *   "live"           — placeholder for real @merkleworks/x402-client integration
 */

const X402_MODE = process.env.X402_MODE || "mock";

/**
 * Fetch a URL with automatic x402 payment handling.
 *
 * @param {string} url - The URL to fetch
 * @param {Object} [opts] - Standard fetch options (method, headers, body)
 * @returns {Promise<Response>} The final response (after payment if needed)
 */
async function x402Fetch(url, opts = {}) {
  const method = (opts.method || "GET").toUpperCase();
  const headers = { "Content-Type": "application/json", ...opts.headers };

  // Step 1: Send original request
  const res = await fetch(url, { ...opts, method, headers });

  // Not a 402 — return as-is
  if (res.status !== 402) return res;

  // Step 2: Parse 402 challenge
  const challengeHeader = res.headers.get("X402-Challenge");
  if (!challengeHeader) return res;

  // Step 3: Obtain proof (mock or live)
  const proofEncoded = await obtainProof(challengeHeader, {
    method,
    url,
    body: opts.body || null,
  });

  if (!proofEncoded) return res;

  // Step 4: Retry with proof
  return fetch(url, {
    ...opts,
    method,
    headers: { ...headers, "X402-Proof": proofEncoded },
  });
}

/**
 * Obtain a payment proof for a given challenge.
 *
 * In mock mode, calls the server's simulation endpoint.
 * In live mode, would use the real x402 client stack.
 */
async function obtainProof(challengeHeader, requestInfo) {
  if (X402_MODE === "live") {
    // ┌─────────────────────────────────────────────────┐
    // │ LIVE MODE — swap in real client-js here:        │
    // │                                                 │
    // │ import { parseChallenge, buildPartialTransaction,│
    // │   HttpDelegator, WoCBroadcaster } from          │
    // │   "@merkleworks/x402-client"                    │
    // │                                                 │
    // │ const { challenge, hash } =                     │
    // │   parseChallenge(challengeHeader)               │
    // │ const partialTx =                               │
    // │   buildPartialTransaction(challenge)            │
    // │ const delegator =                               │
    // │   new HttpDelegator(DELEGATOR_URL)              │
    // │ const { txid, rawtx_hex } =                     │
    // │   await delegator.completeTransaction(...)      │
    // │ const broadcaster =                             │
    // │   new WoCBroadcaster(WOC_MAINNET)              │
    // │ await broadcaster.broadcast(rawtx_hex)          │
    // │ return buildProofHeader({ txid, rawtx_hex,     │
    // │   challengeHash, request })                     │
    // └─────────────────────────────────────────────────┘
    throw new Error(
      "Live mode not yet configured. Set X402_MODE=mock or provide delegator/broadcaster configuration."
    );
  }

  // Mock mode — call the server's simulation endpoint
  const parsed = new URL(requestInfo.url);
  const serverBase = `${parsed.protocol}//${parsed.host}`;

  const simRes = await fetch(`${serverBase}/api/x402/simulate-payment`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      challengeHeader,
      method: requestInfo.method,
      path: parsed.pathname,
      query: parsed.search.replace("?", ""),
    }),
  });

  if (!simRes.ok) return null;

  const result = await simRes.json();
  return result.proofEncoded;
}

module.exports = { x402Fetch, obtainProof, X402_MODE };
