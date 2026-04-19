package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server is the HTTP proxy server.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// NewServer creates a proxy server that listens on the given host:port.
func NewServer(host string, port int, handler http.Handler, logger *slog.Logger) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)
	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// Start begins listening for connections. It blocks until the server is
// shut down or an error occurs.
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.httpServer.Addr, err)
	}

	s.logger.Info("proxy server started",
		"addr", s.httpServer.Addr,
	)

	if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server with the given context timeout.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down proxy server")
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the address the server is configured to listen on.
func (s *Server) Addr() string {
	return s.httpServer.Addr
}
