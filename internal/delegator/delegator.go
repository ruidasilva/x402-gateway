package delegator

import (
	"crypto/subtle"
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

// x402SigHash is SIGHASH_ALL | ANYONECANPAY | FORKID = 0xC1.
// Both client inputs and delegator fee inputs use this flag.
// ANYONECANPAY allows additional inputs to be appended after signing.
var x402SigHash = sighash.Flag(sighash.AllForkID | sighash.AnyOneCanPay)

// requiredSighashByte is the raw byte value (0xC1) that must appear as the
// last byte of every DER signature in the partial tx's client inputs.
const requiredSighashByte = byte(0xC1)

// Delegator validates client-constructed partial transactions and appends
// fee inputs. Per canonical spec, the delegator:
//   - Does NOT construct the transaction (client does)
//   - Does NOT sign nonce or payment inputs (client does)
//   - Does NOT broadcast (client does)
//   - ONLY adds fee inputs, signs those, and returns the completed tx
type Delegator struct {
	key         *ec.PrivateKey
	address     *script.Address
	mainnet     bool
	feePool     pool.Pool    // pool for fee UTXOs (1-sat each)
	replayCache *replay.Cache
	feeRate     float64
	logger      *slog.Logger
}

// New creates a new Delegator.
// The key is used exclusively for signing fee inputs.
func New(
	key *ec.PrivateKey,
	mainnet bool,
	feePool pool.Pool,
	replayCache *replay.Cache,
	feeRate float64,
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
		replayCache: replayCache,
		feeRate:     feeRate,
		logger:      slog.Default().With("component", "delegator"),
	}, nil
}

