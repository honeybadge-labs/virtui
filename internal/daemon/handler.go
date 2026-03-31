package daemon

import (
	"context"
	"time"

	verrors "github.com/rotemtam/virtui/internal/errors"
	"github.com/rotemtam/virtui/internal/pipeline"
	"github.com/rotemtam/virtui/internal/session"
	virtuipb "github.com/rotemtam/virtui/proto/virtui/v1"
)

// Handler implements the VirtuiServiceServer gRPC interface.
type Handler struct {
	virtuipb.UnimplementedVirtuiServiceServer
	manager  *session.Manager
	executor *pipeline.Executor
}

// NewHandler creates a new gRPC handler.
func NewHandler(mgr *session.Manager) *Handler {
	return &Handler{
		manager:  mgr,
		executor: pipeline.NewExecutor(),
	}
}

func (h *Handler) Run(_ context.Context, req *virtuipb.RunRequest) (*virtuipb.RunResponse, error) {
	if len(req.Command) == 0 {
		return nil, verrors.Validation("command is required")
	}
	info, err := h.manager.Create(session.CreateOpts{
		Command:    req.Command,
		Cols:       int(req.Cols),
		Rows:       int(req.Rows),
		Env:        req.Env,
		Dir:        req.Dir,
		Record:     req.Record,
		RecordPath: req.RecordPath,
	})
	if err != nil {
		return nil, verrors.TerminalError(err.Error())
	}
	return &virtuipb.RunResponse{
		SessionId:     info.ID,
		Pid:           int32(info.PID),
		RecordingPath: info.RecordingPath,
	}, nil
}

func (h *Handler) Kill(_ context.Context, req *virtuipb.KillRequest) (*virtuipb.KillResponse, error) {
	if req.SessionId == "" {
		return nil, verrors.Validation("session_id is required")
	}
	if err := h.manager.Kill(req.SessionId, int(req.Signal)); err != nil {
		return nil, err
	}
	return &virtuipb.KillResponse{}, nil
}

func (h *Handler) Sessions(_ context.Context, req *virtuipb.SessionsRequest) (*virtuipb.SessionsResponse, error) {
	infos, err := h.manager.List(req.SessionId)
	if err != nil {
		return nil, err
	}
	resp := &virtuipb.SessionsResponse{}
	for _, info := range infos {
		resp.Sessions = append(resp.Sessions, infoToProto(info))
	}
	return resp, nil
}

func (h *Handler) Resize(_ context.Context, req *virtuipb.ResizeRequest) (*virtuipb.ResizeResponse, error) {
	if req.SessionId == "" {
		return nil, verrors.Validation("session_id is required")
	}
	sess, err := h.manager.Get(req.SessionId)
	if err != nil {
		return nil, err
	}
	if err := sess.Terminal.Resize(int(req.Cols), int(req.Rows)); err != nil {
		return nil, verrors.TerminalError(err.Error())
	}
	return &virtuipb.ResizeResponse{}, nil
}

func (h *Handler) Exec(ctx context.Context, req *virtuipb.ExecRequest) (*virtuipb.ExecResponse, error) {
	if req.SessionId == "" {
		return nil, verrors.Validation("session_id is required")
	}
	sess, err := h.manager.Get(req.SessionId)
	if err != nil {
		return nil, err
	}

	var waitOpts *pipeline.WaitOpts
	if req.Wait != nil {
		waitOpts = convertWaitCondition(req.Wait)
	}

	step := &pipeline.ExecStep{
		Input:     req.Input,
		Wait:      waitOpts,
		TimeoutMs: req.TimeoutMs,
	}

	result, execErr := step.Execute(ctx, sess)
	if execErr != nil {
		return nil, verrors.TerminalError(execErr.Error())
	}
	if !result.Success {
		return nil, verrors.Timeout("exec", req.TimeoutMs)
	}

	return &virtuipb.ExecResponse{
		ScreenText: result.Screen.Text,
		ScreenHash: result.Screen.Hash,
		CursorRow:  uint32(result.Screen.CursorRow),
		CursorCol:  uint32(result.Screen.CursorCol),
		ElapsedMs:  result.ElapsedMs,
	}, nil
}

func (h *Handler) Screenshot(_ context.Context, req *virtuipb.ScreenshotRequest) (*virtuipb.ScreenshotResponse, error) {
	if req.SessionId == "" {
		return nil, verrors.Validation("session_id is required")
	}
	sess, err := h.manager.Get(req.SessionId)
	if err != nil {
		return nil, err
	}
	screen := sess.Terminal.Screen()
	return &virtuipb.ScreenshotResponse{
		ScreenText: screen.Text,
		ScreenHash: screen.Hash,
		CursorRow:  uint32(screen.CursorRow),
		CursorCol:  uint32(screen.CursorCol),
		Cols:       uint32(screen.Cols),
		Rows:       uint32(screen.Rows),
	}, nil
}

func (h *Handler) Press(_ context.Context, req *virtuipb.PressRequest) (*virtuipb.PressResponse, error) {
	if req.SessionId == "" {
		return nil, verrors.Validation("session_id is required")
	}
	sess, err := h.manager.Get(req.SessionId)
	if err != nil {
		return nil, err
	}
	step := &pipeline.PressStep{
		Keys:   req.Keys,
		Repeat: req.Repeat,
	}
	result, execErr := step.Execute(context.Background(), sess)
	if execErr != nil {
		return nil, verrors.TerminalError(execErr.Error())
	}
	return &virtuipb.PressResponse{
		ScreenText: result.Screen.Text,
		ScreenHash: result.Screen.Hash,
	}, nil
}

