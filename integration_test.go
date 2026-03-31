package virtui_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rotemtam/virtui/internal/daemon"
	virtuipb "github.com/rotemtam/virtui/proto/virtui/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func startTestDaemon(t *testing.T) (virtuipb.VirtuiServiceClient, func()) {
	t.Helper()
	// Use /tmp directly to keep socket path short (macOS 104 char limit).
	dir, err := os.MkdirTemp("/tmp", "virtui-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "t.sock")

	srv := daemon.NewServer(socketPath)
	go func() {
		if err := srv.Start(); err != nil {
			// Server stopped
		}
	}()

	// Wait for socket to be available
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	conn, err := grpc.NewClient("unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 5*time.Second)
		}),
	)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	client := virtuipb.NewVirtuiServiceClient(conn)
	cleanup := func() {
		conn.Close()
		srv.Stop()
	}
	return client, cleanup
}

func TestIntegration_RunExecScreenshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()

	// Run bash session
	runResp, err := client.Run(ctx, &virtuipb.RunRequest{
		Command: []string{"bash"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runResp.SessionId == "" {
		t.Fatal("expected non-empty session ID")
	}
	if runResp.Pid == 0 {
		t.Fatal("expected non-zero PID")
	}

	// Give bash a moment to start
	time.Sleep(500 * time.Millisecond)

	// Exec echo
	execResp, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: runResp.SessionId,
		Input:     "echo hello-virtui",
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "hello-virtui"}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !strings.Contains(execResp.ScreenText, "hello-virtui") {
		t.Errorf("screen should contain 'hello-virtui', got:\n%s", execResp.ScreenText)
	}
	if execResp.ScreenHash == "" {
		t.Error("expected non-empty screen hash")
	}

	// Screenshot
	ssResp, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{
		SessionId: runResp.SessionId,
	})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.Contains(ssResp.ScreenText, "hello-virtui") {
		t.Errorf("screenshot should contain 'hello-virtui', got:\n%s", ssResp.ScreenText)
	}

	// Kill
	_, err = client.Kill(ctx, &virtuipb.KillRequest{
		SessionId: runResp.SessionId,
	})
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
}

func TestIntegration_Sessions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()

	// Initially no sessions
	listResp, err := client.Sessions(ctx, &virtuipb.SessionsRequest{})
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(listResp.Sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(listResp.Sessions))
	}

	// Start a session
	runResp, err := client.Run(ctx, &virtuipb.RunRequest{
		Command: []string{"bash"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// List should show 1
	listResp, err = client.Sessions(ctx, &virtuipb.SessionsRequest{})
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(listResp.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(listResp.Sessions))
	}
	if listResp.Sessions[0].SessionId != runResp.SessionId {
		t.Errorf("session ID mismatch")
	}

	// Kill
	_, _ = client.Kill(ctx, &virtuipb.KillRequest{SessionId: runResp.SessionId})
}

func TestIntegration_PressAndType(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()

	runResp, err := client.Run(ctx, &virtuipb.RunRequest{
		Command: []string{"bash"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer client.Kill(ctx, &virtuipb.KillRequest{SessionId: runResp.SessionId})

	time.Sleep(500 * time.Millisecond)

	// Type without enter
	_, err = client.Type(ctx, &virtuipb.TypeRequest{
		SessionId: runResp.SessionId,
		Text:      "echo typed-test",
	})
	if err != nil {
		t.Fatalf("Type: %v", err)
	}

	// Press Enter
	_, err = client.Press(ctx, &virtuipb.PressRequest{
		SessionId: runResp.SessionId,
		Keys:      []string{"Enter"},
		Repeat:    1,
	})
	if err != nil {
		t.Fatalf("Press: %v", err)
	}

	// Wait for output
	time.Sleep(500 * time.Millisecond)

	ssResp, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{
		SessionId: runResp.SessionId,
	})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.Contains(ssResp.ScreenText, "typed-test") {
		t.Errorf("screen should contain 'typed-test', got:\n%s", ssResp.ScreenText)
	}
}
