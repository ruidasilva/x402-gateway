// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//


package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/merkleworks/x402-bsv/internal/hdwallet"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	printBanner()

	// Step 1: Key Management
	xpriv, wif, keys := stepKeyManagement(reader)

	// Step 2: Network
	network := stepNetwork(reader)

	// Step 3: Broadcaster
	broadcaster := stepBroadcaster(reader)

	// Step 4: Pool Configuration
	poolSize, feeRate, threshold, optimalSize := stepPoolConfig(reader)

	// Step 5: Storage
	redisEnabled, redisURL := stepStorage(reader)

	// Step 6: Port
	port := stepPort(reader)

	// Step 7: Payee address
	payeeAddr := stepPayee(reader, keys)

	// Summary
	fmt.Println()
	fmt.Println("  \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500")
	fmt.Println("  Configuration Summary:")
	fmt.Printf("    Network:      %s\n", network)
	fmt.Printf("    Broadcaster:  %s\n", broadcaster)
	fmt.Printf("    Port:         %d\n", port)
	if redisEnabled {
		fmt.Printf("    Storage:      Redis (%s)\n", redisURL)
	} else {
		fmt.Println("    Storage:      in-memory")
	}
	if xpriv != "" {
		fmt.Printf("    Key mode:     HD wallet (xPriv)\n")
		fmt.Printf("    Nonce addr:   %s\n", keys.NonceAddress)
		fmt.Printf("    Fee addr:     %s\n", keys.FeeAddress)
		fmt.Printf("    Treasury:     %s\n", keys.TreasuryAddress)
	} else {
		fmt.Printf("    Key mode:     single key (WIF)\n")
		fmt.Printf("    Address:      %s\n", keys.NonceAddress)
	}
	fmt.Printf("    Payee:        %s\n", payeeAddr)
	fmt.Printf("    Pool size:    %d\n", poolSize)
	fmt.Printf("    Fee rate:     %g sat/byte\n", feeRate)
	fmt.Println("  \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500")
	fmt.Println()

	// Write .env
	if askYesNo(reader, "Write .env file?") {
		writeEnvFile(xpriv, wif, network, broadcaster, port, poolSize, feeRate,
			threshold, optimalSize, redisEnabled, redisURL, payeeAddr)
		fmt.Println("  \u2713 .env written")
	}
	fmt.Println()

	// Start docker compose
	if askYesNo(reader, "Start docker compose?") {
		fmt.Println()
		fmt.Println("  Starting x402 gateway...")
		cmd := exec.Command("docker", "compose", "up", "-d", "--build")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  Error: %s\n", err)
			fmt.Println("  You can start manually: docker compose up -d --build")
		} else {
			fmt.Println()
			fmt.Printf("  \u2713 x402 gateway running!\n")
			fmt.Printf("  Dashboard: http://localhost:%d/\n", port)
		}
	}

	fmt.Println()
}

func printBanner() {
	fmt.Println()
	fmt.Println("  \u2554\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2557")
	fmt.Println("  \u2551     x402 BSV Gateway \u2014 Setup Wizard     \u2551")
	fmt.Println("  \u255a\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u255d")
	fmt.Println()
}

func stepKeyManagement(reader *bufio.Reader) (xpriv, wif string, keys *hdwallet.DerivedKeys) {
	fmt.Println("  [1/7] Key Management")
	fmt.Println("    (a) Generate new HD wallet (xPriv)")
	fmt.Println("    (b) Import existing xPriv")
	fmt.Println("    (c) Import WIF key (legacy)")
	choice := prompt(reader, "  > ")

	switch strings.ToLower(choice) {
	case "a":
		xprivStr, k, err := hdwallet.GenerateXPriv(false) // network set later
		if err != nil {
			fmt.Printf("  Error generating xPriv: %s\n", err)
			os.Exit(1)
		}
		xpriv = xprivStr
		keys = k
		fmt.Println()
		fmt.Printf("  Generated xPriv: %s\n", xpriv)
		fmt.Printf("  Nonce address:   %s\n", keys.NonceAddress)
		fmt.Printf("  Fee address:     %s\n", keys.FeeAddress)
		fmt.Printf("  Treasury:        %s\n", keys.TreasuryAddress)
		fmt.Println()
		fmt.Println("  \u26a0  SAVE YOUR XPRIV \u2014 it cannot be recovered!")
		fmt.Println()

	case "b":
		xpriv = prompt(reader, "  Enter xPriv: ")
		k, err := hdwallet.DeriveFromXPriv(xpriv, false) // network set later
		if err != nil {
			fmt.Printf("  Invalid xPriv: %s\n", err)
			os.Exit(1)
		}
		keys = k
		fmt.Printf("  Nonce address:   %s\n", keys.NonceAddress)
		fmt.Printf("  Fee address:     %s\n", keys.FeeAddress)
		fmt.Printf("  Treasury:        %s\n", keys.TreasuryAddress)
		fmt.Println()

	case "c":
		wif = prompt(reader, "  Enter WIF: ")
		k, err := hdwallet.DeriveFromWIF(wif, false) // network set later
		if err != nil {
			fmt.Printf("  Invalid WIF: %s\n", err)
			os.Exit(1)
		}
		keys = k
		fmt.Printf("  Address: %s\n", keys.NonceAddress)
		fmt.Println()

	default:
		fmt.Println("  Invalid choice. Exiting.")
		os.Exit(1)
	}

	return
}

