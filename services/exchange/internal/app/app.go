// Package app is the exchange service: OAuth connection, portfolio valuation and
// crypto withdrawals against a provider-neutral abstraction. Coinbase is the only
// implemented provider; USD valuation is delegated to the pricing service over real
// Kubernetes HTTP, never by importing pricing logic.
package app

import (
	"context"
	"net/http"
	"time"

	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/coinbase"
	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/provider"
	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/security"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type service struct {
	db          *pgxpool.Pool
	store       *store
	cfg         Config
	cipher      *security.Cipher
	providers   map[string]provider.Provider
	pricingHTTP *http.Client
}

// Run wires dependencies and serves until the context is cancelled.
func Run(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cipher, err := security.NewCipher(cfg.EncryptionKey)
	if err != nil {
		return err
	}
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	// Outbound Coinbase client: bounded timeout, no proxy, redirects never followed so
	// a stubbed 3xx cannot bounce a call to a real provider.
	transport := &http.Transport{Proxy: nil, MaxIdleConns: 16, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second}
	coinbaseClient := &http.Client{
		Timeout:       cfg.HTTPTimeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	s := &service{
		db:          db,
		store:       &store{db: db},
		cfg:         cfg,
		cipher:      cipher,
		providers:   map[string]provider.Provider{"coinbase": coinbase.New(cfg.Coinbase, coinbaseClient, 3)},
		pricingHTTP: &http.Client{Timeout: cfg.HTTPTimeout},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /exchanges/{exchange}/url", s.createURL)
	mux.HandleFunc("GET /exchanges/{exchange}/callback", s.callback)
	mux.HandleFunc("GET /exchanges/{exchange}/accounts/{accountId}/balance", s.balance)
	mux.HandleFunc("POST /exchanges/{exchange}/accounts/{accountId}/withdrawals", s.withdraw)

	return runtime.Serve(ctx, mux, func(ctx context.Context) error {
		var exists bool
		return db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM cex_accounts LIMIT 1)").Scan(&exists)
	})
}

// provider resolves a supported exchange or returns a normalized unsupported error.
func (s *service) provider(name string) (provider.Provider, error) {
	if p, ok := s.providers[name]; ok {
		return p, nil
	}
	return nil, newError(http.StatusBadRequest, codeUnsupportedExchange, name+" is not a supported exchange")
}

// correlationOf returns the caller's correlation id, or a fresh one, bounded in length.
func correlationOf(r *http.Request) string {
	id := r.Header.Get("X-Test-ID")
	if id == "" {
		id = uuid.NewString()
	}
	if len(id) > 128 {
		id = id[:128]
	}
	return id
}
