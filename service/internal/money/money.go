// Package money represents USD amounts as integer cents.
//
// Floats are never used for money. On the wire an amount is a JSON string
// with exactly two decimals ("1234.50"), the same convention most brokerage
// APIs follow.
package money

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Cents is an amount of money in US cents.
type Cents int64

// ErrInvalid is returned when a string is not a valid non-negative amount.
var ErrInvalid = errors.New("amount must be a non-negative decimal with at most 2 fractional digits")

// Parse converts "123", "123.4" or "123.45" into cents.
// Negative values, exponents and more than two decimals are rejected.
func Parse(s string) (Cents, error) {
	if s == "" || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		return 0, ErrInvalid
	}
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" || (hasDot && (frac == "" || len(frac) > 2)) {
		return 0, ErrInvalid
	}
	if !digitsOnly(whole) || !digitsOnly(frac) {
		return 0, ErrInvalid
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > (1<<62)/100 {
		return 0, ErrInvalid
	}
	f, _ := strconv.ParseInt(frac, 10, 64)
	return Cents(w*100 + f), nil
}

// String formats cents as "1234.50".
func (c Cents) String() string {
	sign := ""
	v := int64(c)
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// Mul returns the total for qty units at price c.
func (c Cents) Mul(qty int64) Cents { return Cents(int64(c) * qty) }

// MarshalJSON encodes cents as a JSON string.
func (c Cents) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

// UnmarshalJSON accepts only a JSON string; numbers are rejected on purpose.
func (c *Cents) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return ErrInvalid
	}
	v, err := Parse(s)
	if err != nil {
		return err
	}
	*c = v
	return nil
}

func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