func stepNetwork(reader *bufio.Reader) string {
	fmt.Println("  [2/7] Network")
	fmt.Println("    (a) Testnet (recommended for testing)")
	fmt.Println("    (b) Mainnet")
	choice := prompt(reader, "  > ")

	switch strings.ToLower(choice) {
	case "b":
		fmt.Println()
		return "mainnet"
	default:
		fmt.Println()
		return "testnet"
	}
}

func stepBroadcaster(reader *bufio.Reader) string {
	fmt.Println("  [3/7] Broadcaster")
	fmt.Println("    (a) Mock (demo/offline \u2014 no real transactions)")
	fmt.Println("    (b) WhatsOnChain (testnet/mainnet \u2014 real broadcasts)")
	choice := prompt(reader, "  > ")

	switch strings.ToLower(choice) {
	case "b":
		fmt.Println()
		return "woc"
	default:
		fmt.Println()
		return "mock"
	}
}

func stepPoolConfig(reader *bufio.Reader) (poolSize int, feeRate float64, threshold, optimalSize int) {
	fmt.Println("  [4/7] Pool Configuration")

	poolSize = promptIntDefault(reader, "  Nonce pool size", 100)
	feeRate = promptFloatDefault(reader, "  Fee rate (sat/byte)", 0.001)
	threshold = promptIntDefault(reader, "  Pool refill threshold", 500)
	optimalSize = promptIntDefault(reader, "  Pool optimal size", 5000)

	fmt.Println()
	return
}

func stepStorage(reader *bufio.Reader) (redisEnabled bool, redisURL string) {
	fmt.Println("  [5/7] Storage")
	fmt.Println("    (a) Redis (recommended for production)")
	fmt.Println("    (b) In-memory (demo only, data lost on restart)")
	choice := prompt(reader, "  > ")

	switch strings.ToLower(choice) {
	case "a":
		redisURL = promptDefault(reader, "  Redis URL", "redis://localhost:6379")
		redisEnabled = true
	default:
		redisEnabled = false
	}

	fmt.Println()
	return
}

func stepPort(reader *bufio.Reader) int {
	fmt.Println("  [6/7] Port")
	port := promptIntDefault(reader, "  HTTP port", 8402)
	fmt.Println()
	return port
}

func stepPayee(reader *bufio.Reader, keys *hdwallet.DerivedKeys) string {
	fmt.Println("  [7/7] Payee Address")
	fmt.Printf("    Default payee (receives payments): %s\n", keys.TreasuryAddress)
	addr := promptDefault(reader, "  Payee address", keys.TreasuryAddress)
	fmt.Println()
	return addr
}

// Helpers

func prompt(reader *bufio.Reader, label string) string {
	fmt.Print(label)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func promptDefault(reader *bufio.Reader, label, def string) string {
	fmt.Printf("%s [%s]: ", label, def)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func promptIntDefault(reader *bufio.Reader, label string, def int) int {
	s := promptDefault(reader, label, strconv.Itoa(def))
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func promptFloatDefault(reader *bufio.Reader, label string, def float64) float64 {
	s := promptDefault(reader, label, fmt.Sprintf("%g", def))
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

func askYesNo(reader *bufio.Reader, question string) bool {
	answer := prompt(reader, "  "+question+" (y/n) > ")
	return strings.ToLower(answer) == "y" || strings.ToLower(answer) == "yes"
}

func writeEnvFile(xpriv, wif, network, broadcaster string, port, poolSize int,
	feeRate float64, threshold, optimalSize int, redisEnabled bool, redisURL, payeeAddr string) {

	var lines []string
	lines = append(lines, "# x402 Gateway Configuration")
	lines = append(lines, "# Generated by setup wizard")
	lines = append(lines, "")

	// Key
	if xpriv != "" {
		lines = append(lines, "# HD wallet (xPriv) - derives nonce, fee, and treasury keys")
		lines = append(lines, "XPRIV="+xpriv)
	} else {
		lines = append(lines, "# Legacy single-key mode (WIF)")
		lines = append(lines, "BSV_PRIVATE_KEY="+wif)
	}
	lines = append(lines, "")

	// Network
	lines = append(lines, "BSV_NETWORK="+network)
	lines = append(lines, "BROADCASTER="+broadcaster)
	lines = append(lines, "")

	// Pool
	lines = append(lines, fmt.Sprintf("NONCE_POOL_SIZE=%d", poolSize))
	lines = append(lines, fmt.Sprintf("FEE_RATE=%g", feeRate))
	lines = append(lines, fmt.Sprintf("POOL_REPLENISH_THRESHOLD=%d", threshold))
	lines = append(lines, fmt.Sprintf("POOL_OPTIMAL_SIZE=%d", optimalSize))
	lines = append(lines, "")

	// Port
	lines = append(lines, fmt.Sprintf("PORT=%d", port))
	lines = append(lines, "")

	// Storage
	if redisEnabled {
		lines = append(lines, "REDIS_ENABLED=true")
		lines = append(lines, "REDIS_URL="+redisURL)
	} else {
		lines = append(lines, "REDIS_ENABLED=false")
	}
	lines = append(lines, "")

	// Payee
	if payeeAddr != "" {
		lines = append(lines, "PAYEE_ADDRESS="+payeeAddr)
	}
	lines = append(lines, "")

	content := strings.Join(lines, "\n")
	if err := os.WriteFile(".env", []byte(content), 0600); err != nil {
		fmt.Printf("  Error writing .env: %s\n", err)
	}
}
