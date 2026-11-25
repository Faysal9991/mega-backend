// Package response owns HTTP mappings and compact, pre-commit JSON envelopes.
package response

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
)

// MaxJSONBytes caps each staged envelope, including its trailing newline.
const MaxJSONBytes = 1 << 20

type SuccessEnvelope struct {
	Success   bool   `json:"success"`
	Code      string `json:"code"`
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
	Message   string `json:"message,omitempty"`
	Meta      *Meta  `json:"meta,omitempty"`
}

type ErrorEnvelope struct {
	Success   bool          `json:"success"`
	Code      apperror.Code `json:"code"`
	Message   string        `json:"message"`
	Error     *Details      `json:"error,omitempty"`
	RequestID string        `json:"request_id"`
}

type Details struct {
	Fields []apperror.Field `json:"fields"`
}
type Options struct {
	Message string
	Meta    *Meta
}
type Meta struct {
	Pagination Pagination `json:"pagination"`
}
type Pagination struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// Valid checks metadata only. Endpoints define their ordering and tighter limits.
// A terminal page has has_more=false and next_cursor=null.
func (p Pagination) Valid() bool {
	if p.Limit < 1 || p.Limit > 1000 {
		return false
	}
	if !p.HasMore {
		return p.NextCursor == nil
	}
	if p.NextCursor == nil || !utf8.ValidString(*p.NextCursor) || len(*p.NextCursor) < 1 || len(*p.NextCursor) > 1024 {
		return false
	}
	for _, r := range *p.NextCursor {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// List deliberately serializes nil collections as []. Nullable DTO fields must
// be modeled explicitly; this package never rewrites arbitrary persistence data.
type List[T any] []T

func (list List[T]) MarshalJSON() ([]byte, error) {
	if list == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]T(list))
}

type Writer struct{ logger *slog.Logger }

func New(logger *slog.Logger) (*Writer, error) {
	if logger == nil {
		return nil, errors.New("response: logger is required")
	}
	return &Writer{logger: logger}, nil
}

// Status is the sole application-code to HTTP mapping, at the transport boundary.
func Status(code apperror.Code) int {
	switch code {
	case apperror.InvalidRequest, apperror.InvalidJSON:
		return 400
	case apperror.UnsupportedMediaType:
		return 415
	case apperror.ValidationFailed:
		return 422
	case apperror.Unauthenticated:
		return 401
	case apperror.Forbidden, apperror.WithdrawalNotEligible:
		return 403
	case apperror.NotFound:
		return 404
	case apperror.MethodNotAllowed:
		return 405
	case apperror.Conflict, apperror.FinancialConfigurationRequired, apperror.InsufficientDiamonds:
		return 409
	case apperror.PayloadTooLarge:
		return 413
	case apperror.RateLimited:
		return 429
	case apperror.ServiceUnavailable, apperror.VerifiedActivityUnavailable:
		return 503
	case apperror.RequestTimeout:
		return 504
	default:
		return 500
	}
}

func (wr *Writer) Success(w http.ResponseWriter, r *http.Request, status int, code string, data any, options Options) error {
	if status == 204 {
		return wr.NoContent(w, r)
	}
	if status != 200 && status != 201 && status != 202 || !apperror.SafeCode(code) || apperror.Code(code).Known() || options.Message != "" && !apperror.SafeMessage(options.Message) || options.Meta != nil && !options.Meta.Pagination.Valid() {
		return wr.internal(w, r, "INVALID_RESPONSE_METADATA")
	}
	return wr.encode(w, r, status, SuccessEnvelope{Success: true, Code: code, Data: data, RequestID: requestmeta.ID(r.Context()), Message: options.Message, Meta: options.Meta})
}

func (wr *Writer) Error(w http.ResponseWriter, r *http.Request, err error) error {
	public := apperror.Public(err)
	if public.Code() == apperror.Internal {
		return wr.internal(w, r, "INTERNAL_FAILURE")
	}
	return wr.encode(w, r, Status(public.Code()), errorEnvelope(r, public))
}

func errorEnvelope(r *http.Request, err *apperror.Error) ErrorEnvelope {
	envelope := ErrorEnvelope{Code: err.Code(), Message: err.Message(), RequestID: requestmeta.ID(r.Context())}
	if fields := err.Fields(); len(fields) > 0 {
		envelope.Error = &Details{Fields: fields}
	}
	return envelope
}

func writable(w http.ResponseWriter, r *http.Request) error {
	state, ok := w.(interface{ Status() int })
	if !ok {
		return errors.New("response: commitment tracking is required")
	}
	if state.Status() != 0 {
		return errors.New("response: response is already committed")
	}
	if !requestmeta.ValidID(requestmeta.ID(r.Context())) {
		return errors.New("response: validated correlation is required")
	}
	return nil
}

func headers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Request-ID", requestmeta.ID(r.Context()))
}

func (wr *Writer) NoContent(w http.ResponseWriter, r *http.Request) error {
	if err := writable(w, r); err != nil {
		return err
	}
	w.Header().Del("Content-Type")
	w.Header().Del("Content-Length")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Request-ID", requestmeta.ID(r.Context()))
	w.WriteHeader(204)
	return nil
}

// cappedBuffer bounds staged output. Callers must also bound DTO collections and
// strings: encoding/json can allocate internally before it calls Write.
type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxJSONBytes-b.Len() {
		return 0, errors.New("response: JSON budget exceeded")
	}
	return b.Buffer.Write(p)
}

func (wr *Writer) encode(w http.ResponseWriter, r *http.Request, status int, envelope any) error {
	if err := writable(w, r); err != nil {
		return err
	}
	var output cappedBuffer
	if err := json.NewEncoder(&output).Encode(envelope); err != nil {
		return wr.internal(w, r, "SERIALIZATION_FAILED")
	}
	return wr.write(w, r, status, output.Bytes())
}

func (wr *Writer) internal(w http.ResponseWriter, r *http.Request, failure string) error {
	if err := writable(w, r); err != nil {
		return err
	}
	wr.failure(r, failure)
	// This fallback contains only fixed strings and a bounded correlation ID.
	output, err := json.Marshal(errorEnvelope(r, apperror.New(apperror.Internal, nil)))
	if err != nil {
		return errors.New("response: fallback encoding failed")
	}
	return wr.write(w, r, 500, append(output, '\n'))
}

func (wr *Writer) write(w http.ResponseWriter, r *http.Request, status int, body []byte) error {
	headers(w, r)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return nil
	}
	n, err := w.Write(body)
	if err == nil && n != len(body) {
		err = io.ErrShortWrite
	}
	if err != nil {
		wr.failure(r, "RESPONSE_WRITE_FAILED")
		return errors.New("response: write failed")
	}
	return nil
}

func (wr *Writer) failure(r *http.Request, code string) {
	wr.logger.ErrorContext(r.Context(), "api.response_failed", slog.String("request_id", requestmeta.ID(r.Context())), slog.String("failure_code", code))
}
