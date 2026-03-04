# x402 Adversary Harness

Standalone adversarial test tool that simulates hostile network and miner behaviour against a running x402 gateway. It validates the robustness of the replay cache, nonce reservation, delegation endpoint, mempool gating, and fee pool behaviour — without modifying any gateway code.

## Scenarios

### 1. Miner Ordering Variance (`-adversary=mining`)

Simulates competing transactions with different fee levels racing to spend the same nonce UTXO.

**Attack model:**
- Acquire a challenge (nonce UTXO allocated)
- Fire 3 concurrent delegation requests with the same nonce (txA, txB, txC)
- Only one should win the atomic `TryReserve` race
- Submit a fake "competing" proof with a different txid

**Protocol invariants validated:**
- `TryReserve` is atomic — only one concurrent reservation wins
- Losers get 409 (double_spend) or 202 (nonce_pending)
- Proof with a non-committed txid is rejected
- Replay cache blocks duplicate nonce usage

**Metrics:** `miner_order_tests`, `double_spend_detected`, `nonce_pending`, `fake_proof_rejected`

### 2. Mempool Suppression (`-adversary=mempool`)

Simulates mempool visibility failures where transaction broadcast status doesn't match reality.

**Attack model:**
- **Case A (false negative):** Submit proof with plausible but non-existent txid — gateway's mempool check should fail
- **Case B (false positive):** Submit completely fabricated proof with wrong challenge hash, method, path — binding check should fail
- **Case C (delayed propagation):** Submit proof immediately, wait 0.5-2.5s, retry — tests retry semantics

**Protocol invariants validated:**
- Gateway rejects proofs with unknown txids
- Challenge binding (domain, method, path, body hash) is enforced
- Invalid rawtx deserialization is caught
- Expired challenges are rejected after TTL

**Metrics:** `mempool_false_negative_tests`, `false_negative_rejected`, `false_positive_rejected`, `delayed_propagation_tests`

### 3. Delayed Broadcast (`-adversary=broadcast`)

Simulates scenarios where the delegator returns a signed transaction but broadcast is delayed or never happens.

**Attack model:**
- **Case A (immediate):** Delegate + submit proof immediately
- **Case B (delayed):** Delegate + wait 5-30s + submit proof (may hit challenge TTL)
- **Case C (never):** Delegate but never submit proof (orphaned transaction)

**Protocol invariants validated:**
- Challenges expire after 5-minute TTL
- Expired proofs are correctly rejected (402 vs 200)
- Orphaned delegations don't permanently leak pool resources
- Pool reclaim loop recovers leaked leases

**Metrics:** `broadcast_immediate_success`, `broadcast_delayed_expired`, `orphaned_tx`, `proof_retry_success`

### 4. Fee Delegation Abuse (`-adversary=abuse`)

Hammers the delegation and challenge endpoints with 4 attack phases.

**Phase 1 — Challenge flood:** N concurrent workers acquire challenges as fast as possible, draining the nonce pool.

**Phase 2 — Concurrent same-nonce (RACE-01):** Get one challenge, fire 50 concurrent delegation requests with the same nonce. The RACE-01 fix (`TryReserve/Commit/Release`) must ensure exactly one wins.

**Phase 3 — Duplicate delegation replay:** Delegate once, then try the same nonce again. Second attempt must be rejected (409 or 202).

**Phase 4 — Mid-flight cancellation:** Start delegation, cancel the HTTP request mid-flight. The `defer Release()` unwind must free the reserved nonce.

**Protocol invariants validated:**
- Pool exhaustion returns proper error (503 or 402 with no nonces)
- RACE-01 atomic reservation prevents concurrent bypass
- Replay cache blocks duplicate nonces after commit
- Cancelled requests release reserved nonces via defer

**Metrics:** `nonce_pending`, `replay_cache_conflicts`, `nonce_pool_exhausted`, `delegation_cancelled`

## How to Run

### Prerequisites

1. Gateway running at `http://localhost:8402`
2. For best results, run in **demo mode** (`BROADCASTER=mock`) so pools are auto-seeded

