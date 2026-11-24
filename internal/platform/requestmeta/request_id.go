// Package requestmeta carries validated request correlation without transport cycles.
package requestmeta

import "context"

type idKey struct{}

func ID(ctx context.Context) string { id, _ := ctx.Value(idKey{}).(string); return id }

// WithID accepts only the same bounded syntax as the HTTP correlation middleware.
// Invalid input removes correlation rather than retaining an untrusted value.
func WithID(ctx context.Context, id string) context.Context {
	if !ValidID(id) {
		id = ""
	}
	return context.WithValue(ctx, idKey{}, id)
}

func ValidID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
