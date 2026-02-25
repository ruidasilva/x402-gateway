package dashboard

import (
	"net/http"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/merkle-works/x402-gateway/internal/config"
	"github.com/merkle-works/x402-gateway/internal/hdwallet"
	"github.com/merkle-works/x402-gateway/internal/pool"
)

// DashboardAPI provides HTTP handlers for the React dashboard.
type DashboardAPI struct {
	cfg         *config.Config
	keys        *hdwallet.DerivedKeys
	noncePool   pool.Pool
	feePool     pool.Pool
	stats       *StatsCollector
	treasuryKey *ec.PrivateKey
	mainnet     bool
	broadcaster transaction.Broadcaster
	startTime   time.Time
	payeeAddr   string
}

// NewDashboardAPI creates a new dashboard API instance.
func NewDashboardAPI(
	cfg *config.Config,
	keys *hdwallet.DerivedKeys,
	noncePool, feePool pool.Pool,
	treasuryKey *ec.PrivateKey,
	mainnet bool,
	bcast transaction.Broadcaster,
	startTime time.Time,
	payeeAddr string,
) *DashboardAPI {
	return &DashboardAPI{
		cfg:         cfg,
		keys:        keys,
		noncePool:   noncePool,
		feePool:     feePool,
		stats:       NewStatsCollector(3600, time.Minute), // 1 hour of 1-min buckets
		treasuryKey: treasuryKey,
		mainnet:     mainnet,
		broadcaster: bcast,
		startTime:   startTime,
		payeeAddr:   payeeAddr,
	}
}

// Stats returns the stats collector for use by the logging middleware.
func (d *DashboardAPI) Stats() *StatsCollector {
	return d.stats
}

// RegisterRoutes adds all dashboard API routes to the provided mux.
func (d *DashboardAPI) RegisterRoutes(mux *http.ServeMux) {
	// Config endpoints
	mux.HandleFunc("GET /api/v1/config", d.handleGetConfig())
	mux.HandleFunc("PUT /api/v1/config", d.handleUpdateConfig())

	// Treasury endpoints
	mux.HandleFunc("GET /api/v1/treasury/info", d.handleTreasuryInfo())
	mux.HandleFunc("POST /api/v1/treasury/fanout", d.handleTreasuryFanout())
	mux.HandleFunc("GET /api/v1/treasury/history", d.handleTreasuryHistory())

	// Stats endpoints
	mux.HandleFunc("GET /api/v1/stats/summary", d.handleStatsSummary())
	mux.HandleFunc("GET /api/v1/stats/timeseries", d.handleStatsTimeseries())
}
