# x402 Examples Playground

Stateless payment-gated HTTP using the `402 Payment Required` status code.

This repository provides a developer playground demonstrating x402 with six working examples, a visual payment flow inspector, and a CLI demo.

`x402` · `micropayments` · `http-402` · `bsv` · `bitcoin` · `api-payments` · `machine-commerce` · `stateless-api`

---

Every API endpoint in this playground requires a Bitcoin micropayment before it serves a response. The payment flow follows the x402 protocol:

```
HTTP request
  ↓
402 Payment Required (server issues challenge)
  ↓
Payment broadcast (client builds and broadcasts transaction)
  ↓
Proof attached (client retries with X402-Proof header)
  ↓
Response received (server verifies proof, serves resource)
```

No accounts. No API keys. No subscriptions. Just HTTP requests and Bitcoin transactions.

## Architecture

```
Client (browser / curl / AI agent)
  ↓
Express API (examples/server.js — port 3000)
  ↓
x402 Middleware (challenge issuance + proof verification)
  ↓
Resource Handler (weather data, articles, datasets, etc.)
```

The middleware is responsible for two things only:

1. **Issuing challenges** — when no `X402-Proof` header is present, return `402` with an `X402-Challenge`.
2. **Verifying proofs** — when `X402-Proof` is present, validate it and pass through to the route handler.

Route handlers implement all resource logic. The middleware never touches resource data.

## Quick Start

```bash
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000) in your browser.

The playground contains six examples:

| Example | Endpoint | Price | Method |
|---------|----------|-------|--------|
| Weather API | `GET /api/weather/:city` | 3 sats | GET |
| LLM Summarizer | `POST /api/summarize` | 5 sats | POST |
| Paid Website | `GET /api/articles/:slug` | 2 sats | GET |
| Data Marketplace | `GET /api/financial/sp500/history` | 4 sats | GET |
| Knowledge Query | `POST /api/query` | 3 sats | POST |
| Middleware Demo | `GET /api/middleware/echo` | 1 sat | GET |

Each example has a dedicated playground page at `/weather`, `/summarize`, `/articles`, `/data`, `/query`, and `/middleware`.

**Payments are simulated.** The playground demonstrates the full protocol flow — challenge, payment, proof, verification — but uses simulated transactions instead of real BSV. See [Simulation vs Production](#simulation-vs-production) below.

## Preview-First Paid Content

The paid website example (`/articles`) demonstrates a preview-first pattern where free content is available without payment and full content requires a micropayment.

**Free preview:**

```
GET /api/articles/bitcoin-micropayments
→ 200 OK
```

Returns:

```json
{
  "slug": "bitcoin-micropayments",
  "title": "How Bitcoin Micropayments Change the Web",
  "author": "Satoshi Dev",
  "date": "2025-12-01",
  "preview": "Micropayments have long been theorized...",
  "locked": true,
  "unlock_price": 2,
  "unlock_url": "/api/articles/bitcoin-micropayments?unlock=true"
}
```

**Unlock request (triggers payment challenge):**

```
GET /api/articles/bitcoin-micropayments?unlock=true
→ 402 Payment Required
← X402-Challenge: <base64url-encoded challenge>
```

**Retry with proof:**

```
GET /api/articles/bitcoin-micropayments?unlock=true
X402-Proof: <base64url-encoded proof>
→ 200 OK (full article with content field)
```

The middleware handles only challenge issuance and proof verification. The route itself controls preview logic — if `?unlock=true` is absent, the first handler returns the preview directly without invoking the middleware. If `?unlock=true` is present, the first handler calls `next("route")` and the second handler (with middleware) takes over.

## curl Examples

**Weather API — triggers 402 challenge:**

```bash
curl -i http://localhost:3000/api/weather/london
# → HTTP/1.1 402 Payment Required
# → X402-Challenge: eyJ2IjoiMSIsInNjaGVtZS...
# → X402-Amount-Sats: 3
```

**Articles — free preview (no payment needed):**

```bash
curl http://localhost:3000/api/articles/bitcoin-micropayments
# → 200 OK with preview, locked: true
```

**Articles — unlock request (triggers 402):**

```bash
curl -i http://localhost:3000/api/articles/bitcoin-micropayments?unlock=true
# → HTTP/1.1 402 Payment Required
```

**Data Marketplace — triggers 402:**

```bash
curl -i http://localhost:3000/api/financial/sp500/history
# → HTTP/1.1 402 Payment Required
# → X402-Amount-Sats: 4
```

The frontend playground at `http://localhost:3000` automatically performs the full payment flow — challenge, simulated payment, proof, and verified response — so you can see every step visually.

