# x402 Settlement Gateway

## Protocol Status

| | |
|---|---|
| **x402 Protocol** | v1.0-spec (Frozen) |
| **Specification Repository** | https://github.com/ruidasilva/merkleworks-x402-spec |
| **Reference Implementation** | this repository |

This repository provides the reference implementation of the x402 protocol. The canonical protocol specification is maintained separately in the [merkleworks-x402-spec](https://github.com/ruidasilva/merkleworks-x402-spec) repository.

The x402 protocol defines a stateless settlement-gated HTTP authorization model where request execution is conditioned on verifiable economic settlement.

The protocol specification defines:

- HTTP status semantics (402 Payment Required)
- Challenge / proof wire format
- Deterministic request binding rules
- Settlement verification rules

This repository provides a reference gateway implementation used to test and demonstrate the protocol in real deployments.

The implementation intentionally follows the specification hierarchy:

| Level | Scope |
|-------|-------|
| **Tier 0** | Protocol invariants (North Star) |
| **Tier 1** | Wire protocol specification |
| **Tier 2** | Reference implementation architecture |
| **Code** | Implementation |

When implementation behavior diverges from the specification, the specification prevails.

## Overview

The x402 Settlement Gateway implements settlement-gated HTTP execution using the x402 protocol.

The gateway provides:

- **HTTP 402 challenge–proof–retry flow** for protected endpoints
- **Nonce-UTXO issuance** for replay-safe payment challenges
- **Deterministic request binding** using canonical hashing
- **Fee delegation** — delegator adds miner-fee inputs and signs only its own inputs
- **Optional sponsored settlement** — deployment mode may sponsor service payment and/or miner fees, depending on configuration
- **Stateless proof verification** before endpoint execution
- **Configurable acceptance semantics** (mempool visibility or confirmation depth)
- **Operational monitoring** via React dashboard

Replay protection is enforced by UTXO single-use at the network layer. Correctness does not depend on nonce databases, account ledgers, or balance tracking. Redis and in-memory caches exist as operational aids (lease management, pool indexing), not as correctness primitives.

## Protocol Authority

This implementation conforms to the **x402 Protocol Specification** maintained at:

**[merkleworks-x402-spec](https://github.com/ruidasilva/merkleworks-x402-spec)**

The specification is governed by a tiered authority model:

| Tier | Scope |
|------|-------|
| **0** | Frozen protocol invariants — foundational principles that do not change |
| **1** | Wire-level protocol: HTTP headers, challenge/proof format, status codes |
| **2** | Reference implementation architecture: component roles, signing rules, pool management |

**Authority hierarchy**: Tier 0 → Tier 1 → Tier 2 → Code. The specification is normative. Code conforms to the specification, never the reverse.

## Protocol Stewardship

The x402 protocol was authored by Rui Da Silva and is maintained through the canonical specification repository:

https://github.com/ruidasilva/merkleworks-x402-spec

This repository contains the reference gateway implementation used to demonstrate and test the protocol in production environments.

The protocol specification is intentionally maintained independently of any specific implementation to ensure that:

- Multiple compatible implementations can exist
- The protocol remains infrastructure-neutral
- Specification governance remains stable over time

Implementations must conform to the specification. The specification does not change to accommodate implementation behavior.

## Quick Start

### 1. Generate Keys
```bash
make setup
# or
go run ./cmd/keygen
```

### 2. Start the Server
```bash
# Demo mode (in-memory, mock broadcaster)
make demo

# Production (Redis, real broadcaster)
make deploy
```

### 3. Test the Settlement Flow
```bash
make client
# or
go run ./cmd/client http://localhost:8402/v1/expensive
```

### 4. Open Dashboard
Visit: http://localhost:8402/

### Common Issues During Setup

| Symptom | Cause | Fix |
|---------|-------|-----|
| `bind: address already in use` | Port 8402 is occupied | Set `PORT=8403` in `.env` or stop the conflicting process |
| `REDIS_URL: connection refused` | Redis not running | Start Redis (`docker compose up redis -d`) or set `REDIS_ENABLED=false` |
| `No UTXOs available` on first request | Pools not yet seeded | Use `make demo` (auto-seeds) or POST to `/api/v1/treasury/fanout` |
| `broadcaster: dial tcp: lookup ...` | No network access with `woc` or `composite` | Use `BROADCASTER=mock` for offline development |
| `go: module requires go >= 1.25.0` | Go version too old | Install Go 1.25+ from https://go.dev/dl/ |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                   x402 Settlement Gateway                    │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    │
│  │  Gatekeeper │    │  Delegator  │    │  Dashboard  │    │
│  │  (402 flow) │    │ (fee-only)  │    │  (React UI) │    │
│  └──────┬──────┘    └──────┬──────┘    └──────┬──────┘    │
│         │                  │                  │            │
│  ┌──────┴──────────────────┴──────────────────┴──────┐    │
│  │                   UTXO Pools                       │    │
│  │       ┌──────────────┐  ┌──────────────┐          │    │
│  │       │  Nonce UTXO  │  │  Fee UTXO   │           │    │
│  │       │    Pool      │  │    Pool     │            │    │
│  │       └──────────────┘  └──────────────┘          │    │
│  └────────────────────────────────────────────────────┘    │
│                           │                                 │
│              ┌────────────┴────────────┐                   │
│              │    Redis / In-Memory    │                   │
│              │   (operational store)   │                   │
│              └─────────────────────────┘                   │
└─────────────────────────────────────────────────────────────┘
```

### Component Roles

**Gatekeeper**
- Issues challenges containing a nonce UTXO, price, payee, expiry, and request binding
- Binds challenge to request fields via canonical hashing
- Verifies proofs on retry requests
- Does not sign, construct, or modify transactions

**Delegator**
- Validates client-constructed partial transaction structure
- Adds miner-fee inputs from the Fee UTXO Pool
- Signs only its own fee inputs using `SIGHASH_ALL | ANYONECANPAY | FORKID (0xC1)`
- Returns the completed transaction to the client
- Never broadcasts

**Client**
- Constructs and signs the payment portion of the transaction
- Submits the partial transaction to the delegator for fee completion
- Broadcasts the completed transaction to the network
- Retries the original request with the proof header

**Network**
- Enforces UTXO single-use at the consensus layer
- Provides replay protection — a spent nonce UTXO cannot be spent again
- On-chain finality is the ultimate double-spend arbiter

## Settlement Flow

```
Client                          Gateway                      Network
  │                               │                              │
  │  1. GET /v1/expensive         │                              │
  │  ─────────────────────────►   │                              │
  │                               │                              │
  │  2. 402 + X402-Challenge      │                              │
  │     (nonce UTXO, amount,      │                              │
  │      payee, expiry, binding)  │                              │
  │  ◄─────────────────────────   │                              │
  │                               │                              │
  │  3. Build partial TX          │                              │
  │     (spend nonce UTXO,        │                              │
  │      add payee output,        │                              │
  │      sign with 0xC1)          │                              │
  │                               │                              │
  │  4. POST /delegate/x402       │                              │
  │  ─────────────────────────►   │                              │
  │                               │                              │
  │         5. Validate structure  │                              │
  │            Add fee inputs      │                              │
  │            Sign fee inputs     │                              │
  │                               │                              │
  │  6. Return completed TX       │                              │
  │  ◄─────────────────────────   │                              │
  │                               │                              │
  │  7. Broadcast TX              │                              │
  │  ──────────────────────────────────────────────────────────► │
  │                               │                              │
  │  8. GET /v1/expensive         │                              │
  │     + X402-Proof header       │                              │
  │  ─────────────────────────►   │                              │
  │                               │                              │
  │  9. Verify proof → 200 OK     │                              │
  │     + X402-Receipt            │                              │
  │  ◄─────────────────────────   │                              │
```

The client constructs, signs, and broadcasts the transaction. The delegator adds fee inputs and signs only those. It never holds client keys and never broadcasts.

## Configuration

Create a `.env` file (or use `make setup`):

```bash
# Key Management (choose one)
XPRIV=xprv9s21ZrQH143K...          # HD Wallet (recommended)
# or
BSV_PRIVATE_KEY=L5C...              # Single WIF key (legacy)

# Network
BSV_NETWORK=testnet                 # testnet | mainnet
BROADCASTER=mock                    # mock | woc (WhatsOnChain)

# Server
PORT=8402
FEE_RATE=0.001                      # sat/byte (BSV standard)

# Storage (optional — operational aid, not correctness dependency)
REDIS_ENABLED=false
REDIS_URL=redis://localhost:6379

# Pool Settings
NONCE_POOL_SIZE=100
NONCE_LEASE_TTL=300                 # seconds

# Optional
PAYEE_ADDRESS=                      # defaults to nonce pool address
DAILY_FEE_BUDGET=0                  # 0 = unlimited
POOL_REPLENISH_THRESHOLD=500
POOL_OPTIMAL_SIZE=5000
```

## API Reference

### Endpoint Summary

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/expensive` | Example protected endpoint (402-gated) |
| GET | `/health` | Server health and pool statistics |
| POST | `/delegate/x402` | Fee delegation (binary TX) |
| POST | `/api/v1/tx` | Fee delegation (JSON TX) |
| GET | `/api/v1/config` | Get server configuration |
| PUT | `/api/v1/config` | Update server configuration |
| GET | `/api/v1/stats/summary` | Aggregate statistics |
| GET | `/api/v1/stats/timeseries` | Time-series data |
| GET | `/api/v1/revenue` | Revenue statistics |
| GET | `/api/v1/treasury/info` | Treasury and pool info |
| GET | `/api/v1/treasury/utxos` | Treasury UTXOs |
| POST | `/api/v1/treasury/fanout` | Fan-out UTXOs to a pool |
| POST | `/api/v1/treasury/sweep` | Sweep UTXOs to treasury |
| POST | `/api/v1/treasury/sweep-revenue` | Sweep settlement revenue |
| POST | `/api/v1/broadcast` | Broadcast a raw transaction |
| GET | `/api/v1/health/broadcasters` | Broadcaster health (composite mode) |
| POST | `/api/v1/pools/reconcile` | Detect and remove zombie UTXOs |
| GET | `/api/v1/events/stream` | SSE event stream |

### X402 Protocol Headers

| Header | Direction | Description |
|--------|-----------|-------------|
| `X402-Challenge` | Response (402) | Base64url-encoded Challenge JSON |
| `X402-Accept` | Response (402) | Payment scheme: `bsv-tx-v1` |
| `X402-Proof` | Request (retry) | Client payment proof JSON |
| `X402-Receipt` | Response (200) | Payment receipt hash |
| `X402-Receipt-Time` | Response (200) | ISO 8601 timestamp |
| `X402-Status` | Response | `accepted`, `pending`, `rejected`, or `error` |

### 402 Challenge–Proof Flow

**Step 1 — Client sends request without proof:**

```
GET /v1/expensive HTTP/1.1
```

**Step 2 — Gateway responds 402 with challenge:**

```http
HTTP/1.1 402 Payment Required
X402-Challenge: <base64url-encoded JSON>
X402-Accept: bsv-tx-v1

{"status":402,"code":"payment_required","message":"Payment required. See X402-Challenge header."}
```

The decoded `X402-Challenge` contains:

```json
{
  "v": "1",
  "scheme": "bsv-tx-v1",
  "amount_sats": 100,
  "payee_locking_script_hex": "76a914...88ac",
  "expires_at": 1710360000,
  "domain": "localhost:8402",
  "method": "GET",
  "path": "/v1/expensive",
  "query": "",
  "req_headers_sha256": "e3b0c442...",
  "req_body_sha256": "e3b0c442...",
  "nonce_utxo": {
    "txid": "abcd1234...",
    "vout": 0,
    "satoshis": 1,
    "locking_script_hex": "76a914...88ac"
  },
  "template": null,
  "require_mempool_accept": true,
  "confirmations_required": 0
}
```

**Step 3 — Client builds TX, delegates fees, broadcasts, then retries with proof:**

```http
GET /v1/expensive HTTP/1.1
X402-Proof: {"v":"1","scheme":"bsv-tx-v1","txid":"...","rawtx_b64":"...","challenge_sha256":"...","request":{...}}
```

**Step 4 — Gateway verifies and responds:**

```http
HTTP/1.1 200 OK
X402-Receipt: <hex-hash>
X402-Receipt-Time: 2026-03-13T10:00:00Z
X402-Status: accepted
```

**Error status codes from proof verification:**

| Status | Code | Meaning |
|--------|------|---------|
| 400 | `invalid_proof` | Malformed proof or missing fields |
| 400 | `challenge_not_found` | Challenge hash not in cache |
| 402 | `expired_challenge` | Challenge TTL exceeded |
| 402 | `insufficient_amount` | Payment below required amount |
| 403 | `invalid_binding` | Request fields don't match challenge |
| 403 | `invalid_payee` | Payment output doesn't match payee |
| 409 | `double_spend` | Nonce UTXO already spent |
| 503 | `no_utxos_available` | Nonce pool exhausted |

### POST /api/v1/tx — Fee Delegation

Request:
```json
{
  "txJson": {
    "inputs": [
      {"txid": "abcd...", "vout": 0, "satoshis": 1, "scriptSig": ""}
    ],
    "outputs": [
      {"satoshis": 100, "script": "76a914...88ac"}
    ]
  }
}
```

Success response (200):
```json
{
  "success": true,
  "txid": "abcd...",
  "rawtx": "0100000001...",
  "fee": 1,
  "mode": "raw_transaction_returned"
}
```

Error response (400/503):
```json
{"success": false, "error": "description"}
```

### GET /api/v1/config

Response:
```json
{
  "network": "testnet",
  "port": 8402,
  "broadcaster": "composite",
  "feeRate": 0.5,
  "poolReplenishThreshold": 500,
  "poolOptimalSize": 5000,
  "redisEnabled": true,
  "poolSize": 100,
  "leaseTTLSeconds": 300,
  "payeeAddress": "1A1z...",
  "keyMode": "xpriv",
  "nonceAddress": "1Nonce...",
  "feeAddress": "1Fee...",
  "paymentAddress": "1Pay...",
  "treasuryAddress": "1Treas...",
  "templateMode": false,
  "templatePriceSats": 10,
  "feeUTXOSats": 1,
  "profile": "A (Open Nonce)",
  "delegatorUrl": "http://localhost:8403",
  "delegatorEmbedded": true,
  "broadcasterUrl": "https://api.whatsonchain.com",
  "mode": "live",
  "arcUrl": "https://arc.gorillapool.io"
}
```

### PUT /api/v1/config

Request (all fields optional):
```json
{
  "feeRate": 1.0,
  "poolReplenishThreshold": 200,
  "poolOptimalSize": 2000,
  "broadcaster": "composite"
}
```

Response:
```json
{
  "success": true,
  "updated": {"feeRate": 1.0, "broadcaster": "composite"},
  "restart_required": true,
  "restart_reason": "Pool storage differs between demo and live mode."
}
```

### GET /api/v1/stats/summary

```json
{
  "totalRequests": 150,
  "payments": 50,
  "challenges": 40,
  "errors": 5,
  "avgDurationMs": 45.5,
  "totalFeeSats": 5000,
  "uptimeSeconds": 86400.0,
  "noncePool": {"available": 100, "total": 1000, "spent": 900},
  "feePool": {"available": 500, "total": 1000, "spent": 500},
  "paymentPool": {"available": 200, "total": 500, "spent": 300}
}
```

### GET /api/v1/revenue

```json
{
  "payments": 150,
  "totalSats": 15000,
  "lastTxid": "abcd...",
  "unsweptCount": 5,
  "unsweptSats": 500
}
```

### POST /api/v1/treasury/fanout

Request:
```json
{
  "pool": "nonce",
  "count": 100,
  "fundingTxid": "abcd...",
  "fundingVout": 0,
  "fundingScript": "76a914...88ac",
  "fundingSatoshis": 500000,
  "signingKey": "treasury"
}
```

Response:
```json
{"success": true, "txid": "abcd...", "utxoCount": 100, "pool": "nonce"}
```

### POST /api/v1/treasury/sweep

Request:
```json
{
  "signingKey": "treasury",
  "inputs": [
    {"txid": "abcd...", "vout": 0, "script": "76a914...88ac", "satoshis": 100000}
  ]
}
```

Response:
```json
{"success": true, "txid": "abcd...", "inputSats": 100000, "outputSats": 99990, "fee": 10}
```

### POST /api/v1/pools/reconcile

Request: empty POST body.

Response:
```json
{
  "success": true,
  "pools": [
    {"pool": "nonce", "address": "1A...", "checked": 50, "valid": 45, "marked_spent": 5, "error": null},
    {"pool": "fee", "address": "1B...", "checked": 200, "valid": 195, "marked_spent": 5, "error": null}
  ],
  "total_zombies": 10
}
```

### GET /health

```json
{
  "status": "ok",
  "version": "1.0.0",
  "network": "testnet",
  "profile": "A (Open Nonce)",
  "nonce_pool": {"available": 100, "total": 1000, "spent": 900},
  "fee_pool": {"available": 500, "total": 1000, "spent": 500}
}
```

## Project Structure

```
x402-gateway/
├── cmd/
│   ├── server/          # Main HTTP server
│   ├── client/          # Test CLI client
│   ├── keygen/          # Key generation utility
│   └── setup/           # Interactive setup wizard
├── internal/
│   ├── config/          # Environment configuration
│   ├── hdwallet/        # BIP32 HD wallet derivation
│   ├── pool/            # UTXO pool management (Memory/Redis)
│   ├── gatekeeper/      # HTTP 402 middleware and proof verification
│   ├── delegator/       # Fee-input addition and signing (fee-only)
│   ├── feedelegator/    # Fee delegation HTTP API
│   ├── challenge/       # Challenge/proof construction and hashing
│   ├── replay/          # Operational replay cache (in-memory)
│   ├── pricing/         # Dynamic pricing
│   ├── broadcast/       # TX broadcasting (Mock/WhatsOnChain)
│   ├── treasury/        # Pool funding and fan-out
│   └── dashboard/       # React dashboard API
├── dashboard/           # React frontend source
├── tools/
│   └── adversary-harness/  # Adversarial protocol testing
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── go.mod
```

## Key Concepts

### Nonce UTXO Pool
- Collection of 1-satoshi UTXOs used as challenge identity
- Each challenge references exactly one nonce UTXO that the client must spend
- Single-use is enforced by the network: a spent UTXO cannot be spent again
- Leased to clients with a configurable TTL (default 5 minutes)
- Reclaim loop recovers expired leases every 30 seconds (operational, not correctness-critical)

### Fee UTXO Pool
- Collection of 1-satoshi UTXOs consumed by the delegator to pay miner fees
- Delegator adds fee inputs to the client's partial transaction
- Fee inputs are signed with `SIGHASH_ALL | ANYONECANPAY | FORKID (0xC1)`
- This sighash allows the delegator to sign without invalidating the client's existing signatures

### Challenge
Contains:
- Nonce UTXO outpoint (client must spend this)
- Amount (price in satoshis)
- Payee address (settlement destination)
- Expiry (validity period)
- Request binding (canonical hash of method, path, domain, query, headers, body)

### Proof
Contains:
- Complete signed transaction (spending the nonce UTXO)
- Challenge hash reference
- Request binding for verification

### Sighash 0xC1
`SIGHASH_ALL | ANYONECANPAY | FORKID` — the client signs all outputs but only its own input. The delegator can then append fee inputs without breaking the client's signature. The delegator signs its fee inputs the same way.

## Deployment

### Demo Mode (Local Development)

```bash
make demo
```

Starts with in-memory pools, mock broadcaster, and auto-seeded UTXOs. No external dependencies required.

### Docker Compose (Recommended)

```bash
# Generate keys and .env
make setup

# Start gateway + delegator + Redis
docker compose up -d --build
```

The `docker-compose.yml` starts three services:
- **Redis** — operational store for UTXO pool indexing (port 6379)
- **x402-gateway** — main server with 402 middleware and dashboard (port 8402)
- **x402-delegator** — fee delegation service (port 8403)

Environment variables are loaded from `.env`. Redis is configured with health checks and the gateway waits for Redis readiness before starting.

### Manual Docker Build

```bash
docker build -t x402-gateway .
docker build -f Dockerfile.delegator -t x402-delegator .
```

### Production Checklist

| Item | Detail |
|------|--------|
| **Key generation** | Run `make setup` or `go run ./cmd/keygen` to generate an HD wallet (xpriv). Store securely. |
| **Network** | Set `BSV_NETWORK=mainnet` and `BROADCASTER=composite` (GorillaPool ARC + WhatsOnChain fallback) |
| **Redis** | Set `REDIS_ENABLED=true`. Required for pool persistence across restarts. |
| **Pool seeding** | Fund the treasury address, then POST to `/api/v1/treasury/fanout` for nonce and fee pools |
| **Fee budget** | Set `DAILY_FEE_BUDGET` to limit runaway fee spending under load |
| **TLS** | Terminate TLS at a reverse proxy (nginx, Caddy, ALB). The gateway serves plain HTTP. |
| **Monitoring** | Dashboard at `/`, SSE event stream at `/api/v1/events/stream`, health at `/health` |
| **Backups** | Back up `.env` (contains xpriv). Redis data is operational — pools can be re-seeded from treasury. |

## Development

### Prerequisites
- Go 1.25+
- Node.js 20+ (for dashboard)
- Docker & Docker Compose (optional)
- Redis (optional, for persistent pool indexing)

### Build

```bash
# Build all binaries (includes dashboard)
make build

# Run tests
make test

# Lint
make lint

# Clean
make clean
```

### Dashboard Development

```bash
# Start dashboard dev server (hot reload)
make dashboard-dev

# Build dashboard for production
make dashboard-build
```

### Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Build all binaries |
| `make test` | Run tests |
| `make lint` | Run linter |
| `make run` | Start server |
| `make demo` | Start in demo mode |
| `make client` | Run test client |
| `make setup` | Interactive setup |
| `make deploy` | Docker compose deployment |
| `make dashboard-dev` | Dashboard dev server |
| `make dashboard-build` | Build dashboard |
| `make clean` | Clean build artifacts |

## Security Considerations

### Replay Protection
- Nonce UTXOs are single-use at the network consensus layer
- A spent nonce UTXO cannot be included in another valid transaction
- On-chain finality is the ultimate double-spend arbiter
- An in-memory replay cache provides an operational fast-path to reject obvious replays within a process lifetime, but is not a correctness dependency

### Request Binding
- Proofs are bound to the specific request that triggered the challenge
- Binding includes: method, path, domain, query, headers, body
- Prevents proof reuse across different endpoints or request shapes

### Key Management
- HD wallet recommended for production
- Separate derivation paths for nonce UTXO pool, fee UTXO pool, and treasury
- Delegator holds only the fee key — never the client's keys
- Never expose private keys in logs or error messages

### Fee Budget
- Optional daily fee budget limit
- Prevents runaway fee spending if the gateway is under load
- Set `DAILY_FEE_BUDGET` in satoshis

## Troubleshooting

### "No UTXOs available (pool exhausted)"
- UTXO pools need seeding with 1-satoshi UTXOs
- In demo mode with mock broadcaster, pools auto-seed on startup
- For production: fund the treasury address, then POST to `/api/v1/treasury/fanout`
- Use `/api/v1/pools/reconcile` to detect and remove zombie UTXOs that are spent on-chain but still listed as available

### "SSE not supported" / Dashboard disconnected
- The logging middleware must implement `http.Flusher`
- Fixed in recent versions — update to the latest code

### Redis connection failed
- Ensure Redis is running: `docker compose up redis -d`
- Check `REDIS_URL` format: `redis://host:port`
- In Docker: the gateway uses `redis://redis:6379` (Docker DNS). Ensure both services are on the same Docker network
- For local development without Redis: set `REDIS_ENABLED=false` (pools use in-memory storage, no persistence across restarts)

### Broadcaster errors / "dial tcp: lookup ... no such host"
- The `woc` and `composite` broadcasters require internet access
- For offline development: set `BROADCASTER=mock`
- For `composite` mode: GorillaPool ARC is primary, WhatsOnChain is fallback. If ARC is down, the circuit breaker opens automatically and routes through WoC
- Check broadcaster health: GET `/api/v1/health/broadcasters`

### Port conflicts
- Default ports: 8402 (gateway), 8403 (delegator), 6379 (Redis)
- Override in `.env`: set `PORT` and `DELEGATOR_PORT`
- Docker Compose maps these automatically from `.env`

### Transaction rejected by mempool
- `409 double_spend`: the nonce UTXO was already spent — likely a retry of an already-settled request
- `402 expired_challenge`: the challenge TTL (default 5 min) has passed — request a new challenge
- `402 insufficient_amount`: the payment output is less than the challenged amount

## License

This project is licensed under the Apache License, Version 2.0.

See [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome, but the x402 specification hierarchy must be respected.

- **Tier 0** documents define frozen invariants — these are not open for debate
- **Tier 1** documents define protocol wire semantics
- **Tier 2** documents define the reference implementation mapping
- Code and documentation must conform to those documents, never the reverse

Protocol changes should be proposed in the [specification repository](https://github.com/ruidasilva/merkleworks-x402-spec) first.

Implementation pull requests should:
- Include tests where applicable
- Not introduce behavior that contradicts the frozen specification
- Open an issue before proposing major architectural changes
