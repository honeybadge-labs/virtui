package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	virtuipb "github.com/honeybadge-labs/virtui/proto/virtui/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func outputJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func outputProtoJSON(msg proto.Message) {
	b, err := protojson.MarshalOptions{Indent: "  ", UseProtoNames: true, EmitUnpopulated: true}.Marshal(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling proto: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

func outputRun(resp *virtuipb.RunResponse, jsonMode bool) {
	if jsonMode {
		outputProtoJSON(resp)
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
		outputProtoJSON(resp)
		return
	}
	fmt.Print(resp.ScreenText)
	fmt.Println()
}

func outputScreenshot(resp *virtuipb.ScreenshotResponse, jsonMode bool) {
	if jsonMode {
		outputProtoJSON(resp)
		return
	}
	fmt.Print(resp.ScreenText)
	fmt.Println()
}

func outputPress(resp *virtuipb.PressResponse, jsonMode bool) {
	if jsonMode {
		outputProtoJSON(resp)
		return
	}
	// Silent in text mode
}

func outputType(resp *virtuipb.TypeResponse, jsonMode bool) {
	if jsonMode {
		outputProtoJSON(resp)
		return
	}
	// Silent in text mode
}

func outputWait(resp *virtuipb.WaitResponse, jsonMode bool) {
	if jsonMode {
		outputProtoJSON(resp)
		return
	}
	fmt.Printf("ok (%dms)\n", resp.ElapsedMs)
}

func outputSessions(resp *virtuipb.SessionsResponse, jsonMode bool) {
	if jsonMode {
		outputProtoJSON(resp)
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
		outputProtoJSON(resp)
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
