package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/honeybadge-labs/virtui/internal/client"
	"github.com/honeybadge-labs/virtui/internal/daemon"
	virtuipb "github.com/honeybadge-labs/virtui/proto/virtui/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// CLI is the top-level command structure.
type CLI struct {
	JSON    bool     `help:"Output in JSON format." short:"j"`
	Socket  string   `help:"Daemon socket path." default:"~/.virtui/daemon.sock" env:"VIRTUI_SOCKET"`
	Version VersionCmd `cmd:"" help:"Print version information."`

	Run        RunCmd        `cmd:"" help:"Spawn a new terminal session."`
	Exec       ExecCmd       `cmd:"" help:"Type input, press Enter, optionally wait."`
	Screenshot ScreenshotCmd `cmd:"" help:"Capture terminal screen."`
	Press      PressCmd      `cmd:"" help:"Send a key press."`
	Type       TypeCmd       `cmd:"" help:"Type text without pressing Enter."`
	Wait       WaitCmd       `cmd:"" help:"Wait for a screen condition."`
	Kill       KillCmd       `cmd:"" help:"Terminate a session."`
	Resize     ResizeCmd     `cmd:"" help:"Resize terminal dimensions."`
	Sessions   SessionsCmd   `cmd:"" help:"List or show sessions."`
	Pipeline   PipelineCmd   `cmd:"" help:"Execute a batch of operations."`
	Daemon     DaemonCmd     `cmd:"" help:"Manage the daemon."`
}

type RunCmd struct {
	Command    []string `arg:"" help:"Command to run." passthrough:""`
	Cols       uint32   `help:"Terminal columns." default:"80"`
	Rows       uint32   `help:"Terminal rows." default:"24"`
	Env        []string `help:"Environment variables (KEY=VALUE)." short:"e"`
	Dir        string   `help:"Working directory." short:"d"`
	Record     bool     `help:"Record session in asciicast v2 format."`
	RecordPath string   `help:"Custom recording file path."`
}

type ExecCmd struct {
	Session    string `arg:"" help:"Session ID."`
	Input      string `arg:"" help:"Text to type + Enter."`
	Wait       string `help:"Wait for text to appear." optional:""`
	WaitStable bool   `help:"Wait for screen to stabilize."`
	WaitGone   string `help:"Wait for text to disappear." optional:""`
	WaitRegex  string `help:"Wait for regex match." optional:""`
	Timeout    uint32 `help:"Timeout in ms." default:"30000"`
}

type ScreenshotCmd struct {
	Session string `arg:"" help:"Session ID."`
	NoColor bool   `help:"Omit ANSI color codes from output." name:"no-color"`
}

type PressCmd struct {
	Session string   `arg:"" help:"Session ID."`
	Keys    []string `arg:"" help:"Key names to press."`
	Repeat  uint32   `help:"Repeat count." default:"1"`
}

type TypeCmd struct {
	Session string `arg:"" help:"Session ID."`
	Text    string `arg:"" help:"Text to type."`
}

type WaitCmd struct {
	Session   string `arg:"" help:"Session ID."`
	Text      string `help:"Wait for text to appear." optional:""`
	Stable    bool   `help:"Wait for screen to stabilize."`
	Gone      string `help:"Wait for text to disappear." optional:""`
	Regex     string `help:"Wait for regex match." optional:""`
	Timeout   uint32 `help:"Timeout in ms." default:"30000"`
}

type KillCmd struct {
	Session string `arg:"" help:"Session ID."`
}

type ResizeCmd struct {
	Session string `arg:"" help:"Session ID."`
	Cols    uint32 `help:"Terminal columns." required:""`
	Rows    uint32 `help:"Terminal rows." required:""`
}

type SessionsCmd struct {
	Show SessionsShowCmd `cmd:"" help:"Show details for a session." default:"withargs"`
}

type SessionsShowCmd struct {
	Session string `arg:"" help:"Session ID." optional:""`
}

type PipelineCmd struct {
	Session string `arg:"" help:"Session ID."`
	File    string `help:"JSON file with steps." optional:"" type:"existingfile"`
}

type VersionCmd struct{}

func (cmd *VersionCmd) Run(cli *CLI) error {
	if cli.JSON {
		outputJSON(map[string]any{"version": version, "commit": commit, "date": date})
	} else {
		fmt.Printf("virtui %s (commit: %s, built: %s)\n", version, commit, date)
	}
	return nil
}

type DaemonCmd struct {
	Start  DaemonStartCmd  `cmd:"" help:"Start the daemon."`
	Stop   DaemonStopCmd   `cmd:"" help:"Stop the daemon."`
	Status DaemonStatusCmd `cmd:"" help:"Show daemon status."`
}

type DaemonStartCmd struct {
	Foreground bool `help:"Run in the foreground."`
}

type DaemonStopCmd struct{}

type DaemonStatusCmd struct{}

func connect(cli *CLI) (*client.Client, error) {
	return client.New(cli.Socket)
}