// Accept validates a client-constructed partial transaction, appends fee
// inputs, signs only the fee inputs, and returns the completed transaction.
// It does NOT broadcast — that is the client's responsibility.
func (d *Delegator) Accept(req DelegationRequest) (*DelegationResult, error) {
	d.logger.Info("accepting delegation",
		"challenge_hash", req.ChallengeHash,
		"amount", req.ExpectedAmount,
	)

	// ── Step 1: Validate nonce outpoint is provided ──
	if req.NonceOutpoint == nil {
		return nil, &DelegationError{
			Code:    ErrInvalidProof.Code,
			Message: "nonce_outpoint is required for replay protection",
			Status:  ErrInvalidProof.Status,
		}
	}

	// ── Step 2: Decode partial transaction ──
	if req.PartialTxHex == "" {
		return nil, &DelegationError{
			Code:    ErrInvalidPartialTx.Code,
			Message: "partial_tx_hex is required",
			Status:  ErrInvalidPartialTx.Status,
		}
	}

	partialTxBytes, err := hex.DecodeString(req.PartialTxHex)
	if err != nil {
		return nil, &DelegationError{
			Code:    ErrInvalidPartialTx.Code,
			Message: fmt.Sprintf("invalid partial_tx_hex: %s", err),
			Status:  ErrInvalidPartialTx.Status,
		}
	}

	tx, err := transaction.NewTransactionFromBytes(partialTxBytes)
	if err != nil {
		return nil, &DelegationError{
			Code:    ErrInvalidPartialTx.Code,
			Message: fmt.Sprintf("cannot parse partial transaction: %s", err),
			Status:  ErrInvalidPartialTx.Status,
		}
	}

	// ── Step 3: Structural validation (BEFORE replay cache) ──

	// 3a. Must have at least 1 input (nonce)
	if tx.InputCount() < 1 {
		return nil, &DelegationError{
			Code:    ErrInvalidPartialTx.Code,
			Message: "partial transaction has no inputs",
			Status:  ErrInvalidPartialTx.Status,
		}
	}

	// 3b. Must have at least 1 output (payee)
	if len(tx.Outputs) < 1 {
		return nil, &DelegationError{
			Code:    ErrInvalidPartialTx.Code,
			Message: "partial transaction has no outputs",
			Status:  ErrInvalidPartialTx.Status,
		}
	}

	// 3c. Verify nonce input references the expected outpoint
	if err := verifyNonceInput(tx, req.NonceOutpoint); err != nil {
		return nil, &DelegationError{
			Code:    ErrInvalidPartialTx.Code,
			Message: err.Error(),
			Status:  ErrInvalidPartialTx.Status,
		}
	}

	// 3d. Verify payee output pays >= expected amount to expected script
	if err := verifyPayeeOutput(tx, req.ExpectedPayeeLockingScriptHex, req.ExpectedAmount); err != nil {
		return nil, &DelegationError{
			Code:    ErrInvalidPayee.Code,
			Message: err.Error(),
			Status:  ErrInvalidPayee.Status,
		}
	}

	// 3e. Enforce sighash = 0xC1 on ALL client inputs
	if err := enforceSighash(tx); err != nil {
		return nil, &DelegationError{
			Code:    ErrInvalidSighash.Code,
			Message: err.Error(),
			Status:  ErrInvalidSighash.Status,
		}
	}

	// ── Step 4: Atomic nonce reservation (AFTER structural validation) ──
	// TryReserve holds an exclusive lock for the entire check+insert,
	// eliminating the TOCTOU window that allowed RACE-01.
	nonceTxID := req.NonceOutpoint.TxID
	nonceVout := req.NonceOutpoint.Vout
	reserved, existingTxID, pending := d.replayCache.TryReserve(nonceTxID, nonceVout, req.ChallengeHash)
	if !reserved {
		if pending {
			d.logger.Info("nonce reservation pending (concurrent request)",
				"nonce", fmt.Sprintf("%s:%d", nonceTxID, nonceVout),
			)
			return nil, &DelegationError{
				Code:    ErrNoncePending.Code,
				Message: "nonce is being processed by another request; retry shortly",
				Status:  ErrNoncePending.Status,
			}
		}
		d.logger.Warn("replay detected",
			"nonce", fmt.Sprintf("%s:%d", nonceTxID, nonceVout),
			"existing_txid", existingTxID,
		)
		return nil, &DelegationError{
			Code:    ErrDoubleSpend.Code,
			Message: fmt.Sprintf("nonce already spent in tx %s", existingTxID),
			Status:  ErrDoubleSpend.Status,
		}
	}
	// Reservation acquired. If we return before Commit, release it so the
	// nonce is not stranded in pending state and the client can retry.
	nonceCommitted := false
	defer func() {
		if !nonceCommitted {
			d.replayCache.Release(nonceTxID, nonceVout, req.ChallengeHash)
		}
	}()

	// ── Step 5: Lease fee UTXO ──
	// Record client input count before adding fee input
	clientInputCount := tx.InputCount()

	feeNeeded := CalculateFee(tx, 1, d.feeRate)

	feeUTXO, err := d.feePool.Lease()
	if err != nil {
		return nil, &DelegationError{
			Code:    ErrNoUTXOAvailable.Code,
			Message: fmt.Sprintf("lease fee UTXO: %s", err),
			Status:  ErrNoUTXOAvailable.Status,
		}
	}

	// ── Step 6: Add fee input with 0xC1 sighash ──
	feeUnlocker, err := p2pkh.Unlock(d.key, &x402SigHash)
	if err != nil {
		return nil, fmt.Errorf("create fee unlocker: %w", err)
	}

	err = tx.AddInputFrom(feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Script, feeUTXO.Satoshis, feeUnlocker)
	if err != nil {
		return nil, fmt.Errorf("add fee input: %w", err)
	}

	// ── Step 7: Add change output if needed ──
	// Sum all client input satoshis from the partial tx
	var clientInputSats uint64
	for i := 0; i < clientInputCount; i++ {
		if tx.Inputs[i].SourceTransaction != nil {
			for _, out := range tx.Inputs[i].SourceTransaction.Outputs {
				clientInputSats += out.Satoshis
			}
		}
		// If SourceTransaction is not set, the satoshis are embedded via AddInputFrom
		// and tracked internally by the SDK
	}

	totalInputs := feeUTXO.Satoshis // fee input satoshis are known
	// For change calculation, we use fee UTXO sats since client inputs
	// fund the payee output and any excess goes to change
	var totalOutputSats uint64
	for _, out := range tx.Outputs {
		totalOutputSats += out.Satoshis
	}

	// Change = fee UTXO sats - needed fee (client inputs cover payee output)
	if feeUTXO.Satoshis > feeNeeded+546 { // dust threshold
		change := feeUTXO.Satoshis - feeNeeded
		if err := tx.PayToAddress(d.address.AddressString, change); err != nil {
			return nil, fmt.Errorf("add change output: %w", err)
		}
	}
	_ = totalInputs // used for clarity above

	// ── Step 8: Explicitly sign ONLY the fee input by index ──
	feeInputIdx := uint32(clientInputCount) // fee input is appended after all client inputs
	if tx.Inputs[feeInputIdx].UnlockingScriptTemplate == nil {
		return nil, fmt.Errorf("fee input at index %d has no unlocking script template", feeInputIdx)
	}
	unlockScript, err := tx.Inputs[feeInputIdx].UnlockingScriptTemplate.Sign(tx, feeInputIdx)
	if err != nil {
		return nil, fmt.Errorf("sign fee input: %w", err)
	}
	tx.Inputs[feeInputIdx].UnlockingScript = unlockScript

	txid := tx.TxID().String()

	// ── Step 9: Commit nonce reservation with final txid ──
	if err := d.replayCache.Commit(nonceTxID, nonceVout, req.ChallengeHash, txid); err != nil {
		return nil, fmt.Errorf("replay cache commit: %w", err)
	}
	nonceCommitted = true

	// ── Step 10: Mark fee UTXO spent ──
	d.feePool.MarkSpent(feeUTXO.TxID, feeUTXO.Vout)

	d.logger.Info("delegation accepted",
		"txid", txid,
		"challenge_hash", req.ChallengeHash,
		"nonce", fmt.Sprintf("%s:%d", req.NonceOutpoint.TxID, req.NonceOutpoint.Vout),
		"fee_sats", feeUTXO.Satoshis,
		"fee_needed", feeNeeded,
		"client_inputs", clientInputCount,
	)

	// ── Step 11: Return completed tx — NO broadcast ──
	return &DelegationResult{
		TxID:     txid,
		RawTxHex: tx.Hex(),
		Accepted: true,
	}, nil
}

