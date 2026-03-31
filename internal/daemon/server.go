// Package daemon implements the gRPC server for virtui.
package daemon

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/honeybadge-labs/virtui/internal/session"
	virtuipb "github.com/honeybadge-labs/virtui/proto/virtui/v1"
	"google.golang.org/grpc"
)

// Server is the virtui gRPC daemon.
type Server struct {
	socketPath string
	grpcServer *grpc.Server
	manager    *session.Manager
	listener   net.Listener
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
	handler := NewHandler(mgr)
	virtuipb.RegisterVirtuiServiceServer(gs, handler)
	srv.grpcServer = gs
	return srv
}

// Start begins listening on the Unix domain socket.
func (s *Server) Start() error {
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
	s.listener = lis
	return s.grpcServer.Serve(lis)
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	s.manager.CloseAll()
	s.grpcServer.GracefulStop()
	if s.listener != nil {
		s.listener.Close()
	}
	_ = os.Remove(s.socketPath)
}

// SocketPath returns the socket path.
func (s *Server) SocketPath() string {
	return s.socketPath
}
