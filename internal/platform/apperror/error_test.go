package apperror

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestWrappedErrorsAndPrivacy(t *testing.T) {
	cause := errors.New("PRIVATE_SENTINEL")
	original := New(Conflict, cause)
	wrapped := fmt.Errorf("private wrapper: %w", original)
	var found *Error
	if !errors.As(wrapped, &found) || found != original || !errors.Is(wrapped, New(Conflict, nil)) || !errors.Is(wrapped, cause) || errors.Is(wrapped, New(Forbidden, nil)) {
		t.Fatal("wrapping semantics")
	}
	if Public(wrapped).Code() != Conflict || original.Error() != "CONFLICT" || strings.Contains(original.Message(), "PRIVATE") {
		t.Fatal("unsafe public error")
	}
	for _, err := range []error{cause, nil, New("unknown", cause), (*Error)(nil)} {
		if Public(err).Code() != Internal {
			t.Fatal("unknown error exposed")
		}
	}
	fields := []Field{{"amount", "INVALID_AMOUNT", "Must be positive"}}
	validation := Validation(fields)
	fields[0].Message = "PRIVATE_SENTINEL"
	copy := validation.Fields()
	copy[0].Message = "changed"
	if validation.Fields()[0].Message != "Must be positive" {
		t.Fatal("mutable diagnostics")
	}
	var nilError *Error
	if nilError.Code() != Internal || nilError.Unwrap() != nil || nilError.Fields() != nil {
		t.Fatal("nil safety")
	}
}

func TestRegistryAndSafeMetadata(t *testing.T) {
	codes := Codes()
	codes[0] = "changed"
	for _, code := range Codes() {
		if !code.Known() || code.Message() == "" {
			t.Fatal("invalid registry")
		}
	}
	for _, field := range []Field{{"", "REQUIRED", "Required"}, {"a\n", "REQUIRED", "Required"}, {"a", "bad", "Required"}, {"a", "REQUIRED", "bad\n"}, {"a", "REQUIRED", strings.Repeat("a", 257)}} {
		if Validation([]Field{field}).Code() != Internal {
			t.Fatal("unsafe validation metadata")
		}
	}
	if Validation(make([]Field, MaxFields+1)).Code() != Internal {
		t.Fatal("unbounded fields")
	}
	for _, code := range []string{"", strings.Repeat("A", 65), "a", "1A", "BAD-CODE"} {
		if SafeCode(code) {
			t.Fatal("unsafe code")
		}
	}
	for _, message := range []string{"", " bad", "bad ", "a\x00b", string([]byte{255})} {
		if SafeMessage(message) {
			t.Fatal("unsafe message")
		}
	}
}

func TestValidationCanonicalization(t *testing.T) {
	fields := []Field{{"b", "REQUIRED", "Required"}, {"a", "ZZZ", "Z"}, {"a", "AAA", "B"}, {"a", "AAA", "A"}, {"a", "AAA", "A"}}
	result := Validation(fields).Fields()
	if len(result) != 4 || result[0].Message != "A" || result[1].Message != "B" || result[2].Code != "ZZZ" || result[3].Field != "b" || fields[0].Field != "b" {
		t.Fatal("validation canonicalization/copy")
	}
	if errors.Is(New(Internal, nil), (*Error)(nil)) {
		t.Fatal("nil target matched")
	}
}
