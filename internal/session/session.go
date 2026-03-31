// Package session manages terminal sessions with IDs and metadata.
package session

import (
	"time"

	"github.com/honeybadge-labs/virtui/internal/terminal"
)

// Info contains metadata about a session (safe to send over the wire).
type Info struct {
	ID            string
	PID           int
	Command       []string
	Cols          int
	Rows          int
	Running       bool
	ExitCode      int
	CreatedAt     time.Time
	RecordingPath string
}

// Session wraps a Terminal with identity and metadata.
type Session struct {
	Info     Info
	Terminal *terminal.Emulator
}

// Snapshot returns an updated Info reflecting current terminal state.
func (s *Session) Snapshot() Info {
	info := s.Info
	info.Running = s.Terminal.Running()
	info.ExitCode = s.Terminal.ExitCode()
	return info
}
