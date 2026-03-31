package pipeline

import (
	"fmt"

	virtuipb "github.com/rotemtam/virtui/proto/virtui/v1"
)

// ConvertSteps converts protobuf PipelineSteps to executable Steps.
func ConvertSteps(pbSteps []*virtuipb.PipelineStep) ([]Step, error) {
	steps := make([]Step, 0, len(pbSteps))
	for i, pb := range pbSteps {
		s, err := convertStep(pb)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		steps = append(steps, s)
	}
	return steps, nil
}

func convertStep(pb *virtuipb.PipelineStep) (Step, error) {
	switch s := pb.Step.(type) {
	case *virtuipb.PipelineStep_Exec:
		step := &ExecStep{
			Input:     s.Exec.Input,
			TimeoutMs: s.Exec.TimeoutMs,
		}
		if s.Exec.Wait != nil {
			step.Wait = convertWait(s.Exec.Wait)
		}
		return step, nil
	case *virtuipb.PipelineStep_Press:
		return &PressStep{
			Keys:   s.Press.Keys,
			Repeat: s.Press.Repeat,
		}, nil
	case *virtuipb.PipelineStep_Type:
		return &TypeStep{
			Text: s.Type.Text,
		}, nil
	case *virtuipb.PipelineStep_Wait:
		opts := WaitOpts{}
		if s.Wait.Condition != nil {
			w := convertWait(s.Wait.Condition)
			opts = *w
		}
		return &WaitStep{
			Opts:      opts,
			TimeoutMs: s.Wait.TimeoutMs,
		}, nil
	case *virtuipb.PipelineStep_Screenshot:
		return &ScreenshotStep{}, nil
	case *virtuipb.PipelineStep_Sleep:
		return &SleepStep{
			DurationMs: s.Sleep.DurationMs,
		}, nil
	default:
		return nil, fmt.Errorf("unknown step type: %T", pb.Step)
	}
}

func convertWait(w *virtuipb.WaitCondition) *WaitOpts {
	opts := &WaitOpts{}
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
