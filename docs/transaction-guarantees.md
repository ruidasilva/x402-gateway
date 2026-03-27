# Transaction Guarantees

This document defines the economic invariants that x402 transaction
construction MUST satisfy. These guarantees are enforced in code,
validated by property tests, and guarded by CI.

---

## 1. Exact Value Conservation

Every finalised transaction MUST satisfy:

    total_inputs = total_outputs + miner_fee

Where:

- `total_inputs` = sum of all input UTXO values (nonce + fee inputs)
- `total_outputs` = sum of all output values (payment + change)
- `miner_fee` = `total_inputs - total_outputs` (implicit, no explicit fee output)

No satoshis are ever unaccounted for. The implementation MUST reject
any transaction where this invariant does not hold.

---

## 2. Change Output Rule

If the remainder after subtracting outputs and miner fee from inputs
is greater than or equal to 1 satoshi, the implementation MUST create
a change output.

    remainder = total_inputs - total_outputs_before_change - estimated_fee
    if remainder >= 1:
        create change output with value = remainder

BSV does not enforce a dust threshold. Any output with value >= 1 sat
is valid and relayable. Implementations MUST NOT discard small
remainders into miner fees.

The following behaviour is REQUIRED:

- `remainder = 0` -- no change output. Exact balance.
- `remainder >= 1` -- change output MUST be created.
- `remainder < 0` -- consensus violation. Transaction MUST be rejected.

---

## 3. No Implicit Value Loss

Implementations MUST NOT silently increase miner fees by discarding
change. The only satoshis that go to miners are the calculated
`miner_fee`. All other value MUST be represented as transaction
outputs.

If the actual miner fee (computed as `total_inputs - total_outputs`)
differs from the estimated fee and no change output exists, the
implementation MUST log a warning. This indicates value leakage.

---

## 4. Deterministic Construction

Given the same inputs (nonce UTXO, fee UTXOs, payment amount, fee
rate), the transaction construction MUST produce the same outputs.
There MUST be no randomness or non-determinism in output selection,
change calculation, or fee estimation.

---

## 5. Minimal Transaction Structure

A standard x402 Profile B transaction has:

    Inputs:
      [0] nonce UTXO (1 sat, signed 0xC3 by gateway)
      [1] fee UTXO (N sats, signed 0xC1 by delegator)

    Outputs:
      [0] payment output (amount_sats to payee)
      [1] change output (if remainder >= 1 sat)

Input count SHOULD be exactly 2 under normal operation (1 nonce +
1 fee input). Additional fee inputs are permitted when a single
fee UTXO does not cover the payment amount plus miner fee.

---

## 6. Fee Calculation

The miner fee MUST be calculated as:

    fee = ceil(transaction_size_bytes * fee_rate)
    if fee < 1: fee = 1

Where `fee_rate` is in satoshis per byte. The BSV standard rate
is 0.001 sat/byte (equivalent to 1 sat/KB).

The fee estimate MUST account for:

- All existing inputs (with actual script sizes)
- All existing outputs (with actual script sizes)
- Planned additional inputs (estimated at 148 bytes each for P2PKH)
- A potential change output (estimated at 34 bytes for P2PKH)

---

## 7. Enforcement

These guarantees are enforced at three levels:

1. **Code** -- The delegator's `Accept()` function validates
   `total_inputs >= total_outputs` before returning and logs
   warnings if actual fee differs from estimate.

2. **Tests** -- Property-style tests verify conservation across
   30+ scenarios and a 600-value sweep confirms no hardcoded
   threshold discards small change.

3. **CI** -- All tests run on every push. Any conservation
   violation fails the build.
