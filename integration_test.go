package virtui_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/honeybadge-labs/virtui/internal/daemon"
	virtuipb "github.com/honeybadge-labs/virtui/proto/virtui/v1"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
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
	if ssResult := resp.Results[2].GetScreenshot(); ssResult == nil {
		t.Fatal("expected screenshot result at step 2")
	} else if !strings.Contains(ssResult.ScreenText, "pipeline-step-1") {
		t.Errorf("screenshot should contain 'pipeline-step-1', got:\n%s", ssResult.ScreenText)
	}
}

// TestIntegration_PipelineSkillExample validates that the JSON format documented
// in SKILL.md (the recommended Claude Code pipeline pattern) parses correctly
// via protojson and executes end-to-end.
func TestIntegration_PipelineSkillExample(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	// This is the exact JSON shape from SKILL.md § "Running a command reliably from Claude Code".
	// Uses snake_case keys to match the documented examples.
	skillJSON := `{"steps":[
		{"type":{"text":"echo hello world"}},
		{"press":{"keys":["Enter"]}},
		{"wait":{"condition":{"text":"hello world"},"timeout_ms":5000}},
		{"screenshot":{}}
	],"stop_on_error":true}`

	req := &virtuipb.PipelineRequest{SessionId: sid}
	if err := protojson.Unmarshal([]byte(skillJSON), req); err != nil {
		t.Fatalf("protojson.Unmarshal of SKILL.md example failed: %v", err)
	}
	req.SessionId = sid

	if len(req.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(req.Steps))
	}
	// Verify each step parsed into the correct oneof variant.
	if req.Steps[0].GetType() == nil {
		t.Fatal("step 0: expected TypeRequest, got nil")
	}
	if req.Steps[1].GetPress() == nil {
		t.Fatal("step 1: expected PressRequest, got nil")
	}
	if req.Steps[2].GetWait() == nil {
		t.Fatal("step 2: expected WaitRequest, got nil")
	}
	if req.Steps[3].GetScreenshot() == nil {
		t.Fatal("step 3: expected ScreenshotRequest, got nil")
	}

	resp, err := client.Pipeline(ctx, req)
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

	// The screenshot at step 3 should contain our echoed text.
	if ssResult := resp.Results[3].GetScreenshot(); ssResult == nil {
		t.Fatal("expected screenshot result at step 3")
	} else if !strings.Contains(ssResult.ScreenText, "hello world") {
		t.Errorf("screenshot should contain 'hello world', got:\n%s", ssResult.ScreenText)
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

// TestIntegration_SharedParentSocketPath verifies that the daemon works when
// the socket lives under a shared directory like /tmp (the parent is not owned
// or exclusively permissioned by the daemon). This is a regression test to
// ensure peer-credential auth does not depend on parent directory permissions.
func TestIntegration_SharedParentSocketPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Place the socket directly inside /tmp (a shared, world-writable dir).
	dir, err := os.MkdirTemp("/tmp", "virtui-shared-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// Ensure the parent is world-readable, simulating /tmp or similar.
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	socketPath := filepath.Join(dir, "t.sock")

	srv := daemon.NewServer(socketPath)
	go func() {
		_ = srv.Start()
	}()
	defer srv.Stop()

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
	defer func() { _ = conn.Close() }()

	client := virtuipb.NewVirtuiServiceClient(conn)
	ctx := context.Background()

	// Same-user connection should succeed.
	_, err = client.Sessions(ctx, &virtuipb.SessionsRequest{})
	if err != nil {
		t.Fatalf("Sessions RPC failed: %v", err)
	}
}

// TestIntegration_PeerAuth_RejectsDifferentUID verifies that the daemon rejects
// connections from a different UID. It builds a static Linux virtui binary,
// starts the daemon on the host, then runs the binary inside a Docker container
// as UID 65534 (nobody). The authListener should close the connection before
// gRPC processes the request.
func TestIntegration_PeerAuth_RejectsDifferentUID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS != "linux" {
		t.Skip("skipping: Docker socket mount + peer credentials requires Linux host")
	}
	// Skip if Docker is not available.
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("skipping: docker not found in PATH")
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("skipping: docker daemon not reachable: %v\n%s", err, out)
	}

	// Build a static Linux binary.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "virtui")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/virtui")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// Start the daemon with a socket in /tmp.
	sockDir, err := os.MkdirTemp("/tmp", "virtui-peerauth-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	// Make the socket directory world-accessible so the container user
	// (UID 65534) can traverse into it and reach the socket. This ensures
	// that a non-zero exit is caused by authListener rejecting the peer
	// UID, not by filesystem permission errors on the mount.
	if err := os.Chmod(sockDir, 0o755); err != nil {
		t.Fatalf("chmod sockDir: %v", err)
	}
	socketPath := filepath.Join(sockDir, "t.sock")

	srv := daemon.NewServer(socketPath)
	go func() { _ = srv.Start() }()
	defer srv.Stop()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Make the socket file itself world-accessible so the only barrier
	// is the authListener peer-credential check, not file permissions.
	if err := os.Chmod(socketPath, 0o777); err != nil {
		t.Fatalf("chmod socket: %v", err)
	}

	ctx := context.Background()

	// Run virtui as UID 65534 (nobody) inside a container.
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "alpine:latest",
			Cmd:   []string{"/mnt/virtui", "--json", "--socket", "/sock/t.sock", "sessions"},
			HostConfigModifier: func(hc *dockercontainer.HostConfig) {
				hc.Mounts = []mount.Mount{
					{Type: mount.TypeBind, Source: binPath, Target: "/mnt/virtui", ReadOnly: true},
					{Type: mount.TypeBind, Source: sockDir, Target: "/sock"},
				}
			},
			User:       "65534",
			WaitingFor: wait.ForExit(),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	defer func() {
		_ = ctr.Terminate(ctx)
	}()

	// The container should have exited with a non-zero code because the
	// authListener rejected the connection (different UID).
	state, err := ctr.State(ctx)
	if err != nil {
		t.Fatalf("container state: %v", err)
	}
	if state.ExitCode == 0 {
		logs, _ := ctr.Logs(ctx)
		if logs != nil {
			buf := make([]byte, 4096)
			n, _ := logs.Read(buf)
			t.Fatalf("expected non-zero exit code, got 0; logs:\n%s", buf[:n])
		}
		t.Fatal("expected non-zero exit code, got 0")
	}
}

