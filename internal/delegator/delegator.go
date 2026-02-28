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
	feePool     pool.Pool          // pool for fee UTXOs (1-sat each)
	paymentPool pool.Pool          // pool for payment UTXOs (100-sat each)
	paymentKey  *ec.PrivateKey     // key for signing payment inputs
	noncePool   pool.Pool          // pool for nonce UTXOs (1-sat each, replay protection)
	nonceKey    *ec.PrivateKey     // key for signing nonce inputs
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
	noncePool pool.Pool,
	nonceKey *ec.PrivateKey,
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
		noncePool:   noncePool,
		nonceKey:    nonceKey,
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

	// Validate nonce UTXO is provided
	if req.NonceUTXO == nil {
		return nil, &DelegationError{
			Code:    ErrInvalidProof.Code,
			Message: "nonce_utxo is required for replay protection",
			Status:  ErrInvalidProof.Status,
		}
	}

	// 1. Check replay cache — has this nonce outpoint already been spent?
	if existingTxID, _, found := d.replayCache.Check(req.NonceUTXO.TxID, req.NonceUTXO.Vout); found {
		d.logger.Warn("replay detected",
			"nonce", fmt.Sprintf("%s:%d", req.NonceUTXO.TxID, req.NonceUTXO.Vout),
			"existing_txid", existingTxID,
		)
		return nil, &DelegationError{
			Code:    ErrDoubleSpend.Code,
			Message: fmt.Sprintf("nonce already spent in tx %s", existingTxID),
			Status:  ErrDoubleSpend.Status,
		}
	}

	// 2. Build the full transaction
	tx := transaction.NewTransaction()

	// Input 0: nonce UTXO (replay protection — must be spent)
	nonceUnlocker, err := p2pkh.Unlock(d.nonceKey, &stdSigHash)
	if err != nil {
		return nil, fmt.Errorf("create nonce unlocker: %w", err)
	}

	err = tx.AddInputFrom(
		req.NonceUTXO.TxID,
		req.NonceUTXO.Vout,
		req.NonceUTXO.LockingScriptHex,
		req.NonceUTXO.Satoshis,
		nonceUnlocker,
	)
	if err != nil {
		return nil, fmt.Errorf("add nonce input: %w", err)
	}

	// Input 1: payment UTXO (provides funds for the payee output)
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

	// Input 2: fee UTXO(s) (provides miner fee)
	// Account for 1 additional input (fee) beyond the nonce + payment already on the tx
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
	totalInputs := req.NonceUTXO.Satoshis + paymentUTXO.Satoshis + feeUTXO.Satoshis
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
	d.noncePool.MarkSpent(req.NonceUTXO.TxID, req.NonceUTXO.Vout)
	d.paymentPool.MarkSpent(paymentUTXO.TxID, paymentUTXO.Vout)
	d.feePool.MarkSpent(feeUTXO.TxID, feeUTXO.Vout)
	d.replayCache.Record(req.NonceUTXO.TxID, req.NonceUTXO.Vout, txid, req.ChallengeHash)

	d.logger.Info("delegation accepted",
		"txid", txid,
		"challenge_hash", req.ChallengeHash,
		"nonce", fmt.Sprintf("%s:%d", req.NonceUTXO.TxID, req.NonceUTXO.Vout),
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
