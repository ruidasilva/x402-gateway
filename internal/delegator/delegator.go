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

// stdSigHash is SIGHASH_ALL | FORKID. The delegator signs all inputs/outputs.
var stdSigHash = sighash.Flag(sighash.AllForkID)

// Delegator builds full transactions, signs, and optionally broadcasts.
// This is the foundational settlement primitive — the economic kernel.
// It does NOT understand HTTP semantics or business logic.
type Delegator struct {
	key         *ec.PrivateKey
	address     *script.Address
	mainnet     bool
	feePool     pool.Pool     // pool for fee UTXOs (1-sat each)
	paymentPool pool.Pool     // pool for payment UTXOs (100-sat each)
	paymentKey  *ec.PrivateKey // key for signing payment inputs
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
	feePool pool.Pool,
	paymentPool pool.Pool,
	paymentKey *ec.PrivateKey,
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
		feePool:     feePool,
		paymentPool: paymentPool,
		paymentKey:  paymentKey,
		broadcaster: broadcaster,
		replayCache: replayCache,
		feeRate:     feeRate,
		broadcast:   doBroadcast,
		logger:      slog.Default().With("component", "delegator"),
	}, nil
}

// Accept builds a full transaction, signs, and optionally broadcasts.
func (d *Delegator) Accept(req DelegationRequest) (*DelegationResult, error) {
	d.logger.Info("accepting delegation",
		"challenge_hash", req.ChallengeHash,
		"amount", req.ExpectedAmount,
	)

	// 1. Check replay cache — has this challenge already been settled?
	if existingTxID, found := d.replayCache.Check(req.ChallengeHash); found {
		d.logger.Warn("replay detected",
			"challenge_hash", req.ChallengeHash,
			"existing_txid", existingTxID,
		)
		return nil, &DelegationError{
			Code:    ErrDoubleSpend.Code,
			Message: fmt.Sprintf("challenge already settled in tx %s", existingTxID),
			Status:  ErrDoubleSpend.Status,
		}
	}

	// 2. Build the full transaction
	tx := transaction.NewTransaction()

	// Input 0: payment UTXO (provides funds for the payee output)
	paymentUTXO, err := d.paymentPool.Lease()
	if err != nil {
		return nil, &DelegationError{
			Code:    ErrNoUTXOAvailable.Code,
			Message: fmt.Sprintf("lease payment UTXO: %s", err),
			Status:  ErrNoUTXOAvailable.Status,
		}
	}

	paymentUnlocker, err := p2pkh.Unlock(d.paymentKey, &stdSigHash)
	if err != nil {
		return nil, fmt.Errorf("create payment unlocker: %w", err)
	}

	err = tx.AddInputFrom(paymentUTXO.TxID, paymentUTXO.Vout, paymentUTXO.Script, paymentUTXO.Satoshis, paymentUnlocker)
	if err != nil {
		return nil, fmt.Errorf("add payment input: %w", err)
	}

	// Output 0: payee
	payeeScriptBytes, err := hex.DecodeString(req.ExpectedPayeeLockingScriptHex)
	if err != nil {
		return nil, &DelegationError{
			Code:    ErrInvalidProof.Code,
			Message: fmt.Sprintf("invalid payee script hex: %s", err),
			Status:  ErrInvalidProof.Status,
		}
	}
	payeeScript := script.Script(payeeScriptBytes)
	tx.AddOutput(&transaction.TransactionOutput{
		Satoshis:      uint64(req.ExpectedAmount),
		LockingScript: &payeeScript,
	})

	// Input 1+: fee UTXO(s) (provides miner fee)
	feeNeeded := CalculateFee(tx, 1, d.feeRate)

	feeUTXO, err := d.feePool.Lease()
	if err != nil {
		return nil, &DelegationError{
			Code:    ErrNoUTXOAvailable.Code,
			Message: fmt.Sprintf("lease fee UTXO: %s", err),
			Status:  ErrNoUTXOAvailable.Status,
		}
	}

	feeUnlocker, err := p2pkh.Unlock(d.key, &stdSigHash)
	if err != nil {
		return nil, fmt.Errorf("create fee unlocker: %w", err)
	}

	err = tx.AddInputFrom(feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Script, feeUTXO.Satoshis, feeUnlocker)
	if err != nil {
		return nil, fmt.Errorf("add fee input: %w", err)
	}

	// Calculate total inputs and add change if needed
	totalInputs := paymentUTXO.Satoshis + feeUTXO.Satoshis
	totalOutputs := uint64(req.ExpectedAmount)
	remainder := totalInputs - totalOutputs

	if remainder > feeNeeded+546 { // dust threshold
		change := remainder - feeNeeded
		if err := tx.PayToAddress(d.address.AddressString, change); err != nil {
			return nil, fmt.Errorf("add change output: %w", err)
		}
	}

	// Sign all inputs
	if err := tx.SignUnsigned(); err != nil {
		return nil, fmt.Errorf("sign inputs: %w", err)
	}

	txid := tx.TxID().String()

	// Broadcast (if configured)
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

	// Mark UTXOs as spent and record in replay cache
	d.paymentPool.MarkSpent(paymentUTXO.TxID, paymentUTXO.Vout)
	d.feePool.MarkSpent(feeUTXO.TxID, feeUTXO.Vout)
	d.replayCache.Record(req.ChallengeHash, txid)

	d.logger.Info("delegation accepted",
		"txid", txid,
		"challenge_hash", req.ChallengeHash,
		"payment_sats", paymentUTXO.Satoshis,
		"fee_sats", feeUTXO.Satoshis,
		"fee_needed", feeNeeded,
		"broadcast", d.broadcast,
	)

	return &DelegationResult{
		TxID:     txid,
		RawTxHex: tx.Hex(),
		Accepted: true,
	}, nil
}
