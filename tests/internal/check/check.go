// Package check holds the assertions used by tests.
//
// It is intentionally small and stdlib-only. Field assertions are "soft"
// (t.Errorf) so one run reports every mismatching field; preconditions use
// NoError, which stops the test.
package check

import (
	"reflect"
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/service"
)

// NoError stops the test: nothing after a failed precondition is meaningful.
func NoError(t testing.TB, err error, action string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

// Equal reports a mismatch on a named field and lets the test continue.
func Equal[T comparable](t testing.TB, field string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}

func NotEqual[T comparable](t testing.TB, field string, got, notWant T) {
	t.Helper()
	if got == notWant {
		t.Errorf("%s: must not be %v", field, got)
	}
}

func DeepEqual(t testing.TB, field string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got  %#v\n want %#v", field, got, want)
	}
}

func Nil[T any](t testing.TB, field string, got *T) {
	t.Helper()
	if got != nil {
		t.Errorf("%s: want null, got %v", field, *got)
	}
}

// Value fails the test if p is nil, otherwise returns *p.
func Value[T any](t testing.TB, field string, p *T) T {
	t.Helper()
	if p == nil {
		t.Fatalf("%s: want a value, got null", field)
	}
	return *p
}

func True(t testing.TB, cond bool, format string, args ...any) {
	t.Helper()
	if !cond {
		t.Errorf(format, args...)
	}
}

// APIError asserts err is the documented rejection (status + code) and
// returns it for further checks (e.g. Field). A 5xx, a transport error or a
// success all fail the test.
func APIError(t testing.TB, err error, status int, code string) *service.APIError {
	t.Helper()
	if err == nil {
		t.Fatalf("want API error %d %s, got success", status, code)
	}
	e, ok := service.AsAPIError(err)
	if !ok {
		t.Fatalf("want API error %d %s, got: %v", status, code, err)
	}
	if e.Status != status || e.Code != code {
		t.Fatalf("want API error %d %s, got %d %s (%s)", status, code, e.Status, e.Code, e.Message)
	}
	return e
}

// ValidationError asserts a 422 validation_error that names field.
func ValidationError(t testing.TB, err error, field string) {
	t.Helper()
	e := APIError(t, err, 422, "validation_error")
	if e.Field != field {
		t.Errorf("validation field: got %q, want %q", e.Field, field)
	}
}
