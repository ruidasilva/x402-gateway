package broadcast

import (
	"context"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// MockBroadcaster is a development broadcaster that accepts all transactions
// without actually broadcasting to the network.
// Replace with a real broadcaster (WoC, ARC) for testnet/mainnet.
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
