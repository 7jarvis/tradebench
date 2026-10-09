// Package money lets tests compute expected amounts from their inputs
// instead of hard-coding magic numbers. It is written independently from the
// service implementation on purpose: the tests must not reuse the code they
// verify.
package money

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// Amount is a number of cents.
type Amount int64

// Of parses "190.00" / "190" / "-5.50". Invalid input fails the test.
func Of(t testing.TB, s string) Amount {
	t.Helper()
	neg := strings.HasPrefix(s, "-")
	whole, frac, _ := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	if len(frac) > 2 {
		t.Fatalf("money.Of(%q): more than 2 decimals", s)
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err1 := strconv.ParseInt(whole, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil {
		t.Fatalf("money.Of(%q): not an amount", s)
	}
	v := Amount(w*100 + f)
	if neg {
		v = -v
	}
	return v
}

func (a Amount) Mul(qty int) Amount    { return a * Amount(qty) }
func (a Amount) Add(b Amount) Amount   { return a + b }
func (a Amount) Sub(b Amount) Amount   { return a - b }
func (a Amount) DivRound(n int) Amount { return (a + Amount(n)/2) / Amount(n) }

// String formats as the API does: "1234.50", "-5.00".
func (a Amount) String() string {
	sign, v := "", int64(a)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}
