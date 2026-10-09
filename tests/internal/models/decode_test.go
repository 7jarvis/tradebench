package models

import (
	"errors"
	"strings"
	"testing"
)

// Self-tests of the contract layer: a framework that silently accepts a
// broken payload is worse than no framework.

const validAccount = `{"id":"73732228-c05f-4683-b015-04c688c7a1ff","owner_name":"Jane","status":"active",
"currency":"USD","cash":"10.00","buying_power":"10.00","created_at":"2026-10-09T16:17:49.872274Z"}`

func TestDecodeAcceptsValidAccount(t *testing.T) {
	if _, err := Decode[Account]([]byte(validAccount)); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := map[string]struct{ body, wantErr string }{
		"missing field": {
			strings.Replace(validAccount, `"currency":"USD",`, "", 1), "currency: required field is missing"},
		"unknown field": {
			strings.Replace(validAccount, `"status"`, `"extra":1,"status"`, 1), "unknown field"},
		"money as number": {
			strings.Replace(validAccount, `"cash":"10.00"`, `"cash":10`, 1), "cannot unmarshal number"},
		"money with 1 decimal": {
			strings.Replace(validAccount, `"cash":"10.00"`, `"cash":"10.0"`, 1), "cash"},
		"bad enum": {
			strings.Replace(validAccount, `"active"`, `"frozen"`, 1), "status"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode[Account]([]byte(tc.body))
			var ce *ContractError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want contract error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestDecodeListRejectsNull(t *testing.T) {
	if _, err := DecodeList[Account]([]byte("null")); err == nil {
		t.Fatal("null list must be rejected")
	}
	got, err := DecodeList[Account]([]byte("[]"))
	if err != nil || len(got) != 0 {
		t.Fatalf("empty list: %v %v", got, err)
	}
}

func TestOrderInvariants(t *testing.T) {
	filledWithoutPrice := Order{
		ID: "73732228-c05f-4683-b015-04c688c7a1ff", ClientOrderID: "x", AccountID: "73732228-c05f-4683-b015-04c688c7a1ff",
		Symbol: "AAPL", Side: "buy", Type: "market", Qty: "1", Status: "filled", FilledQty: "1",
	}
	filledWithoutPrice.CreatedAt = filledWithoutPrice.CreatedAt.AddDate(2026, 0, 0)
	filledWithoutPrice.UpdatedAt = filledWithoutPrice.CreatedAt
	if err := filledWithoutPrice.Validate(); err == nil || !strings.Contains(err.Error(), "filled order") {
		t.Fatalf("want invariant error, got %v", err)
	}
}