func TestIntegration_Shutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Use /tmp directly to keep socket path short (macOS 104 char limit).
	dir, err := os.MkdirTemp("/tmp", "virtui-shutdown-")
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
	defer conn.Close()

	client := virtuipb.NewVirtuiServiceClient(conn)
	ctx := context.Background()

	// Create a session to verify it gets cleaned up
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

	// Call Shutdown
	_, err = client.Shutdown(ctx, &virtuipb.ShutdownRequest{})
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Wait for socket to be removed (server-side postcondition)
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			return // success
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("socket was not removed after Shutdown")
}

func TestIntegration_CLISmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build the binary
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "virtui")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/virtui")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	// Use a temp socket
	sockDir, err := os.MkdirTemp("/tmp", "virtui-cli-smoke-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "d.sock")

	run := func(args ...string) ([]byte, error) {
		a := append([]string{"--json", "--socket", sock}, args...)
		c := exec.Command(binPath, a...)
		return c.CombinedOutput()
	}

	// daemon start (background)
	out, err := run("daemon", "start")
	if err != nil {
		t.Fatalf("daemon start: %v\n%s", err, out)
	}
	var startResult map[string]any
	if err := json.Unmarshal(out, &startResult); err != nil {
		t.Fatalf("parse start output: %v\n%s", err, out)
	}
	if startResult["socket"] == nil {
		t.Errorf("expected socket in start output, got: %s", out)
	}

	// daemon status → running
	out, err = run("daemon", "status")
	if err != nil {
		t.Fatalf("daemon status: %v\n%s", err, out)
	}
	var statusResult map[string]any
	if err := json.Unmarshal(out, &statusResult); err != nil {
		t.Fatalf("parse status output: %v\n%s", err, out)
	}
	if statusResult["running"] != true {
		t.Errorf("expected running=true, got: %s", out)
	}

	// daemon stop
	out, err = run("daemon", "stop")
	if err != nil {
		t.Fatalf("daemon stop: %v\n%s", err, out)
	}
	var stopResult map[string]any
	if err := json.Unmarshal(out, &stopResult); err != nil {
		t.Fatalf("parse stop output: %v\n%s", err, out)
	}
	if stopResult["ok"] != true {
		t.Errorf("expected ok=true, got: %s", out)
	}

	// daemon status → not running
	out, err = run("daemon", "status")
	if err != nil {
		t.Fatalf("daemon status after stop: %v\n%s", err, out)
	}
	var statusResult2 map[string]any
	if err := json.Unmarshal(out, &statusResult2); err != nil {
		t.Fatalf("parse status output: %v\n%s", err, out)
	}
	if statusResult2["running"] != false {
		t.Errorf("expected running=false after stop, got: %s", out)
	}
}

