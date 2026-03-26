// derivekeys prints WIF keys and addresses derived from the configured XPRIV.
// Used to obtain the keys needed for the CLI client during e2e testing.
package main

import (
	"fmt"
	"os"

	"github.com/merkleworks/x402-bsv/internal/hdwallet"
)

func main() {
	xpriv := os.Getenv("XPRIV")
	if xpriv == "" {
		fmt.Fprintf(os.Stderr, "XPRIV environment variable is required\n")
		fmt.Fprintf(os.Stderr, "Usage: XPRIV=xprv... go run ./cmd/derivekeys\n")
		os.Exit(1)
	}
	network := os.Getenv("BSV_NETWORK")
	mainnet := network == "mainnet" || network == ""

	keys, err := hdwallet.DeriveFromXPriv(xpriv, mainnet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("NONCE_KEY_WIF=%s\n", keys.NonceKey.Wif())
	fmt.Printf("FEE_KEY_WIF=%s\n", keys.FeeKey.Wif())
	fmt.Printf("PAYMENT_KEY_WIF=%s\n", keys.PaymentKey.Wif())
	fmt.Printf("TREASURY_KEY_WIF=%s\n", keys.TreasuryKey.Wif())
	fmt.Printf("NONCE_ADDRESS=%s\n", keys.NonceAddress)
	fmt.Printf("FEE_ADDRESS=%s\n", keys.FeeAddress)
	fmt.Printf("PAYMENT_ADDRESS=%s\n", keys.PaymentAddress)
	fmt.Printf("TREASURY_ADDRESS=%s\n", keys.TreasuryAddress)
}
