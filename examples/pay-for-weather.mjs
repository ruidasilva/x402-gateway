#!/usr/bin/env node
// x402 Demo — Pay-per-request weather API
//
// Demonstrates the full x402 stateless payment flow:
//   1. Request resource → 402 + challenge
//   2. Decode challenge, extract template
//   3. Send template to delegator for fee completion
//   4. Broadcast completed transaction
//   5. Build proof, retry request → 200 + JSON
//
// Usage:
//   node examples/pay-for-weather.mjs [BASE_URL]
//   Default: http://localhost:8402
//
// Requirements:
//   - Node.js 18+ (uses native fetch + crypto)
//   - x402 gateway running (make demo)

import { createHash } from "node:crypto"

const BASE = process.argv[2] || "http://localhost:8402"
const ENDPOINT = `${BASE}/api/weather`
const DELEGATOR = `${BASE}/delegate/x402`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function sha256hex(data) {
  return createHash("sha256").update(
    typeof data === "string" ? Buffer.from(data, "utf-8") : data,
  ).digest("hex")
}

function base64urlEncode(buf) {
  return Buffer.from(buf).toString("base64url")
}

// Canonical header-binding hash per x402 spec §4.
// Allowlist: accept, content-length, content-type, x402-client, x402-idempotency-key
// Each header: lowercase name, trimmed value, sorted, "name:value\n"
function hashHeaders() {
  const keys = ["accept", "content-length", "content-type", "x402-client", "x402-idempotency-key"]
  const canonical = keys.map(k => `${k}:`).join("\n") + "\n"
  return sha256hex(canonical)
}

function hashBody() {
  return sha256hex("") // empty body for GET
}

// Canonical JSON (RFC 8785): sorted keys, no whitespace, integers without decimals.
function canonicalize(val) {
  if (val === null || val === undefined) return "null"
  if (typeof val === "boolean") return val ? "true" : "false"
  if (typeof val === "number") return Number.isFinite(val) ? String(val) : "null"
  if (typeof val === "string") return JSON.stringify(val)
  if (Array.isArray(val)) return "[" + val.map(canonicalize).join(",") + "]"
  const keys = Object.keys(val).sort()
  const pairs = keys.filter(k => val[k] !== undefined).map(k => JSON.stringify(k) + ":" + canonicalize(val[k]))
  return "{" + pairs.join(",") + "}"
}

// ---------------------------------------------------------------------------
// Main flow
// ---------------------------------------------------------------------------

async function main() {
  console.log(`\n  x402 Demo — Pay-per-request Weather API`)
  console.log(`  ────────────────────────────────────────`)
  console.log(`  Target: ${ENDPOINT}\n`)

  // ── Step 1: Request resource → 402 ──────────────────────────────────
  console.log("Step 1: GET /api/weather")
  const res1 = await fetch(ENDPOINT)
  console.log(`  → ${res1.status} ${res1.statusText}`)

  if (res1.status !== 402) {
    console.log(`  Expected 402, got ${res1.status}. Is the server running?`)
    process.exit(1)
  }

  // ── Step 2: Decode challenge ────────────────────────────────────────
  const challengeB64 = res1.headers.get("x402-challenge")
  if (!challengeB64) { console.error("  No X402-Challenge header"); process.exit(1) }

  const challengeBytes = Buffer.from(challengeB64, "base64url")
  const challenge = JSON.parse(challengeBytes.toString("utf-8"))
  const challengeHash = sha256hex(Buffer.from(canonicalize(challenge), "utf-8"))

  console.log(`\nStep 2: Challenge decoded`)
  console.log(`  Amount:  ${challenge.amount_sats} sat`)
  console.log(`  Path:    ${challenge.path}`)
  console.log(`  Nonce:   ${challenge.nonce_utxo.txid.slice(0, 16)}...`)
  console.log(`  Hash:    ${challengeHash.slice(0, 24)}...`)

  if (!challenge.template) {
    console.log(`\n  ⚠ No template in challenge (Profile A). This demo requires Profile B.`)
    console.log(`  Start the server with TEMPLATE_MODE=true`)
    process.exit(1)
  }

  // ── Step 3: Send template to delegator ──────────────────────────────
  // Profile B: the challenge includes a pre-signed template with the
  // nonce input already signed by the gateway (0xC3 sighash). We send
  // it to the embedded delegator which adds fee inputs and signs them.
  console.log(`\nStep 3: Calling delegator`)
  const delegRes = await fetch(DELEGATOR, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ partial_tx: challenge.template.rawtx_hex }),
  })

  if (!delegRes.ok) {
    const err = await delegRes.text()
    console.error(`  Delegator error ${delegRes.status}: ${err}`)
    process.exit(1)
  }

  const deleg = await delegRes.json()
  const completedTxHex = deleg.completed_tx
  const txid = deleg.txid
  console.log(`  TxID:    ${txid.slice(0, 24)}...`)

  // ── Step 4: Broadcast ───────────────────────────────────────────────
  // In mock mode the gateway's MockBroadcaster accepts anything.
  // In live mode this would go to the BSV network.
  console.log(`\nStep 4: Broadcasting`)
  const bcastRes = await fetch(`${BASE}/api/v1/broadcast`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ txhex: completedTxHex }),
  }).catch(() => null)

  // Broadcast may fail in demo mode (mock). That's okay — the gateway's
  // mempool checker (also mock) will still accept the proof.
  if (bcastRes && bcastRes.ok) {
    console.log(`  → Broadcast accepted`)
  } else {
    console.log(`  → Broadcast skipped (mock mode)`)
  }

  // ── Step 5: Build proof ─────────────────────────────────────────────
  const url = new URL(ENDPOINT)
  const rawtxB64 = Buffer.from(completedTxHex, "hex").toString("base64")

  const proof = {
    v: 1,
    scheme: "bsv-tx-v1",
    challenge_sha256: challengeHash,
    request: {
      method: "GET",
      path: url.pathname,
      query: "",
      req_headers_sha256: hashHeaders(),
      req_body_sha256: hashBody(),
    },
    payment: {
      txid,
      rawtx_b64: rawtxB64,
    },
  }

  const proofHeader = base64urlEncode(JSON.stringify(proof))

  console.log(`\nStep 5: Proof built`)
  console.log(`  Header:  ${proofHeader.slice(0, 40)}... (${proofHeader.length} chars)`)

  // ── Step 6: Retry with proof ────────────────────────────────────────
  console.log(`\nStep 6: Retry with X402-Proof`)
  const res2 = await fetch(ENDPOINT, {
    headers: { "X402-Proof": proofHeader },
  })

  console.log(`  → ${res2.status} ${res2.statusText}`)

  if (res2.status === 200) {
    const data = await res2.json()
    console.log(`\n  ✓ Payment successful!\n`)
    console.log(`  Response:`)
    console.log(`  ${JSON.stringify(data, null, 2).split("\n").join("\n  ")}`)

    const receipt = res2.headers.get("x402-receipt")
    if (receipt) console.log(`\n  Receipt: ${receipt.slice(0, 24)}...`)
  } else {
    const body = await res2.text()
    console.log(`\n  ✗ Payment failed: ${body}`)
    process.exit(1)
  }

  console.log()
}

main().catch(err => { console.error(err); process.exit(1) })
