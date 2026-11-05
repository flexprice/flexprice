package types

import (
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/shopspring/decimal"
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
	"clf": {Symbol: "CLF", Precision: 4},
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
	"kwd": {Symbol: "KD", Precision: 3},
	"ngn": {Symbol: "NGN", Precision: 2},
	"aed": {Symbol: "AED", Precision: 2},
	"afn": {Symbol: "Af", Precision: 2},
	"all": {Symbol: "ALL", Precision: 2},
	"amd": {Symbol: "AMD", Precision: 2},
	"aoa": {Symbol: "AOA", Precision: 2},
	"ars": {Symbol: "AR$", Precision: 2},
	"awg": {Symbol: "AWG", Precision: 2},
	"azn": {Symbol: "man.", Precision: 2},
	"bam": {Symbol: "KM", Precision: 2},
	"bbd": {Symbol: "BBD", Precision: 2},
	"bdt": {Symbol: "Tk", Precision: 2},
	"bhd": {Symbol: "BD", Precision: 3},
	"bif": {Symbol: "FBu", Precision: 0},
	"bmd": {Symbol: "BMD", Precision: 2},
	"bnd": {Symbol: "BN$", Precision: 2},
	"bob": {Symbol: "Bs", Precision: 2},
	"bsd": {Symbol: "BSD", Precision: 2},
	"btn": {Symbol: "BTN", Precision: 2},
	"bwp": {Symbol: "BWP", Precision: 2},
	"byn": {Symbol: "BYN", Precision: 2},
	"bzd": {Symbol: "BZ$", Precision: 2},
	"cdf": {Symbol: "CDF", Precision: 2},
	"cop": {Symbol: "CO$", Precision: 2},
	"crc": {Symbol: "₡", Precision: 2},
	"cup": {Symbol: "CUP", Precision: 2},
	"cve": {Symbol: "CV$", Precision: 2},
	"djf": {Symbol: "Fdj", Precision: 0},
	"dop": {Symbol: "RD$", Precision: 2},
	"dzd": {Symbol: "DA", Precision: 2},
	"egp": {Symbol: "EGP", Precision: 2},
	"ern": {Symbol: "Nfk", Precision: 2},
	"etb": {Symbol: "Br", Precision: 2},
	"fjd": {Symbol: "FJD", Precision: 2},
	"fkp": {Symbol: "FKP", Precision: 2},
	"gel": {Symbol: "GEL", Precision: 2},
	"ghs": {Symbol: "GH₵", Precision: 2},
	"gip": {Symbol: "GIP", Precision: 2},
	"gmd": {Symbol: "GMD", Precision: 2},
	"gnf": {Symbol: "FG", Precision: 0},
	"gtq": {Symbol: "GTQ", Precision: 2},
	"gyd": {Symbol: "GYD", Precision: 2},
	"hnl": {Symbol: "HNL", Precision: 2},
	"htg": {Symbol: "HTG", Precision: 2},
	"iqd": {Symbol: "IQD", Precision: 3},
	"irr": {Symbol: "IRR", Precision: 2},
	"isk": {Symbol: "Ikr", Precision: 0},
	"jmd": {Symbol: "J$", Precision: 2},
	"jod": {Symbol: "JD", Precision: 3},
	"kes": {Symbol: "Ksh", Precision: 2},
	"kgs": {Symbol: "KGS", Precision: 2},
	"khr": {Symbol: "KHR", Precision: 2},
	"kmf": {Symbol: "CF", Precision: 0},
	"kpw": {Symbol: "KPW", Precision: 2},
	"kyd": {Symbol: "KYD", Precision: 2},
	"kzt": {Symbol: "KZT", Precision: 2},
	"lak": {Symbol: "LAK", Precision: 2},
	"lbp": {Symbol: "LB£", Precision: 2},
	"lkr": {Symbol: "SLRs", Precision: 2},
	"lrd": {Symbol: "LRD", Precision: 2},
	"lsl": {Symbol: "LSL", Precision: 2},
	"lyd": {Symbol: "LD", Precision: 3},
	"mad": {Symbol: "MAD", Precision: 2},
	"mdl": {Symbol: "MDL", Precision: 2},
	"mga": {Symbol: "MGA", Precision: 2},
	"mkd": {Symbol: "MKD", Precision: 2},
	"mmk": {Symbol: "MMK", Precision: 2},
	"mnt": {Symbol: "MNT", Precision: 2},
	"mop": {Symbol: "MOP$", Precision: 2},
	"mru": {Symbol: "MRU", Precision: 2},
	"mur": {Symbol: "MURs", Precision: 2},
	"mvr": {Symbol: "MVR", Precision: 2},
	"mwk": {Symbol: "MWK", Precision: 2},
	"mzn": {Symbol: "MTn", Precision: 2},
	"nad": {Symbol: "N$", Precision: 2},
	"nio": {Symbol: "C$", Precision: 2},
	"npr": {Symbol: "NPRs", Precision: 2},
	"omr": {Symbol: "OMR", Precision: 3},
	"pab": {Symbol: "B/.", Precision: 2},
	"pen": {Symbol: "S/.", Precision: 2},
	"pgk": {Symbol: "PGK", Precision: 2},
	"pkr": {Symbol: "PKRs", Precision: 2},
	"pyg": {Symbol: "₲", Precision: 0},
	"qar": {Symbol: "QR", Precision: 2},
	"rsd": {Symbol: "din.", Precision: 2},
	"rwf": {Symbol: "RWF", Precision: 0},
	"sbd": {Symbol: "SBD", Precision: 2},
	"scr": {Symbol: "SCR", Precision: 2},
	"sdg": {Symbol: "SDG", Precision: 2},
	"shp": {Symbol: "SHP", Precision: 2},
	"sle": {Symbol: "Le", Precision: 2},
	"sos": {Symbol: "Ssh", Precision: 2},
	"srd": {Symbol: "SRD", Precision: 2},
	"ssp": {Symbol: "SSP", Precision: 2},
	"stn": {Symbol: "Db", Precision: 2},
	"syp": {Symbol: "SY£", Precision: 2},
	"szl": {Symbol: "SZL", Precision: 2},
	"tjs": {Symbol: "TJS", Precision: 2},
	"tmt": {Symbol: "TMT", Precision: 2},
	"tnd": {Symbol: "DT", Precision: 3},
	"top": {Symbol: "T$", Precision: 2},
	"ttd": {Symbol: "TT$", Precision: 2},
	"tzs": {Symbol: "TSh", Precision: 2},
	"uah": {Symbol: "₴", Precision: 2},
	"ugx": {Symbol: "USh", Precision: 0},
	"uyu": {Symbol: "$U", Precision: 2},
	"uzs": {Symbol: "UZS", Precision: 2},
	"ves": {Symbol: "Bs.S", Precision: 2},
	"vuv": {Symbol: "VUV", Precision: 0},
	"wst": {Symbol: "WST", Precision: 2},
	"xaf": {Symbol: "FCFA", Precision: 0},
	"xcd": {Symbol: "XCD", Precision: 2},
	"xcg": {Symbol: "Cg", Precision: 2},
	"xof": {Symbol: "CFA", Precision: 0},
	"xpf": {Symbol: "XPF", Precision: 0},
	"yer": {Symbol: "YR", Precision: 2},
	"zmw": {Symbol: "ZK", Precision: 2},
	"zwg": {Symbol: "ZiG", Precision: 2},
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
	if config, ok := CURRENCY_CONFIG[strings.ToLower(code)]; ok {
		return config.Precision
	}
	return DEFAULT_PRECISION
}

