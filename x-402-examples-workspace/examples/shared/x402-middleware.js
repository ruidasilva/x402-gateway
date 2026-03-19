/**
 * x402 Gateway Simulation Middleware for Express
 *
 * ┌─────────────────────────────────────────────────────────────┐
 * │ PURPOSE: Simulates the x402 payment gateway for the        │
 * │ developer playground. NOT a production gateway.             │
 * │                                                             │
 * │ PROTOCOL-ALIGNED (matches reference gateway behaviour):     │
 * │   ✓ 402 response with X402-Challenge header                │
 * │   ✓ Challenge structure (v, scheme, amount_sats, nonce_utxo,│
 * │     payee_locking_script_hex, expires_at, request binding)  │
 * │   ✓ Base64url encoding of challenge and proof               │
 * │   ✓ Challenge cache with TTL cleanup                        │
 * │   ✓ Replay cache keyed on nonce outpoint                    │
 * │   ✓ Single-use challenge deletion after verification        │
 * │   ✓ Receipt hash = SHA-256(txid + challenge_hash)           │
 * │   ✓ Response headers: X402-Receipt, X402-Receipt-Time,      │
 * │     X402-Status, X402-Amount-Sats                           │
 * │   ✓ Error codes: invalid_proof, challenge_not_found,        │
 * │     expired_challenge, double_spend                         │
 * │   ✓ Header name X402-Proof (read via lowercase x402-proof)  │
 * │                                                             │
 * │ SIMULATED (skipped — requires live BSV infrastructure):     │
 * │   ✗ payee_locking_script_hex — random, not real address     │
 * │   ✗ nonce_utxo — random, not a real funded UTXO             │
 * │   ✗ Transaction verification (decode rawtx, verify sigs)    │
 * │   ✗ Nonce spend verification (tx must spend nonce input)    │
 * │   ✗ Payee output verification (tx must pay correct amount)  │
 * │   ✗ Request binding verification (header/body hash match)   │
 * │   ✗ Mempool acceptance check                                │
 * │   ✗ Constant-time comparison (timing attack prevention)     │
 * │                                                             │
 * │ IN PRODUCTION: Deploy the Go gateway from                   │
 * │ /context/x402-gateway-reference-implementation              │
 * │ and proxy requests through it.                              │
 * └─────────────────────────────────────────────────────────────┘
 */

const crypto = require("crypto");

// In-memory challenge store
// [PROTOCOL-ALIGNED] Matches gateway's ChallengeCache.Store/Lookup/Delete
const challengeCache = new Map();

// In-memory replay cache keyed on nonce outpoint
// [PROTOCOL-ALIGNED] Matches gateway's ReplayCache.Check/Record
const replayCache = new Set();

/**
 * Creates x402 payment-gating middleware.
 *
 * Usage:
 *   app.get("/api/weather/:city", x402Middleware({ price: 3 }), handler)
 *
 * @param {Object} options
 * @param {number} options.price - Price in satoshis
 * @param {string} [options.description] - Human-readable description
 * @returns {Function} Express middleware
 */
function x402Middleware({ price, description = "" }) {
  return function (req, res, next) {
    // [PROTOCOL-ALIGNED] Header name matches reference: X402-Proof
    // Express lowercases all incoming headers, so we read "x402-proof"
    const proofHeader = req.headers["x402-proof"];

    if (!proofHeader) {
      return issueChallenge(req, res, price, description);
    }

    return verifyProof(req, res, next, proofHeader, price);
  };
}

/**
 * Stage 1: No proof → issue 402 challenge
 *
 * [PROTOCOL-ALIGNED] Challenge structure matches:
 *   /context/x402-gateway-reference-implementation/internal/challenge/types.go
 *
 * [SIMULATED] payee_locking_script_hex and nonce_utxo use random values.
 * In production the gateway uses a real treasury address and leases a
 * real UTXO from a funded nonce pool.
 */
function issueChallenge(req, res, price, description) {
  const now = Math.floor(Date.now() / 1000);

  // [PROTOCOL-ALIGNED] Challenge field names and types match the spec
  const challenge = {
    v: "1",
    scheme: "bsv-tx-v1",
    amount_sats: price,
    // [SIMULATED] Random P2PKH — production uses real treasury address
    payee_locking_script_hex:
      "76a914" + crypto.randomBytes(20).toString("hex") + "88ac",
    // [PROTOCOL-ALIGNED] TTL-based expiry
    expires_at: now + 600,
    // [PROTOCOL-ALIGNED] Request binding fields
    domain: req.headers.host || "localhost:3000",
    method: req.method.toUpperCase(),
    path: req.path,
    query: req.query ? new URLSearchParams(req.query).toString() : "",
    // [NOT YET IMPLEMENTED] Production gateways also include:
    //   req_headers_sha256 — SHA-256 of canonical request headers
    //   req_body_sha256    — SHA-256 of request body
    // Without these, replay protection relies solely on the nonce UTXO.
    // The proof already includes these fields (built by the payment client),
    // but the challenge should too so the gateway can verify the binding.
    // [SIMULATED] Random nonce UTXO — production leases from NoncePool
    nonce_utxo: {
      txid: crypto.randomBytes(32).toString("hex"),
      vout: 0,
      satoshis: 1,
      locking_script_hex:
        "76a914" + crypto.randomBytes(20).toString("hex") + "88ac",
    },
  };

  // [PROTOCOL-ALIGNED] Challenge hash = SHA-256 of canonical JSON
  const challengeHash = crypto
    .createHash("sha256")
    .update(JSON.stringify(challenge))
    .digest("hex");

  // [PROTOCOL-ALIGNED] Cache challenge for later lookup
  challengeCache.set(challengeHash, { challenge, createdAt: now });

  // [PROTOCOL-ALIGNED] TTL cleanup of expired challenges
  for (const [key, val] of challengeCache) {
    if (now - val.createdAt > 900) challengeCache.delete(key);
  }

  // [PROTOCOL-ALIGNED] Base64url encoding of challenge JSON
  const encoded = Buffer.from(JSON.stringify(challenge)).toString("base64url");

  // [PROTOCOL-ALIGNED] Response headers match reference gateway
  res.set({
    "X402-Challenge": encoded,
    "X402-Accept": "bsv-tx-v1",
    "X402-Amount-Sats": String(price),
    "Content-Type": "application/json",
  });

  res.status(402).json({
    error: "Payment Required",
    amount_sats: price,
    description,
    challenge_hash: challengeHash,
    scheme: "bsv-tx-v1",
  });
}

