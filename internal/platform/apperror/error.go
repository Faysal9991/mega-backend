// Package apperror defines safe application failures independently of HTTP.
package apperror

import (
	"cmp"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Code string

const (
	InvalidRequest                 Code = "INVALID_REQUEST"
	InvalidJSON                    Code = "INVALID_JSON"
	UnsupportedMediaType           Code = "UNSUPPORTED_MEDIA_TYPE"
	ValidationFailed               Code = "VALIDATION_FAILED"
	Unauthenticated                Code = "UNAUTHENTICATED"
	Forbidden                      Code = "FORBIDDEN"
	NotFound                       Code = "NOT_FOUND"
	MethodNotAllowed               Code = "METHOD_NOT_ALLOWED"
	Conflict                       Code = "CONFLICT"
	PayloadTooLarge                Code = "PAYLOAD_TOO_LARGE"
	RateLimited                    Code = "RATE_LIMITED"
	ServiceUnavailable             Code = "SERVICE_UNAVAILABLE"
	RequestTimeout                 Code = "REQUEST_TIMEOUT"
	Internal                       Code = "INTERNAL_ERROR"
	FinancialConfigurationRequired Code = "FINANCIAL_CONFIGURATION_REQUIRED"
	InsufficientDiamonds           Code = "INSUFFICIENT_DIAMONDS"
	VerifiedActivityUnavailable    Code = "VERIFIED_ACTIVITY_UNAVAILABLE"
	WithdrawalNotEligible          Code = "WITHDRAWAL_NOT_ELIGIBLE"
	LiveListRestartRequired        Code = "LIVE_LIST_RESTART_REQUIRED"
	MaxFields                           = 20
)

// Codes returns a fresh registry slice; callers cannot mutate shared definitions.
func Codes() []Code {
	return []Code{InvalidRequest, InvalidJSON, UnsupportedMediaType, ValidationFailed, Unauthenticated, Forbidden, NotFound, MethodNotAllowed, Conflict, PayloadTooLarge, RateLimited, ServiceUnavailable, RequestTimeout, Internal, FinancialConfigurationRequired, InsufficientDiamonds, VerifiedActivityUnavailable, WithdrawalNotEligible, LiveListRestartRequired}
}

func (code Code) Message() string {
	switch code {
	case LiveListRestartRequired:
		return "Live room list changed; restart pagination"
	case FinancialConfigurationRequired:
		return "Financial configuration required"
	case InsufficientDiamonds:
		return "Insufficient diamonds"
	case VerifiedActivityUnavailable:
		return "Verified activity unavailable"
	case WithdrawalNotEligible:
		return "Withdrawal eligibility requirements not met"
	case InvalidRequest:
		return "Invalid request"
	case InvalidJSON:
		return "Invalid JSON request"
	case UnsupportedMediaType:
		return "Unsupported media type"
	case ValidationFailed:
		return "Invalid request data"
	case Unauthenticated:
		return "Authentication required"
	case Forbidden:
		return "Access forbidden"
	case NotFound:
		return "Resource not found"
	case MethodNotAllowed:
		return "Method not allowed"
	case Conflict:
		return "Resource conflict"
	case PayloadTooLarge:
		return "Request body too large"
	case RateLimited:
		return "Too many requests"
	case ServiceUnavailable:
		return "Service temporarily unavailable"
	case RequestTimeout:
		return "Request deadline exceeded"
	default:
		return "Internal server error"
	}
}

func (code Code) Known() bool { return slices.Contains(Codes(), code) }

type Field struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error exposes no cause/field data through Error() or exported struct fields.
type Error struct {
	code   Code
	cause  error
	fields []Field
}

func New(code Code, cause error) *Error {
	if !code.Known() {
		code = Internal
	}
	return &Error{code: code, cause: cause}
}

func Validation(fields []Field) *Error {
	if len(fields) > MaxFields {
		return New(Internal, nil)
	}
	for _, field := range fields {
		if !ValidField(field) {
			return New(Internal, nil)
		}
	}
	fields = slices.Clone(fields)
	slices.SortFunc(fields, func(a, b Field) int {
		if n := cmp.Compare(a.Field, b.Field); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Code, b.Code); n != 0 {
			return n
		}
		return cmp.Compare(a.Message, b.Message)
	})
	return &Error{code: ValidationFailed, fields: slices.Compact(fields)}
}

func (e *Error) Code() Code {
	if e == nil || !e.code.Known() {
		return Internal
	}
	return e.code
}
func (e *Error) Message() string { return e.Code().Message() }
func (e *Error) Error() string   { return string(e.Code()) }
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
func (e *Error) Fields() []Field {
	if e == nil {
		return nil
	}
	return slices.Clone(e.fields)
}
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e != nil && other != nil && e.Code() == other.Code()
}

func Public(err error) *Error {
	var typed *Error
	if errors.As(err, &typed) && typed != nil {
		return typed
	}
	return New(Internal, err)
}

// SafeCode and SafeMessage validate programmer-selected public metadata, not
// user values. Callers must never construct messages by echoing private input.
func SafeCode(code string) bool {
	if len(code) < 1 || len(code) > 64 || code[0] < 'A' || code[0] > 'Z' {
		return false
	}
	for _, c := range code {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func SafeMessage(message string) bool {
	if len(message) < 1 || len(message) > 256 || !utf8.ValidString(message) || strings.TrimSpace(message) != message {
		return false
	}
	for _, c := range message {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func ValidField(field Field) bool {
	if len(field.Field) < 1 || len(field.Field) > 64 || !SafeCode(field.Code) || !SafeMessage(field.Message) {
		return false
	}
	for _, c := range field.Field {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
