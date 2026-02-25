# x402 Gateway

A BSV (Bitcoin SV) payment protocol gateway that enables HTTP services to require micropayments via Bitcoin transactions. Implements the x402 protocol specification for Pay-Per-Request microservices.

## Overview

The x402 Gateway provides:
- **HTTP 402 Payment Required** flow for protected endpoints
- **UTXO Pool Management** for nonces and transaction fees
- **Fee Delegation** - server pays miner fees on behalf of clients
- **Replay Protection** - prevents double-spending attacks
- **React Dashboard** - real-time monitoring and management
- **Redis Support** - persistent pools for production deployments

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

### 3. Test the Payment Flow
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
│                        x402 Gateway                         │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    │
│  │  Gatekeeper │    │  Delegator  │    │  Dashboard  │    │
│  │  (402 flow) │    │ (TX settle) │    │  (React UI) │    │
│  └──────┬──────┘    └──────┬──────┘    └──────┬──────┘    │
│         │                  │                  │            │
│  ┌──────┴──────────────────┴──────────────────┴──────┐    │
│  │                    UTXO Pools                      │    │
│  │         ┌────────────┐  ┌────────────┐            │    │
│  │         │ Nonce Pool │  │  Fee Pool  │            │    │
│  │         └────────────┘  └────────────┘            │    │
│  └────────────────────────────────────────────────────┘    │
│                           │                                 │
│              ┌────────────┴────────────┐                   │
│              │    Redis / In-Memory    │                   │
│              └─────────────────────────┘                   │
└─────────────────────────────────────────────────────────────┘
```

## Payment Flow

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
  │     (nonce input + payee)     │                              │
  │                               │                              │
  │  4. POST /delegate/x402       │                              │
  │  ─────────────────────────►   │                              │
  │                               │  5. Add fees, sign, broadcast│
  │                               │  ────────────────────────►   │
  │  6. Return signed TX          │                              │
  │  ◄─────────────────────────   │                              │
  │                               │                              │
  │  7. GET /v1/expensive         │                              │
  │     + X402-Proof header       │                              │
  │  ─────────────────────────►   │                              │
  │                               │                              │
  │  8. 200 OK + X402-Receipt     │                              │
  │  ◄─────────────────────────   │                              │
  │                               │                              │
```

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

### Payment Flow Endpoints

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
│   ├── pool/            # UTXO pool management (Memory/Redis)
│   ├── gatekeeper/      # HTTP 402 middleware & verification
│   ├── delegator/       # Transaction settlement
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

### Nonce Pool
- Collection of 1-satoshi UTXOs
- Each payment requires spending one nonce
- Provides unique identity for each transaction
- Leased to clients with TTL (default 5 minutes)

### Fee Pool
- Collection of 1-satoshi UTXOs for miner fees
- Server adds fee inputs to client transactions
- Uses `SIGHASH_ALL | ANYONECANPAY | FORKID (0xC1)`
- Allows signing without invalidating client signatures

### Delegator
- Core settlement primitive
- Validates partial transactions
- Adds fee inputs and signs them
- Broadcasts to network
- Marks UTXOs as spent

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
- Replay cache prevents double-spend
- Both gatekeeper and delegator check independently

### Request Binding
- Proofs are bound to specific requests
- Includes: method, path, domain, query, headers, body
- Prevents proof reuse across different endpoints

### Key Management
- HD wallet recommended for production
- Separate keys for nonce, fee, and treasury pools
- Never expose private keys in logs

### Fee Budget
- Optional daily fee budget limit
- Prevents runaway spending
- Set `DAILY_FEE_BUDGET` in satoshis

## Troubleshooting

### "No UTXOs available (pool exhausted)"
- Pools need seeding with UTXOs
- In demo mode with mock broadcaster, pools auto-seed
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
