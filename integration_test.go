package virtui_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/honeybadge-labs/virtui/internal/daemon"
	virtuipb "github.com/honeybadge-labs/virtui/proto/virtui/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func startTestDaemon(t *testing.T) (virtuipb.VirtuiServiceClient, func()) {
	t.Helper()
	// Use /tmp directly to keep socket path short (macOS 104 char limit).
	dir, err := os.MkdirTemp("/tmp", "virtui-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "t.sock")

	srv := daemon.NewServer(socketPath)
	go func() {
		_ = srv.Start()
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
		_ = conn.Close()
		srv.Stop()
	}
	return client, cleanup
}

// runBash is a helper that starts a bash session and returns its ID.
// It registers cleanup to kill the session on test completion.
func runBash(t *testing.T, ctx context.Context, client virtuipb.VirtuiServiceClient, opts ...func(*virtuipb.RunRequest)) string {
	t.Helper()
	req := &virtuipb.RunRequest{
		Command: []string{"bash"},
		Cols:    80,
		Rows:    24,
	}
	for _, o := range opts {
		o(req)
	}
	resp, err := client.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() {
		_, _ = client.Kill(ctx, &virtuipb.KillRequest{SessionId: resp.SessionId})
	})
	// Give bash a moment to start
	time.Sleep(500 * time.Millisecond)
	return resp.SessionId
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
	defer func() { _, _ = client.Kill(ctx, &virtuipb.KillRequest{SessionId: runResp.SessionId}) }()

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

func TestIntegration_ExecWaitStable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	// Exec with --wait-stable: screen should settle after echo completes
	resp, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     "echo stable-test",
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Stable{Stable: true}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Exec wait-stable: %v", err)
	}
	if !strings.Contains(resp.ScreenText, "stable-test") {
		t.Errorf("screen should contain 'stable-test', got:\n%s", resp.ScreenText)
	}
	if resp.ElapsedMs < 500 {
		// stable wait requires 500ms of no changes
		t.Logf("elapsed %dms (stable wait needs ~500ms settling)", resp.ElapsedMs)
	}
}

func TestIntegration_ExecWaitRegex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	// Exec with --wait-regex
	resp, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     "echo version-1.42.0",
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Regex{Regex: `version-\d+\.\d+\.\d+`}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Exec wait-regex: %v", err)
	}
	if !strings.Contains(resp.ScreenText, "version-1.42.0") {
		t.Errorf("screen should contain 'version-1.42.0', got:\n%s", resp.ScreenText)
	}
}

func TestIntegration_ExecWaitGone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	// First put some text on screen
	_, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     "echo LOADING",
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "LOADING"}},
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("setup exec: %v", err)
	}

	// Now clear the screen, then wait for LOADING to be gone
	_, err = client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     "clear",
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Gone{Gone: "LOADING"}},
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Exec wait-gone: %v", err)
	}
}

func TestIntegration_Wait(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	// Start a delayed echo in background, then wait for it
	_, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     "sleep 1 && echo READY",
		TimeoutMs: 2000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	resp, err := client.Wait(ctx, &virtuipb.WaitRequest{
		SessionId: sid,
		Condition: &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "READY"}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !strings.Contains(resp.ScreenText, "READY") {
		t.Errorf("screen should contain 'READY', got:\n%s", resp.ScreenText)
	}
}

func TestIntegration_Resize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	// Resize
	_, err := client.Resize(ctx, &virtuipb.ResizeRequest{
		SessionId: sid,
		Cols:      120,
		Rows:      40,
	})
	if err != nil {
		t.Fatalf("Resize: %v", err)
	}

	// Give the pump goroutine time to apply the pending resize
	// after SIGWINCH triggers output from the shell.
	time.Sleep(1 * time.Second)

	// Verify via screenshot that dimensions changed
	ss, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{SessionId: sid})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if ss.Cols != 120 || ss.Rows != 40 {
		t.Errorf("expected 120x40, got %dx%d", ss.Cols, ss.Rows)
	}
}

func TestIntegration_SessionNotRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()

	// Run a command that exits immediately
	resp, err := client.Run(ctx, &virtuipb.RunRequest{
		Command: []string{"bash", "-c", "exit 0"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Wait for the process to exit
	time.Sleep(500 * time.Millisecond)

	// Exec should fail with FailedPrecondition
	_, err = client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: resp.SessionId,
		Input:     "echo should-fail",
		TimeoutMs: 2000,
	})
	if err == nil {
		t.Fatal("expected error for dead session")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %v", st.Code())
	}

	// Screenshot should still work on dead session
	ss, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{SessionId: resp.SessionId})
	if err != nil {
		t.Fatalf("Screenshot on dead session should work: %v", err)
	}
	if ss.ScreenHash == "" {
		t.Error("expected non-empty hash from dead session screenshot")
	}
}

