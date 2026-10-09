package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Validatable is implemented by every response model.
type Validatable interface {
	Validate() error
}

// ContractError means the API answered, but not in the documented shape.
type ContractError struct {
	Model string
	Err   error
	Body  []byte
}

func (e *ContractError) Error() string {
	return fmt.Sprintf("contract violation in %s: %v\nbody: %s", e.Model, e.Err, e.Body)
}

func (e *ContractError) Unwrap() error { return e.Err }

// Decode parses body into T and fails on:
//   - unknown fields (the API added or renamed something),
//   - missing fields (every field without omitempty is required, null allowed),
//   - wrong types,
//   - T.Validate() errors (formats, enums, cross-field invariants).
func Decode[T Validatable](body []byte) (T, error) {
	var v T
	name := reflect.TypeOf(v).Name()
	if err := requireFields(body, reflect.TypeOf(v), ""); err != nil {
		return v, &ContractError{Model: name, Err: err, Body: body}
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, &ContractError{Model: name, Err: err, Body: body}
	}
	if err := v.Validate(); err != nil {
		return v, &ContractError{Model: name, Err: err, Body: body}
	}
	return v, nil
}

// DecodeList decodes a JSON array of T. A null body is a violation: empty
// collections must be returned as [].
func DecodeList[T Validatable](body []byte) ([]T, error) {
	var zero T
	name := "[]" + reflect.TypeOf(zero).Name()
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		if err == nil {
			err = errors.New("expected a JSON array, got null")
		}
		return nil, &ContractError{Model: name, Err: err, Body: body}
	}
	out := make([]T, 0, len(raw))
	for i, item := range raw {
		v, err := Decode[T](item)
		if err != nil {
			return nil, &ContractError{Model: name, Err: fmt.Errorf("item %d: %w", i, err), Body: body}
		}
		out = append(out, v)
	}
	return out, nil
}

var timeType = reflect.TypeOf(time.Time{})

// requireFields checks that every json field of t without ",omitempty" is
// present in body, recursing into nested structs.
func requireFields(body []byte, t reflect.Type, path string) error {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t == timeType {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return fmt.Errorf("%sexpected a JSON object: %w", path, err)
	}
	var errs []error
	for i := range t.NumField() {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		raw, ok := obj[name]
		if !ok {
			if !strings.Contains(opts, "omitempty") {
				errs = append(errs, fmt.Errorf("%s%s: required field is missing", path, name))
			}
			continue
		}
		if string(raw) != "null" {
			if err := requireFields(raw, f.Type, path+name+"."); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
