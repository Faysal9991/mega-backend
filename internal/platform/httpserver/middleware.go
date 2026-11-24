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

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// RequestID delegates to the shared validated correlation context.
func RequestID(ctx context.Context) string { return requestmeta.ID(ctx) }

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
				// Entropy failure cannot produce valid correlation; fail closed.
				panic(http.ErrAbortHandler)
			}
			id = hex.EncodeToString(entropy[:])
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(requestmeta.WithID(r.Context(), id)))
	})
}

func validRequestID(id string) bool { return requestmeta.ValidID(id) }

func requestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tracked := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			started := time.Now()
			returned := false
			defer func() {
				status := tracked.Status()
				outcome := "aborted"
				if returned {
					outcome = "completed"
					if status == 0 {
						status = http.StatusOK
					}
				}
				// Zero means no final status was committed before abort.
				// Observe unwinding without recovering or replacing its panic.
				route := "unmatched"
				if rc := chi.RouteContext(r.Context()); rc != nil && status != 404 && status != 405 {
					if pattern := rc.RoutePattern(); pattern != "" {
						route = pattern
					}
				}
				logger.LogAttrs(r.Context(), slog.LevelInfo, "http.request_completed",
					slog.String("request_id", RequestID(r.Context())), slog.String("method", safeMethod(r.Method)),
					slog.String("route", route), slog.Int("status", status), slog.String("outcome", outcome),
					slog.Float64("duration_ms", float64(time.Since(started))/float64(time.Millisecond)),
					slog.Int("response_bytes", tracked.BytesWritten()))
			}()
			next.ServeHTTP(tracked, r)
			returned = true
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

func recoverPanic(logger *slog.Logger, responses *response.Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if value := recover(); value != nil {
					if value == http.ErrAbortHandler {
						panic(http.ErrAbortHandler)
					}
					// No panic value, stack, URL or raw error ever reaches log output.
					logger.LogAttrs(r.Context(), slog.LevelError, "http.handler_panicked", slog.String("request_id", RequestID(r.Context())))
					tracked := w.(chimiddleware.WrapResponseWriter)
					if tracked.Status() == 0 {
						infrastructureError(responses, w, r, apperror.Internal)
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

func admission(maximum int, responses *response.Writer) func(http.Handler) http.Handler {
	slots := make(chan struct{}, maximum)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
				next.ServeHTTP(w, r)
			default:
				w.Header().Set("Retry-After", "1")
				infrastructureError(responses, w, r, apperror.ServiceUnavailable)
			}
		})
	}
}

func bodyLimit(maximum int64, responses *response.Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maximum {
				infrastructureError(responses, w, r, apperror.PayloadTooLarge)
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

func requestDeadline(timeout time.Duration, responses *response.Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && w.(chimiddleware.WrapResponseWriter).Status() == 0 {
				infrastructureError(responses, w, r, apperror.RequestTimeout)
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

// A failed write is terminal. Never attempt another envelope on this writer.
func infrastructureError(responses *response.Writer, w http.ResponseWriter, r *http.Request, code apperror.Code) {
	if err := responses.Error(w, r, apperror.New(code, nil)); err != nil {
		panic(http.ErrAbortHandler)
	}
}