## CLI Demo

The CLI demo runs the full x402 payment flow from the terminal. Start the server first (`npm run dev`), then:

```bash
node examples/cli-demo.js weather london
```

```
  → Requesting GET http://localhost:3000/api/weather/london
  ← 402 Payment Required
    Amount: 3 sats
  → Paying 3 sats (simulated)
  → Broadcasting transaction a1b2c3d4e5f6...
  ← Payment accepted
  ← 200 OK receipt: 9f8e7d6c5b4a...

  London: 12°C, Cloudy
  Humidity: 78% · Wind: 15 kph
```

All six examples are supported:

```bash
node examples/cli-demo.js weather tokyo
node examples/cli-demo.js summarize "Bitcoin enables micropayments on the web."
node examples/cli-demo.js articles bitcoin-micropayments
node examples/cli-demo.js data sp500
node examples/cli-demo.js query "What is x402?"
node examples/cli-demo.js middleware echo
```

## x402Fetch — Minimal Protocol Client

`examples/client/x402Fetch.js` is a minimal reference client (~70 lines) that performs the x402 loop automatically:

```javascript
const { x402Fetch } = require("./examples/client/x402Fetch");

const response = await x402Fetch("http://localhost:3000/api/weather/london");
console.log(await response.json());
```

The client handles: request → detect 402 → pay → attach proof → retry.

It does **not** implement wallets, UTXO management, balances, or accounting.

### Mock vs Live mode

Set the `X402_MODE` environment variable:

| Mode | Behaviour |
|------|-----------|
| `mock` (default) | Uses the server's `/api/x402/simulate-payment` endpoint |
| `live` | Placeholder for real `@merkleworks/x402-client` integration |

```bash
# Default — uses simulation
X402_MODE=mock node examples/cli-demo.js weather london

# Future — real payments (not yet configured)
X402_MODE=live node examples/cli-demo.js weather london
```

## Payment Flow Inspector

The playground UI includes a real-time Payment Flow Inspector that visualizes each step of the x402 protocol as it happens. Click "Send Request" on any example to see:

1. **Request Sent** — the original HTTP request
2. **402 Payment Required** — the challenge from the server
3. **Payment Broadcast** — transaction built and broadcast
4. **Proof Attached** — proof header constructed
5. **Response Received** — verified response from the server

Raw HTTP headers are shown at each step so developers can understand the wire protocol.

## How the Payment Flow Works

### 1. Request Without Payment

```
GET /api/weather/london
→ 402 Payment Required
← X402-Challenge: <base64url-encoded challenge>
← X402-Accept: bsv-tx-v1
← X402-Amount-Sats: 3
```

### 2. Client Processes Payment

The client parses the challenge (using `parseChallenge()` from client-js), builds a transaction, delegates for fee-funding, broadcasts, and constructs a proof.

### 3. Retry With Proof

```
GET /api/weather/london
→ X402-Proof: <base64url-encoded proof>
← 200 OK
← X402-Receipt: <receipt hash>
← X402-Receipt-Time: <ISO timestamp>
← X402-Status: accepted
```

## Simulation vs Production

This playground **simulates** the parts of the x402 flow that require live BSV infrastructure:

| Simulated | What it does | Production replacement |
|-----------|-------------|----------------------|
| Transaction building | Random bytes | `buildPartialTransaction()` from `@merkleworks/x402-client` |
| Fee delegation | `SimulatedDelegator` returns random tx | `HttpDelegator` pointing at a real delegator service |
| Broadcasting | `SimulatedBroadcaster` returns immediately | `WoCBroadcaster` broadcasting to WhatsOnChain |
| txid computation | Random 32 bytes | Computed from actual transaction double-SHA256 |
| Transaction verification | Proof structure checked, tx not decoded | Go gateway decodes rawtx, verifies signatures |
| Nonce/payee verification | Skipped | Gateway checks nonce spend + payee output |

