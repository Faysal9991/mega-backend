package validation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
)

type Decoder struct {
	maxBytes  int64
	snakeCase bool
}

func NewDecoder(maxBytes int64) (*Decoder, error) {
	if maxBytes < 1 || maxBytes > 16<<20 {
		return nil, errors.New("validation: invalid body budget")
	}
	return &Decoder{maxBytes: maxBytes}, nil
}

// Decode stages at most maxBytes of JSON (plus the MaxBytesReader overflow
// probe), performs a strict token pass, then decodes directly into an object DTO.
// A failed decode must not be used by a handler; dst may be partially populated.
func (d *Decoder) Decode(r *http.Request, dst any) error {
	value := reflect.ValueOf(dst)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return apperror.New(apperror.Internal, nil)
	}
	if _, custom := dst.(json.Unmarshaler); custom {
		return apperror.New(apperror.Internal, nil)
	}
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		return apperror.New(apperror.UnsupportedMediaType, nil)
	}
	media, parameters, err := mime.ParseMediaType(values[0])
	if err != nil || media != "application/json" {
		return apperror.New(apperror.UnsupportedMediaType, nil)
	}
	for key, value := range parameters {
		if key != "charset" || !strings.EqualFold(value, "utf-8") {
			return apperror.New(apperror.UnsupportedMediaType, nil)
		}
	}
	encodings := r.Header.Values("Content-Encoding")
	if len(encodings) > 1 || len(encodings) == 1 && !strings.EqualFold(encodings[0], "identity") {
		return apperror.New(apperror.UnsupportedMediaType, nil)
	}
	if r.Body == nil {
		return apperror.New(apperror.InvalidJSON, nil)
	}
	if r.ContentLength > d.maxBytes {
		return apperror.New(apperror.PayloadTooLarge, nil)
	}
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, d.maxBytes))
	if err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			return apperror.New(apperror.PayloadTooLarge, err)
		}
		return apperror.New(apperror.InvalidRequest, err)
	}
	if !utf8.Valid(body) {
		return apperror.New(apperror.InvalidJSON, nil)
	}
	probe := json.NewDecoder(bytes.NewReader(body))
	probe.UseNumber()
	first, err := probe.Token()
	if err != nil {
		return apperror.New(apperror.InvalidJSON, err)
	}
	if first != json.Delim('{') {
		return apperror.New(apperror.InvalidRequest, nil)
	}
	if err := object(probe, 0, d.snakeCase); err != nil {
		return apperror.New(apperror.InvalidJSON, err)
	}
	if _, err := probe.Token(); err != io.EOF {
		return apperror.New(apperror.InvalidJSON, nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return apperror.New(apperror.InvalidJSON, err)
	}
	return nil
}

func object(decoder *json.Decoder, depth int, snakeCase bool) error {
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return errors.New("invalid object key")
		}
		if snakeCase {
			for i, b := range []byte(name) {
				if !(b >= 'a' && b <= 'z' || i > 0 && (b >= '0' && b <= '9' || b == '_')) {
					return errors.New("canonical snake_case object key required")
				}
			}
		}
		folded := fold(name)
		if seen[folded] {
			return errors.New("duplicate object key")
		}
		seen[folded] = true
		if err := jsonValue(decoder, depth+1, snakeCase); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil {
		return err
	}
	if end != json.Delim('}') {
		return errors.New("invalid object end")
	}
	return nil
}

func jsonValue(decoder *json.Decoder, depth int, snakeCase bool) error {
	if depth > 64 {
		return errors.New("JSON nesting limit exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		return object(decoder, depth, snakeCase)
	case json.Delim('['):
		for decoder.More() {
			if err := jsonValue(decoder, depth+1, snakeCase); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return errors.New("invalid array end")
		}
	}
	return nil
}

// Match encoding/json's Unicode simple-fold equivalence, not just ASCII lower.
func fold(key string) string {
	return strings.Map(func(r rune) rune {
		smallest := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < smallest {
				smallest = next
			}
		}
		return smallest
	}, key)
}

// NewSnakeCaseDecoder is opt-in for contracts that reject Go's implicit
// case-insensitive JSON field aliases. Existing endpoint decoding is unchanged.
func NewSnakeCaseDecoder(maxBytes int64) (*Decoder, error) {
	d, e := NewDecoder(maxBytes)
	if e != nil {
		return nil, e
	}
	d.snakeCase = true
	return d, nil
}
