package app

import "testing"

func TestOrderValidation(t *testing.T) {
	good := orderInput{Asset: "BTC", Currency: "USD", Amount: "0.1", Exchange: "coinbase"}
	if !validInput(good) {
		t.Fatal("valid order rejected")
	}
	for _, amount := range []string{"0", "-1", "NaN", "1e100", "0.123456789", "999999999999999999999"} {
		in := good
		in.Amount = amount
		if validInput(in) {
			t.Errorf("accepted invalid amount %q", amount)
		}
	}
	for _, asset := range []string{"BTC/USD", "../BTC", "btc", ""} {
		in := good
		in.Asset = asset
		if validInput(in) {
			t.Errorf("accepted invalid asset %q", asset)
		}
	}
	in := good
	in.Exchange = "unknown"
	if validInput(in) {
		t.Fatal("accepted unknown exchange")
	}
}
