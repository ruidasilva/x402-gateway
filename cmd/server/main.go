package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"
	"github.com/redis/go-redis/v9"

	"github.com/merkle-works/x402-gateway/internal/broadcast"
	"github.com/merkle-works/x402-gateway/internal/config"
	"github.com/merkle-works/x402-gateway/internal/dashboard"
	"github.com/merkle-works/x402-gateway/internal/delegator"
	"github.com/merkle-works/x402-gateway/internal/feedelegator"
	"github.com/merkle-works/x402-gateway/internal/gatekeeper"
	"github.com/merkle-works/x402-gateway/internal/hdwallet"
	"github.com/merkle-works/x402-gateway/internal/pool"
	"github.com/merkle-works/x402-gateway/internal/pricing"
	"github.com/merkle-works/x402-gateway/internal/replay"
	"github.com/merkle-works/x402-gateway/internal/treasury"
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
		"pool_size", cfg.PoolSize,
		"fee_rate", cfg.FeeRate,
	)

	mainnet := cfg.IsMainnet()

	// Derive keys — xPriv (HD wallet) or WIF (legacy single-key)
	var keys *hdwallet.DerivedKeys
	if cfg.XPRIV != "" {
		keys, err = hdwallet.DeriveFromXPriv(cfg.XPRIV, mainnet)
		if err != nil {
			logger.Error("invalid XPRIV", "error", err)
			os.Exit(1)
		}
		logger.Info("HD wallet mode (xPriv)",
			"nonce_address", keys.NonceAddress,
			"fee_address", keys.FeeAddress,
			"payment_address", keys.PaymentAddress,
			"treasury_address", keys.TreasuryAddress,
		)
	} else {
		keys, err = hdwallet.DeriveFromWIF(cfg.BSVPrivateKey, mainnet)
		if err != nil {
			logger.Error("invalid BSV_PRIVATE_KEY", "error", err)
			os.Exit(1)
		}
		logger.Info("single-key mode (WIF)", "address", keys.FeeAddress)
	}

	// Fee key is used by delegator and fee delegator
	key := keys.FeeKey

	// Select broadcaster based on config (wrapped in Swappable for hot-swap via dashboard)
	var inner transaction.Broadcaster
	demoMode := false
	switch cfg.Broadcaster {
	case "woc":
		inner = broadcast.NewWoCBroadcaster(mainnet)
	case "mock":
		inner = &broadcast.MockBroadcaster{}
		demoMode = true
	default:
		logger.Error("unsupported BROADCASTER value", "value", cfg.Broadcaster)
		os.Exit(1)
	}
	bcast := broadcast.NewSwappable(inner, cfg.Broadcaster)

	// Create UTXO pools — either Redis-backed (production) or in-memory (demo)
	// Three pools:
	//   - Nonce pool: 1-sat UTXOs for replay protection (each challenge binds to one)
	//   - Fee pool: 1-sat UTXOs for miner fees
	//   - Payment pool: 100-sat UTXOs for service payments
	var noncePool, feePool, paymentPool pool.Pool
	var rdb *redis.Client // hoisted for use by treasury watcher
	if cfg.RedisEnabled {
		// Redis-backed pools — persistent across restarts
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			logger.Error("invalid REDIS_URL", "error", err)
			os.Exit(1)
		}
		rdb = redis.NewClient(opts)

		// Verify Redis connectivity
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			logger.Error("cannot connect to Redis", "url", cfg.RedisURL, "error", err)
			os.Exit(1)
		}
		logger.Info("connected to Redis", "url", cfg.RedisURL)

		np, err := pool.NewRedisPool(rdb, "nonce:", keys.NonceKey, mainnet, cfg.LeaseTTL)
		if err != nil {
			logger.Error("failed to create nonce pool", "error", err)
			os.Exit(1)
		}
		noncePool = np

		fp, err := pool.NewRedisPool(rdb, "fee:", keys.FeeKey, mainnet, cfg.LeaseTTL)
		if err != nil {
			logger.Error("failed to create fee pool", "error", err)
			os.Exit(1)
		}
		feePool = fp

		pp, err := pool.NewRedisPool(rdb, "payment:", keys.PaymentKey, mainnet, cfg.LeaseTTL)
		if err != nil {
			logger.Error("failed to create payment pool", "error", err)
			os.Exit(1)
		}
		paymentPool = pp
	} else {
		// In-memory pools — for demo mode and testing
		np, err := pool.NewMemoryPool(keys.NonceKey, mainnet, cfg.LeaseTTL, bcast)
		if err != nil {
			logger.Error("failed to create nonce pool", "error", err)
			os.Exit(1)
		}
		noncePool = np

		fp, err := pool.NewMemoryPool(keys.FeeKey, mainnet, cfg.LeaseTTL, bcast)
		if err != nil {
			logger.Error("failed to create fee pool", "error", err)
			os.Exit(1)
		}
		feePool = fp

		pp, err := pool.NewMemoryPool(keys.PaymentKey, mainnet, cfg.LeaseTTL, bcast)
		if err != nil {
			logger.Error("failed to create payment pool", "error", err)
			os.Exit(1)
		}
		paymentPool = pp
	}

	// Demo mode — auto-seed pools with synthetic UTXOs when using MockBroadcaster.
	if demoMode {
		seedDemoPools(noncePool, feePool, paymentPool, cfg.PoolSize, cfg.FeeRate, logger)
	}

	// Create Treasury UTXO watcher (polls WoC for unspent UTXOs)
	var watcher *treasury.TreasuryWatcher
	if cfg.TreasuryPollInterval > 0 {
		var err error
		watcher, err = treasury.NewTreasuryWatcher(
			mainnet,
			keys.TreasuryAddress,
			keys.TreasuryKey,
			time.Duration(cfg.TreasuryPollInterval)*time.Second,
			rdb, // nil if Redis not enabled
		)
		if err != nil {
			logger.Error("failed to create treasury watcher", "error", err)
			os.Exit(1)
		}
		logger.Info("treasury watcher configured",
			"address", keys.TreasuryAddress,
			"interval_s", cfg.TreasuryPollInterval,
		)
	}

	// Create event bus for SSE streaming to dashboard
	eventBus := NewEventBus()

	// Create replay cache (10 minute TTL, 10K entries)
	replayCache := replay.New(10*time.Minute, 10000)

	// Create delegator (the foundational settlement primitive)
	// Delegator only adds fee inputs and signs those — client constructs partial tx
	deleg, err := delegator.New(key, mainnet, feePool, replayCache, cfg.FeeRate)
	if err != nil {
		logger.Error("failed to create delegator", "error", err)
		os.Exit(1)
	}

	// Determine payee address and locking script
	payeeAddr := cfg.PayeeAddress
	if payeeAddr == "" {
		payeeAddr = feePool.Address()
		logger.Info("no PAYEE_ADDRESS set, using delegator address", "address", payeeAddr)
	}

	// Convert payee address to locking script hex
	payeeLockingScriptHex, err := addressToLockingScriptHex(payeeAddr)
	if err != nil {
		logger.Error("failed to derive payee locking script", "error", err)
		os.Exit(1)
	}

	// Create bounded challenge cache (5 min TTL, 10K max)
	challengeCache := gatekeeper.NewChallengeCache(5*time.Minute, 10000)

	// Create fee delegator handler (Node.js-compatible POST /api/v1/tx)
	feeDelegatorHandler, err := feedelegator.NewHandler(keys.FeeKey, mainnet, feePool, cfg.FeeRate)
	if err != nil {
		logger.Error("failed to create fee delegator handler", "error", err)
		os.Exit(1)
	}

	// Record server start time for uptime tracking
	startTime := time.Now()

	// Create dashboard API (React dashboard backend)
	dashAPI := dashboard.NewDashboardAPI(
		cfg, keys, noncePool, feePool, paymentPool,
		keys.TreasuryKey, mainnet, bcast,
		startTime, payeeAddr,
		watcher,
	)

	// Start pool lease reclaim loops
	stop := make(chan struct{})
	noncePool.StartReclaimLoop(30*time.Second, stop)
	feePool.StartReclaimLoop(30*time.Second, stop)
	paymentPool.StartReclaimLoop(30*time.Second, stop)

	// Start treasury watcher polling loop
	if watcher != nil {
		watcher.Start(stop)
	}

	// Setup HTTP mux
	mux := http.NewServeMux()

	// --- Unprotected endpoints ---

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":       "ok",
			"version":      "1.0.0",
			"network":      cfg.BSVNetwork,
			"nonce_pool":   noncePool.Stats(),
			"fee_pool":     feePool.Stats(),
			"payment_pool": paymentPool.Stats(),
		})
	})

	// Delegation endpoint (called by client directly)
	mux.HandleFunc("POST /delegate/x402", func(w http.ResponseWriter, r *http.Request) {
		var req delegator.DelegationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid request body: " + err.Error(),
			})
			return
		}

		// Enrich request with server-side data
		if req.ExpectedPayeeLockingScriptHex == "" {
			req.ExpectedPayeeLockingScriptHex = payeeLockingScriptHex
		}

		result, err := deleg.Accept(req)
		if err != nil {
			if delegErr, ok := err.(*delegator.DelegationError); ok {
				w.Header().Set("Content-Type", "application/json")
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
		MempoolChecker:        bcast, // Swappable implements broadcast.MempoolChecker
		NoncePool:             noncePool,
		ReplayCache:           replayCache,
		ChallengeCache:        challengeCache,
		PayeeLockingScriptHex: payeeLockingScriptHex,
		Network:               cfg.BSVNetwork,
		PricingFunc:           pricing.Fixed(100),
		ChallengeTTL:          5 * time.Minute,
		BindHeaders:           gatekeeper.HeaderAllowlist,
	}

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

	// --- Fee delegator API (Node.js-compatible drop-in) ---
	mux.HandleFunc("POST /api/v1/tx", feeDelegatorHandler.HandleDelegateTx())
	mux.HandleFunc("GET /api/utxo/stats", feeDelegatorHandler.HandleUTXOStats(cfg.RedisEnabled))
	mux.HandleFunc("GET /api/utxo/health", feeDelegatorHandler.HandleUTXOHealth())
	mux.HandleFunc("GET /api/health", feeDelegatorHandler.HandleHealth(startTime))

	// --- Dashboard API (React dashboard backend) ---
	dashAPI.RegisterRoutes(mux)

	// SSE event stream
	mux.HandleFunc("GET /api/v1/events/stream", handleEvents(eventBus))

	// --- Demo/Testing endpoints ---
	mux.HandleFunc("POST /demo/build-proof", handleBuildProof(demoClientDeps{
		nonceKey:    keys.NonceKey,
		paymentKey:  keys.PaymentKey,
		noncePool:   noncePool,
		paymentPool: paymentPool,
	}, deleg, payeeLockingScriptHex))
	mux.HandleFunc("GET /demo/events", handleEvents(eventBus)) // backward compat
	mux.HandleFunc("GET /demo/info", handleDemoInfo(cfg, noncePool, feePool, paymentPool, payeeAddr))

	// --- React Dashboard SPA ---
	mux.HandleFunc("GET /", handleDashboardSPA())

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      loggingMiddleware(mux, eventBus, dashAPI.Stats()),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Info("shutting down", "signal", sig.String())
		close(stop)
		server.Close()
	}()

	logger.Info("x402 gateway starting", "addr", addr, "payee", payeeAddr, "network", cfg.BSVNetwork)
	fmt.Printf("\n  x402 BSV Gateway v1.0\n")
	fmt.Printf("  ─────────────────────\n")
	fmt.Printf("  Network:    %s\n", cfg.BSVNetwork)
	if cfg.XPRIV != "" {
		fmt.Printf("  Key mode:   HD wallet (xPriv)\n")
		fmt.Printf("  Nonce:      %s\n", keys.NonceAddress)
		fmt.Printf("  Fee addr:   %s\n", keys.FeeAddress)
		fmt.Printf("  Payment:    %s\n", keys.PaymentAddress)
		fmt.Printf("  Treasury:   %s\n", keys.TreasuryAddress)
	} else {
		fmt.Printf("  Key mode:   single key (WIF)\n")
		fmt.Printf("  Address:    %s\n", keys.FeeAddress)
	}
	fmt.Printf("  Payee:      %s\n", payeeAddr)
	fmt.Printf("  Port:       %d\n", cfg.Port)
	fmt.Printf("  Pool size:  %d\n", cfg.PoolSize)
	if cfg.RedisEnabled {
		fmt.Printf("  Storage:    Redis (%s)\n", cfg.RedisURL)
	} else {
		fmt.Printf("  Storage:    in-memory\n")
	}
	if demoMode {
		fmt.Printf("  Mode:       demo (MockBroadcaster)\n")
	} else {
		fmt.Printf("  Mode:       live (%s)\n", cfg.Broadcaster)
	}
	if watcher != nil {
		fmt.Printf("  Watcher:    every %ds at %s\n", cfg.TreasuryPollInterval, keys.TreasuryAddress)
	} else {
		fmt.Printf("  Watcher:    disabled\n")
	}
	fmt.Printf("  Dashboard:  http://localhost:%d/\n", cfg.Port)
	fmt.Printf("\n  Endpoints:\n")
	fmt.Printf("    GET  /health          Health check\n")
	fmt.Printf("    POST /delegate/x402   Delegation (x402)\n")
	fmt.Printf("    GET  /v1/expensive    Protected (100 sats)\n")
	fmt.Printf("    POST /api/v1/tx       Fee delegator API (Node.js-compat)\n")
	fmt.Printf("    GET  /api/utxo/stats  UTXO pool stats\n")
	fmt.Printf("    GET  /api/utxo/health UTXO pool health\n")
	fmt.Printf("    GET  /api/health      API health\n")
	fmt.Printf("    GET  /api/v1/config   Dashboard config\n")
	fmt.Printf("    GET  /api/v1/stats/*  Dashboard analytics\n")
	fmt.Printf("    GET  /api/v1/treasury/* Treasury mgmt\n")
	fmt.Printf("    GET  /                Dashboard (React SPA)\n")
	fmt.Printf("\n")

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func addressToLockingScriptHex(addr string) (string, error) {
	a, err := script.NewAddressFromString(addr)
	if err != nil {
		return "", fmt.Errorf("parse address %q: %w", addr, err)
	}
	s, err := p2pkh.Lock(a)
	if err != nil {
		return "", fmt.Errorf("create locking script: %w", err)
	}
	return hex.EncodeToString(*s), nil
}

func loggingMiddleware(next http.Handler, eventBus *EventBus, stats *dashboard.StatsCollector) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", rw.status, "duration_ms", duration.Milliseconds())

		// Record stats for dashboard analytics
		if stats != nil {
			stats.Record(dashboard.RequestStat{
				Timestamp: time.Now(),
				Path:      r.URL.Path,
				Method:    r.Method,
				Status:    rw.status,
				Duration:  duration,
			})
		}

		if eventBus != nil {
			eventBus.Emit(Event{
				Type:       eventTypeFromStatus(rw.status, r.URL.Path),
				Path:       r.URL.Path,
				Method:     r.Method,
				Status:     rw.status,
				DurationMS: duration.Milliseconds(),
				Timestamp:  time.Now(),
				Details:    eventDetailsFromHeaders(rw, r),
			})
		}
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

// Flush implements http.Flusher for SSE support.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
