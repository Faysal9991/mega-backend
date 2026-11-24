// Package httpserver owns HTTP transport lifecycle and infrastructure middleware.
package httpserver

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
)

// Server is a single-use lifecycle owner. New requires a validated configuration.
type Server struct {
	http            *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

func New(cfg config.HTTPConfig, logger *slog.Logger, router http.Handler) (*Server, error) {
	if logger == nil || router == nil {
		return nil, errors.New("httpserver: logger and router are required")
	}
	return &Server{http: &http.Server{
		Addr: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), Handler: router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: cfg.MaxHeaderBytes,
		// net/http diagnostics can contain raw panic/request/TLS information. Use
		// only safe explicit events from this package; never bridge raw text logs.
		ErrorLog: log.New(io.Discard, "", 0),
	}, logger: logger, shutdownTimeout: cfg.ShutdownTimeout}, nil
}

func (s *Server) Address() string { return s.http.Addr }

// Serve owns the supplied listener. Binding is deliberately the caller's step.
func (s *Server) Serve(listener net.Listener) error {
	err := s.http.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }
func (s *Server) Close() error                       { return s.http.Close() }

// Run coordinates serving and bounded drain. The signal context does not become
// the request BaseContext: cancellation initiates drain without aborting work.
func (s *Server) Run(ctx context.Context, listener net.Listener) error {
	if ctx.Err() != nil {
		return listener.Close()
	}
	served := make(chan error, 1)
	go func() { served <- s.Serve(listener) }()
	select {
	case err := <-served:
		return errors.Join(err, s.Close())
	case <-ctx.Done():
		s.logger.Info("http.shutdown_started")
		drain, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		shutdownErr := s.Shutdown(drain)
		var closeErr error
		if shutdownErr != nil {
			s.logger.Warn("http.shutdown_forced", slog.String("failure_code", "DRAIN_INCOMPLETE"))
			closeErr = s.Close()
		}
		serveErr := <-served
		if shutdownErr == nil {
			s.logger.Info("http.shutdown_completed")
		}
		return errors.Join(shutdownErr, closeErr, serveErr)
	}
}
