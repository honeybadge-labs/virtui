package terminal

import (
	"io"
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
)

func TestScreen_DefaultTextDoesNotGainANSI(t *testing.T) {
	screen := screenFromANSI(t, 16, 1, "PLAIN")

	if screen.Text != "PLAIN" {
		t.Fatalf("expected plain screen text, got %q", screen.Text)
	}
	if screen.ANSI != "PLAIN" {
		t.Fatalf("expected plain ANSI output, got %q", screen.ANSI)
	}
}

func TestScreen_PreservesTrailingStyledSpaces(t *testing.T) {
	screen := screenFromANSI(t, 8, 1, "X\033[41m  \033[0m")

	if screen.Text != "X" {
		t.Fatalf("screen text should stay trimmed, got %q", screen.Text)
	}
	if stripANSI(screen.ANSI) != "X  " {
		t.Fatalf("styled trailing spaces should survive in screen_ansi, got %q", screen.ANSI)
	}
	if !strings.Contains(screen.ANSI, "41") {
		t.Fatalf("expected red background SGR in screen_ansi, got %q", screen.ANSI)
	}
}

func TestScreen_PreservesRowsMadeOnlyOfStyledSpaces(t *testing.T) {
	screen := screenFromANSI(t, 5, 2, "\033[42m     \033[0m\nX")
	lines := strings.Split(screen.ANSI, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected two ANSI lines, got %q", screen.ANSI)
	}

	if stripANSI(lines[0]) != "     " {
		t.Fatalf("styled-only row should preserve its spaces, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "42") {
		t.Fatalf("expected green background SGR in first line, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "X") {
		t.Fatalf("expected visible content on second line, got %q", lines[1])
	}
}

func TestScreen_ANSIReplayIsStableForStyledContent(t *testing.T) {
	source := strings.Join([]string{
		"\033[31mR\033[0m",
		" ",
		"\033[4mU\033[0m",
		" ",
		"\033[31;47;7mX\033[0m",
		" ",
		"\033[38;2;1;0;0mT\033[0m",
		" ",
		"\033[48;2;0;1;0mB\033[0m",
	}, "")

	screen := screenFromANSI(t, 16, 1, source)
	replayed := screenFromANSI(t, 16, 1, screen.ANSI)

	if replayed.Text != screen.Text {
		t.Fatalf("replayed ANSI changed screen text: got %q want %q", replayed.Text, screen.Text)
	}
	if replayed.ANSI != screen.ANSI {
		t.Fatalf("replayed ANSI should be stable:\nfirst:  %q\nsecond: %q", screen.ANSI, replayed.ANSI)
	}
}

func screenFromANSI(t *testing.T, cols, rows int, ansi string) Screen {
	t.Helper()

	vt := vt10x.New(vt10x.WithSize(cols, rows), vt10x.WithWriter(io.Discard))
	if _, err := vt.Write([]byte(ansi)); err != nil {
		t.Fatalf("write ANSI to vt10x: %v", err)
	}

	return (&Emulator{vt: vt}).Screen()
}

func stripANSI(s string) string {
	var out strings.Builder
	inEscape := false

	for _, r := range s {
		switch {
		case inEscape && r == 'm':
			inEscape = false
		case inEscape:
			continue
		case r == '\033':
			inEscape = true
		default:
			out.WriteRune(r)
		}
	}

	return out.String()
}
