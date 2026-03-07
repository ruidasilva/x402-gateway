# x402 Settlement Gateway

A BSV (Bitcoin SV) settlement-gated HTTP authorization protocol. The gateway intercepts requests to protected endpoints, issues cryptographic challenges, and verifies on-chain settlement proofs before granting access. Implements the x402 protocol specification for settlement-gated microservices.

## Overview

The x402 Settlement Gateway provides:
- **HTTP 402 Settlement-Gated Authorization** for protected endpoints
- **Nonce Source Management** for challenge identity and replay prevention
- **Fee Delegation** - server adds miner fee inputs to client-constructed transactions
- **Replay Protection** - prevents double-spending attacks
- **React Dashboard** - real-time monitoring and management
- **Redis Support** - persistent nonce sources for production deployments

## Protocol Authority

This implementation conforms to the **x402 Protocol Specification** maintained at:

**[merkleworks-x402-spec](https://github.com/ruidasilva/merkleworks-x402-spec)**

The specification repository contains the normative documents that govern this codebase:

| Document | Authority |
|----------|-----------|
| `north-star.md` | Tier 0 — Philosophical foundation and design invariants |
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
│  │                  Nonce Sources                     │    │
│  │       ┌──────────────┐  ┌────────────┐            │    │
│  │       │ Nonce Source │  │ Fee Source │             │    │
│  │       └──────────────┘  └────────────┘            │    │
│  └────────────────────────────────────────────────────┘    │
│                           │                                 │
│              ┌────────────┴────────────┐                   │
│              │    Redis / In-Memory    │                   │
│              └─────────────────────────┘                   │
└─────────────────────────────────────────────────────────────┘
```

## Settlement Flow

```
Client                          Gateway                      Blockchain
  │                               │                              │
  │  1. GET /v1/expensive         │                              │
  │  ─────────────────────────►   │                              │
  │                               │                              │
  │  2. 402 + X402-Challenge      │                              │
  │  ◄─────────────────────────   │                              │
  │                               │                              │
  │  3. Build partial TX          │                              │
  │     (nonce input + payee,     │                              │
  │      signed with 0xC1)        │                              │
  │                               │                              │
  │  4. POST /delegate/x402       │                              │
  │  ─────────────────────────►   │                              │
  │                               │  5. Add fee inputs, sign     │
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
  │  9. 200 OK + X402-Receipt     │                              │
  │  ◄─────────────────────────   │                              │
  │                               │                              │
```

**Key**: The client constructs, signs, and broadcasts the transaction. The gateway's delegator only adds fee inputs and signs those — it never holds the client's keys and never broadcasts.

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

# Storage (optional)
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
| `/nonce/lease` | GET | Allocate a nonce UTXO |
| `/delegate/x402` | POST | Fee delegation for partial TX |
| `/health` | GET | Server health and pool stats |

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
| `/api/v1/treasury/fanout` | POST | Create UTXOs from funding |
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
│   ├── pool/            # Nonce source management (Memory/Redis)
│   ├── gatekeeper/      # HTTP 402 middleware & verification
│   ├── delegator/       # Fee-only settlement (adds fee inputs to client TX)
│   ├── feedelegator/    # Fee delegation API
│   ├── challenge/       # Challenge/proof protocol
│   ├── replay/          # Replay attack prevention
│   ├── pricing/         # Dynamic pricing
│   ├── broadcast/       # TX broadcasting (Mock/WhatsOnChain)
│   ├── treasury/        # Pool funding & fan-out
│   └── dashboard/       # React dashboard API
├── dashboard/           # React frontend source
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── go.mod
```

## Key Concepts

### Nonce Source
- Collection of 1-satoshi UTXOs that provide challenge identity
- Each settlement requires spending one nonce UTXO
- Prevents replay attacks — each nonce is single-use
- Leased to clients with TTL (default 5 minutes)
- Reclaim loop recovers expired leases every 30 seconds

### Fee Source
- Collection of 1-satoshi UTXOs for miner fees
- Delegator adds fee inputs to client-constructed partial transactions
- Uses `SIGHASH_ALL | ANYONECANPAY | FORKID (0xC1)`
- Allows the delegator to sign fee inputs without invalidating client signatures

### Delegator
- Core settlement primitive
- Validates client-constructed partial transactions
- Adds fee inputs and signs them (0xC1 sighash)
- Returns the completed transaction to the client
- Client is responsible for broadcasting to network
- Marks fee UTXOs as spent

### Gatekeeper
- HTTP middleware for 402 flow
- Issues challenges with nonce UTXOs
- Verifies proofs (does NOT call delegator)
- Validates request binding (method, path, domain, body, headers)
- Prevents replay attacks

### Challenge
Contains:
- Nonce UTXO (client must spend)
- Amount (price in satoshis)
- Payee (payment destination)
- Expiry (validity period)
- Request binding (prevents replay across endpoints)

### Proof
Contains:
- Complete signed transaction
- Challenge hash reference
- Request binding verification

## Docker Deployment

### With Local go-sdk Dependency

The Dockerfile is configured to include the local `go-sdk` from the parent directory:

```bash
# From x402-gateway directory
docker compose up -d --build
```

The `docker-compose.yml` sets:
- Build context to parent directory
- Redis for persistent pools
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
- Redis (optional, for production)

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
- Nonce UTXOs are single-use
- In-memory replay cache prevents obvious replays within a process lifetime
- On-chain finality is the ultimate double-spend arbiter
- Delegator checks nonce outpoint against replay cache before adding fees

### Request Binding
- Proofs are bound to specific requests
- Includes: method, path, domain, query, headers, body
- Prevents proof reuse across different endpoints

### Key Management
- HD wallet recommended for production
- Separate keys for nonce source, fee source, and treasury
- Delegator only holds the fee key — never the client's nonce or payment keys
- Never expose private keys in logs

### Fee Budget
- Optional daily fee budget limit
- Prevents runaway spending
- Set `DAILY_FEE_BUDGET` in satoshis

## Troubleshooting

### "No UTXOs available (pool exhausted)"
- Nonce sources need seeding with 1-satoshi UTXOs
- In demo mode with mock broadcaster, nonce sources auto-seed
- For production, use Treasury fan-out or fund manually

### "SSE not supported" / Dashboard disconnected
- The logging middleware must implement `http.Flusher`
- Fixed in recent versions

### Redis connection failed
- Ensure Redis is running
- Check `REDIS_URL` format: `redis://host:port`
- Verify network connectivity in Docker

## License

[Your License Here]

## Contributing

[Contribution Guidelines]
