// Package client provides a gRPC client wrapper for the virtui daemon.
package client

import (
	"context"
	"fmt"
	"net"
	"time"

	virtuipb "github.com/honeybadge-labs/virtui/proto/virtui/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps the gRPC client for the virtui daemon.
type Client struct {
	conn    *grpc.ClientConn
	service virtuipb.VirtuiServiceClient
}

// New creates a new client connected to the daemon at the given socket path.
func New(socketPath string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, "unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 5*time.Second)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon at %s: %w", socketPath, err)
	}
	return &Client{
		conn:    conn,
		service: virtuipb.NewVirtuiServiceClient(conn),
	}, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Service returns the raw gRPC service client.
func (c *Client) Service() virtuipb.VirtuiServiceClient {
	return c.service
}

// Run starts a new terminal session.
func (c *Client) Run(ctx context.Context, req *virtuipb.RunRequest) (*virtuipb.RunResponse, error) {
	return c.service.Run(ctx, req)
}

// Kill terminates a session.
func (c *Client) Kill(ctx context.Context, req *virtuipb.KillRequest) (*virtuipb.KillResponse, error) {
	return c.service.Kill(ctx, req)
}

// Sessions lists sessions.
func (c *Client) Sessions(ctx context.Context, req *virtuipb.SessionsRequest) (*virtuipb.SessionsResponse, error) {
	return c.service.Sessions(ctx, req)
}

// Resize changes terminal dimensions.
func (c *Client) Resize(ctx context.Context, req *virtuipb.ResizeRequest) (*virtuipb.ResizeResponse, error) {
	return c.service.Resize(ctx, req)
}

// Exec types input, presses Enter, and optionally waits.
func (c *Client) Exec(ctx context.Context, req *virtuipb.ExecRequest) (*virtuipb.ExecResponse, error) {
	return c.service.Exec(ctx, req)
}

// Screenshot captures the terminal screen.
func (c *Client) Screenshot(ctx context.Context, req *virtuipb.ScreenshotRequest) (*virtuipb.ScreenshotResponse, error) {
	return c.service.Screenshot(ctx, req)
}

// Press sends key presses.
func (c *Client) Press(ctx context.Context, req *virtuipb.PressRequest) (*virtuipb.PressResponse, error) {
	return c.service.Press(ctx, req)
}

// Type sends text without Enter.
func (c *Client) Type(ctx context.Context, req *virtuipb.TypeRequest) (*virtuipb.TypeResponse, error) {
	return c.service.Type(ctx, req)
}

// Wait waits for a screen condition.
func (c *Client) Wait(ctx context.Context, req *virtuipb.WaitRequest) (*virtuipb.WaitResponse, error) {
	return c.service.Wait(ctx, req)
}

// Pipeline executes a batch of operations.
func (c *Client) Pipeline(ctx context.Context, req *virtuipb.PipelineRequest) (*virtuipb.PipelineResponse, error) {
	return c.service.Pipeline(ctx, req)
}

// Watch subscribes to screen change events.
func (c *Client) Watch(ctx context.Context, req *virtuipb.WatchRequest) (virtuipb.VirtuiService_WatchClient, error) {
	return c.service.Watch(ctx, req)
}
