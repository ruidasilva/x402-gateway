package broadcast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// ---------------------------------------------------------------------------
// MockBroadcaster — development/demo mode
// ---------------------------------------------------------------------------

// MockBroadcaster is a development broadcaster that accepts all transactions
// without actually broadcasting to the network.
type MockBroadcaster struct{}

// Broadcast accepts the transaction and returns its txid.
func (m *MockBroadcaster) Broadcast(tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	return &transaction.BroadcastSuccess{
		Txid:    tx.TxID().String(),
		Message: "mock broadcast accepted",
	}, nil
}

// BroadcastCtx accepts the transaction and returns its txid.
func (m *MockBroadcaster) BroadcastCtx(ctx context.Context, tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	return m.Broadcast(tx)
}

// ---------------------------------------------------------------------------
// WoCBroadcaster — WhatsOnChain (testnet/mainnet)
// ---------------------------------------------------------------------------

// WoCBroadcaster broadcasts transactions via the WhatsOnChain API.
type WoCBroadcaster struct {
	baseURL    string
	httpClient *http.Client
}

// NewWoCBroadcaster creates a WoC broadcaster for the given network.
func NewWoCBroadcaster(mainnet bool) *WoCBroadcaster {
	network := "test"
	if mainnet {
		network = "main"
	}
	return &WoCBroadcaster{
		baseURL: fmt.Sprintf("https://api.whatsonchain.com/v1/bsv/%s", network),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Broadcast sends a raw transaction to WoC and returns the txid.
func (w *WoCBroadcaster) Broadcast(tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	url := w.baseURL + "/tx/raw"

	body, _ := json.Marshal(map[string]string{
		"txhex": tx.Hex(),
	})

	resp, err := w.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, &transaction.BroadcastFailure{
			Code:        "NETWORK_ERROR",
			Description: fmt.Sprintf("WoC request failed: %s", err),
		}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, &transaction.BroadcastFailure{
			Code:        fmt.Sprintf("HTTP_%d", resp.StatusCode),
			Description: string(respBody),
		}
	}

	// WoC returns the txid as a plain string (JSON-quoted)
	txid := string(respBody)
	// Remove surrounding quotes if present
	if len(txid) > 2 && txid[0] == '"' && txid[len(txid)-1] == '"' {
		txid = txid[1 : len(txid)-1]
	}

	return &transaction.BroadcastSuccess{
		Txid:    txid,
		Message: "WoC broadcast accepted",
	}, nil
}

// BroadcastCtx is the context-aware version.
func (w *WoCBroadcaster) BroadcastCtx(ctx context.Context, tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	return w.Broadcast(tx)
}