func main() {
	cli := CLI{}
	ctx := kong.Parse(&cli,
		kong.Name("virtui"),
		kong.Description("TUI automation for AI agents."),
		kong.UsageOnError(),
	)
	// Expand ~ in socket path
	if strings.HasPrefix(cli.Socket, "~/") {
		home, _ := os.UserHomeDir()
		cli.Socket = filepath.Join(home, cli.Socket[2:])
	}
	err := ctx.Run(&cli)
	if err != nil {
		if cli.JSON {
			outputError(err)
		} else {
			ctx.FatalIfErrorf(err)
		}
		os.Exit(1)
	}
}

func (cmd *RunCmd) Run(cli *CLI) error {
	// Default to caller's working directory so sessions don't inherit
	// the daemon's CWD (which is "/" when started in background).
	if cmd.Dir == "" {
		cmd.Dir, _ = os.Getwd()
	}
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()
	resp, err := c.Run(context.Background(), &virtuipb.RunRequest{
		Command:    cmd.Command,
		Cols:       cmd.Cols,
		Rows:       cmd.Rows,
		Env:        cmd.Env,
		Dir:        cmd.Dir,
		Record:     cmd.Record,
		RecordPath: cmd.RecordPath,
	})
	if err != nil {
		return err
	}
	outputRun(resp, cli.JSON)
	return nil
}

func (cmd *ExecCmd) Run(cli *CLI) error {
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()
	req := &virtuipb.ExecRequest{
		SessionId: cmd.Session,
		Input:     cmd.Input,
		TimeoutMs: cmd.Timeout,
	}
	if cmd.Wait != "" {
		req.Wait = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: cmd.Wait}}
	} else if cmd.WaitStable {
		req.Wait = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Stable{Stable: true}}
	} else if cmd.WaitGone != "" {
		req.Wait = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Gone{Gone: cmd.WaitGone}}
	} else if cmd.WaitRegex != "" {
		req.Wait = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Regex{Regex: cmd.WaitRegex}}
	}
	resp, err := c.Exec(context.Background(), req)
	if err != nil {
		return err
	}
	outputExec(resp, cli.JSON)
	return nil
}

func (cmd *ScreenshotCmd) Run(cli *CLI) error {
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()
	resp, err := c.Screenshot(context.Background(), &virtuipb.ScreenshotRequest{
		SessionId: cmd.Session,
		NoColor:   cmd.NoColor,
	})
	if err != nil {
		return err
	}
	outputScreenshot(resp, cli.JSON)
	return nil
}

func (cmd *PressCmd) Run(cli *CLI) error {
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()
	resp, err := c.Press(context.Background(), &virtuipb.PressRequest{
		SessionId: cmd.Session,
		Keys:      cmd.Keys,
		Repeat:    cmd.Repeat,
	})
	if err != nil {
		return err
	}
	outputPress(resp, cli.JSON)
	return nil
}

func (cmd *TypeCmd) Run(cli *CLI) error {
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()
	resp, err := c.Type(context.Background(), &virtuipb.TypeRequest{
		SessionId: cmd.Session,
		Text:      cmd.Text,
	})
	if err != nil {
		return err
	}
	outputType(resp, cli.JSON)
	return nil
}

func (cmd *WaitCmd) Run(cli *CLI) error {
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()
	req := &virtuipb.WaitRequest{
		SessionId: cmd.Session,
		TimeoutMs: cmd.Timeout,
	}
	if cmd.Text != "" {
		req.Condition = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Text{Text: cmd.Text}}
	} else if cmd.Stable {
		req.Condition = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Stable{Stable: true}}
	} else if cmd.Gone != "" {
		req.Condition = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Gone{Gone: cmd.Gone}}
	} else if cmd.Regex != "" {
		req.Condition = &virtuipb.WaitCondition{Condition: &virtuipb.WaitCondition_Regex{Regex: cmd.Regex}}
	}
	resp, err := c.Wait(context.Background(), req)
	if err != nil {
		return err
	}
	outputWait(resp, cli.JSON)
	return nil
}

func (cmd *KillCmd) Run(cli *CLI) error {
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Kill(context.Background(), &virtuipb.KillRequest{
		SessionId: cmd.Session,
	})
	if err != nil {
		return err
	}
	if cli.JSON {
		outputJSON(map[string]any{"ok": true})
	} else {
		fmt.Println("ok")
	}
	return nil
}

func (cmd *ResizeCmd) Run(cli *CLI) error {
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Resize(context.Background(), &virtuipb.ResizeRequest{
		SessionId: cmd.Session,
		Cols:      cmd.Cols,
		Rows:      cmd.Rows,
	})
	if err != nil {
		return err
	}
	if cli.JSON {
		outputJSON(map[string]any{"ok": true})
	} else {
		fmt.Println("ok")
	}
	return nil
}

func (cmd *SessionsShowCmd) Run(cli *CLI) error {
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()
	resp, err := c.Sessions(context.Background(), &virtuipb.SessionsRequest{
		SessionId: cmd.Session,
	})
	if err != nil {
		return err
	}
	outputSessions(resp, cli.JSON)
	return nil
}

