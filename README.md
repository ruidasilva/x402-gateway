# x402 — Stateless settlement-gated HTTP protocol

x402 uses HTTP 402 to require verifiable economic settlement before executing a request. Payment replaces identity: no accounts, no API keys, no subscriptions. The server issues a challenge, the client settles on-chain, and the proof unlocks the resource.

This repository is the **reference implementation** of the x402 protocol (v1.0, frozen). It enforces stateless verification, deterministic request binding, and on-chain settlement gating. The implementation has been validated through strict conformance audit, adversarial execution testing, and cross-language vector verification.

| | |
|---|---|
| **Protocol specification** | [merkleworks-x402-spec](https://github.com/ruidasilva/merkleworks-x402-spec) (authoritative) |
| **Reference implementation** | this repository |
| **Spec version** | 1.0 (frozen) |

When implementation behavior diverges from the specification, the specification prevails.

### Interoperability

x402 interoperability is defined by canonical test vectors. Independent implementations MUST reproduce these vectors byte-for-byte. Deviation indicates non-compliance with x402 v1.0.

- [VECTORS.md](VECTORS.md) -- normative specification of the test vectors
- [testdata/x402-vectors-v1.json](testdata/x402-vectors-v1.json) -- the canonical vector file
- [testdata/verify_vectors.py](testdata/verify_vectors.py) -- reference verifier (Python, zero dependencies)

---

## Start here

### 1. Run the system

```bash
# Docker (production-like: gateway + delegator + Redis)
docker compose up --build

# Or local demo (in-memory, mock broadcaster, no Docker required)
make demo
```

### 2. Verify it works

```bash
curl -i http://localhost:8402/v1/expensive
```

Expected response:

```
HTTP/1.1 402 Payment Required
X402-Challenge: eyJhbW91bnRfc2F0cyI6MTAwLC...
X402-Accept: bsv-tx-v1
```

The `X402-Challenge` header contains a base64url-encoded JSON challenge object. The server requires 100 satoshis to unlock `/v1/expensive`.

### 3. Run the full protocol flow

Open the Developer Playground at `http://localhost:8402` and click **Run Full Flow**. The playground executes all six protocol steps:

1. Requests the protected resource (receives 402 + challenge)
2. Decodes the challenge and extracts the nonce UTXO
3. Calls the delegator to construct and sign the settlement transaction
4. Broadcasts the transaction to the BSV network
5. Builds the proof (binds the transaction to the original request)
6. Retries the request with the `X402-Proof` header

Expected final result: **HTTP 200 OK** with the protected resource.

---

## Protocol flow

```
Client                          Server
  |                               |
  |  GET /v1/expensive            |
  |------------------------------>|
  |                               |
  |  402 + X402-Challenge         |
  |<------------------------------|
  |                               |
  |  [construct tx, sign, broadcast, build proof]
  |                               |
  |  GET /v1/expensive            |
  |  X402-Proof: <base64url>      |
  |------------------------------>|
  |                               |
  |  200 OK                       |
  |<------------------------------|
```

1. **Request** -- Client requests a protected resource.
2. **Challenge** -- Server responds with 402 and an `X402-Challenge` header containing the payment terms: amount, payee script, nonce UTXO, request binding hashes, and expiry.
3. **Settlement** -- Client constructs a BSV transaction spending the nonce UTXO and paying the required amount to the payee.
4. **Delegation** (optional) -- A fee delegator adds miner-fee inputs and signs only its own inputs.
5. **Broadcast** -- Client broadcasts the transaction to the BSV network.
6. **Proof** -- Client retries the request with an `X402-Proof` header containing the transaction, request binding, and challenge reference. Server verifies statelessly and serves the resource.

The server does not trust the client. All verification is performed independently from the proof and on-chain data.

---

## Key properties

- **Stateless verification** -- The server verifies each proof independently. No sessions, no client state, no nonce databases. Correctness derives from on-chain settlement, not server-side tracking.
- **Deterministic request binding** -- Each proof is cryptographically bound to the exact HTTP method, path, query string, body hash, and header hash. A proof for one request cannot unlock a different request.
- **UTXO nonce model** -- Each challenge references a specific nonce UTXO. The payment transaction must spend that UTXO. Bitcoin consensus guarantees single-spend, providing replay protection without persistent state.
- **Mempool-gated execution** -- When `require_mempool_accept` is true (default), the server verifies mempool acceptance before serving the resource. No execution occurs on pending or failed transactions.
- **Client-broadcast model** -- The client MUST broadcast the settlement transaction. The server and delegator never broadcast.
- **Ordered verification** -- The server follows a strict verification sequence (spec Section 7): decode proof, validate challenge hash, verify request binding, check expiry, decode transaction, verify nonce spend, verify payment output, check mempool acceptance.
- **Exact value conservation** -- Implementations MUST preserve exact value conservation in transaction construction. Any remainder MUST be represented as a change output when >= 1 sat. No value may be implicitly discarded into transaction fees. BSV does not enforce a dust threshold.

---

## Test vectors

The file `testdata/x402-vectors-v1.json` contains 9 canonical test vectors. These vectors are normative -- they define the interoperability contract for x402 v1.0. All base64 encodings use base64url (RFC 4648, no padding). See [VECTORS.md](VECTORS.md) for the full specification.

Verify with Go:

```bash
go test ./internal/challenge/ -run TestVectors -v
```

Verify with Python (zero dependencies):

```bash
python3 testdata/verify_vectors.py
```

Expected: `VERDICT: PASS -- all 25 checks passed`

---

## Repository structure

```
internal/gatekeeper/     Middleware: challenge issuance, proof verification, replay protection
internal/challenge/      Challenge struct, canonical JSON (RFC 8785), hashing, binding verification
internal/feedelegator/   Fee delegation: adds miner-fee inputs, signs only its own inputs
internal/pool/           UTXO pool management (nonce, fee, payment) — Redis or in-memory
internal/replay/         LRU replay detection cache (defence-in-depth, not a correctness gate)
internal/broadcast/      BSV transaction broadcasting (GorillaPool ARC, WhatsOnChain, mock)

cmd/server/              Gateway server (HTTP, middleware, dashboard, embedded delegator)
cmd/client/              CLI client (full 402 flow: challenge → tx → delegate → broadcast → proof)
cmd/vecgen/              Canonical test vector generator

client-js/               TypeScript client library (@merkleworks/x402-client)
dashboard/               React developer playground and monitoring dashboard

testdata/                Canonical test vectors, Python verifier, documentation
postman/                 Postman collections for manual protocol testing
```

### Test suites

| Suite | Location | Purpose |
|---|---|---|
| Canonical JSON + vectors | `internal/challenge/*_test.go` | RFC 8785 compliance, golden hash, vector consumption |
| Middleware + verification | `internal/gatekeeper/middleware_test.go` | Replay rejection, tampered output/script detection |
| Adversarial | `internal/gatekeeper/adversarial_test.go` | Binding replay, concurrent duplicate, mempool edge cases |
| E2E protocol flow | `internal/gatekeeper/e2e_test.go` | Full 7-step flow, challenge round-trip, proof round-trip |
| Proof parsing | `internal/gatekeeper/proof_test.go` | Format validation, required fields, status code mapping |
| Cross-language | `testdata/verify_vectors.py` | Independent Python verification of all vectors |

Run all protocol tests:

```bash
go test ./internal/challenge/ ./internal/gatekeeper/ ./internal/replay/ -v
```

---

## Configuration

```bash
cp .env.example .env
# Edit .env — see comments for all options
```

Key variables:

| Variable | Required | Description |
|---|---|---|
| `XPRIV` | yes | BIP32 extended private key (HD wallet) |
| `BROADCASTER` | yes | `mock` (demo), `woc` (WhatsOnChain), `composite` (GorillaPool + WoC) |
| `FEE_RATE` | yes | Fee rate in sat/byte (BSV standard: `0.001`) |
| `BSV_NETWORK` | no | `mainnet` or `testnet` (default: `testnet`) |
| `PORT` | no | HTTP port (default: `8402`) |
| `REDIS_ENABLED` | no | `true` for persistent UTXO pools (default: `false`) |

Full configuration reference: [docs/configuration.md](docs/configuration.md)

---

## Development

### Prerequisites

- Go 1.22+
- Node.js 18+ (for dashboard build)
- Redis 7+ (optional, for persistent pools)

### Build and test

```bash
make build          # Build server + client binaries
make test           # Run all tests
make demo           # Start in demo mode (mock broadcaster, in-memory pools)
make client         # Run CLI client against local server
```

### Protocol constraints

The x402 protocol specification (v1.0) is frozen. Changes to this implementation must preserve:

- Canonical JSON encoding (RFC 8785 sorted keys, no whitespace, integer numbers)
- Test vector compatibility (`testdata/x402-vectors-v1.json` must not change)
- Verification order (invariant V-1: decode, challenge hash, binding, expiry, tx, nonce, payee, mempool)
- Stateless correctness (no persistent state required for verification)
- Replay protection via settlement-layer nonce spend (not server-side tracking)

If a change alters canonical encoding or verification behavior, it must be accompanied by updated test vectors and cross-language verification.

### External API assumptions

**WhatsOnChain (WoC) UTXO endpoint:**

- `200` with `[]` = valid address with no unspent outputs.
- `200` with `[{...}]` = valid unspent output list.
- `404` = endpoint failure or invalid route. MUST be treated as error. The watcher MUST NOT silently clear UTXOs on 404.
- Any non-200 response MUST be treated as a transient failure. Previously fetched UTXOs MUST be preserved until a successful poll replaces them.
- Malformed JSON (e.g. object instead of array) MUST be treated as error.

### Transaction value conservation

Implementations MUST preserve exact value conservation in transaction construction. Any remainder MUST be represented as a change output when >= 1 sat. No value may be implicitly discarded into transaction fees. BSV does not enforce a dust threshold.

---

## Documentation

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | Components, settlement flow, key concepts |
| [Configuration](docs/configuration.md) | Environment variables and `.env` setup |
| [API Reference](docs/api-reference.md) | Endpoints, headers, request/response formats |
| [Deployment](docs/deployment.md) | Demo, Docker, production checklist |
| [Testing](docs/testing/README.md) | Test suites, Postman collections |
| [Troubleshooting](docs/troubleshooting.md) | Common issues and fixes |
| [Transaction Guarantees](docs/transaction-guarantees.md) | Value conservation, change rules, fee model |

### Protocol and governance

| Document | Description |
|---|---|
| [Protocol](PROTOCOL.md) | Specification hierarchy and protocol overview |
| [Governance](GOVERNANCE.md) | Authority model and contribution policy |
| [Contributing](CONTRIBUTING.md) | Development setup and PR process |
| [Security](SECURITY.md) | Vulnerability reporting |
| [Changelog](CHANGELOG.md) | Release history |

### v1.0.1-bsv-fix -- Economic Correctness Release

This release removes all BTC-era dust threshold assumptions and enforces
BSV-correct transaction economics:

- Exact value conservation (`total_inputs = total_outputs + fee + change`)
- Change output created for any remainder >= 1 sat (no implicit discard)
- Deterministic fee model (~1 sat per request at 0.001 sat/byte)
- No implicit fee inflation from discarded change
- Property-tested across 30+ scenarios with 600-value sweep regression guard
- CI regression guard prevents re-introduction of hardcoded thresholds

---

## License

Apache License 2.0 -- see [LICENSE](LICENSE).
