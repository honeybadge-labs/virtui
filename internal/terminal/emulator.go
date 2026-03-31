package terminal

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty/v2"
	"github.com/hinshun/vt10x"
)

// Emulator implements Terminal using creack/pty + vt10x.
type Emulator struct {
	mu   sync.RWMutex
	ptyF *os.File
	vt   vt10x.Terminal
	cmd  *exec.Cmd
	cols int
	rows int

	// subscribers for screen change notifications
	subMu   sync.Mutex
	subs    map[int]chan struct{}
	nextSub int

	// process state
	exitCode int
	exited   bool
	exitOnce sync.Once
	exitCh   chan struct{} // closed when process exits

	// recording
	recorder *Recorder
}

// EmulatorOpts configures a new emulator.
type EmulatorOpts struct {
	Command  []string
	Cols     int
	Rows     int
	Env      []string
	Dir      string
	Recorder *Recorder
}

// NewEmulator creates and starts a new terminal emulator.
func NewEmulator(opts EmulatorOpts) (*Emulator, error) {
	if len(opts.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	if opts.Cols == 0 {
		opts.Cols = 80
	}
	if opts.Rows == 0 {
		opts.Rows = 24
	}

	cmd := exec.Command(opts.Command[0], opts.Command[1:]...)
	cmd.Env = append(os.Environ(), opts.Env...)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(opts.Cols),
		Rows: uint16(opts.Rows),
	})
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	vt := vt10x.New(vt10x.WithSize(opts.Cols, opts.Rows), vt10x.WithWriter(ptmx))

	e := &Emulator{
		ptyF:     ptmx,
		vt:       vt,
		cmd:      cmd,
		cols:     opts.Cols,
		rows:     opts.Rows,
		subs:     make(map[int]chan struct{}),
		exitCode: -1,
		exitCh:   make(chan struct{}),
		recorder: opts.Recorder,
	}

	// Start pump goroutine: reads PTY output -> writes to vt10x -> notifies subscribers
	go e.pump()
	// Start wait goroutine: waits for process exit
	go e.waitExit()

	return e, nil
}

// pump reads from PTY and feeds data to vt10x.
func (e *Emulator) pump() {
	var r io.Reader = e.ptyF
	if e.recorder != nil {
		// Tee PTY output through the recorder so it captures raw bytes.
		r = io.TeeReader(e.ptyF, &recorderOutputWriter{rec: e.recorder})
	}
	br := bufio.NewReader(r)
	for {
		err := e.vt.Parse(br)
		if err != nil {
			return
		}
		e.notifySubscribers()
	}
}

// recorderOutputWriter is an io.Writer that records output events.
type recorderOutputWriter struct {
	rec *Recorder
}

func (w *recorderOutputWriter) Write(p []byte) (int, error) {
	w.rec.Output(p)
	return len(p), nil
}

// waitExit waits for the child process to exit and records the exit code.
func (e *Emulator) waitExit() {
	err := e.cmd.Wait()
	e.mu.Lock()
	e.exited = true
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			e.exitCode = exitErr.ExitCode()
		} else {
			e.exitCode = -1
		}
	} else {
		e.exitCode = 0
	}
	e.mu.Unlock()
	e.exitOnce.Do(func() {
		close(e.exitCh)
	})
	e.notifySubscribers()
}

func (e *Emulator) Write(p []byte) (int, error) {
	if e.recorder != nil {
		e.recorder.Input(p)
	}
	return e.ptyF.Write(p)
}

func (e *Emulator) Screen() Screen {
	e.vt.Lock()
	defer e.vt.Unlock()

	cols, rows := e.vt.Size()
	cursor := e.vt.Cursor()

	var sb strings.Builder
	for row := range rows {
		var line strings.Builder
		for col := range cols {
			g := e.vt.Cell(col, row)
			ch := g.Char
			if ch == 0 {
				line.WriteRune(' ')
			} else {
				line.WriteRune(ch)
			}
		}
		sb.WriteString(strings.TrimRight(line.String(), " "))
		if row < rows-1 {
			sb.WriteRune('\n')
		}
	}

	text := sb.String()
	return Screen{
		Text:      text,
		Hash:      ComputeHash(text),
		CursorRow: cursor.Y,
		CursorCol: cursor.X,
		Cols:      cols,
		Rows:      rows,
	}
}

func (e *Emulator) Resize(cols, rows int) error {
	e.mu.Lock()
	e.cols = cols
	e.rows = rows
	e.mu.Unlock()

	e.vt.Lock()
	e.vt.Resize(cols, rows)
	e.vt.Unlock()

	return pty.Setsize(e.ptyF, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

func (e *Emulator) PID() int {
	if e.cmd.Process != nil {
		return e.cmd.Process.Pid
	}
	return 0
}

func (e *Emulator) Running() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return !e.exited
}

func (e *Emulator) ExitCode() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.exitCode
}

// ExitCh returns a channel that is closed when the process exits.
func (e *Emulator) ExitCh() <-chan struct{} {
	return e.exitCh
}

func (e *Emulator) Subscribe() (updates <-chan struct{}, cancel func()) {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	ch := make(chan struct{}, 1)
	id := e.nextSub
	e.nextSub++
	e.subs[id] = ch
	cancel = func() {
		e.subMu.Lock()
		delete(e.subs, id)
		e.subMu.Unlock()
	}
	return ch, cancel
}

func (e *Emulator) notifySubscribers() {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	for _, ch := range e.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (e *Emulator) Close() error {
	if e.recorder != nil {
		_ = e.recorder.Close()
	}
	if e.cmd.Process != nil {
		_ = e.cmd.Process.Signal(syscall.SIGTERM)
	}
	return e.ptyF.Close()
}
