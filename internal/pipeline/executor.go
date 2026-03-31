package pipeline

import (
	"context"

	"github.com/rotemtam/virtui/internal/session"
)

// Executor runs a sequence of pipeline steps.
type Executor struct{}

// NewExecutor creates a new pipeline executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// Run executes steps sequentially. If stopOnError is true, it stops at the first failure.
func (e *Executor) Run(ctx context.Context, sess *session.Session, steps []Step, stopOnError bool) []Result {
	results := make([]Result, 0, len(steps))
	for i, step := range steps {
		result, err := step.Execute(ctx, sess)
		if err != nil {
			results = append(results, Result{
				StepIndex: i,
				Success:   false,
				Error:     err.Error(),
			})
			if stopOnError {
				break
			}
			continue
		}
		result.StepIndex = i
		results = append(results, *result)
		if !result.Success && stopOnError {
			break
		}
	}
	return results
}
