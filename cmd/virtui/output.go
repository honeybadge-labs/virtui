package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	virtuipb "github.com/honeybadge-labs/virtui/proto/virtui/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func outputJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func outputRun(resp *virtuipb.RunResponse, jsonMode bool) {
	if jsonMode {
		outputJSON(map[string]any{
			"session_id":     resp.SessionId,
			"pid":            resp.Pid,
			"recording_path": resp.RecordingPath,
		})
		return
	}
	fmt.Printf("session_id: %s\n", resp.SessionId)
	fmt.Printf("pid: %d\n", resp.Pid)
	if resp.RecordingPath != "" {
		fmt.Printf("recording: %s\n", resp.RecordingPath)
	}
}

func outputExec(resp *virtuipb.ExecResponse, jsonMode bool) {
	if jsonMode {
		outputJSON(map[string]any{
			"screen_text": resp.ScreenText,
			"screen_hash": resp.ScreenHash,
			"cursor_row":  resp.CursorRow,
			"cursor_col":  resp.CursorCol,
			"elapsed_ms":  resp.ElapsedMs,
		})
		return
	}
	fmt.Print(resp.ScreenText)
	fmt.Println()
}

func outputScreenshot(resp *virtuipb.ScreenshotResponse, jsonMode bool) {
	if jsonMode {
		outputJSON(map[string]any{
			"screen_text": resp.ScreenText,
			"screen_hash": resp.ScreenHash,
			"cursor_row":  resp.CursorRow,
			"cursor_col":  resp.CursorCol,
			"cols":        resp.Cols,
			"rows":        resp.Rows,
		})
		return
	}
	fmt.Print(resp.ScreenText)
	fmt.Println()
}

func outputPress(resp *virtuipb.PressResponse, jsonMode bool) {
	if jsonMode {
		outputJSON(map[string]any{
			"screen_text": resp.ScreenText,
			"screen_hash": resp.ScreenHash,
		})
		return
	}
	// Silent in text mode
}

func outputType(resp *virtuipb.TypeResponse, jsonMode bool) {
	if jsonMode {
		outputJSON(map[string]any{
			"screen_text": resp.ScreenText,
			"screen_hash": resp.ScreenHash,
		})
		return
	}
	// Silent in text mode
}

func outputWait(resp *virtuipb.WaitResponse, jsonMode bool) {
	if jsonMode {
		outputJSON(map[string]any{
			"screen_text": resp.ScreenText,
			"screen_hash": resp.ScreenHash,
			"elapsed_ms":  resp.ElapsedMs,
		})
		return
	}
	fmt.Printf("ok (%dms)\n", resp.ElapsedMs)
}

func outputSessions(resp *virtuipb.SessionsResponse, jsonMode bool) {
	if jsonMode {
		sessions := make([]map[string]any, 0, len(resp.Sessions))
		for _, s := range resp.Sessions {
			sessions = append(sessions, map[string]any{
				"session_id":     s.SessionId,
				"pid":            s.Pid,
				"command":        s.Command,
				"cols":           s.Cols,
				"rows":           s.Rows,
				"running":        s.Running,
				"exit_code":      s.ExitCode,
				"created_at":     s.CreatedAt,
				"recording_path": s.RecordingPath,
			})
		}
		outputJSON(sessions)
		return
	}
	if len(resp.Sessions) == 0 {
		fmt.Println("No active sessions.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tPID\tCOMMAND\tSIZE\tSTATUS")
	for _, s := range resp.Sessions {
		status := "running"
		if !s.Running {
			status = fmt.Sprintf("exited(%d)", s.ExitCode)
		}
		cmd := strings.Join(s.Command, " ")
		_, _ = fmt.Fprintf(w, "%s\t%d\t%s\t%dx%d\t%s\n", s.SessionId, s.Pid, cmd, s.Cols, s.Rows, status)
	}
	_ = w.Flush()
}

func outputPipeline(resp *virtuipb.PipelineResponse, jsonMode bool) {
	if jsonMode {
		b, err := protojson.Marshal(resp)
		if err == nil {
			_, _ = os.Stdout.Write(b)
			_, _ = os.Stdout.Write([]byte("\n"))
		} else {
			outputJSON(resp)
		}
		return
	}
	for _, r := range resp.Results {
		if r.Success {
			fmt.Printf("step %d: ok\n", r.StepIndex)
		} else {
			fmt.Printf("step %d: error: %s\n", r.StepIndex, r.ErrorMessage)
		}
	}
}
