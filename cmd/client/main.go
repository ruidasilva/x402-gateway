package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/merkle-works/x402-gateway/internal/challenge"
	"github.com/merkle-works/x402-gateway/internal/gatekeeper"
)

func main() {
	delegatorURL := flag.String("delegator", "", "Delegator URL (default: same host as target)")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: x402-client [--delegator <url>] <url>\n")
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  x402-client http://localhost:8402/v1/expensive\n")
		os.Exit(1)
	}

	targetURL := flag.Arg(0)

	// Step 1: Make initial request — expect 402
	fmt.Printf("→ GET %s\n", targetURL)
	resp, err := http.Get(targetURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPaymentRequired {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("← %d (expected 402)\n", resp.StatusCode)
		fmt.Printf("%s\n", body)
		os.Exit(0)
	}

	fmt.Printf("← 402 Payment Required\n")

	// Step 2: Parse challenge from WWW-Authenticate header
	authHeader := resp.Header.Get("WWW-Authenticate")
	if authHeader == "" {
		fmt.Fprintf(os.Stderr, "Error: no WWW-Authenticate header in 402 response\n")
		os.Exit(1)
	}

	// Strip "X402 " prefix
	if !strings.HasPrefix(authHeader, "X402 ") {
		fmt.Fprintf(os.Stderr, "Error: WWW-Authenticate header does not start with 'X402 '\n")
		os.Exit(1)
	}
	encoded := strings.TrimPrefix(authHeader, "X402 ")

	ch, err := challenge.Decode(encoded)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding challenge: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("  Challenge:\n")
	fmt.Printf("    Scheme:  %s\n", ch.Scheme)
	fmt.Printf("    Payee:   %s\n", ch.Payee)
	fmt.Printf("    Amount:  %d sats\n", ch.Amount)
	fmt.Printf("    Nonce:   %s:%d\n", ch.Nonce.TxID, ch.Nonce.Vout)
	fmt.Printf("    Hash:    %s\n", ch.ChallengeSHA256)

	// Step 3: Build partial transaction
	// Input 0: nonce UTXO (unsigned in v0.1)
	// Output 0: payee address for the challenge amount
	partialTx := transaction.NewTransaction()

	// Add nonce input (no signing template — unsigned in v0.1)
	err = partialTx.AddInputFrom(
		ch.Nonce.TxID,
		ch.Nonce.Vout,
		ch.Nonce.Script,
		ch.Nonce.Satoshis,
		nil, // no unlocking template — client doesn't sign in v0.1
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding nonce input: %s\n", err)
		os.Exit(1)
	}

	// Add payee output
	payeeAddr, err := script.NewAddressFromString(ch.Payee)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing payee address: %s\n", err)
		os.Exit(1)
	}
	_ = payeeAddr

	err = partialTx.PayToAddress(ch.Payee, ch.Amount)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding payee output: %s\n", err)
		os.Exit(1)
	}

	partialTxHex := hex.EncodeToString(partialTx.Bytes())
	fmt.Printf("  Partial TX: %s...\n", truncate(partialTxHex, 40))

	// Step 4: Encode proof
	proof := &gatekeeper.Proof{
		PartialTxHex:  partialTxHex,
		ChallengeHash: ch.ChallengeSHA256,
	}
	proofEncoded, err := gatekeeper.EncodeProof(proof)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding proof: %s\n", err)
		os.Exit(1)
	}

	// Step 5: Retry request with proof
	fmt.Printf("\n→ GET %s (with X-402-Proof)\n", targetURL)

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating request: %s\n", err)
		os.Exit(1)
	}
	req.Header.Set("X-402-Proof", proofEncoded)

	// If delegator URL is provided, we'd normally POST to it first.
	// For v0.1 with the integrated gateway, the proof goes directly to the same endpoint.
	_ = delegatorURL

	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	defer resp2.Body.Close()

	body, _ := io.ReadAll(resp2.Body)
	fmt.Printf("← %d\n", resp2.StatusCode)

	if receipt := resp2.Header.Get("X-402-Receipt"); receipt != "" {
		fmt.Printf("  Receipt: %s\n", receipt)
	}
	if txid := resp2.Header.Get("X-402-TxID"); txid != "" {
		fmt.Printf("  TxID:    %s\n", txid)
	}

	// Pretty-print JSON response
	var prettyJSON map[string]any
	if err := json.Unmarshal(body, &prettyJSON); err == nil {
		formatted, _ := json.MarshalIndent(prettyJSON, "  ", "  ")
		fmt.Printf("  %s\n", formatted)
	} else {
		fmt.Printf("  %s\n", body)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
