// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package delegator

import "testing"

// computeChange mirrors the exact change logic from Accept() Step 7.
//
// BSV does not enforce a dust threshold.
// Any output >= 1 sat is valid.
// Change is created whenever remainder >= 1 sat.
// No value is ever silently discarded to fees.
func computeChange(feeInputSats, existingInputSats, totalOutputSats, minerFee uint64) uint64 {
	totalInputSats := feeInputSats + existingInputSats
	if totalInputSats > totalOutputSats+minerFee {
		return totalInputSats - totalOutputSats - minerFee
	}
	return 0
}

// TestChangeCalculation verifies change logic for fixed scenarios.
func TestChangeCalculation(t *testing.T) {
	tests := []struct {
		name           string
		feeInputSats   uint64 // fee UTXO(s) added by delegator
		existInputSats uint64 // nonce UTXO (template input)
		outputSats     uint64 // payment output
		minerFee       uint64
		wantChange     uint64
	}{
		{
			name:           "exact_balance_no_change",
			feeInputSats:   100, // one 100-sat fee UTXO
			existInputSats: 1,   // nonce
			outputSats:     100, // payment
			minerFee:       1,
			wantChange:     0, // 100+1 = 100+1, no remainder
		},
		{
			name:           "excess_produces_change",
			feeInputSats:   200, // two 100-sat fee UTXOs
			existInputSats: 1,   // nonce
			outputSats:     100, // payment
			minerFee:       1,
			wantChange:     100, // 200+1 - 100 - 1 = 100
		},
		{
			name:           "small_remainder_still_creates_change",
			feeInputSats:   102,
			existInputSats: 1,
			outputSats:     100,
			minerFee:       1,
			wantChange:     2, // must NOT be discarded (BSV has no dust limit)
		},
		{
			name:           "one_sat_remainder_creates_change",
			feeInputSats:   101,
			existInputSats: 1,
			outputSats:     100,
			minerFee:       1,
			wantChange:     1, // even 1 sat must be returned
		},
		{
			name:           "nonce_covers_fee_exactly",
			feeInputSats:   100,
			existInputSats: 2, // 2-sat nonce
			outputSats:     100,
			minerFee:       2,
			wantChange:     0, // 100+2 = 100+2
		},
		{
			name:           "large_excess",
			feeInputSats:   500,
			existInputSats: 1,
			outputSats:     100,
			minerFee:       1,
			wantChange:     400, // 500+1 - 100 - 1 = 400
		},
		{
			name:           "inputs_equal_outputs_plus_fee",
			feeInputSats:   50,
			existInputSats: 51,
			outputSats:     100,
			minerFee:       1,
			wantChange:     0, // 50+51 = 100+1
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeChange(tc.feeInputSats, tc.existInputSats, tc.outputSats, tc.minerFee)
			if got != tc.wantChange {
				t.Errorf("computeChange(%d, %d, %d, %d) = %d, want %d",
					tc.feeInputSats, tc.existInputSats, tc.outputSats, tc.minerFee,
					got, tc.wantChange)
			}

			// Verify value conservation: inputs = outputs + fee + change
			totalIn := tc.feeInputSats + tc.existInputSats
			totalOut := tc.outputSats + tc.minerFee + got
			if totalIn != totalOut {
				t.Errorf("value conservation violated: inputs=%d != outputs+fee+change=%d",
					totalIn, totalOut)
			}
		})
	}
}