func (cmd *PipelineCmd) Run(cli *CLI) error {
	c, err := connect(cli)
	if err != nil {
		return err
	}
	defer c.Close()

	var data []byte
	if cmd.File != "" {
		data, err = os.ReadFile(cmd.File)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	}

	req := &virtuipb.PipelineRequest{
		SessionId: cmd.Session,
	}
	if err := protojson.Unmarshal(data, req); err != nil {
		return fmt.Errorf("parse pipeline JSON: %w", err)
	}
	// Ensure session_id from arg takes precedence
	req.SessionId = cmd.Session

	resp, err := c.Pipeline(context.Background(), req)
	if err != nil {
		return err
	}
	outputPipeline(resp, cli.JSON)
	return nil
}

func (cmd *DaemonStartCmd) Run(cli *CLI) error {
	if cmd.Foreground {
		return runDaemonForeground(cli.Socket, cli.JSON)
	}
	return runDaemonBackground(cli.Socket, cli.JSON)
}

func runDaemonForeground(socketPath string, jsonMode bool) error {
	srv := daemon.NewServer(socketPath)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		srv.Stop()
	}()
	if err := srv.Listen(); err != nil {
		return err
	}
	// Output AFTER socket is bound — safe to report ready
	if jsonMode {
		outputJSON(map[string]any{"socket": socketPath})
	} else {
		fmt.Fprintf(os.Stderr, "virtui daemon listening on %s\n", socketPath)
	}
	return srv.Serve()
}

func runDaemonBackground(socketPath string, jsonMode bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	// Create log directory
	dir := filepath.Dir(socketPath)
	_ = os.MkdirAll(dir, 0o755)
	logPath := filepath.Join(dir, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	attr := &os.ProcAttr{
		Dir:   "/",
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, logFile, logFile},
		Sys: &syscall.SysProcAttr{
			Setsid: true,
		},
	}
	proc, err := os.StartProcess(exe, []string{exe, "--socket", socketPath, "daemon", "start", "--foreground"}, attr)
	if err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start daemon: %w", err)
	}
	pid := proc.Pid
	_ = logFile.Close()

	// Wait for socket to become reachable before releasing the process handle.
	// If readiness times out, kill the orphaned child so callers don't get a
	// "failed start" while a daemon is still running in the background.
	if !waitForReady(socketPath, 5*time.Second) {
		_ = proc.Kill()
		_, _ = proc.Wait()
		return fmt.Errorf("daemon process started (pid %d) but not reachable; check %s",
			pid, logPath)
	}
	_ = proc.Release()
	if jsonMode {
		outputJSON(map[string]any{"pid": pid, "socket": socketPath})
	} else {
		fmt.Fprintf(os.Stderr, "daemon started (pid %d), socket: %s\n", pid, socketPath)
	}
	return nil
}

func (cmd *DaemonStopCmd) Run(cli *CLI) error {
	c, err := client.New(cli.Socket)
	if err != nil {
		// If we can't connect, try to remove the socket file
		_ = os.Remove(cli.Socket)
		if cli.JSON {
			outputJSON(map[string]any{"ok": true, "message": "daemon not running"})
		} else {
			fmt.Fprintln(os.Stderr, "daemon not running (cleaned up socket)")
		}
		return nil
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = c.Shutdown(ctx, &virtuipb.ShutdownRequest{})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.Unavailable:
			// Server died mid-RPC — already stopping
		case codes.Unimplemented:
			return fmt.Errorf("daemon does not support graceful shutdown (running old version); kill it manually or upgrade")
		default:
			return fmt.Errorf("shutdown: %w", err)
		}
	}

	// Wait for daemon to actually stop
	if !waitForStop(cli.Socket, 10*time.Second) {
		return fmt.Errorf("shutdown initiated but daemon did not stop within 10s")
	}

	if cli.JSON {
		outputJSON(map[string]any{"ok": true})
	} else {
		fmt.Fprintln(os.Stderr, "daemon stopped")
	}
	return nil
}

func (cmd *DaemonStatusCmd) Run(cli *CLI) error {
	c, err := client.New(cli.Socket)
	if err != nil {
		if cli.JSON {
			outputJSON(map[string]any{"running": false, "socket": cli.Socket})
		} else {
			fmt.Println("daemon: not running")
		}
		return nil
	}
	_ = c.Close()
	if cli.JSON {
		outputJSON(map[string]any{"running": true, "socket": cli.Socket})
	} else {
		fmt.Printf("daemon: running (socket: %s)\n", cli.Socket)
	}
	return nil
}

// waitForReady polls until the Unix socket accepts connections or times out.
func waitForReady(socketPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// waitForStop polls until the socket file is removed, which is the last action
// in Server.Stop() — after GracefulStop/force-stop and all in-flight RPCs have
// drained. Checking file removal rather than connection refusal avoids reporting
// success during the GracefulStop drain window.
func waitForStop(socketPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
