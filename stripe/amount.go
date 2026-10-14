package stripe

import "strings"

// toSmallestUnit converts a major-unit amount (e.g. dollars) into the smallest
// currency unit Stripe expects (e.g. cents). Zero-decimal currencies (JPY, KRW,
// …) are passed through unchanged. Currency comparison is case-insensitive.
func toSmallestUnit(amount int64, currency string) int64 {
	if _, ok := zeroDecimal[strings.ToUpper(currency)]; ok {
		return amount
	}
	return amount * 100
}

// zeroDecimal lists the currencies Stripe treats as having no minor unit.
// See https://stripe.com/docs/currencies#zero-decimal
var zeroDecimal = map[string]struct{}{
	"BIF": {}, "CLP": {}, "DJF": {}, "GNF": {}, "JPY": {}, "KMF": {},
	"KRW": {}, "MGA": {}, "PYG": {}, "RWF": {}, "UGX": {}, "VND": {},
	"VUV": {}, "XAF": {}, "XOF": {}, "XPF": {},
}