/**
 * Stage 2: Proof present → verify and gate
 *
 * [PROTOCOL-ALIGNED] Verification steps match reference gateway order:
 *   1. Parse proof
 *   2. Check required fields
 *   3. Look up challenge
 *   4. Check expiry
 *   5. Check replay via nonce outpoint
 *   6. Mark nonce spent
 *   7. Delete challenge (single-use)
 *   8. Build receipt
 *
 * [SIMULATED] The following verification steps are SKIPPED:
 *   - Decode rawtx and verify txid matches transaction hash
 *   - Verify nonce UTXO is spent as input in the transaction
 *   - Verify payee output pays >= amount_sats to payee script
 *   - Verify request binding (domain, method, path, query, header/body hashes)
 *   - Mempool acceptance check
 *   - Constant-time string comparison
 *
 * In production, the Go gateway performs all of these checks.
 */
function verifyProof(req, res, next, proofHeader, price) {
  try {
    // [PROTOCOL-ALIGNED] Proof is base64url-encoded JSON
    const proofJson = Buffer.from(proofHeader, "base64url").toString("utf-8");
    const proof = JSON.parse(proofJson);

    // [PROTOCOL-ALIGNED] Required proof fields match Proof type in client-js
    if (!proof.txid || !proof.challenge_sha256 || !proof.rawtx_b64) {
      return res.status(400).json({
        error: "Invalid proof",
        code: "invalid_proof",
      });
    }

    // [PROTOCOL-ALIGNED] Look up original challenge by hash
    const cached = challengeCache.get(proof.challenge_sha256);
    if (!cached) {
      return res.status(400).json({
        error: "Challenge not found or expired",
        code: "challenge_not_found",
      });
    }

    const { challenge } = cached;

    // [PROTOCOL-ALIGNED] Check challenge TTL
    const now = Math.floor(Date.now() / 1000);
    if (challenge.expires_at <= now) {
      challengeCache.delete(proof.challenge_sha256);
      return res.status(402).json({
        error: "Challenge expired",
        code: "expired_challenge",
      });
    }

    // [PROTOCOL-ALIGNED] Replay check via nonce outpoint key
    const nonceKey = `${challenge.nonce_utxo.txid}:${challenge.nonce_utxo.vout}`;
    if (replayCache.has(nonceKey)) {
      return res.status(409).json({
        error: "Payment already used (replay detected)",
        code: "double_spend",
      });
    }

    // [SIMULATED] In production, the gateway also verifies:
    //   - computedTxID matches proof.txid (constant-time)
    //   - transaction spends the nonce UTXO
    //   - transaction pays >= amount_sats to payee script
    //   - request binding matches (domain, method, path, query, headers, body)
    //   - mempool acceptance (if RequireMempoolAccept is set)

    // [PROTOCOL-ALIGNED] Mark nonce as spent (matches NoncePool.MarkSpent)
    replayCache.add(nonceKey);

    // [PROTOCOL-ALIGNED] Delete challenge — single-use
    challengeCache.delete(proof.challenge_sha256);

    // [PROTOCOL-ALIGNED] Receipt = SHA-256(txid + challenge_hash)
    const receiptHash = crypto
      .createHash("sha256")
      .update(proof.txid + proof.challenge_sha256)
      .digest("hex");

    // [PROTOCOL-ALIGNED] Response headers match reference gateway
    res.set({
      "X402-Receipt": receiptHash,
      "X402-Receipt-Time": new Date().toISOString(),
      "X402-Status": "accepted",
      "X402-Amount-Sats": String(price),
    });

    // Attach payment info to request for downstream route handlers
    req.x402 = {
      txid: proof.txid,
      amount: price,
      receipt: receiptHash,
      challengeHash: proof.challenge_sha256,
    };

    next();
  } catch (err) {
    res.status(400).json({
      error: "Malformed proof",
      code: "invalid_proof",
      detail: err.message,
    });
  }
}

module.exports = { x402Middleware };
