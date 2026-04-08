// Package pipeline provides a sequential step executor for batch operations.
package pipeline

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/honeybadge-labs/virtui/internal/session"
	"github.com/honeybadge-labs/virtui/internal/terminal"
)

// Result is the outcome of a single pipeline step.
type Result struct {
	StepIndex int
	Success   bool
	Error     string
	Screen    *terminal.Screen
	ElapsedMs int64
}

// Step is an individual operation in a pipeline.
type Step interface {
	Execute(ctx context.Context, sess *session.Session) (*Result, error)
}

// ExecStep types input, presses Enter, and optionally waits.
type ExecStep struct {
	Input     string
	Wait      *WaitOpts
	TimeoutMs uint32
}

// WaitOpts describes what to wait for.
type WaitOpts struct {
	Text   string
	Stable bool
	Gone   string
	Regex  string
}

func (s *ExecStep) Execute(ctx context.Context, sess *session.Session) (*Result, error) {
	start := time.Now()
	// Type the input + Enter
	data := []byte(s.Input + "\r")
	if _, err := sess.Terminal.Write(data); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	// Wait if requested
	if s.Wait != nil {
		timeout := time.Duration(s.TimeoutMs) * time.Millisecond
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		if err := waitCondition(ctx, sess, s.Wait, timeout); err != nil {
			return &Result{Success: false, Error: err.Error(), ElapsedMs: time.Since(start).Milliseconds()}, nil
		}
	}
	screen := sess.Terminal.Screen()
	return &Result{
		Success:   true,
		Screen:    &screen,
		ElapsedMs: time.Since(start).Milliseconds(),
	}, nil
}

// PressStep sends key presses.
type PressStep struct {
	Keys   []string
	Repeat uint32
}

func (s *PressStep) Execute(_ context.Context, sess *session.Session) (*Result, error) {
	start := time.Now()
	repeat := int(s.Repeat)
	if repeat == 0 {
		repeat = 1
	}
	for range repeat {
		for _, key := range s.Keys {
			seq, ok := terminal.ResolveKey(key)
			if !ok {
				return nil, fmt.Errorf("unknown key: %s", key)
			}
			if _, err := sess.Terminal.Write([]byte(seq)); err != nil {
				return nil, fmt.Errorf("write key: %w", err)
			}
		}
	}
	screen := sess.Terminal.Screen()
	return &Result{
		Success:   true,
		Screen:    &screen,
		ElapsedMs: time.Since(start).Milliseconds(),
	}, nil
}

// TypeStep sends text without Enter.
type TypeStep struct {
	Text string
}

func (s *TypeStep) Execute(_ context.Context, sess *session.Session) (*Result, error) {
	start := time.Now()
	if _, err := sess.Terminal.Write([]byte(s.Text)); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	screen := sess.Terminal.Screen()
	return &Result{
		Success:   true,
		Screen:    &screen,
		ElapsedMs: time.Since(start).Milliseconds(),
	}, nil
}

// WaitStep waits for a screen condition.
type WaitStep struct {
	Opts      WaitOpts
	TimeoutMs uint32
}

func (s *WaitStep) Execute(ctx context.Context, sess *session.Session) (*Result, error) {
	start := time.Now()
	timeout := time.Duration(s.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if err := waitCondition(ctx, sess, &s.Opts, timeout); err != nil {
		return &Result{Success: false, Error: err.Error(), ElapsedMs: time.Since(start).Milliseconds()}, nil
	}
	screen := sess.Terminal.Screen()
	return &Result{
		Success:   true,
		Screen:    &screen,
		ElapsedMs: time.Since(start).Milliseconds(),
	}, nil
}

// ScreenshotStep captures the screen.
type ScreenshotStep struct {
	NoColor bool
}

func (s *ScreenshotStep) Execute(_ context.Context, sess *session.Session) (*Result, error) {
	screen := sess.Terminal.Screen()
	if s.NoColor {
		screen.ANSI = ""
	}
	return &Result{
		Success: true,
		Screen:  &screen,
	}, nil
}

// SleepStep pauses for a duration.
type SleepStep struct {
	DurationMs uint32
}

func (s *SleepStep) Execute(ctx context.Context, _ *session.Session) (*Result, error) {
	select {
	case <-time.After(time.Duration(s.DurationMs) * time.Millisecond):
		return &Result{Success: true}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// waitCondition blocks until the given condition is met or timeout.
func waitCondition(ctx context.Context, sess *session.Session, opts *WaitOpts, timeout time.Duration) error {
	deadline := time.After(timeout)
	updates, cancel := sess.Terminal.Subscribe()
	defer cancel()

	// Pre-compile regex once outside the hot loop.
	var re *regexp.Regexp
	if opts.Regex != "" {
		var err error
		re, err = regexp.Compile(opts.Regex)
		if err != nil {
			return fmt.Errorf("invalid regex %q: %w", opts.Regex, err)
		}
	}

	check := func() bool {
		screen := sess.Terminal.Screen()
		if opts.Text != "" {
			return strings.Contains(screen.Text, opts.Text)
		}
		if opts.Gone != "" {
			return !strings.Contains(screen.Text, opts.Gone)
		}
		if re != nil {
			return re.MatchString(screen.Text)
		}
		return false
	}

	// For stable wait, track screen hash changes
	if opts.Stable {
		return waitStable(ctx, sess, deadline, updates)
	}

	// Check immediately
	if check() {
		return nil
	}

	for {
		select {
		case <-deadline:
			return fmt.Errorf("wait timed out after %v", timeout)
		case <-ctx.Done():
			return ctx.Err()
		case <-updates:
			if check() {
				return nil
			}
		}
	}
}

// waitStable waits until the screen hasn't changed for 500ms.
func waitStable(ctx context.Context, sess *session.Session, deadline <-chan time.Time, updates <-chan struct{}) error {
	const stableDelay = 500 * time.Millisecond
	lastHash := sess.Terminal.Screen().Hash
	stableTimer := time.NewTimer(stableDelay)
	defer stableTimer.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("wait-stable timed out")
		case <-ctx.Done():
			return ctx.Err()
		case <-updates:
			screen := sess.Terminal.Screen()
			if screen.Hash != lastHash {
				lastHash = screen.Hash
				stableTimer.Reset(stableDelay)
			}
		case <-stableTimer.C:
			return nil
		}
	}
}
