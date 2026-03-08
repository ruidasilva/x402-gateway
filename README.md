# x402 Settlement Gateway

A reference implementation of the x402 settlement-gated HTTP protocol using Bitcoin SV.

The gateway intercepts requests to protected endpoints, issues cryptographic challenges backed by nonce UTXOs, and verifies on-chain settlement proofs before granting access.

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

The specification repository contains the normative documents that govern this codebase:

| Document | Authority |
|----------|-----------|
| `north-star.md` | Tier 0 — Frozen invariants and constitutional doctrine |
| `protocol-spec.md` | Tier 1 — Wire-level protocol: HTTP headers, challenge/proof format, status codes |
| `reference-impl-spec.md` | Tier 2 — Implementation architecture: component roles, signing rules, pool management |

**Authority hierarchy**: Tier 0 → Tier 1 → Tier 2 → Code. Documents are normative. Code conforms to documents, never the reverse. When code contradicts a spec document, the code is wrong.

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

## API Endpoints

### Protected Endpoints (402 Gated)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/expensive` | GET | Example protected resource (100 sats) |

### Settlement Flow Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/nonce/lease` | GET | Allocate a nonce UTXO for challenge construction |
| `/delegate/x402` | POST | Fee delegation — add fee inputs to client partial TX |
| `/health` | GET | Server health and pool statistics |

### Fee Delegator API (Node.js Compatible)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/tx` | POST | Fee delegation with JSON TX |
| `/api/utxo/stats` | GET | Pool statistics |
| `/api/utxo/health` | GET | Pool health status |
| `/api/health` | GET | API uptime |

### Dashboard API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/config` | GET/PUT | Server configuration |
| `/api/v1/stats/summary` | GET | Aggregate statistics |
| `/api/v1/stats/timeseries` | GET | Time-series data |
| `/api/v1/treasury/info` | GET | Treasury status |
| `/api/v1/treasury/fanout` | POST | Create UTXOs from funding TX |
| `/api/v1/events/stream` | GET | SSE event stream |

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

## Docker Deployment

### With Local go-sdk Dependency

The Dockerfile is configured to include the local `go-sdk` from the parent directory:

```bash
# From x402-gateway directory
docker compose up -d --build
```

The `docker-compose.yml` sets:
- Build context to parent directory
- Redis for pool indexing (operational store)
- Environment variables from `.env`

### Manual Docker Build

```bash
# From parent directory (where go-sdk and x402-gateway both exist)
docker build -f x402-gateway/Dockerfile -t x402-gateway .
```

## Development

### Prerequisites
- Go 1.21+
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
- For production, use Treasury fan-out or fund manually

### "SSE not supported" / Dashboard disconnected
- The logging middleware must implement `http.Flusher`
- Fixed in recent versions

### Redis connection failed
- Ensure Redis is running
- Check `REDIS_URL` format: `redis://host:port`
- Verify network connectivity in Docker

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
