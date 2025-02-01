package types

import "strings"

// CurrencyConfig holds configuration for different currencies and their symbols
var CURRENCY_CONFIG = map[string]CurrencyConfig{
	USD: {Symbol: "$", Precision: 2},
	EUR: {Symbol: "€", Precision: 2},
	GBP: {Symbol: "£", Precision: 2},
	AUD: {Symbol: "AUS", Precision: 2},
	CAD: {Symbol: "CAD", Precision: 2},
	JPY: {Symbol: "¥", Precision: 0},
	INR: {Symbol: "₹", Precision: 2},
	// TODO add more currencies later
}

type CurrencyConfig struct {
	Precision int32
	Symbol    string
}

const (
	USD = "usd"
	EUR = "eur"
	GBP = "gbp"
	AUD = "aud"
	CAD = "cad"
	JPY = "jpy"
	INR = "inr"

	DEFAULT_PRECISION = 2
)

// GetCurrencySymbol returns the symbol for a given currency code
// if the code is not found, it returns the code itself
func GetCurrencySymbol(code string) string {
	if config, ok := CURRENCY_CONFIG[strings.ToLower(code)]; ok {
		return config.Symbol
	}
	return code
}

// GetCurrencyPrecision returns the precision for a given currency code
// if the code is not found, it returns the default precision of 2
func GetCurrencyPrecision(code string) int32 {
	if config, ok := CURRENCY_CONFIG[code]; ok {
		return config.Precision
	}
	return DEFAULT_PRECISION
}

func GetCurrencyConfig(code string) CurrencyConfig {
	if config, ok := CURRENCY_CONFIG[code]; ok {
		return config
	}
	return CurrencyConfig{Precision: DEFAULT_PRECISION}
}

func IsMatchingCurrency(a, b string) bool {
	return strings.EqualFold(a, b)
}
