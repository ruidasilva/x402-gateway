# Audit Reference — x402 Settlement Gateway

Version: v1.0.1-bsv-fix
Commit: 1a54d60

---

## System Purpose

x402 is a stateless settlement-gated HTTP gateway. It issues payment
challenges (HTTP 402), verifies on-chain settlement proofs, and gates
resource access. No accounts, API keys, or subscriptions are used.
Authorization derives entirely from verifiable economic settlement.

---

## Core Invariants

### Value Conservation

Every finalised transaction MUST satisfy:

    total_inputs = total_outputs + miner_fee

Where `total_outputs` includes both the payment output and any change
output. No satoshis are ever unaccounted for.

**Enforcement:** Pre-return validation in `internal/delegator/delegator.go`
returns an error if `total_inputs < total_outputs` and logs a warning
if `actual_miner_fee != estimated_fee` with no change output.

### Change Rule

    remainder = total_inputs - payment_output - miner_fee
    if remainder >= 1 sat: change output MUST be created
    if remainder == 0: no change output
    if remainder < 0: transaction MUST be rejected

BSV does not enforce a dust threshold. No minimum output value is
applied beyond 1 satoshi. This implementation contains no hardcoded
threshold (the BTC-era 546-sat threshold was removed in v1.0.1).

**Enforcement:** CI regression guard in `.github/workflows/ci.yml`
fails the build if `> 546`, `>= 546`, `dust_threshold`, or
`dustThreshold` appear in Go source files (excluding tests).

### No Implicit Value Loss

The only satoshis going to miners are the calculated `miner_fee`.
All other value MUST be represented as transaction outputs. If the
actual miner fee differs from the estimate and no change output
exists, the system logs a structured warning.

---

## Transaction Model

A standard x402 Profile B transaction:

    Inputs:
      [0] nonce UTXO    — 1 sat, signed 0xC3 by gateway
      [1] fee UTXO      — N sats, signed 0xC1 by delegator

    Outputs:
      [0] payment output — amount_sats to payee locking script
      [1] change output  — remainder to delegator address (if >= 1 sat)

Input count is typically 2. Additional fee inputs are permitted when
a single fee UTXO does not cover payment plus miner fee.

### Fee Calculation

    fee = max(1, floor(transaction_size_bytes * fee_rate))
    fee_rate = 0.001 sat/byte (BSV standard: 1 sat/KB)

A typical 2-input, 1-output transaction is ~340 bytes. At standard
rate: `340 * 0.001 = 0.34`, rounded up to 1 sat.

---

## Determinism

Given identical inputs (nonce UTXO, fee UTXOs, payment amount, fee
rate), transaction construction produces identical outputs. No
randomness or non-determinism exists in output selection, change
calculation, or fee estimation.

---

## External Dependency Assumptions

### WhatsOnChain (WoC) UTXO API

| Response | Interpretation |
|---|---|
| `200` with `[]` | Valid address, no unspent outputs |
| `200` with `[{...}]` | Valid unspent output list |
| `404` | Endpoint failure — treated as error |
| Non-200 | Transient failure — previous UTXOs preserved |
| Malformed JSON | Parse error — treated as error |

The watcher MUST NOT silently clear UTXOs on any error condition.
Previously fetched UTXOs are preserved until a successful poll
replaces them.

---

## Security Posture

- **No hidden thresholds.** No hardcoded constants determine whether
  change is created or discarded.
- **Stateless replay protection.** Replay protection derives from
  UTXO single-spend at the settlement layer (Bitcoin consensus).
  The replay cache is a performance optimisation only and is not a
  correctness gate.
- **Client-broadcast model.** The server and delegator never broadcast
  transactions. The client is responsible for broadcast.
- **Constant-time comparisons.** All security-sensitive string
  comparisons (txid, script hex, challenge hash) use `crypto/subtle`.

---

## Testing Coverage

### Fixed Cases (7 scenarios)

`internal/delegator/change_test.go` — `TestChangeCalculation`

- Exact balance (no change)
- Excess inputs (change created)
- Small remainder (1-5 sats — MUST create change)
- Nonce contributing to fee
- Large excess
- Mixed input sizes

### Property Tests (30 scenarios)

`internal/delegator/change_test.go` — `TestValueConservation_Property`

For each scenario, verifies:
1. `total_inputs = total_outputs + fee + change` (exact conservation)
2. No underflow
3. No implicit fee inflation (`implicit_fee == declared_fee`)

### Regression Sweep (600 values)

`internal/delegator/change_test.go` — `TestChangeNeverUsesHardcodedThreshold`

Iterates remainder from 1 to 600 sats. Verifies that `computeChange`
returns the exact remainder for every value. Catches any re-introduced
hardcoded threshold.

### CI Guards

- `go test ./... -v -count=1` — all tests on every push
- Regression grep — fails build if BTC dust patterns found in source
