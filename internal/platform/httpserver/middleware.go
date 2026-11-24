package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ""
		count := 0
		for key, values := range r.Header {
			if strings.EqualFold(key, "X-Request-ID") {
				for _, value := range values {
					count++
					id = value
				}
			}
		}
		if count != 1 || !validRequestID(id) {
			var entropy [16]byte
			if _, err := rand.Read(entropy[:]); err != nil {
				InfrastructureError(w, http.StatusServiceUnavailable)
				return
			}
			id = hex.EncodeToString(entropy[:])
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func validRequestID(id string) bool {
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

func requestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tracked := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			started := time.Now()
			defer func() {
				status := tracked.Status()
				if status == 0 {
					status = http.StatusOK
				}
				route := "unmatched"
				if rc := chi.RouteContext(r.Context()); rc != nil && status != 404 && status != 405 {
					if pattern := rc.RoutePattern(); pattern != "" {
						route = pattern
					}
				}
				logger.LogAttrs(r.Context(), slog.LevelInfo, "http.request_completed",
					slog.String("request_id", RequestID(r.Context())), slog.String("method", safeMethod(r.Method)),
					slog.String("route", route), slog.Int("status", status),
					slog.Float64("duration_ms", float64(time.Since(started))/float64(time.Millisecond)),
					slog.Int("response_bytes", tracked.BytesWritten()))
			}()
			next.ServeHTTP(tracked, r)
		})
	}
}

func safeMethod(method string) string {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		return method
	}
	return "OTHER"
}

func recoverPanic(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if value := recover(); value != nil {
					// No panic value, stack, URL or raw error ever reaches log output.
					logger.LogAttrs(r.Context(), slog.LevelError, "http.handler_panicked", slog.String("request_id", RequestID(r.Context())))
					tracked := w.(chimiddleware.WrapResponseWriter)
					if tracked.Status() == 0 {
						InfrastructureError(w, http.StatusInternalServerError)
						return
					}
					// Already committed: abort the connection/HTTP2 stream. Never append
					// a synthetic 500 to a successful or partially written response.
					panic(http.ErrAbortHandler)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func admission(maximum int) func(http.Handler) http.Handler {
	slots := make(chan struct{}, maximum)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
				next.ServeHTTP(w, r)
			default:
				w.Header().Set("Retry-After", "1")
				InfrastructureError(w, http.StatusServiceUnavailable)
			}
		})
	}
}

func bodyLimit(maximum int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maximum {
				InfrastructureError(w, http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maximum)
			next.ServeHTTP(w, r)
		})
	}
}

// IsBodyTooLarge lets consuming handlers map streamed/chunked read errors to 413
// before committing. Middleware never reads or buffers the body itself.
func IsBodyTooLarge(err error) bool {
	var oversized *http.MaxBytesError
	return errors.As(err, &oversized)
}

func requestDeadline(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && w.(chimiddleware.WrapResponseWriter).Status() == 0 {
				InfrastructureError(w, http.StatusGatewayTimeout)
			}
		})
	}
}

// ClientIP uses only the connection peer. Forwarded headers are deliberately
// unsupported until a trusted-proxy policy is separately approved.
func ClientIP(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}
