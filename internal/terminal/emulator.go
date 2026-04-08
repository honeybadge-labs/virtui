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

	for row := range rows {
		var textLine strings.Builder

		// First pass: find the last column that has a non-space char OR
		// non-default styling (so styled trailing spaces are preserved in ANSI).
		lastInteresting := -1
		for col := cols - 1; col >= 0; col-- {
			g := e.vt.Cell(col, row)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			if ch != ' ' || !isDefaultStyle(g) {
				lastInteresting = col
				break
			}
		}

		// Build plain text (always trim trailing spaces for text).
		for col := range cols {
			g := e.vt.Cell(col, row)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			textLine.WriteRune(ch)
		}
		textBuf.WriteString(strings.TrimRight(textLine.String(), " "))

		// Build ANSI string up to lastInteresting column (preserving styled spaces).
		var ansiLine strings.Builder
		var curFG, curBG vt10x.Color
		var curMode int16
		curFG = vt10x.DefaultFG
		curBG = vt10x.DefaultBG
		sgrActive := false

		for col := 0; col <= lastInteresting; col++ {
			g := e.vt.Cell(col, row)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}

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

		if sgrActive {
			ansiBuf.WriteString(ansiLine.String())
			ansiBuf.WriteString("\033[0m")
		} else {
			ansiBuf.WriteString(ansiLine.String())
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

// isDefaultStyle returns true if a glyph has no styling (default FG/BG, no attrs).
func isDefaultStyle(g vt10x.Glyph) bool {
	return isDefaultColor(g.FG) && isDefaultColor(g.BG) &&
		g.Mode&(vtAttrBold|vtAttrItalic|vtAttrUnderline|vtAttrBlink|vtAttrReverse) == 0
}

// isDefaultColor returns true for vt10x default color sentinels.
// DefaultFG=1<<24, DefaultBG=1<<24+1, DefaultCursor=1<<24+2.
func isDefaultColor(c vt10x.Color) bool {
	return c >= vt10x.DefaultFG
}

// buildSGR returns an ANSI SGR escape sequence for the given style.
//
// vt10x already materializes reverse video by swapping FG/BG in the stored
// glyph (state.go:setChar), so we must NOT re-emit SGR 7 — the colors in
// the glyph are already the final display values. Similarly, bold→bright
// color promotion is pre-applied by vt10x for FG<8.
//
// Known limitation: vt10x stores both 256-color palette indices and 24-bit
// RGB values in the same uint32, so low-valued truecolors (e.g. #000001 =
// palette index 1) are indistinguishable from palette colors. Such colors
// will be emitted as palette codes instead of 38;2;R;G;B.
func buildSGR(fg, bg vt10x.Color, mode int16) string {
	defFG := isDefaultColor(fg)
	defBG := isDefaultColor(bg)
	// Mask out reverse — already materialized by vt10x.
	attrs := mode & (vtAttrBold | vtAttrItalic | vtAttrUnderline | vtAttrBlink)

	if defFG && defBG && attrs == 0 {
		return "\033[0m"
	}

	var params []string
	params = append(params, "0")

	if attrs&vtAttrBold != 0 {
		params = append(params, "1")
	}
	if attrs&vtAttrItalic != 0 {
		params = append(params, "3")
	}
	if attrs&vtAttrUnderline != 0 {
		params = append(params, "4")
	}
	if attrs&vtAttrBlink != 0 {
		params = append(params, "5")
	}

	if !defFG {
		params = append(params, fgSGR(fg)...)
	}
	if !defBG {
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