func TestIntegration_ScreenHashChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	// Take initial screenshot
	ss1, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{SessionId: sid})
	if err != nil {
		t.Fatalf("Screenshot 1: %v", err)
	}
	if ss1.ScreenHash == "" {
		t.Fatal("expected non-empty hash")
	}
	if len(ss1.ScreenHash) != 64 {
		t.Errorf("expected 64-char SHA-256 hex, got %d chars", len(ss1.ScreenHash))
	}

	// Change the screen
	_, err = client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     "echo hash-change-test",
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "hash-change-test"}},
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	// Take second screenshot — hash should differ
	ss2, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{SessionId: sid})
	if err != nil {
		t.Fatalf("Screenshot 2: %v", err)
	}
	if ss1.ScreenHash == ss2.ScreenHash {
		t.Error("screen hash should change after new output")
	}
}

func TestIntegration_Recording(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()

	recordDir := t.TempDir()
	castPath := filepath.Join(recordDir, "test.cast")

	// Run with recording
	runResp, err := client.Run(ctx, &virtuipb.RunRequest{
		Command:    []string{"bash"},
		Cols:       80,
		Rows:       24,
		Record:     true,
		RecordPath: castPath,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runResp.RecordingPath != castPath {
		t.Errorf("expected recording path %q, got %q", castPath, runResp.RecordingPath)
	}
	sid := runResp.SessionId

	time.Sleep(500 * time.Millisecond)

	// Execute a command to generate input + output events
	_, err = client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     "echo recorded-output",
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "recorded-output"}},
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	// Kill to flush recording
	_, err = client.Kill(ctx, &virtuipb.KillRequest{SessionId: sid})
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Verify the cast file
	f, err := os.Open(castPath)
	if err != nil {
		t.Fatalf("open cast: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// Line 1: header
	if !scanner.Scan() {
		t.Fatal("expected header line")
	}
	var header map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		t.Fatalf("parse header: %v", err)
	}
	if header["version"] != float64(2) {
		t.Errorf("expected version 2, got %v", header["version"])
	}
	if header["width"] != float64(80) {
		t.Errorf("expected width 80, got %v", header["width"])
	}
	if header["height"] != float64(24) {
		t.Errorf("expected height 24, got %v", header["height"])
	}

	// Remaining lines: events
	var hasInput, hasOutput bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `"i"`) {
			hasInput = true
		}
		if strings.Contains(line, `"o"`) {
			hasOutput = true
		}
	}
	if !hasInput {
		t.Error("recording should contain input events")
	}
	if !hasOutput {
		t.Error("recording should contain output events")
	}
}

func TestIntegration_Pipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	resp, err := client.Pipeline(ctx, &virtuipb.PipelineRequest{
		SessionId:   sid,
		StopOnError: true,
		Steps: []*virtuipb.PipelineStep{
			{Step: &virtuipb.PipelineStep_Exec{Exec: &virtuipb.ExecRequest{
				Input:     "echo pipeline-step-1",
				Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "pipeline-step-1"}},
				TimeoutMs: 5000,
			}}},
			{Step: &virtuipb.PipelineStep_Sleep{Sleep: &virtuipb.SleepStep{DurationMs: 200}}},
			{Step: &virtuipb.PipelineStep_Screenshot{Screenshot: &virtuipb.ScreenshotRequest{}}},
			{Step: &virtuipb.PipelineStep_Exec{Exec: &virtuipb.ExecRequest{
				Input:     "echo pipeline-step-2",
				Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "pipeline-step-2"}},
				TimeoutMs: 5000,
			}}},
		},
	})
	if err != nil {
		t.Fatalf("Pipeline: %v", err)
	}
	if len(resp.Results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(resp.Results))
	}
	for i, r := range resp.Results {
		if !r.Success {
			t.Errorf("step %d failed: %s", i, r.ErrorMessage)
		}
	}

	// The screenshot step (index 2) should have captured pipeline-step-1
	ssResult := resp.Results[2].GetScreenshot()
	if ssResult == nil {
		t.Fatal("expected screenshot result at step 2")
	}
	if !strings.Contains(ssResult.ScreenText, "pipeline-step-1") {
		t.Errorf("screenshot should contain 'pipeline-step-1', got:\n%s", ssResult.ScreenText)
	}
}

func TestIntegration_PressRepeat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	// Type several characters using press with repeat
	_, err := client.Press(ctx, &virtuipb.PressRequest{
		SessionId: sid,
		Keys:      []string{"a"},
		Repeat:    5,
	})
	if err != nil {
		t.Fatalf("Press repeat: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	ss, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{SessionId: sid})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.Contains(ss.ScreenText, "aaaaa") {
		t.Errorf("screen should contain 'aaaaa' after 5x press, got:\n%s", ss.ScreenText)
	}
}
