package delegator

import (
	"encoding/hex"
	"fmt"
	"log/slog"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	sighash "github.com/bsv-blockchain/go-sdk/transaction/sighash"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"

	"github.com/merkle-works/x402-gateway/internal/pool"
	"github.com/merkle-works/x402-gateway/internal/replay"
)

// x402SigHash is SIGHASH_ALL | ANYONECANPAY | FORKID = 0x41 | 0x80 = 0xC1.
// This allows the delegator to sign fee inputs without invalidating
// the client's signature on the nonce input.
var x402SigHash = sighash.Flag(sighash.AllForkID | sighash.AnyOneCanPay)

// Delegator validates partial transactions, adds fee inputs, signs, and broadcasts.
// This is the foundational settlement primitive — the economic kernel.
// It does NOT understand HTTP semantics or business logic.
type Delegator struct {
	key         *ec.PrivateKey
	address     *script.Address
	mainnet     bool
	noncePool   pool.Pool
	feePool     pool.Pool // separate pool for fee UTXOs
	broadcaster transaction.Broadcaster
	replayCache *replay.Cache
	feeRate     float64
	broadcast   bool // if true, delegator broadcasts; if false, client broadcasts
	logger      *slog.Logger
}

// New creates a new Delegator.
func New(
	key *ec.PrivateKey,
	mainnet bool,
	noncePool pool.Pool,
	feePool pool.Pool,
	broadcaster transaction.Broadcaster,
	replayCache *replay.Cache,
	feeRate float64,
	doBroadcast bool,
) (*Delegator, error) {
	addr, err := script.NewAddressFromPublicKey(key.PubKey(), mainnet)
	if err != nil {
		return nil, fmt.Errorf("derive address: %w", err)
	}

	return &Delegator{
		key:         key,
		address:     addr,
		mainnet:     mainnet,
		noncePool:   noncePool,
		feePool:     feePool,
		broadcaster: broadcaster,
		replayCache: replayCache,
		feeRate:     feeRate,
		broadcast:   doBroadcast,
		logger:      slog.Default().With("component", "delegator"),
	}, nil
}

