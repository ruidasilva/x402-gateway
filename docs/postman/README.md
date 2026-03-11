# x402 Profile B — Postman Collection

This Postman collection demonstrates the full x402 client-orchestrated settlement flow using the true protocol — no demo client helper endpoints, no BSV key management required.

When the gateway runs in Profile B (Template Mode), the 402 challenge includes a pre-signed partial transaction template. Profile B uses `SIGHASH_SINGLE|ANYONECANPAY|FORKID` (0xC3) so the client only needs to extract the template, send it to the delegator for fee sponsorship, broadcast, build a proof header, and retry with proof. The client is pure transport (HTTP + JSON + base64) — no BSV signing is needed.

## What This Collection Demonstrates

This collection walks through the x402 settlement flow with the gateway running in **Profile B (Gateway Template)** mode.

The template from the 402 challenge is sent directly to the delegator's `POST /delegate/x402` endpoint. The delegator adds fee inputs, signs them, and returns the completed transaction. The client builds the X402-Proof header using standard HTTP + JSON + base64 operations.

The five requests demonstrate:

1. **Unpaid request** — triggers a 402 challenge containing the template
2. **Decode challenge & call delegator** — extracts the template, sends to delegator, receives completed tx
3. **Broadcast** — placeholder (skipped in demo mode, required in production)
4. **Build proof & paid retry** — constructs the X402-Proof header and retries the request
5. **Replay protection** — confirms the nonce is consumed and cannot be reused

## How to Use

### 1. Start the gateway and delegator

```bash
# Start gateway (port 8402)
TEMPLATE_MODE=true BROADCASTER=mock DELEGATOR_EMBEDDED=true go run ./cmd/server

# Or start them separately:
TEMPLATE_MODE=true BROADCASTER=mock go run ./cmd/server       # Gateway on :8402
TEMPLATE_MODE=true BROADCASTER=mock go run ./cmd/delegator    # Delegator on :8403
```

The gateway starts on port **8402** by default, the delegator on port **8403**.

### 2. Import the collection

Open Postman and import `x402-profile-b.postman_collection.json` from this directory.

The collection includes all necessary variables pre-configured — no separate environment file is needed.

### 3. Set URLs (if not default)

The collection variables `gatewayUrl` and `delegatorUrl` default to `http://localhost:8402` and `http://localhost:8403` respectively. Update them in the collection variables panel if your services run on different hosts or ports.

If using `DELEGATOR_EMBEDDED=true`, set `delegatorUrl` to the same value as `gatewayUrl`.

### 4. Run requests in order

Execute requests **1 through 5 sequentially**. Each request's test script extracts variables needed by subsequent steps:

| Step | Request | What Happens |
|------|---------|--------------|
| 1 | `GET /v1/expensive` | Receives 402 + X402-Challenge |
| 2-3 | `POST /delegate/x402` | Pre-request: decodes challenge, extracts template. HTTP: sends template to delegator |
| 4 | `GET /health` | Broadcast placeholder (no-op in demo mode) |
| 5-6 | `GET /v1/expensive` + proof | Pre-request: builds X402-Proof header. HTTP: submits proof, receives 200 |
| Bonus | `GET /v1/expensive` + same proof | Tests replay protection |

Open the **Postman Console** (View → Show Postman Console) to see detailed logs from each test script.

## Expected Results

### Step 1 — 402 Payment Required

The gateway returns HTTP 402 with headers:
- `X402-Challenge` — base64url-encoded challenge JSON
- `X402-Accept: bsv-tx-v1`

The decoded challenge contains a `template` field (Profile B) with:
- `rawtx_hex` — the pre-signed partial transaction
- `price_sats` — the payment amount locked in the template

### Steps 2-3 — Decode Challenge & Delegate

The pre-request script decodes the challenge, extracts the template `rawtx_hex`, and computes the challenge SHA-256 hash. The HTTP request sends `{"partial_tx": "<template_hex>"}` to the delegator, which:
- Adds fee inputs (signed with 0xC1)
- Returns `{"completed_tx": "<hex>", "txid": "<hex>"}`

### Step 4 — Broadcast (Demo Mode: Skipped)

In demo mode (mock broadcaster), broadcasting is not needed — the mock broadcaster always reports transactions as visible. In production, the client would broadcast the completed tx to the BSV network.

### Steps 5-6 — Build Proof & Access Resource

The pre-request script builds the X402-Proof header:
- Converts completed tx hex to base64
- Computes challenge SHA-256
- Constructs proof JSON with request binding (domain, method, path, query, header/body hashes)
- Base64url-encodes the proof

The gateway validates the proof and returns:
- HTTP 200 with the gated content
- `X402-Receipt` header confirming payment acceptance

### Bonus — Replay Protection

A replay of the same proof is expected to return 200 (idempotent re-serve of the same challenge), 400 (challenge consumed), or 409 (double spend). Bitcoin consensus guarantees single-spend, providing cryptographic replay protection.

## Profile B Settlement Model

```
Client (Postman)        Gateway (:8402)       Delegator (:8403)     BSV Network
  |                       |                      |                    |
  | 1. GET /protected     |                      |                    |
  |─ ─ ─ ─ ─ ─ ─ ─ ─ ─ >|                      |                    |
  |                       | 402 + Challenge       |                    |
  |                       | (template in body)    |                    |
  |< ─ ─ ─ ─ ─ ─ ─ ─ ─ ─|                      |                    |
  |                       |                      |                    |
  | 2. Decode challenge   |                      |                    |
  |   (client-side)       |                      |                    |
  |                       |                      |                    |
  | 3. POST /delegate/x402|                      |                    |
  |   {partial_tx: hex}   |                      |                    |
  |─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─>|                    |
  |                       |                      | add fee inputs     |
  |                       |                      | sign fee inputs    |
  |< ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ | {completed_tx}     |
  |                       |                      |                    |
  | 4. Broadcast          |                      |                    |
  |─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ >|
  |                       |                      |                    |
  | 5. Build X402-Proof   |                      |                    |
  |   (client-side)       |                      |                    |
  |                       |                      |                    |
  | 6. GET /protected     |                      |                    |
  |   + X402-Proof header |                      |                    |
  |─ ─ ─ ─ ─ ─ ─ ─ ─ ─ >|                      |                    |
  |                       | verify proof          |                    |
  |       200 OK          |                      |                    |
  |< ─ ─ ─ ─ ─ ─ ─ ─ ─ ─|                      |                    |
```

## Collection Variables

| Variable | Description | Set By |
|----------|-------------|--------|
| `gatewayUrl` | Gateway base URL | Pre-configured (`http://localhost:8402`) |
| `delegatorUrl` | Delegator base URL | Pre-configured (`http://localhost:8403`) |
| `challengeEncoded` | Raw base64url challenge string | Step 1 |
| `challengeJSON` | Decoded challenge JSON | Step 2 (pre-request) |
| `challengeSha256` | SHA-256 hash of the challenge JSON | Step 2 (pre-request) |
| `templateRawtxHex` | Template transaction hex from challenge | Step 2 (pre-request) |
| `completedTxHex` | Completed transaction hex from delegator | Step 3 |
| `txid` | Transaction ID from delegator | Step 3 |
| `proofHeader` | Base64url-encoded X402-Proof | Step 5 (pre-request) |
