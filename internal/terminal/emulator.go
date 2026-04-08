package terminal

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty/v2"
	"github.com/hinshun/vt10x"
)

// vt10x attribute flags (unexported in the library).
const (
	vtAttrReverse   = 1 << 0 // 1
	vtAttrUnderline = 1 << 1 // 2
	vtAttrBold      = 1 << 2 // 4
	_               = 1 << 3 // 8 (gfx, unused for SGR)
	vtAttrItalic    = 1 << 4 // 16
	vtAttrBlink     = 1 << 5 // 32
)

// Emulator implements Terminal using creack/pty + vt10x.
type Emulator struct {
	mu   sync.RWMutex
	ptyF *os.File
	vt   vt10x.Terminal
	cmd  *exec.Cmd
	cols int
	rows int

	// pending resize (applied in pump goroutine to avoid vt lock contention)
	pendingResize chan [2]int

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
		ptyF:          ptmx,
		vt:            vt,
		cmd:           cmd,
		cols:          opts.Cols,
		rows:          opts.Rows,
		pendingResize: make(chan [2]int, 1),
		subs:          make(map[int]chan struct{}),
		exitCode:      -1,
		exitCh:        make(chan struct{}),
		recorder:      opts.Recorder,
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
		// Apply any pending resize between Parse calls (when the vt lock is free).
		select {
		case sz := <-e.pendingResize:
			e.vt.Resize(sz[0], sz[1])
		default:
		}
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

	var textBuf strings.Builder
	var ansiBuf strings.Builder

	// Track current SGR state for delta encoding.
	var curFG, curBG vt10x.Color
	var curMode int16
	sgrActive := false // whether we've emitted any SGR in this row

	for row := range rows {
		var textLine strings.Builder
		var ansiLine strings.Builder

		// Reset SGR tracking at the start of each row.
		curFG = vt10x.DefaultFG
		curBG = vt10x.DefaultBG
		curMode = 0
		sgrActive = false

		for col := range cols {
			g := e.vt.Cell(col, row)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}

			// Build plain text.
			textLine.WriteRune(ch)

			// Build ANSI: emit SGR if style changed.
			if g.FG != curFG || g.BG != curBG || g.Mode != curMode {
				sgr := buildSGR(g.FG, g.BG, g.Mode)
				if sgr != "" {
					ansiLine.WriteString(sgr)
					sgrActive = true
				}
				curFG = g.FG
				curBG = g.BG
				curMode = g.Mode
			}

			ansiLine.WriteRune(ch)
		}

		textBuf.WriteString(strings.TrimRight(textLine.String(), " "))

		ansiStr := ansiLine.String()
		// Reset at end of row if any SGR was emitted.
		if sgrActive {
			ansiStr = strings.TrimRight(ansiStr, " ")
			ansiBuf.WriteString(ansiStr)
			ansiBuf.WriteString("\033[0m")
		} else {
			ansiBuf.WriteString(strings.TrimRight(ansiStr, " "))
		}

		if row < rows-1 {
			textBuf.WriteRune('\n')
			ansiBuf.WriteRune('\n')
		}
	}

	text := textBuf.String()
	return Screen{
		Text:      text,
		ANSI:      ansiBuf.String(),
		Hash:      ComputeHash(text),
		CursorRow: cursor.Y,
		CursorCol: cursor.X,
		Cols:      cols,
		Rows:      rows,
	}
}

// buildSGR returns an ANSI SGR escape sequence for the given style.
// It always emits a full reset + re-apply to keep the logic simple and correct.
func buildSGR(fg, bg vt10x.Color, mode int16) string {
	isDefaultFG := fg == vt10x.DefaultFG
	isDefaultBG := bg == vt10x.DefaultBG
	hasAttrs := mode&(vtAttrBold|vtAttrItalic|vtAttrUnderline|vtAttrBlink|vtAttrReverse) != 0

	// If everything is default, just reset.
	if isDefaultFG && isDefaultBG && !hasAttrs {
		return "\033[0m"
	}

	var params []string

	// Reset first to clear previous state.
	params = append(params, "0")

	// Attributes.
	if mode&vtAttrBold != 0 {
		params = append(params, "1")
	}
	if mode&vtAttrItalic != 0 {
		params = append(params, "3")
	}
	if mode&vtAttrUnderline != 0 {
		params = append(params, "4")
	}
	if mode&vtAttrBlink != 0 {
		params = append(params, "5")
	}
	if mode&vtAttrReverse != 0 {
		params = append(params, "7")
	}

	// Foreground.
	if !isDefaultFG {
		params = append(params, fgSGR(fg)...)
	}

	// Background.
	if !isDefaultBG {
		params = append(params, bgSGR(bg)...)
	}

	return "\033[" + strings.Join(params, ";") + "m"
}

// fgSGR returns SGR parameters for a foreground color.
func fgSGR(c vt10x.Color) []string {
	n := uint32(c)
	switch {
	case n < 8:
		return []string{strconv.Itoa(30 + int(n))}
	case n < 16:
		return []string{strconv.Itoa(90 + int(n-8))}
	case n < 256:
		return []string{"38", "5", strconv.Itoa(int(n))}
	default:
		// 24-bit RGB: vt10x encodes as r<<16 | g<<8 | b
		r := (n >> 16) & 0xFF
		g := (n >> 8) & 0xFF
		b := n & 0xFF
		return []string{"38", "2", strconv.Itoa(int(r)), strconv.Itoa(int(g)), strconv.Itoa(int(b))}
	}
}

// bgSGR returns SGR parameters for a background color.
func bgSGR(c vt10x.Color) []string {
	n := uint32(c)
	switch {
	case n < 8:
		return []string{strconv.Itoa(40 + int(n))}
	case n < 16:
		return []string{strconv.Itoa(100 + int(n-8))}
	case n < 256:
		return []string{"48", "5", strconv.Itoa(int(n))}
	default:
		// 24-bit RGB: vt10x encodes as r<<16 | g<<8 | b
		r := (n >> 16) & 0xFF
		g := (n >> 8) & 0xFF
		b := n & 0xFF
		return []string{"48", "2", strconv.Itoa(int(r)), strconv.Itoa(int(g)), strconv.Itoa(int(b))}
	}
}

func (e *Emulator) Resize(cols, rows int) error {
	e.mu.Lock()
	e.cols = cols
	e.rows = rows
	e.mu.Unlock()

	// Enqueue vt resize to be applied in the pump goroutine (avoids
	// contending with the vt lock that Parse holds while blocking on IO).
	select {
	case e.pendingResize <- [2]int{cols, rows}:
	default:
		// Replace any already-pending resize with the latest one.
		select {
		case <-e.pendingResize:
		default:
		}
		e.pendingResize <- [2]int{cols, rows}
	}

	// Set PTY size — this sends SIGWINCH to the child, causing it to
	// write escape sequences that wake up Parse and drain the pending resize.
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
