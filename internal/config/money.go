package config

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// offsets is Meta's currency table: every ad account currency and how many
// minor units make one major unit. Budgets are written in the minor unit.
// Source: Marketing API, "Currency codes"
// (developers.facebook.com/documentation/ads-commerce/marketing-api/currencies),
// read on 2026-09-25. Meta's offsets differ from ISO 4217 for some currencies
// (HUF, ISK and TWD have offset 1, BHD has 100), so never derive them from ISO.
var offsets = map[string]int64{
	"AED": 100, "ARS": 100, "AUD": 100, "BDT": 100, "BGN": 100, "BHD": 100, "BOB": 100, "BRL": 100,
	"CAD": 100, "CHF": 100, "CLP": 1, "CNY": 100, "COP": 1, "CRC": 1, "CZK": 100, "DKK": 100,
	"DZD": 100, "EGP": 100, "EUR": 100, "FBZ": 100, "GBP": 100, "GTQ": 100, "HKD": 100, "HNL": 100,
	"HRK": 100, "HUF": 1, "IDR": 1, "ILS": 100, "INR": 100, "ISK": 1, "JOD": 100, "JPY": 1,
	"KES": 100, "KRW": 1, "LTL": 100, "LVL": 100, "MOP": 100, "MXN": 100, "MYR": 100, "NGN": 100,
	"NIO": 100, "NOK": 100, "NZD": 100, "PEN": 100, "PHP": 100, "PKR": 100, "PLN": 100, "PYG": 1,
	"QAR": 100, "RON": 100, "RSD": 100, "RUB": 100, "SAR": 100, "SEK": 100, "SGD": 100, "SKK": 100,
	"THB": 100, "TRY": 100, "TWD": 1, "UAH": 100, "USD": 100, "UYU": 100, "VEF": 100, "VES": 100,
	"VND": 1, "ZAR": 100,
}

// NormalizeCurrency upper-cases a currency code and checks it against
// Meta's table.
func NormalizeCurrency(s string) (string, error) {
	c := strings.ToUpper(strings.TrimSpace(s))
	if _, ok := offsets[c]; !ok {
		return "", fmt.Errorf("%q is not an ad account currency Meta supports (for example CHF, EUR, USD)", s)
	}
	return c, nil
}

// Offset returns how many minor units make one major unit: 100 for CHF, 1
// for JPY.
func Offset(currency string) (int64, bool) {
	o, ok := offsets[currency]
	return o, ok
}

var majorPattern = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]+))?$`)

// maxMajor bounds an amount far below int64 overflow.
const maxMajor = 1_000_000_000

// ParseMajor reads an amount in major units ("30", "29.50") for currency and
// returns it in the minor unit Meta uses (3000, 2950). Only a dot separates
// decimals, with at most as many places as the currency has (two for CHF,
// none for JPY); zero, negatives, exponents and thousands separators are
// refused.
func ParseMajor(s, currency string) (int64, error) {
	off, ok := offsets[currency]
	if !ok {
		return 0, fmt.Errorf("unknown currency %q; set the currency first", currency)
	}
	s = strings.TrimSpace(s)
	if strings.Contains(s, ",") {
		return 0, fmt.Errorf("use a dot as the decimal separator and no thousands separator (29.50, not 29,50), got %q", s)
	}
	places := 0
	if off == 100 {
		places = 2
	}
	m := majorPattern.FindStringSubmatch(s)
	switch {
	case m == nil:
		if places == 0 {
			return 0, fmt.Errorf("want a whole amount in %s such as 3000, got %q", currency, s)
		}
		return 0, fmt.Errorf("want an amount in %s such as 30 or 29.50, got %q", currency, s)
	case len(m[2]) > places && places == 0:
		return 0, fmt.Errorf("want a whole amount in %s such as 3000, got %q", currency, s)
	case len(m[2]) > places:
		return 0, fmt.Errorf("want an amount in %s with at most two decimal places, got %q", currency, s)
	}
	units, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || units > maxMajor {
		return 0, fmt.Errorf("amount %q is too large", s)
	}
	var frac int64
	if places == 2 {
		frac, _ = strconv.ParseInt(m[2]+strings.Repeat("0", 2-len(m[2])), 10, 64)
	}
	minor := units*off + frac
	if minor == 0 {
		return 0, errors.New("amount must be greater than 0")
	}
	return minor, nil
}

// FormatMinor renders a minor-unit amount in major units with the currency:
// 3000 CHF becomes "30.00 CHF", 5000 JPY "5000 JPY".
func FormatMinor(minor int64, currency string) string {
	if offsets[currency] == 100 {
		return fmt.Sprintf("%d.%02d %s", minor/100, minor%100, currency)
	}
	return fmt.Sprintf("%d %s", minor, currency)
}