// Accept validates a partial transaction, adds fee input(s), signs, and optionally broadcasts.
// Called by the client directly (NOT by the gatekeeper).
func (d *Delegator) Accept(req DelegationRequest) (*DelegationResult, error) {
	d.logger.Info("accepting delegation",
		"nonce", fmt.Sprintf("%s:%d", req.NonceTxID, req.NonceVout),
		"challenge_hash", req.ChallengeHash,
	)

	// 1. Check replay cache
	if existingTxID, found := d.replayCache.Check(req.NonceTxID, req.NonceVout); found {
		d.logger.Warn("replay detected",
			"nonce", fmt.Sprintf("%s:%d", req.NonceTxID, req.NonceVout),
			"existing_txid", existingTxID,
		)
		return nil, &DelegationError{
			Code:    ErrDoubleSpend.Code,
			Message: fmt.Sprintf("nonce already spent in tx %s", existingTxID),
			Status:  ErrDoubleSpend.Status,
		}
	}

	// 2. Check nonce pool — must be a leased nonce from our pool
	nonceUTXO := d.noncePool.Lookup(req.NonceTxID, req.NonceVout)
	if nonceUTXO == nil {
		return nil, ErrInvalidNonce
	}
	if nonceUTXO.Status != pool.StatusLeased {
		return nil, &DelegationError{
			Code:    ErrInvalidNonce.Code,
			Message: fmt.Sprintf("nonce is in status %q, expected %q", nonceUTXO.Status, pool.StatusLeased),
			Status:  ErrInvalidNonce.Status,
		}
	}

	// 3. Parse the partial transaction (hex-encoded)
	tx, err := transaction.NewTransactionFromHex(req.PartialTxHex)
	if err != nil {
		return nil, &DelegationError{
			Code:    ErrInvalidProof.Code,
			Message: fmt.Sprintf("parse partial tx: %s", err),
			Status:  ErrInvalidProof.Status,
		}
	}

	// 4. Validate structure: exactly 1 input (nonce), exactly 1 output (payee)
	if tx.InputCount() != 1 {
		return nil, &DelegationError{
			Code:    ErrInvalidProof.Code,
			Message: fmt.Sprintf("expected 1 input, got %d", tx.InputCount()),
			Status:  ErrInvalidProof.Status,
		}
	}
	if tx.OutputCount() != 1 {
		return nil, ErrUnexpectedOutputs
	}

	// 5. Validate nonce input is unsigned (delegator adds signatures, not the client in v0.1)
	input0 := tx.Inputs[0]
	if input0.UnlockingScript != nil && len(*input0.UnlockingScript) > 0 {
		return nil, ErrNonceAlreadySigned
	}

	// 6. Validate input 0 references the nonce UTXO
	inputTxID := input0.SourceTXID.String() // display-format (byte-reversed) to match pool keys
	if inputTxID != req.NonceTxID || input0.SourceTxOutIndex != req.NonceVout {
		return nil, &DelegationError{
			Code:    ErrInvalidNonce.Code,
			Message: fmt.Sprintf("input 0 references %s:%d, expected %s:%d", inputTxID, input0.SourceTxOutIndex, req.NonceTxID, req.NonceVout),
			Status:  ErrInvalidNonce.Status,
		}
	}

	// 7. Validate output 0 pays the expected payee the expected amount
	output0 := tx.Outputs[0]
	if req.ExpectedPayeeLockingScriptHex != "" {
		actualScriptHex := hex.EncodeToString(*output0.LockingScript)
		if actualScriptHex != req.ExpectedPayeeLockingScriptHex {
			return nil, ErrInvalidPayee
		}
	}
	if req.ExpectedAmount > 0 && int64(output0.Satoshis) < req.ExpectedAmount {
		return nil, &DelegationError{
			Code:    ErrInsufficientAmount.Code,
			Message: fmt.Sprintf("output 0 pays %d sats, minimum is %d", output0.Satoshis, req.ExpectedAmount),
			Status:  ErrInsufficientAmount.Status,
		}
	}

	// 8. Hydrate nonce input with source UTXO data (required for signing)
	nonceScript, err := script.NewFromHex(req.NonceLockingScriptHex)
	if err != nil {
		return nil, &DelegationError{
			Code:    ErrInvalidProof.Code,
			Message: fmt.Sprintf("invalid nonce script: %s", err),
			Status:  ErrInvalidProof.Status,
		}
	}
	input0.SetSourceTxOutput(&transaction.TransactionOutput{
		Satoshis:      uint64(req.NonceSatoshis),
		LockingScript: nonceScript,
	})

	// 9. Add fee input(s)
	feeNeeded := CalculateFee(tx, 1, d.feeRate)

	feeUTXO, err := d.feePool.Lease()
	if err != nil {
		return nil, &DelegationError{
			Code:    ErrNoFeeUTXO.Code,
			Message: fmt.Sprintf("lease fee UTXO: %s", err),
			Status:  ErrNoFeeUTXO.Status,
		}
	}

	// Create the unlock template with 0xC1 sighash
	unlocker, err := p2pkh.Unlock(d.key, &x402SigHash)
	if err != nil {
		return nil, fmt.Errorf("create fee unlocker: %w", err)
	}

	err = tx.AddInputFrom(feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Script, feeUTXO.Satoshis, unlocker)
	if err != nil {
		return nil, fmt.Errorf("add fee input: %w", err)
	}

	// 10. Add change output if fee UTXO is larger than needed
	if feeUTXO.Satoshis > feeNeeded+546 { // dust threshold
		change := feeUTXO.Satoshis - feeNeeded
		if err := tx.PayToAddress(d.address.AddressString, change); err != nil {
			return nil, fmt.Errorf("add change output: %w", err)
		}
	}

	// 11. Sign only the fee input(s) — SignUnsigned skips inputs with existing UnlockingScript
	if err := tx.SignUnsigned(); err != nil {
		return nil, fmt.Errorf("sign fee inputs: %w", err)
	}

	txid := tx.TxID().String()

	// 12. Broadcast (if configured — default per spec)
	if d.broadcast {
		success, failure := d.broadcaster.Broadcast(tx)
		if failure != nil {
			d.logger.Error("broadcast failed",
				"code", failure.Code,
				"description", failure.Description,
			)
			return nil, &DelegationError{
				Code:    ErrMempoolRejected.Code,
				Message: fmt.Sprintf("%s: %s", failure.Code, failure.Description),
				Status:  ErrMempoolRejected.Status,
			}
		}
		txid = success.Txid
	}

	// 13. Mark nonce as spent and record in replay cache
	d.noncePool.MarkSpent(req.NonceTxID, req.NonceVout)
	d.feePool.MarkSpent(feeUTXO.TxID, feeUTXO.Vout)
	d.replayCache.Record(req.NonceTxID, req.NonceVout, txid)

	d.logger.Info("delegation accepted",
		"txid", txid,
		"nonce", fmt.Sprintf("%s:%d", req.NonceTxID, req.NonceVout),
		"fee", feeNeeded,
		"broadcast", d.broadcast,
	)

	return &DelegationResult{
		TxID:     txid,
		RawTxHex: tx.Hex(),
		Accepted: true,
	}, nil
}
