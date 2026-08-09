package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ceserve/courier-os/internal/platform/config"
)

// Server wraps http.Server with graceful shutdown.
type Server struct {
	srv *http.Server
	log *slog.Logger
	cfg config.HTTPConfig
}

// NewServer builds the API server.
func NewServer(cfg config.HTTPConfig, h http.Handler, log *slog.Logger) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              cfg.Addr,
			Handler:           h,
			ReadTimeout:       cfg.ReadTimeout,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		},
		log: log,
		cfg: cfg,
	}
}

// Run serves until ctx is cancelled, then drains in-flight requests within
// ShutdownTimeout before forcing closure.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", slog.String("addr", s.cfg.Addr))
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	s.log.Info("http server shutting down", slog.Duration("grace", s.cfg.ShutdownTimeout))
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		s.log.Error("graceful shutdown failed; forcing close", slog.String("error", err.Error()))
		_ = s.srv.Close()
		return err
	}
	s.log.Info("http server stopped")
	return nil
}

// RunAux serves an auxiliary listener (metrics) with the same lifecycle rules.
func RunAux(ctx context.Context, addr string, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("auxiliary server listening", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
