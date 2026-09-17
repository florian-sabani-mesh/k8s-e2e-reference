package app

import (
	"regexp"

	"github.com/shopspring/decimal"
)

// fiatAssets are quoted and displayed at 2 decimal places; crypto uses 8.
var fiatAssets = map[string]bool{"USD": true, "EUR": true, "GBP": true}

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,10}$`)
var amountPattern = regexp.MustCompile(`^[0-9]{1,20}(\.[0-9]{1,18})?$`)

// normalizeAmount returns a canonical decimal string used for storage and hashing, so
// "0.25", "0.250" and "0.2500" all map to one representation.
func normalizeAmount(raw string) string {
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return "0"
	}
	return d.String()
}

// formatAssetAmount renders a balance amount with asset-appropriate precision.
func formatAssetAmount(asset string, d decimal.Decimal) string {
	if fiatAssets[asset] {
		return d.StringFixed(2)
	}
	return d.StringFixed(8)
}
