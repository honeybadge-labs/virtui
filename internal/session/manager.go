package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	verrors "github.com/honeybadge-labs/virtui/internal/errors"
	"github.com/honeybadge-labs/virtui/internal/terminal"
)

// CreateOpts are options for creating a new session.
type CreateOpts struct {
	Command    []string
	Cols       int
	Rows       int
	Env        []string
	Dir        string
	Record     bool
	RecordPath string
}

// Manager manages the lifecycle of terminal sessions.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// Create starts a new terminal session.
func (m *Manager) Create(opts CreateOpts) (*Info, error) {
	id := uuid.New().String()[:8]

	var rec *terminal.Recorder
	var recordingPath string
	if opts.Record {
		recordingPath = opts.RecordPath
		if recordingPath == "" {
			home, _ := os.UserHomeDir()
			dir := filepath.Join(home, ".virtui", "recordings")
			_ = os.MkdirAll(dir, 0o755)
			recordingPath = filepath.Join(dir, id+".cast")
		}
		f, err := os.Create(recordingPath)
		if err != nil {
			return nil, fmt.Errorf("create recording file: %w", err)
		}
		cols := opts.Cols
		if cols == 0 {
			cols = 80
		}
		rows := opts.Rows
		if rows == 0 {
			rows = 24
		}
		var recErr error
		rec, recErr = terminal.NewRecorder(f, cols, rows)
		if recErr != nil {
			f.Close()
			return nil, fmt.Errorf("create recorder: %w", recErr)
		}
	}

	em, err := terminal.NewEmulator(terminal.EmulatorOpts{
		Command:  opts.Command,
		Cols:     opts.Cols,
		Rows:     opts.Rows,
		Env:      opts.Env,
		Dir:      opts.Dir,
		Recorder: rec,
	})
	if err != nil {
		return nil, err
	}

	sess := &Session{
		Info: Info{
			ID:            id,
			PID:           em.PID(),
			Command:       opts.Command,
			Cols:          opts.Cols,
			Rows:          opts.Rows,
			Running:       true,
			ExitCode:      -1,
			CreatedAt:     time.Now(),
			RecordingPath: recordingPath,
		},
		Terminal: em,
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	info := sess.Snapshot()
	return &info, nil
}

// Get returns a session by ID.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[id]
	if !ok {
		return nil, verrors.SessionNotFound(id)
	}
	return sess, nil
}

// List returns info for all sessions, or a single one if id is non-empty.
func (m *Manager) List(id string) ([]Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if id != "" {
		sess, ok := m.sessions[id]
		if !ok {
			return nil, verrors.SessionNotFound(id)
		}
		return []Info{sess.Snapshot()}, nil
	}
	infos := make([]Info, 0, len(m.sessions))
	for _, sess := range m.sessions {
		infos = append(infos, sess.Snapshot())
	}
	return infos, nil
}

// Kill sends a signal to a session's process.
func (m *Manager) Kill(id string, signal int) error {
	sess, err := m.Get(id)
	if err != nil {
		return err
	}
	if !sess.Terminal.Running() {
		// Clean up even if not running
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		_ = sess.Terminal.Close()
		return nil
	}
	sig := syscall.Signal(signal)
	if signal == 0 {
		sig = syscall.SIGTERM
	}
	if sess.Terminal.PID() > 0 {
		proc, err := os.FindProcess(sess.Terminal.PID())
		if err == nil {
			_ = proc.Signal(sig)
		}
	}
	_ = sess.Terminal.Close()
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	return nil
}

// CloseAll terminates all sessions.
func (m *Manager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, sess := range m.sessions {
		_ = sess.Terminal.Close()
		delete(m.sessions, id)
	}
	return nil
}