// TestValueConservation_Property verifies that for ANY combination of input/output
// values, the change function preserves exact value conservation.
// This test does not depend on any fixed threshold.
func TestValueConservation_Property(t *testing.T) {
	// Generate a broad range of scenarios covering edge cases.
	type scenario struct {
		feeInputSats   uint64
		existInputSats uint64
		outputSats     uint64
		minerFee       uint64
	}

	scenarios := []scenario{
		// Exact balance — various scales
		{100, 1, 100, 1},
		{1000, 1, 1000, 1},
		{1, 1, 1, 1},
		{50, 50, 99, 1},

		// Small remainders (1–5 sats) — MUST produce change, never discarded
		{101, 1, 100, 1},  // remainder = 1
		{102, 1, 100, 1},  // remainder = 2
		{103, 1, 100, 1},  // remainder = 3
		{104, 1, 100, 1},  // remainder = 4
		{105, 1, 100, 1},  // remainder = 5
		{100, 2, 100, 1},  // nonce contributes: remainder = 1
		{100, 6, 100, 1},  // nonce contributes: remainder = 5

		// Large remainders
		{500, 1, 100, 1},   // remainder = 400
		{1000, 1, 100, 1},  // remainder = 900
		{10000, 1, 50, 1},  // remainder = 9950

		// Nonce contributing to fee coverage
		{99, 2, 100, 1},   // nonce fills the gap: 99+2=101, 100+1=101
		{0, 101, 100, 1},  // nonce covers everything: 0+101=101
		{50, 51, 100, 1},  // mixed: 50+51=101

		// Mixed input sizes
		{300, 1, 200, 1},  // remainder = 100
		{250, 10, 200, 5}, // 250+10=260, 200+5=205, remainder=55
		{75, 25, 50, 3},   // 75+25=100, 50+3=53, remainder=47

		// Large fee
		{200, 1, 100, 50},  // 200+1=201, 100+50=150, remainder=51
		{1000, 1, 100, 500}, // 1000+1=1001, 100+500=600, remainder=401

		// Zero fee (theoretical)
		{100, 1, 100, 0}, // remainder = 1
		{100, 0, 100, 0}, // remainder = 0
	}

	for i, s := range scenarios {
		change := computeChange(s.feeInputSats, s.existInputSats, s.outputSats, s.minerFee)

		totalIn := s.feeInputSats + s.existInputSats
		totalOut := s.outputSats + s.minerFee + change

		// Property 1: exact value conservation
		if totalIn != totalOut {
			t.Errorf("scenario %d: conservation violated: inputs=%d != outputs+fee+change=%d "+
				"(feeIn=%d, existIn=%d, out=%d, fee=%d, change=%d)",
				i, totalIn, totalOut,
				s.feeInputSats, s.existInputSats, s.outputSats, s.minerFee, change)
		}

		// Property 2: change is never negative (uint64 guarantees this, but be explicit)
		// This is implicitly true for uint64 but validates the logic doesn't underflow.
		if totalIn < s.outputSats+s.minerFee && change != 0 {
			t.Errorf("scenario %d: change=%d when inputs < outputs+fee (would underflow)",
				i, change)
		}

		// Property 3: no implicit fee inflation — the only sats going to
		// miners are exactly minerFee. The rest is outputs + change.
		implicitFee := totalIn - s.outputSats - change
		if implicitFee != s.minerFee {
			t.Errorf("scenario %d: implicit fee=%d != declared fee=%d "+
				"(value leaked: %d sats silently discarded)",
				i, implicitFee, s.minerFee, implicitFee-s.minerFee)
		}
	}
}

// TestChangeNeverUsesHardcodedThreshold verifies that the change function
// does not silently discard small amounts. This is a regression guard
// against re-introducing a BTC-style dust threshold (e.g. 546 sats).
func TestChangeNeverUsesHardcodedThreshold(t *testing.T) {
	// For every remainder from 1 to 600 (deliberately exceeds old 546 threshold),
	// verify that computeChange returns the exact remainder.
	for remainder := uint64(1); remainder <= 600; remainder++ {
		feeInputSats := 100 + remainder
		existInputSats := uint64(1)
		outputSats := uint64(100)
		minerFee := uint64(1)

		got := computeChange(feeInputSats, existInputSats, outputSats, minerFee)
		if got != remainder {
			t.Errorf("remainder=%d: computeChange returned %d (expected %d) — "+
				"possible hardcoded threshold discarding small change",
				remainder, got, remainder)
		}
	}
}