func (h *Handler) Type(_ context.Context, req *virtuipb.TypeRequest) (*virtuipb.TypeResponse, error) {
	if req.SessionId == "" {
		return nil, verrors.Validation("session_id is required")
	}
	sess, err := h.manager.Get(req.SessionId)
	if err != nil {
		return nil, err
	}
	step := &pipeline.TypeStep{
		Text: req.Text,
	}
	result, execErr := step.Execute(context.Background(), sess)
	if execErr != nil {
		return nil, verrors.TerminalError(execErr.Error())
	}
	return &virtuipb.TypeResponse{
		ScreenText: result.Screen.Text,
		ScreenHash: result.Screen.Hash,
	}, nil
}

func (h *Handler) Wait(ctx context.Context, req *virtuipb.WaitRequest) (*virtuipb.WaitResponse, error) {
	if req.SessionId == "" {
		return nil, verrors.Validation("session_id is required")
	}
	sess, err := h.manager.Get(req.SessionId)
	if err != nil {
		return nil, err
	}
	opts := pipeline.WaitOpts{}
	if req.Condition != nil {
		w := convertWaitCondition(req.Condition)
		opts = *w
	}
	step := &pipeline.WaitStep{
		Opts:      opts,
		TimeoutMs: req.TimeoutMs,
	}
	result, execErr := step.Execute(ctx, sess)
	if execErr != nil {
		return nil, verrors.TerminalError(execErr.Error())
	}
	if !result.Success {
		return nil, verrors.Timeout("wait", req.TimeoutMs)
	}
	resp := &virtuipb.WaitResponse{
		ElapsedMs: result.ElapsedMs,
	}
	if result.Screen != nil {
		resp.ScreenText = result.Screen.Text
		resp.ScreenHash = result.Screen.Hash
	}
	return resp, nil
}

func (h *Handler) Pipeline(ctx context.Context, req *virtuipb.PipelineRequest) (*virtuipb.PipelineResponse, error) {
	if req.SessionId == "" {
		return nil, verrors.Validation("session_id is required")
	}
	sess, err := h.manager.Get(req.SessionId)
	if err != nil {
		return nil, err
	}
	steps, err := pipeline.ConvertSteps(req.Steps)
	if err != nil {
		return nil, verrors.Validation(err.Error())
	}
	results := h.executor.Run(ctx, sess, steps, req.StopOnError)
	resp := &virtuipb.PipelineResponse{}
	for _, r := range results {
		pr := &virtuipb.PipelineStepResult{
			StepIndex:    uint32(r.StepIndex),
			Success:      r.Success,
			ErrorMessage: r.Error,
		}
		if r.Screen != nil {
			// Set the appropriate result based on step type
			pr.Result = &virtuipb.PipelineStepResult_Screenshot{
				Screenshot: &virtuipb.ScreenshotResponse{
					ScreenText: r.Screen.Text,
					ScreenHash: r.Screen.Hash,
					CursorRow:  uint32(r.Screen.CursorRow),
					CursorCol:  uint32(r.Screen.CursorCol),
					Cols:       uint32(r.Screen.Cols),
					Rows:       uint32(r.Screen.Rows),
				},
			}
		}
		resp.Results = append(resp.Results, pr)
	}
	return resp, nil
}

func (h *Handler) Watch(req *virtuipb.WatchRequest, stream virtuipb.VirtuiService_WatchServer) error {
	if req.SessionId == "" {
		return verrors.Validation("session_id is required")
	}
	sess, err := h.manager.Get(req.SessionId)
	if err != nil {
		return err
	}
	updates, cancel := sess.Terminal.Subscribe()
	defer cancel()

	lastHash := ""
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-sess.Terminal.ExitCh():
			// Send exit event
			screen := sess.Terminal.Screen()
			_ = stream.Send(&virtuipb.WatchEvent{
				Type:       virtuipb.WatchEventType_WATCH_EVENT_TYPE_EXITED,
				ScreenText: screen.Text,
				ScreenHash: screen.Hash,
				ExitCode:   int32(sess.Terminal.ExitCode()),
			})
			return nil
		case <-updates:
			screen := sess.Terminal.Screen()
			if screen.Hash != lastHash {
				lastHash = screen.Hash
				if err := stream.Send(&virtuipb.WatchEvent{
					Type:       virtuipb.WatchEventType_WATCH_EVENT_TYPE_SCREEN_CHANGED,
					ScreenText: screen.Text,
					ScreenHash: screen.Hash,
				}); err != nil {
					return err
				}
			}
		}
	}
}

func infoToProto(info session.Info) *virtuipb.SessionInfo {
	return &virtuipb.SessionInfo{
		SessionId:     info.ID,
		Pid:           int32(info.PID),
		Command:       info.Command,
		Cols:          uint32(info.Cols),
		Rows:          uint32(info.Rows),
		Running:       info.Running,
		ExitCode:      int32(info.ExitCode),
		CreatedAt:     info.CreatedAt.Unix(),
		RecordingPath: info.RecordingPath,
	}
}

func convertWaitCondition(w *virtuipb.WaitCondition) *pipeline.WaitOpts {
	opts := &pipeline.WaitOpts{}
	switch c := w.Condition.(type) {
	case *virtuipb.WaitCondition_Text:
		opts.Text = c.Text
	case *virtuipb.WaitCondition_Stable:
		opts.Stable = c.Stable
	case *virtuipb.WaitCondition_Gone:
		opts.Gone = c.Gone
	case *virtuipb.WaitCondition_Regex:
		opts.Regex = c.Regex
	}
	return opts
}

// Small sleep to let terminal settle after write operations.
func smallDelay() {
	time.Sleep(50 * time.Millisecond)
}

var _ = smallDelay // suppress unused warning, used implicitly in some flows