// --- ANSI Screenshot Tests ---

func TestIntegration_ScreenshotANSI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	_, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     `echo -e "\033[31mRED\033[0m PLAIN"`,
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "PLAIN"}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	resp, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{
		SessionId: sid,
	})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.Contains(resp.ScreenText, "RED PLAIN") {
		t.Errorf("screen_text should contain 'RED PLAIN', got:\n%s", resp.ScreenText)
	}
	if !strings.Contains(resp.ScreenAnsi, "\033[") {
		t.Errorf("screen_ansi should contain ANSI SGR codes, got:\n%s", resp.ScreenAnsi)
	}
	if !strings.Contains(resp.ScreenAnsi, "RED") {
		t.Errorf("screen_ansi should contain 'RED', got:\n%s", resp.ScreenAnsi)
	}
	if !strings.Contains(resp.ScreenAnsi, "PLAIN") {
		t.Errorf("screen_ansi should contain 'PLAIN', got:\n%s", resp.ScreenAnsi)
	}
}

func TestIntegration_ScreenshotANSI_Background(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	_, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     `echo -e "\033[42mGREEN_BG\033[0m"`,
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "GREEN_BG"}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	resp, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{
		SessionId: sid,
	})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.Contains(resp.ScreenAnsi, "\033[") {
		t.Errorf("screen_ansi should contain ANSI SGR codes for background")
	}
	if !strings.Contains(resp.ScreenAnsi, "GREEN_BG") {
		t.Errorf("screen_ansi should contain 'GREEN_BG'")
	}
}

func TestIntegration_ScreenshotANSI_Bold(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	_, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     `echo -e "\033[1mBOLD\033[0m \033[4mUNDERLINE\033[0m"`,
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "UNDERLINE"}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	resp, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{
		SessionId: sid,
	})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	// Bold SGR code is \033[0;1m (reset + bold)
	if !strings.Contains(resp.ScreenAnsi, ";1") {
		t.Errorf("screen_ansi should contain bold SGR param, got:\n%s", resp.ScreenAnsi)
	}
	// Underline SGR code is \033[0;4m (reset + underline)
	if !strings.Contains(resp.ScreenAnsi, ";4") {
		t.Errorf("screen_ansi should contain underline SGR param, got:\n%s", resp.ScreenAnsi)
	}
}

func TestIntegration_ScreenshotNoColor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	_, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     `echo -e "\033[31mRED\033[0m"`,
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "RED"}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	resp, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{
		SessionId: sid,
		NoColor:   true,
	})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if resp.ScreenAnsi != "" {
		t.Errorf("screen_ansi should be empty with no_color=true, got:\n%s", resp.ScreenAnsi)
	}
	if !strings.Contains(resp.ScreenText, "RED") {
		t.Errorf("screen_text should still contain 'RED', got:\n%s", resp.ScreenText)
	}
}

