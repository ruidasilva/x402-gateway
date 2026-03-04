package treasury

import (
	"fmt"
	"log/slog"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	sighash "github.com/bsv-blockchain/go-sdk/transaction/sighash"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"

	"github.com/merkle-works/x402-gateway/internal/pool"
)

// FanoutRequest describes the funding UTXO and desired output count for a fan-out tx.
type FanoutRequest struct {
	FundingTxID     string  // txid of the funding UTXO
	FundingVout     uint32  // vout of the funding UTXO
	FundingScript   string  // hex locking script of the funding UTXO
	FundingSatoshis uint64  // value of the funding UTXO
	OutputCount     int     // number of UTXOs to create
	FeeRate         float64 // fee rate in sat/byte
	TargetAddress   string  // optional: address for outputs (defaults to signing key's address)
	OutputSatoshis  uint64  // optional: satoshis per output (defaults to 1)
}

// FanoutResult contains the broadcast txid and the newly created UTXOs.
type FanoutResult struct {
	TxID  string      // txid of the fan-out transaction
	UTXOs []pool.UTXO // newly created 1-sat UTXOs
}

// BuildFanout constructs and broadcasts a fan-out transaction that splits one
// large funding UTXO into N × 1-sat UTXOs.
//
// All outputs are 1-sat — both nonce and fee pools use identical denominations.
// The fee delegator collects multiple 1-sat UTXOs when it needs more than 1 sat
// for miner fees.
func BuildFanout(
	key *ec.PrivateKey,
	mainnet bool,
	req FanoutRequest,
	bcast transaction.Broadcaster,
) (*FanoutResult, error) {
	if req.OutputCount <= 0 || req.OutputCount > 10000 {
		return nil, fmt.Errorf("output count must be between 1 and 10000, got %d", req.OutputCount)
	}

	logger := slog.Default().With("component", "treasury-fanout")

	// Derive output address (use target if specified, else derive from key)
	var addr *script.Address
	var err error
	if req.TargetAddress != "" {
		addr, err = script.NewAddressFromString(req.TargetAddress)
		if err != nil {
			return nil, fmt.Errorf("parse target address: %w", err)
		}
	} else {
		addr, err = script.NewAddressFromPublicKey(key.PubKey(), mainnet)
		if err != nil {
			return nil, fmt.Errorf("derive address: %w", err)
		}
	}

	// Build the fan-out transaction
	tx := transaction.NewTransaction()

	// Standard signing (SIGHASH_ALL | FORKID)
	allForkID := sighash.AllForkID
	unlocker, err := p2pkh.Unlock(key, &allForkID)
	if err != nil {
		return nil, fmt.Errorf("create unlocker: %w", err)
	}

	// Add funding input
	err = tx.AddInputFrom(
		req.FundingTxID,
		req.FundingVout,
		req.FundingScript,
		req.FundingSatoshis,
		unlocker,
	)
	if err != nil {
		return nil, fmt.Errorf("add funding input: %w", err)
	}

	// Determine output denomination (default to 1 sat if not specified)
	outputSats := req.OutputSatoshis
	if outputSats == 0 {
		outputSats = 1
	}

	// Add N × outputSats outputs
	addrStr := addr.AddressString
	for i := 0; i < req.OutputCount; i++ {
		if err := tx.PayToAddress(addrStr, outputSats); err != nil {
			return nil, fmt.Errorf("add output %d: %w", i, err)
		}
	}

	// Calculate fee (1 sat/KB = 0.001 sat/byte, always ceil, min 1 sat)
	// Each P2PKH output is ~34 bytes, each P2PKH input is ~148 bytes, overhead ~10 bytes
	estimatedSize := 10 + 148 + (req.OutputCount * 34) + 34 // +34 for potential change output
	fee := uint64(ceilSats(float64(estimatedSize) * req.FeeRate))
	if fee < 1 {
		fee = 1
	}

	totalOutputSats := uint64(req.OutputCount) * outputSats
	requiredSats := totalOutputSats + fee
	if req.FundingSatoshis < requiredSats {
		return nil, fmt.Errorf("insufficient funding: need %d sats (%d outputs × %d sats + %d fee), have %d",
			requiredSats, req.OutputCount, outputSats, fee, req.FundingSatoshis)
	}

	// Add change output if there's leftover above dust
	change := req.FundingSatoshis - totalOutputSats - fee
	if change > 546 { // dust threshold
		if err := tx.PayToAddress(addrStr, change); err != nil {
			return nil, fmt.Errorf("add change output: %w", err)
		}
	}

	// Sign
	if err := tx.Sign(); err != nil {
		return nil, fmt.Errorf("sign transaction: %w", err)
	}

	// Broadcast
	success, failure := bcast.Broadcast(tx)
	if failure != nil {
		return nil, fmt.Errorf("broadcast failed: %s - %s", failure.Code, failure.Description)
	}

	txid := success.Txid

	// Derive locking script hex
	lockScript, err := p2pkh.Lock(addr)
	if err != nil {
		return nil, fmt.Errorf("derive locking script: %w", err)
	}
	scriptHex := fmt.Sprintf("%x", *lockScript)

	// Build the UTXO list
	utxos := make([]pool.UTXO, req.OutputCount)
	for i := 0; i < req.OutputCount; i++ {
		utxos[i] = pool.UTXO{
			TxID:     txid,
			Vout:     uint32(i),
			Script:   scriptHex,
			Satoshis: outputSats,
		}
	}

	logger.Info("fan-out complete",
		"txid", txid,
		"outputs", req.OutputCount,
		"sats_per_output", outputSats,
		"funding_sats", req.FundingSatoshis,
		"fee", fee,
		"change", change,
	)

	return &FanoutResult{
		TxID:  txid,
		UTXOs: utxos,
	}, nil
}

// ceilSats rounds a float up to the nearest integer (ceiling).
func ceilSats(f float64) uint64 {
	u := uint64(f)
	if float64(u) < f {
		return u + 1
	}
	return u
}
