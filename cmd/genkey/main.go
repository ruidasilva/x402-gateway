package main

import (
	"fmt"
	"log"

	"github.com/merkle-works/x402-gateway/internal/hdwallet"
)

func main() {
	xpriv, keys, err := hdwallet.GenerateXPriv(true) // true = mainnet
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("         NEW MAINNET HD WALLET - BACKUP THIS!")
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("XPRIV (add to .env):")
	fmt.Println(xpriv)
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("                   MAINNET ADDRESSES")
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Printf("Fee Pool:     %s\n", keys.FeeAddress)
	fmt.Printf("Payment Pool: %s\n", keys.PaymentAddress)
	fmt.Printf("Treasury:     %s\n", keys.TreasuryAddress)
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("  FUND THE TREASURY ADDRESS TO SEED THE UTXO POOLS")
	fmt.Println("════════════════════════════════════════════════════════════")
}
