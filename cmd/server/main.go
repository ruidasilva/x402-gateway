package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/merkle-works/x402-gateway/internal/broadcast"
	"github.com/merkle-works/x402-gateway/internal/config"
	"github.com/merkle-works/x402-gateway/internal/delegator"
	"github.com/merkle-works/x402-gateway/internal/gatekeeper"
	"github.com/merkle-works/x402-gateway/internal/nonce"
	"github.com/merkle-works/x402-gateway/internal/pricing"
	"github.com/merkle-works/x402-gateway/internal/replay"
)

func main() {
	// Setup structured logging
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	logger := slog.Default()

	// Load config
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger.Info("config loaded",
		"network", cfg.BSVNetwork,
		"port", cfg.Port,
		"nonce_pool_size", cfg.NoncePoolSize,
		"fee_rate", cfg.FeeRate,
	)

	// Parse the delegator private key
	key, err := ec.PrivateKeyFromWif(cfg.BSVPrivateKey)
	if err != nil {
		logger.Error("invalid BSV_PRIVATE_KEY", "error", err)
		os.Exit(1)
	}

	mainnet := cfg.IsMainnet()

	// For v0.1, we use a mock broadcaster.
	// TODO: Replace with a real WoC/ARC broadcaster for testnet/mainnet.
	broadcaster := &broadcast.MockBroadcaster{}

	// Create nonce pool
	noncePool, err := nonce.NewPool(key, mainnet, cfg.NonceLeaseTTL, broadcaster)
	if err != nil {
		logger.Error("failed to create nonce pool", "error", err)
		os.Exit(1)
	}

	// Create fee UTXO pool (separate pool for fee inputs with larger denominations)
	feePool, err := nonce.NewPool(key, mainnet, cfg.NonceLeaseTTL, broadcaster)
	if err != nil {
		logger.Error("failed to create fee pool", "error", err)
		os.Exit(1)
	}

	// Create replay cache (10 minute TTL, 10K entries)
	replayCache := replay.New(10*time.Minute, 10000)

	// Create delegator
	deleg, err := delegator.New(key, mainnet, noncePool, feePool, broadcaster, replayCache, cfg.FeeRate)
	if err != nil {
		logger.Error("failed to create delegator", "error", err)
		os.Exit(1)
	}

	// Determine payee address (default to the delegator's own address)
	payeeAddr := cfg.PayeeAddress
	if payeeAddr == "" {
		payeeAddr = noncePool.Address()
		logger.Info("no PAYEE_ADDRESS set, using delegator address", "address", payeeAddr)
	}

	// Start nonce lease reclaim loop
	stop := make(chan struct{})
	noncePool.StartReclaimLoop(30*time.Second, stop)

	// Setup HTTP mux
	mux := http.NewServeMux()

	// --- Unprotected endpoints ---

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": "0.1.0",
			"network": cfg.BSVNetwork,
			"nonce_pool": noncePool.Stats(),
			"fee_pool":   feePool.Stats(),
		})
	})

	// Nonce lease endpoint
	mux.HandleFunc("GET /nonce/lease", func(w http.ResponseWriter, r *http.Request) {
		n, err := noncePool.Lease()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{
				"error": err.Error(),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"nonce": n,
		})
	})

	// Delegation endpoint
	mux.HandleFunc("POST /delegate/x402", func(w http.ResponseWriter, r *http.Request) {
		var req delegator.DelegationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid request body: " + err.Error(),
			})
			return
		}

		result, err := deleg.Accept(req)
		if err != nil {
			if delegErr, ok := err.(*delegator.DelegationError); ok {
				w.WriteHeader(delegErr.Status)
				json.NewEncoder(w).Encode(delegErr)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{
				"error": err.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// --- Protected endpoint (gated by x402 middleware) ---

	gatekeeperCfg := gatekeeper.Config{
		Delegator:    deleg,
		NoncePool:    noncePool,
		PayeeAddress: payeeAddr,
		Network:      cfg.BSVNetwork,
		PricingFunc:  pricing.Fixed(100), // 100 sats per request
		ChallengeTTL: 5 * time.Minute,
		BindHeaders:  []string{},
	}

	// Demo expensive endpoint
	expensive := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data":      "This response cost 100 satoshis via x402",
			"timestamp": time.Now().Unix(),
			"path":      r.URL.Path,
		})
	})

	mux.Handle("GET /v1/expensive", gatekeeper.Middleware(gatekeeperCfg)(expensive))
	mux.Handle("POST /v1/expensive", gatekeeper.Middleware(gatekeeperCfg)(expensive))

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      loggingMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Info("shutting down", "signal", sig.String())
		close(stop)
		server.Close()
	}()

	logger.Info("x402 gateway starting",
		"addr", addr,
		"payee", payeeAddr,
		"network", cfg.BSVNetwork,
	)
	fmt.Printf("\n  x402 BSV Gateway v0.1\n")
	fmt.Printf("  ─────────────────────\n")
	fmt.Printf("  Network:    %s\n", cfg.BSVNetwork)
	fmt.Printf("  Address:    %s\n", payeeAddr)
	fmt.Printf("  Port:       %d\n", cfg.Port)
	fmt.Printf("  Nonce pool: %d\n\n", cfg.NoncePoolSize)
	fmt.Printf("  Endpoints:\n")
	fmt.Printf("    GET  /health          Health check\n")
	fmt.Printf("    GET  /nonce/lease     Lease a nonce UTXO\n")
	fmt.Printf("    POST /delegate/x402   Submit payment proof\n")
	fmt.Printf("    GET  /v1/expensive    Protected demo endpoint (100 sats)\n\n")

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

// loggingMiddleware logs each HTTP request.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

