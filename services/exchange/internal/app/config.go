package app

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/coinbase"
)

// scopes are the least-privilege Coinbase App permissions this sample needs:
// identify the user, read asset balances, send crypto, and obtain a refresh token.
var scopes = []string{"wallet:user:read", "wallet:accounts:read", "wallet:transactions:send", "offline_access"}

// Config is the fully externalized exchange service configuration.
type Config struct {
	DatabaseURL       string
	PricingURL        string
	RedirectAllowlist []string
	Coinbase          coinbase.Config
	EncryptionKey     string
	AppMode           string
	HTTPTimeout       time.Duration
	OAuthTTL          time.Duration
}

func loadConfig() (Config, error) {
	cfg := Config{
		DatabaseURL:   runtime.Required("DATABASE_URL"),
		PricingURL:    strings.TrimRight(runtime.Required("PRICING_URL"), "/"),
		EncryptionKey: runtime.Required("TOKEN_ENCRYPTION_KEY"),
		AppMode:       runtime.Env("APP_MODE", ""),
		HTTPTimeout:   runtime.Duration("COINBASE_TIMEOUT", "5s"),
		OAuthTTL:      runtime.Duration("OAUTH_STATE_TTL", "10m"),
		Coinbase: coinbase.Config{
			AuthorizeURL: runtime.Required("COINBASE_AUTHORIZE_URL"),
			TokenURL:     runtime.Required("COINBASE_TOKEN_URL"),
			APIBaseURL:   strings.TrimRight(runtime.Required("COINBASE_API_BASE_URL"), "/"),
			ClientID:     runtime.Required("COINBASE_CLIENT_ID"),
			ClientSecret: runtime.Required("COINBASE_CLIENT_SECRET"),
			CallbackURL:  runtime.Required("COINBASE_CALLBACK_URL"),
			Scopes:       scopes,
			Version:      runtime.Env("COINBASE_API_VERSION", "2024-11-01"),
		},
	}
	for _, raw := range strings.Split(runtime.Env("REDIRECT_URL_ALLOWLIST", ""), ",") {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			cfg.RedirectAllowlist = append(cfg.RedirectAllowlist, strings.TrimRight(trimmed, "/"))
		}
	}
	if len(cfg.RedirectAllowlist) == 0 {
		return Config{}, fmt.Errorf("REDIRECT_URL_ALLOWLIST must list at least one allowed origin")
	}
	if err := cfg.guard(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// guard refuses to start in E2E mode if any Coinbase endpoint points at a public
// Coinbase host. This is a defense against a misconfiguration turning a test into a
// real-money call; it is application-level protection, not an egress firewall.
func (c Config) guard() error {
	if c.AppMode != "e2e" {
		return nil
	}
	endpoints := map[string]string{
		"COINBASE_TOKEN_URL":     c.Coinbase.TokenURL,
		"COINBASE_API_BASE_URL":  c.Coinbase.APIBaseURL,
		"COINBASE_AUTHORIZE_URL": c.Coinbase.AuthorizeURL,
	}
	for name, raw := range endpoints {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return fmt.Errorf("invalid %s", name)
		}
		host := strings.ToLower(parsed.Hostname())
		if host == "coinbase.com" || strings.HasSuffix(host, ".coinbase.com") {
			return fmt.Errorf("E2E mode refuses public Coinbase host for %s", name)
		}
	}
	return nil
}

// allowedRedirect reports whether a caller-supplied application redirectUrl is
// permitted. Only the scheme+host+port origin is compared against the allowlist, so
// arbitrary open redirects are rejected.
func (c Config) allowedRedirect(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	origin := parsed.Scheme + "://" + parsed.Host
	for _, allowed := range c.RedirectAllowlist {
		if origin == allowed {
			return true
		}
	}
	return false
}