// verifyNonceInput checks that the partial transaction contains an input
// spending the expected nonce outpoint. Uses constant-time comparison.
func verifyNonceInput(tx *transaction.Transaction, nonce *NonceOutpointRef) error {
	if nonce == nil {
		return fmt.Errorf("nonce outpoint is nil")
	}
	for _, input := range tx.Inputs {
		if input.SourceTXID != nil &&
			constantTimeEqual(input.SourceTXID.String(), nonce.TxID) &&
			input.SourceTxOutIndex == nonce.Vout {
			return nil
		}
	}
	return fmt.Errorf("partial tx does not contain nonce input %s:%d", nonce.TxID, nonce.Vout)
}

// verifyPayeeOutput checks that the transaction has at least one output
// paying >= minAmount to the expected payee locking script.
// Uses constant-time comparison for the script hex.
func verifyPayeeOutput(tx *transaction.Transaction, expectedScriptHex string, minAmount int64) error {
	for _, out := range tx.Outputs {
		scriptHex := hex.EncodeToString(*out.LockingScript)
		if constantTimeEqual(scriptHex, expectedScriptHex) && int64(out.Satoshis) >= minAmount {
			return nil
		}
	}
	return fmt.Errorf("no output paying >= %d sats to expected payee", minAmount)
}

// enforceSighash validates that all inputs in the partial transaction use
// SIGHASH_ALL|ANYONECANPAY|FORKID (0xC1). This guarantees that appending
// fee inputs will not invalidate client signatures.
//
// Parses the P2PKH scriptSig structure:
//
//	<sig_push_opcode> <DER_signature || sighash_byte> <pubkey_push_opcode> <pubkey>
//
// The sighash byte is the last byte of the signature data chunk.
func enforceSighash(tx *transaction.Transaction) error {
	for i, input := range tx.Inputs {
		if input.UnlockingScript == nil || len(*input.UnlockingScript) == 0 {
			return fmt.Errorf("input %d has no unlocking script", i)
		}

		sighashByte, err := extractSighashByte(*input.UnlockingScript)
		if err != nil {
			return fmt.Errorf("input %d: %w", i, err)
		}

		if sighashByte != requiredSighashByte {
			return fmt.Errorf("input %d uses sighash 0x%02X, required 0xC1 (SIGHASH_ALL|ANYONECANPAY|FORKID)", i, sighashByte)
		}
	}
	return nil
}

// extractSighashByte parses a P2PKH scriptSig to extract the sighash flag byte.
// P2PKH scriptSig format: <push_opcode> <signature_data> <push_opcode> <pubkey_data>
// The sighash byte is the last byte of the signature_data portion.
func extractSighashByte(scriptSig script.Script) (byte, error) {
	if len(scriptSig) < 2 {
		return 0, fmt.Errorf("scriptSig too short (%d bytes)", len(scriptSig))
	}

	// First byte is the push opcode indicating signature data length
	sigPushLen := int(scriptSig[0])

	// Validate: push opcode should be a direct data push (1-75 bytes)
	if sigPushLen < 1 || sigPushLen > 75 {
		return 0, fmt.Errorf("unexpected signature push opcode: 0x%02X", scriptSig[0])
	}

	// Ensure we have enough bytes
	if len(scriptSig) < 1+sigPushLen {
		return 0, fmt.Errorf("scriptSig truncated: need %d bytes for signature, have %d", sigPushLen, len(scriptSig)-1)
	}

	// The sighash byte is the last byte of the pushed signature data
	return scriptSig[sigPushLen], nil
}

// constantTimeEqual compares two strings in constant time to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