Everything else is **protocol-aligned** — real 402 status codes, real challenge/proof structures, real header names, real `parseChallenge()` from client-js, real SHA-256 hashing, real challenge caching with TTL, real replay protection via nonce outpoints.

### Switching to real payments

**Client side** — replace the simulation layer with the real `X402Client`:

```javascript
import { X402Client, HttpDelegator, WoCBroadcaster, WOC_MAINNET } from "@merkleworks/x402-client"

const client = new X402Client({
  delegator: new HttpDelegator("https://your-delegator.example.com/delegate"),
  broadcaster: new WoCBroadcaster(WOC_MAINNET),
})

// The client handles the entire flow automatically:
// request → detect 402 → build tx → delegate → broadcast → retry with proof
const response = await client.fetch("https://api.example.com/weather/london")
```

**Server side** — replace `x402-middleware.js` with the production Go gateway from `/context/x402-gateway-reference-implementation` and proxy requests through it.

## Adding x402 to Your Own API

Use the Express middleware to gate any endpoint:

```javascript
const { x402Middleware } = require("./examples/shared/x402-middleware");

app.get("/api/my-endpoint",
  x402Middleware({ price: 3, description: "My endpoint — 3 sats" }),
  (req, res) => {
    // req.x402 contains payment info (txid, receipt, etc.)
    res.json({ data: "protected content", payment: req.x402 });
  }
);
```

## Project Structure

```
x402-examples-workspace/
├── package.json                           # npm install / npm run dev
├── README.md
├── examples/
│   ├── server.js                          # Main Express server (port 3000)
│   ├── cli-demo.js                        # CLI demo — terminal payment flow
│   ├── client/
│   │   └── x402Fetch.js                   # Minimal x402 protocol client
│   ├── shared/
│   │   ├── x402-middleware.js             # Gateway simulation middleware
│   │   ├── x402-client-adapter.js         # Adapter wrapping real client-js
│   │   └── x402-payment-simulator.js      # Payment simulation (uses adapter)
│   ├── paid-api-weather/routes.js         # Weather API (3 sats)
│   ├── llm-summarizer/routes.js           # Text summarizer (5 sats)
│   ├── paid-website/routes.js             # Paid articles — preview-first (2 sats)
│   ├── data-marketplace/routes.js         # Financial datasets (4 sats)
│   ├── knowledge-query/routes.js          # Knowledge Q&A (3 sats)
│   └── middleware-demo/routes.js          # Express middleware demo (1-10 sats)
├── frontend-playground/
│   └── public/
│       ├── index.html                     # SPA — 6 example pages + landing
│       ├── styles.css                     # Dark theme
│       └── app.js                         # Payment flow logic + inspector
└── context/                               # Reference implementations (read-only)
    └── x402-gateway-reference-implementation/
        └── client-js/                     # @merkleworks/x402-client (imported via adapter)
```

## Protocol Headers Reference

All header names match the reference implementation exactly:

| Header | Direction | Purpose |
|--------|-----------|---------|
| `X402-Challenge` | Response (402) | Base64url-encoded challenge JSON |
| `X402-Accept` | Response (402) | Supported payment scheme (`bsv-tx-v1`) |
| `X402-Amount-Sats` | Response | Price in satoshis |
| `X402-Proof` | Request (retry) | Base64url-encoded proof JSON |
| `X402-Receipt` | Response (200) | SHA-256(txid + challenge_hash) |
| `X402-Receipt-Time` | Response (200) | ISO 8601 timestamp of receipt |
| `X402-Status` | Response | `accepted`, `pending`, `rejected`, or `error` |

## Related repositories & docs

- [Merkleworks x402 reference implementation docs + live demo](https://x402.merkleworks.io/)
- [Merkleworks x402 GitBook](https://x402.gitbook.io/x402)
- [x402 protocol specification](https://github.com/ruidasilva/merkleworks-x402-spec)
- [`merkleworks/x402-developer-skills`](https://github.com/merkleworks/x402-developer-skills)
