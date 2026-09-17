package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// balance values a connected account's stored portfolio in USD. It reads the balance
// snapshot persisted at connection time and prices each non-USD asset through the
// pricing service over real Kubernetes HTTP. USD is valued at 1 without a lookup.
func (s *service) balance(w http.ResponseWriter, r *http.Request) {
	correlation := correlationOf(r)
	prov, err := s.provider(r.PathValue("exchange"))
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	accountID := r.PathValue("accountId")
	if _, err := uuid.Parse(accountID); err != nil {
		writeError(w, correlation, newError(http.StatusBadRequest, codeInvalidAccountID, "accountId must be a UUID"))
		return
	}
	quote := r.URL.Query().Get("quote")
	if quote == "" {
		quote = "USD"
	}
	if quote != "USD" {
		writeError(w, correlation, newError(http.StatusBadRequest, codeInvalidRequest, "only USD quote is supported"))
		return
	}

	conn, err := s.store.getConnection(r.Context(), accountID, prov.Name())
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	balances, err := s.store.getBalances(r.Context(), conn.ID)
	if err != nil {
		writeError(w, correlation, err)
		return
	}

	total := decimal.Zero
	asOf := time.Now().UTC()
	assets := make([]map[string]any, 0, len(balances))
	for _, b := range balances {
		amount, err := decimal.NewFromString(b.Amount)
		if err != nil {
			writeError(w, correlation, newError(http.StatusInternalServerError, codeInternal, "stored amount is not decimal"))
			return
		}
		price := decimal.NewFromInt(1)
		if b.Asset != quote {
			price, err = s.priceUSD(r.Context(), b.Asset, correlation)
			if err != nil {
				writeError(w, correlation, err)
				return
			}
		}
		value := amount.Mul(price)
		total = total.Add(value)
		asOf = b.FetchedAt.UTC()
		assets = append(assets, map[string]any{
			"providerAccountId": b.ProviderAccountID,
			"asset":             b.Asset,
			"amount":            formatAssetAmount(b.Asset, amount),
			"price":             map[string]string{"amount": price.StringFixed(2), "currency": quote},
			"value":             map[string]string{"amount": value.StringFixed(2), "currency": quote},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"accountId":     accountID,
		"exchange":      prov.Name(),
		"quoteCurrency": quote,
		"total":         total.StringFixed(2),
		"asOf":          asOf.Format(time.RFC3339),
		"assets":        assets,
	})
}

// priceUSD fetches a spot price from the pricing service, propagating the correlation
// id so the ticker request is traceable all the way to WireMock.
func (s *service) priceUSD(ctx context.Context, asset, correlation string) (decimal.Decimal, error) {
	endpoint := s.cfg.PricingURL + "/prices/" + url.PathEscape(asset) + "?quote=USD"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return decimal.Decimal{}, newError(http.StatusBadGateway, codeUpstreamUnavailable, "cannot build pricing request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Test-ID", correlation)
	resp, err := s.pricingHTTP.Do(req)
	if err != nil {
		return decimal.Decimal{}, newError(http.StatusBadGateway, codeUpstreamUnavailable, "pricing service unavailable")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return decimal.Decimal{}, newError(http.StatusBadGateway, codeUpstreamUnavailable, "pricing service returned an error for "+asset)
	}
	var parsed struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return decimal.Decimal{}, newError(http.StatusBadGateway, codeUpstreamInvalid, "pricing response was not valid JSON")
	}
	price, err := decimal.NewFromString(parsed.Price)
	if err != nil || !price.IsPositive() {
		return decimal.Decimal{}, newError(http.StatusBadGateway, codeUpstreamInvalid, "pricing response had no valid price")
	}
	return price, nil
}
