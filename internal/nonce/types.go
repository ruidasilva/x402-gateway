package nonce

import "time"

// NonceStatus represents the lifecycle state of a nonce UTXO.
type NonceStatus string

const (
	StatusAvailable NonceStatus = "available"
	StatusLeased    NonceStatus = "leased"
	StatusSpent     NonceStatus = "spent"
)

// NonceUTXO represents a 1-satoshi UTXO used as a cryptographic nonce.
// Single-spend enforcement on-chain guarantees replay protection.
type NonceUTXO struct {
	TxID      string      `json:"txid"`
	Vout      uint32      `json:"vout"`
	Script    string      `json:"script"`   // hex-encoded locking script (P2PKH)
	Satoshis  uint64      `json:"satoshis"` // always 1
	Status    NonceStatus `json:"status"`
	LeasedAt  time.Time   `json:"leased_at,omitempty"`
	ExpiresAt time.Time   `json:"expires_at,omitempty"`
}

// Outpoint returns the canonical "txid:vout" string for this nonce.
func (n *NonceUTXO) Outpoint() string {
	return n.TxID + ":" + itoa(n.Vout)
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for v > 0 {
		buf = append(buf, byte('0'+v%10))
		v /= 10
	}
	// reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