func GetCurrencyConfig(code string) CurrencyConfig {
	if config, ok := CURRENCY_CONFIG[strings.ToLower(code)]; ok {
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
		return ierr.NewError("invalid currency code").
			WithHint("Currency code must be 3 characters long").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ToSmallestUnit converts a decimal amount to the currency's smallest unit (e.g. cents for USD,
// fils for KWD, yen for JPY) as an integer. Uses the currency's precision: multiplier = 10^precision.
func ToSmallestUnit(amount decimal.Decimal, currency string) int64 {
	precision := GetCurrencyPrecision(currency)
	multiplier := decimal.New(1, precision) // 10^precision
	result := amount.Mul(multiplier).IntPart()
	if result < 0 {
		return 0
	}
	return result
}

func ToSignedSmallestUnit(amount decimal.Decimal, currency string) int64 {
	precision := GetCurrencyPrecision(currency)
	multiplier := decimal.New(1, precision)
	return amount.Mul(multiplier).IntPart()
}

// FromSmallestUnit converts an integer amount in the currency's smallest unit (e.g. cents for
// USD, fils for KWD, yen for JPY — JPY's smallest unit is 1 yen since it has zero decimal
// places) back to a decimal amount. Inverse of ToSmallestUnit.
func FromSmallestUnit(smallestUnitAmount int64, currency string) decimal.Decimal {
	precision := GetCurrencyPrecision(currency)
	divisor := decimal.New(1, precision) // 10^precision
	return decimal.NewFromInt(smallestUnitAmount).Div(divisor)
}

// RoundToCurrencyPrecision rounds a decimal amount to the appropriate currency precision.
//
// This function ensures monetary amounts are displayed and processed with the correct
// number of decimal places for the given currency (e.g., 2 for USD, 0 for JPY).
//
// Currency Precision Examples:
//
//	USD, EUR, GBP: 2 decimal places ($10.99)
//	JPY, KRW:      0 decimal places (¥1000)
//
// Usage:
//
//	amount := decimal.NewFromFloat(10.999)
//	rounded := types.RoundToCurrencyPrecision(amount, "usd") // Returns 11.00
func RoundToCurrencyPrecision(amount decimal.Decimal, currency string) decimal.Decimal {
	precision := GetCurrencyPrecision(currency)
	return amount.Round(precision)
}
