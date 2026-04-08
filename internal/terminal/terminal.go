// Package terminal provides the core terminal abstraction backed by PTY + vt10x.
package terminal

import (
	"crypto/sha256"
	"fmt"
)

// Screen represents a snapshot of the terminal screen.
type Screen struct {
	Text      string // Full screen text with newlines
	ANSI      string // Screen content with ANSI SGR escape codes
	Hash      string // SHA-256 hex of Text
	CursorRow int
	CursorCol int
	Cols      int
	Rows      int
}

// ComputeHash returns a SHA-256 hex of the given text.
func ComputeHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}

// Terminal is the interface for interacting with a terminal session.
type Terminal interface {
	// Write sends bytes to the PTY stdin.
	Write(p []byte) (int, error)
	// Screen returns a snapshot of the terminal screen.
	Screen() Screen
	// Resize changes the terminal dimensions.
	Resize(cols, rows int) error
	// PID returns the process ID of the child process.
	PID() int
	// Running returns whether the child process is still alive.
	Running() bool
	// ExitCode returns the exit code of the child process (-1 if still running).
	ExitCode() int
	// Subscribe returns a channel that receives a notification on screen changes,
	// and a cancel function to unsubscribe.
	Subscribe() (updates <-chan struct{}, cancel func())
	// Close terminates the terminal and cleans up resources.
	Close() error
}