### Start gateway in demo mode

```bash
# Docker
docker compose run -d --service-ports -e BROADCASTER=mock --name x402-test x402-gateway

# Local
BROADCASTER=mock go run ./cmd/server
```

### Run all scenarios

```bash
go run ./tools/adversary-harness -adversary=all -clients=10 -duration=60s
```

### Run a single scenario

```bash
go run ./tools/adversary-harness -adversary=mining -clients=20 -duration=30s
go run ./tools/adversary-harness -adversary=mempool -clients=10 -duration=30s
go run ./tools/adversary-harness -adversary=broadcast -clients=5 -duration=60s
go run ./tools/adversary-harness -adversary=abuse -clients=50 -duration=30s
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-adversary` | `all` | Scenario: `mining`, `mempool`, `broadcast`, `abuse`, `all` |
| `-clients` | `10` | Concurrent workers per scenario |
| `-duration` | `30s` | Total test duration (split equally across scenarios) |
| `-url` | `http://localhost:8402` | Gateway base URL |
| `-verbose` | `false` | Enable debug logging |

## Expected Behaviour

### PASS

```
RESULT: PASS (all protocol invariants held)
```

All adversarial attacks were correctly rejected. No fake proofs were accepted, no duplicate delegations succeeded, and no resource leaks occurred.

### FAIL

```
⚠️  CRITICAL FAILURE: CRITICAL_fake_proof_accepted = 3
RESULT: FAIL (1 critical vulnerabilities detected)
```

Any metric prefixed with `CRITICAL_` indicates a protocol invariant violation:

| Critical Metric | Meaning |
|----------------|---------|
| `CRITICAL_fake_proof_accepted` | Gateway accepted a proof with a fabricated txid |
| `CRITICAL_false_negative_accepted` | Mempool gating bypassed with non-existent tx |
| `CRITICAL_false_positive_accepted` | Fabricated proof with wrong binding accepted |
| `CRITICAL_duplicate_accepted` | Same nonce used twice for delegation |
| `CRITICAL_delayed_accepted_*` | Fake proof accepted on immediate or retry |

## What Each Attack Validates

| Attack | Validates |
|--------|-----------|
| Competing delegations (same nonce) | `TryReserve` atomicity (RACE-01 fix) |
| Fake txid in proof | Mempool gating, challenge cache binding |
| Wrong method/path in proof | Request binding verification |
| Expired challenge submission | TTL enforcement |
| Nonce pool flood | Graceful degradation under exhaustion |
| Duplicate delegation | Replay cache `Check()` + `Record()` |
| Mid-flight cancellation | `defer Release()` unwind path |
| Orphaned transaction | Pool reclaim loop recovery |

## Metrics Output

Metrics are printed every 5 seconds during the run and once at completion:

```
╔══════════════════════════════════════════════╗
║  ADVERSARY HARNESS STATUS  (30s elapsed)
╠══════════════════════════════════════════════╣
║  challenges_requested                    100  ║
║  delegation_rejected_400                 300  ║
║  double_spend_detected                    50  ║
║  miner_order_tests                       150  ║
║  nonce_pending                            25  ║
╠══════════════════════════════════════════════╣
║  avg_delegation                          1ms  ║
╚══════════════════════════════════════════════╝
```

## Architecture

```
tools/adversary-harness/
├── main.go              # CLI entrypoint, scenario orchestration
├── client/
│   └── gateway_client.go  # HTTP client for all gateway endpoints
├── metrics/
│   └── metrics.go         # Thread-safe atomic counters + reporter
├── scenarios/
│   ├── miner_ordering.go      # Scenario 1: competing transactions
│   ├── mempool_suppression.go # Scenario 2: visibility failures
│   ├── delayed_broadcast.go   # Scenario 3: timing attacks
│   └── fee_abuse.go           # Scenario 4: concurrent hammering
└── README.md
```

The harness is a **black-box** testing tool — it only uses the gateway's public HTTP API. No gateway code is modified, no Redis is accessed directly, and all adversarial behaviour is simulated client-side.
