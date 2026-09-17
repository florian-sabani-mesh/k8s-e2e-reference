package app

import (
	"log/slog"
	"net/http"
	"time"

	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/provider"
	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/security"
	"github.com/google/uuid"
)

// createURL issues an OAuth authorization URL and persists the server-side session
// that binds our internal accountId to a random, single-use, expiring state. The
// browser never carries the accountId as authority.
func (s *service) createURL(w http.ResponseWriter, r *http.Request) {
	correlation := correlationOf(r)
	prov, err := s.provider(r.PathValue("exchange"))
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	accountID := r.URL.Query().Get("accountId")
	if _, err := uuid.Parse(accountID); err != nil {
		writeError(w, correlation, newError(http.StatusBadRequest, codeInvalidAccountID, "accountId must be a UUID"))
		return
	}
	redirectURL := r.URL.Query().Get("redirectUrl")
	if !s.cfg.allowedRedirect(redirectURL) {
		writeError(w, correlation, newError(http.StatusBadRequest, codeInvalidRedirect, "redirectUrl is not in the allowed list"))
		return
	}

	state, err := security.RandomState()
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	verifier, challenge, err := security.PKCE()
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	verifierCipher, err := s.cipher.Encrypt(verifier)
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	expiresAt := time.Now().Add(s.cfg.OAuthTTL)
	session := oauthSession{
		ID:           uuid.NewString(),
		Exchange:     prov.Name(),
		AccountID:    accountID,
		RedirectURL:  redirectURL,
		CodeVerifier: verifierCipher,
		ExpiresAt:    expiresAt,
	}
	if err := s.store.createSession(r.Context(), session, security.HashState(state)); err != nil {
		writeError(w, correlation, err)
		return
	}
	slog.Info("oauth url issued", "correlation_id", correlation, "account_id", accountID, "exchange", prov.Name())
	writeJSON(w, http.StatusOK, map[string]any{
		"exchange":         prov.Name(),
		"accountId":        accountID,
		"authorizationUrl": prov.AuthorizationURL(state, challenge),
		"redirectUrl":      redirectURL,
		"state":            state,
		"expiresAt":        expiresAt.UTC().Format(time.RFC3339),
	})
}

// callback completes the OAuth flow: validate and consume state, exchange the code,
// fetch the user and accounts, persist the encrypted connection and balances, then
// redirect the browser to the previously validated application redirectUrl.
func (s *service) callback(w http.ResponseWriter, r *http.Request) {
	correlation := correlationOf(r)
	prov, err := s.provider(r.PathValue("exchange"))
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, correlation, newError(http.StatusBadRequest, codeInvalidRequest, "code and state are required"))
		return
	}
	session, err := s.store.consumeSession(r.Context(), prov.Name(), security.HashState(state))
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	verifier, err := s.cipher.Decrypt(session.CodeVerifier)
	if err != nil {
		writeError(w, correlation, err)
		return
	}

	ctx := provider.WithCorrelation(r.Context(), correlation)
	token, err := prov.ExchangeAuthorizationCode(ctx, code, verifier)
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	user, err := prov.GetUser(ctx, token.AccessToken)
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	accounts, err := prov.ListAccounts(ctx, token.AccessToken)
	if err != nil {
		writeError(w, correlation, err)
		return
	}

	accessCipher, err := s.cipher.Encrypt(token.AccessToken)
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	refreshCipher, err := s.cipher.Encrypt(token.RefreshToken)
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	balances := make([]balanceRow, 0, len(accounts))
	for _, a := range accounts {
		balances = append(balances, balanceRow{
			ProviderAccountID: a.ID,
			Asset:             a.Asset,
			AccountName:       a.Name,
			AccountType:       a.Type,
			Amount:            normalizeAmount(a.Amount),
			Primary:           a.Primary,
		})
	}
	cexAccountID, err := s.store.persistConnection(ctx, connectParams{
		AccountID:         session.AccountID,
		Exchange:          prov.Name(),
		ProviderUserID:    user.ID,
		ProviderUserName:  user.Name,
		NativeCurrency:    user.NativeCurrency,
		CountryCode:       user.CountryCode,
		AccessTokenCipher: accessCipher,
		RefreshCipher:     refreshCipher,
		TokenExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
		GrantedScopes:     token.Scope,
		Balances:          balances,
	})
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	slog.Info("exchange connected", "correlation_id", correlation, "account_id", session.AccountID,
		"cex_account_id", cexAccountID, "exchange", prov.Name(), "provider_account_count", len(accounts))

	// Never place tokens or secrets in the redirect; only a non-sensitive status.
	target := session.RedirectURL + separator(session.RedirectURL) + "exchange=" + prov.Name() + "&status=connected"
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusFound)
}

func separator(raw string) string {
	for _, c := range raw {
		if c == '?' {
			return "&"
		}
	}
	return "?"
}
