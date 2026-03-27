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