func TestIntegration_Screenshot256Color(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	_, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     `echo -e "\033[38;5;196mBRIGHT_RED\033[0m"`,
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "BRIGHT_RED"}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	resp, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{
		SessionId: sid,
	})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.Contains(resp.ScreenAnsi, "38;5;196") {
		t.Errorf("screen_ansi should contain 256-color SGR '38;5;196', got:\n%s", resp.ScreenAnsi)
	}
	if !strings.Contains(resp.ScreenAnsi, "BRIGHT_RED") {
		t.Errorf("screen_ansi should contain 'BRIGHT_RED'")
	}
}

func TestIntegration_ScreenshotANSI_DefaultCells(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	client, cleanup := startTestDaemon(t)
	defer cleanup()
	ctx := context.Background()
	sid := runBash(t, ctx, client)

	_, err := client.Exec(ctx, &virtuipb.ExecRequest{
		SessionId: sid,
		Input:     "echo PLAIN_TEXT",
		Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "PLAIN_TEXT"}},
		TimeoutMs: 10000,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	resp, err := client.Screenshot(ctx, &virtuipb.ScreenshotRequest{
		SessionId: sid,
	})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.Contains(resp.ScreenAnsi, "PLAIN_TEXT") {
		t.Errorf("screen_ansi should contain 'PLAIN_TEXT'")
	}
	// Find position of PLAIN_TEXT and check no SGR before it
	idx := strings.Index(resp.ScreenAnsi, "PLAIN_TEXT")
	if idx > 0 {
		before := resp.ScreenAnsi[:idx]
		// The text "PLAIN_TEXT" appears in the echo command on one line
		// and as output on the next. Check the output line specifically.
		lines := strings.Split(resp.ScreenAnsi, "\n")
		for _, line := range lines {
			// Find lines that start with PLAIN_TEXT (the output line, not the echo cmd)
			if strings.HasPrefix(line, "PLAIN_TEXT") {
				if strings.Contains(line[:len("PLAIN_TEXT")], "\033[") {
					t.Errorf("default-styled text should not have SGR codes before it, got line: %q", line)
				}
				break
			}
		}
		_ = before // used above conceptually
	}
}

func TestIntegration_PipelineScreenshotANSI(t *testing.T) {
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
				Input:     `echo -e "\033[31mPIPE_RED\033[0m"`,
				Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "PIPE_RED"}},
				TimeoutMs: 10000,
			}}},
			{Step: &virtuipb.PipelineStep_Screenshot{Screenshot: &virtuipb.ScreenshotRequest{}}},
		},
	})
	if err != nil {
		t.Fatalf("Pipeline: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	ssResult := resp.Results[1].GetScreenshot()
	if ssResult == nil {
		t.Fatal("expected screenshot result at step 1")
		return // unreachable but satisfies staticcheck SA5011
	}
	if ssResult.ScreenAnsi == "" {
		t.Error("pipeline screenshot screen_ansi should be non-empty")
	}
	if !strings.Contains(ssResult.ScreenAnsi, "PIPE_RED") {
		t.Errorf("pipeline screenshot screen_ansi should contain 'PIPE_RED', got:\n%s", ssResult.ScreenAnsi)
	}
}

func TestIntegration_PipelineScreenshotNoColor(t *testing.T) {
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
				Input:     `echo -e "\033[31mPIPE_NOCOLOR\033[0m"`,
				Wait:      &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: "PIPE_NOCOLOR"}},
				TimeoutMs: 10000,
			}}},
			{Step: &virtuipb.PipelineStep_Screenshot{Screenshot: &virtuipb.ScreenshotRequest{
				NoColor: true,
			}}},
		},
	})
	if err != nil {
		t.Fatalf("Pipeline: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	ssResult := resp.Results[1].GetScreenshot()
	if ssResult == nil {
		t.Fatal("expected screenshot result at step 1")
		return // unreachable but satisfies staticcheck SA5011
	}
	if ssResult.ScreenAnsi != "" {
		t.Errorf("pipeline screenshot with no_color=true should have empty screen_ansi, got:\n%s", ssResult.ScreenAnsi)
	}
	if !strings.Contains(ssResult.ScreenText, "PIPE_NOCOLOR") {
		t.Errorf("screen_text should still contain 'PIPE_NOCOLOR', got:\n%s", ssResult.ScreenText)
	}
}
