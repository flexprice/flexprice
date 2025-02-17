package types

import (
	"strings"

	"github.com/flexprice/flexprice/internal/errors"
)

// CurrencyConfig holds configuration for different currencies and their symbols
var CURRENCY_CONFIG = map[string]CurrencyConfig{
	"usd": {Symbol: "$", Precision: 2},
	"eur": {Symbol: "€", Precision: 2},
	"gbp": {Symbol: "£", Precision: 2},
	"aud": {Symbol: "AUS", Precision: 2},
	"cad": {Symbol: "CAD", Precision: 2},
	"jpy": {Symbol: "¥", Precision: 0},
	"inr": {Symbol: "₹", Precision: 2},
	"idr": {Symbol: "Rp", Precision: 2},
	"sgd": {Symbol: "S$", Precision: 2},
	"thb": {Symbol: "฿", Precision: 2},
	"myr": {Symbol: "RM", Precision: 2},
	"php": {Symbol: "₱", Precision: 2},
	"vnd": {Symbol: "₫", Precision: 0},
	"hkd": {Symbol: "HK$", Precision: 2},
	"krw": {Symbol: "₩", Precision: 0},
	"nzd": {Symbol: "NZ$", Precision: 2},
	"brl": {Symbol: "R$", Precision: 2},
	"chf": {Symbol: "CHF", Precision: 2},
	"clp": {Symbol: "CLP$", Precision: 0},
	"cny": {Symbol: "CN¥", Precision: 2},
	"czk": {Symbol: "CZK", Precision: 2},
	"dkk": {Symbol: "DKK", Precision: 2},
	"huf": {Symbol: "HUF", Precision: 2},
	"ils": {Symbol: "₪", Precision: 2},
	"mxn": {Symbol: "MX$", Precision: 2},
	"nok": {Symbol: "NOK", Precision: 2},
	"pln": {Symbol: "PLN", Precision: 2},
	"ron": {Symbol: "RON", Precision: 2},
	"rub": {Symbol: "₽", Precision: 2},
	"sar": {Symbol: "SAR", Precision: 2},
	"sek": {Symbol: "SEK", Precision: 2},
	"try": {Symbol: "TRY", Precision: 2},
	"twd": {Symbol: "NT$", Precision: 2},
	"zar": {Symbol: "ZAR", Precision: 2},
	// TODO add more currencies later
}

type CurrencyConfig struct {
	Precision int32
	Symbol    string
}

const (
	DEFAULT_PRECISION = 2
)

// GetCurrencySymbol returns the symbol for a given currency code
// if the code is not found, it returns the code itself
func GetCurrencySymbol(code string) string {
	if config, ok := CURRENCY_CONFIG[strings.ToLower(code)]; ok {
		return config.Symbol
	}
	return strings.ToUpper(code)
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

// ValidateCurrencyCode validates a currency code
// it checks if the currency code is 3 characters long
// and if it is a valid currency code
// TODO : use some library to validate iso 3166-1 alpha-3 currency codes
func ValidateCurrencyCode(currency string) error {
	if len(currency) != 3 {
		return errors.New(errors.ErrCodeValidation, "invalid currency code")
	}
	return nil
}
