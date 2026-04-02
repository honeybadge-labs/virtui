// Package daemon implements the gRPC server for virtui.
package daemon

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/honeybadge-labs/virtui/internal/session"
	virtuipb "github.com/honeybadge-labs/virtui/proto/virtui/v1"
	"google.golang.org/grpc"
)

const shutdownGrace = 5 * time.Second

// Server is the virtui gRPC daemon.
type Server struct {
	socketPath string
	grpcServer *grpc.Server
	manager    *session.Manager
	listener   net.Listener
	mu         sync.Mutex
	stopped    bool
}

// NewServer creates a new daemon server.
func NewServer(socketPath string) *Server {
	mgr := session.NewManager()
	srv := &Server{
		socketPath: socketPath,
		manager:    mgr,
	}
	gs := grpc.NewServer(
		grpc.UnaryInterceptor(ErrorInterceptor),
	)
	handler := NewHandler(mgr, srv.Stop)
	virtuipb.RegisterVirtuiServiceServer(gs, handler)
	srv.grpcServer = gs
	return srv
}

// Listen binds the Unix domain socket. After Listen returns successfully the
// socket is reachable and callers may report readiness.
func (s *Server) Listen() error {
	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	// Remove stale socket
	_ = os.Remove(s.socketPath)

	lis, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.mu.Lock()
	if s.stopped {
		// Stop() was called before Listen() finished — tear down immediately.
		s.mu.Unlock()
		_ = lis.Close()
		_ = os.Remove(s.socketPath)
		return fmt.Errorf("server stopped before listen completed")
	}
	s.listener = lis
	s.mu.Unlock()
	return nil
}

// Serve blocks, serving gRPC requests on the listener created by Listen.
func (s *Server) Serve() error {
	return s.grpcServer.Serve(s.listener)
}

// Start calls Listen then Serve (backward compat for tests).
func (s *Server) Start() error {
	if err := s.Listen(); err != nil {
		return err
	}
	return s.Serve()
}

// Stop gracefully shuts down the server with a bounded grace period.
func (s *Server) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.mu.Unlock()

	_ = s.manager.CloseAll()

	done := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		s.grpcServer.Stop() // force-stop after grace period
	}

	if s.listener != nil {
		_ = s.listener.Close()
	}
	_ = os.Remove(s.socketPath)
}

// SocketPath returns the socket path.
func (s *Server) SocketPath() string {
	return s.socketPath
}
