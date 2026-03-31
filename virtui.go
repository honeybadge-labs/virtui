// Package virtui provides a Go SDK for programmatically driving terminal applications.
//
// Usage:
//
//	c, err := virtui.Connect("~/.virtui/daemon.sock")
//	defer c.Close()
//
//	sess, err := c.Run(ctx, "bash")
//	screen, err := c.Exec(ctx, sess.SessionID, "echo hello", virtui.WaitText("hello"))
//	c.Kill(ctx, sess.SessionID)
package virtui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rotemtam/virtui/internal/client"
	virtuipb "github.com/rotemtam/virtui/proto/virtui/v1"
)

// Client is the public Go SDK client for virtui.
type Client struct {
	c *client.Client
}

// Connect creates a new client connected to the daemon.
// The socketPath supports ~ expansion.
func Connect(socketPath string) (*Client, error) {
	if strings.HasPrefix(socketPath, "~/") {
		home, _ := os.UserHomeDir()
		socketPath = filepath.Join(home, socketPath[2:])
	}
	c, err := client.New(socketPath)
	if err != nil {
		return nil, err
	}
	return &Client{c: c}, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.c.Close()
}

// RunResult is the result of starting a session.
type RunResult struct {
	SessionID     string
	PID           int
	RecordingPath string
}

// RunOpts are options for starting a session.
type RunOpts struct {
	Cols       uint32
	Rows       uint32
	Env        []string
	Dir        string
	Record     bool
	RecordPath string
}

// Run starts a new terminal session.
func (c *Client) Run(ctx context.Context, command []string, opts ...RunOpts) (*RunResult, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	req := &virtuipb.RunRequest{
		Command: command,
		Cols:    80,
		Rows:    24,
	}
	if len(opts) > 0 {
		o := opts[0]
		if o.Cols > 0 {
			req.Cols = o.Cols
		}
		if o.Rows > 0 {
			req.Rows = o.Rows
		}
		req.Env = o.Env
		req.Dir = o.Dir
		req.Record = o.Record
		req.RecordPath = o.RecordPath
	}
	resp, err := c.c.Run(ctx, req)
	if err != nil {
		return nil, err
	}
	return &RunResult{
		SessionID:     resp.SessionId,
		PID:           int(resp.Pid),
		RecordingPath: resp.RecordingPath,
	}, nil
}

// ScreenResult is a terminal screen snapshot.
type ScreenResult struct {
	Text      string
	Hash      string
	CursorRow int
	CursorCol int
	ElapsedMs int64
}

// WaitOption configures what to wait for after exec.
type WaitOption func(*virtuipb.ExecRequest)

// WaitText waits for text to appear on screen.
func WaitText(text string) WaitOption {
	return func(req *virtuipb.ExecRequest) {
		req.Wait = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: text}}
	}
}

// WaitStable waits for the screen to stabilize.
func WaitStable() WaitOption {
	return func(req *virtuipb.ExecRequest) {
		req.Wait = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Stable{Stable: true}}
	}
}

// WaitGone waits for text to disappear from screen.
func WaitGone(text string) WaitOption {
	return func(req *virtuipb.ExecRequest) {
		req.Wait = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Gone{Gone: text}}
	}
}

// WaitRegex waits for a regex match on screen.
func WaitRegex(pattern string) WaitOption {
	return func(req *virtuipb.ExecRequest) {
		req.Wait = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Regex{Regex: pattern}}
	}
}

// WithTimeout sets the wait timeout in milliseconds.
func WithTimeout(ms uint32) WaitOption {
	return func(req *virtuipb.ExecRequest) {
		req.TimeoutMs = ms
	}
}

// Exec types input, presses Enter, and optionally waits.
func (c *Client) Exec(ctx context.Context, sessionID, input string, opts ...WaitOption) (*ScreenResult, error) {
	req := &virtuipb.ExecRequest{
		SessionId: sessionID,
		Input:     input,
		TimeoutMs: 30000,
	}
	for _, opt := range opts {
		opt(req)
	}
	resp, err := c.c.Exec(ctx, req)
	if err != nil {
		return nil, err
	}
	return &ScreenResult{
		Text:      resp.ScreenText,
		Hash:      resp.ScreenHash,
		CursorRow: int(resp.CursorRow),
		CursorCol: int(resp.CursorCol),
		ElapsedMs: resp.ElapsedMs,
	}, nil
}

// Screenshot captures the current terminal screen.
func (c *Client) Screenshot(ctx context.Context, sessionID string) (*ScreenResult, error) {
	resp, err := c.c.Screenshot(ctx, &virtuipb.ScreenshotRequest{SessionId: sessionID})
	if err != nil {
		return nil, err
	}
	return &ScreenResult{
		Text:      resp.ScreenText,
		Hash:      resp.ScreenHash,
		CursorRow: int(resp.CursorRow),
		CursorCol: int(resp.CursorCol),
	}, nil
}

// Press sends key presses.
func (c *Client) Press(ctx context.Context, sessionID string, keys ...string) error {
	_, err := c.c.Press(ctx, &virtuipb.PressRequest{
		SessionId: sessionID,
		Keys:      keys,
		Repeat:    1,
	})
	return err
}

// Type sends text without pressing Enter.
func (c *Client) Type(ctx context.Context, sessionID, text string) error {
	_, err := c.c.Type(ctx, &virtuipb.TypeRequest{
		SessionId: sessionID,
		Text:      text,
	})
	return err
}

// Kill terminates a session.
func (c *Client) Kill(ctx context.Context, sessionID string) error {
	_, err := c.c.Kill(ctx, &virtuipb.KillRequest{SessionId: sessionID})
	return err
}

// Resize changes terminal dimensions.
func (c *Client) Resize(ctx context.Context, sessionID string, cols, rows uint32) error {
	_, err := c.c.Resize(ctx, &virtuipb.ResizeRequest{
		SessionId: sessionID,
		Cols:      cols,
		Rows:      rows,
	})
	return err
}
