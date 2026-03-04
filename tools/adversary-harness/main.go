// adversary-harness is a standalone adversarial test tool for the x402 gateway.
//
// It simulates four classes of hostile behaviour:
//   - Miner ordering variance (competing transactions)
//   - Mempool suppression (visibility failures)
//   - Delayed broadcast (timing attacks)
//   - Fee delegation abuse (concurrent hammering)
//
// Usage:
//
//	go run ./tools/adversary-harness -adversary=all -clients=50 -duration=60s
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/merkle-works/x402-gateway/tools/adversary-harness/client"
	"github.com/merkle-works/x402-gateway/tools/adversary-harness/metrics"
	"github.com/merkle-works/x402-gateway/tools/adversary-harness/scenarios"
)

func main() {
	// CLI flags
	adversary := flag.String("adversary", "all", "Scenario to run: mining, mempool, broadcast, abuse, all")
	clientCount := flag.Int("clients", 10, "Number of concurrent clients per scenario")
	duration := flag.Duration("duration", 30*time.Second, "Test duration")
	baseURL := flag.String("url", "http://localhost:8402", "Gateway base URL")
	verbose := flag.Bool("verbose", false, "Enable debug logging")
	flag.Parse()

	// Setup logging
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	// Banner
	fmt.Println()
	fmt.Println("  ╔═══════════════════════════════════════════════╗")
	fmt.Println("  ║   x402 ADVERSARY HARNESS                     ║")
	fmt.Println("  ║   Adversarial protocol testing tool           ║")
	fmt.Println("  ╠═══════════════════════════════════════════════╣")
	fmt.Printf("  ║  Target:    %-34s ║\n", *baseURL)
	fmt.Printf("  ║  Scenario:  %-34s ║\n", *adversary)
	fmt.Printf("  ║  Clients:   %-34d ║\n", *clientCount)
	fmt.Printf("  ║  Duration:  %-34s ║\n", *duration)
	fmt.Println("  ╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Create gateway client
	gw := client.New(*baseURL)

	// Verify gateway is reachable
	health, err := gw.Health()
	if err != nil {
		logger.Error("cannot reach gateway", "url", *baseURL, "error", err)
		os.Exit(1)
	}
	logger.Info("gateway connected",
		"version", health.Version,
		"network", health.Network,
		"nonce_available", health.NoncePool.Available,
		"fee_available", health.FeePool.Available,
	)

	// Create metrics collector
	m := metrics.New()

	// Context with timeout and signal handling
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-sigCh:
			logger.Info("received signal, stopping", "signal", sig)
			cancel()
		case <-ctx.Done():
		}
	}()

	// Start metrics reporter (every 5 seconds)
	stop := make(chan struct{})
	m.StartReporter(5*time.Second, stop)

	// Determine which scenarios to run
	scenarioList := parseScenarios(*adversary)

	// Each scenario gets an equal share of the total duration
	perScenario := *duration / time.Duration(len(scenarioList))
	logger.Info("starting scenarios", "scenarios", scenarioList, "per_scenario", perScenario)
	startTime := time.Now()

	// Run scenarios sequentially, each with its own time budget
	for _, s := range scenarioList {
		select {
		case <-ctx.Done():
			goto done
		default:
		}

		scenarioCtx, scenarioCancel := context.WithTimeout(ctx, perScenario)

		logger.Info("═══════════════════════════════════════")
		logger.Info("SCENARIO: "+strings.ToUpper(s), "budget", perScenario)
		logger.Info("═══════════════════════════════════════")

		switch s {
		case "mining":
			scenarios.MinerOrdering(scenarioCtx, gw, m, *clientCount, logger)
		case "mempool":
			scenarios.MempoolSuppression(scenarioCtx, gw, m, *clientCount, logger)
		case "broadcast":
			scenarios.DelayedBroadcast(scenarioCtx, gw, m, *clientCount, logger)
		case "abuse":
			scenarios.FeeAbuse(scenarioCtx, gw, m, *clientCount, logger)
		default:
			logger.Error("unknown scenario", "name", s)
		}

		scenarioCancel()
	}

done:
	elapsed := time.Since(startTime)
	close(stop)

	// Final report
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println("  FINAL RESULTS")
	fmt.Printf("  Elapsed: %s\n", elapsed.Truncate(time.Millisecond))
	fmt.Println("═══════════════════════════════════════════════")
	m.Report()

	// Final health check
	healthAfter, err := gw.Health()
	if err == nil {
		fmt.Printf("\n  Pool state after test:\n")
		fmt.Printf("    Nonce:   %d avail / %d leased / %d spent / %d total\n",
			healthAfter.NoncePool.Available, healthAfter.NoncePool.Leased,
			healthAfter.NoncePool.Spent, healthAfter.NoncePool.Total)
		fmt.Printf("    Fee:     %d avail / %d leased / %d spent / %d total\n",
			healthAfter.FeePool.Available, healthAfter.FeePool.Leased,
			healthAfter.FeePool.Spent, healthAfter.FeePool.Total)
		fmt.Printf("    Payment: %d avail / %d leased / %d spent / %d total\n",
			healthAfter.PaymentPool.Available, healthAfter.PaymentPool.Leased,
			healthAfter.PaymentPool.Spent, healthAfter.PaymentPool.Total)
	}

	// Check for CRITICAL failures
	snap := m.Snapshot()
	criticals := 0
	for k, v := range snap {
		if strings.HasPrefix(k, "CRITICAL_") && v > 0 {
			fmt.Printf("\n  ⚠️  CRITICAL FAILURE: %s = %d\n", k, v)
			criticals++
		}
	}

	if criticals > 0 {
		fmt.Printf("\n  RESULT: FAIL (%d critical vulnerabilities detected)\n\n", criticals)
		os.Exit(1)
	} else {
		fmt.Printf("\n  RESULT: PASS (all protocol invariants held)\n\n")
	}
}

func parseScenarios(input string) []string {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "all" {
		return []string{"mining", "mempool", "broadcast", "abuse"}
	}
	parts := strings.Split(input, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
