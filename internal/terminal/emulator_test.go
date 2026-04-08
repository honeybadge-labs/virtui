package terminal

import (
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
)

// TestBuildSGR_DefaultReturnsReset verifies that all-default style emits a reset.
func TestBuildSGR_DefaultReturnsReset(t *testing.T) {
	sgr := buildSGR(vt10x.DefaultFG, vt10x.DefaultBG, 0)
	if sgr != "\033[0m" {
		t.Errorf("expected reset, got %q", sgr)
	}
}

// TestBuildSGR_ReverseNotEmitted verifies that attrReverse is NOT re-emitted
// in SGR output because vt10x already materializes reverse by swapping FG/BG
// in the stored glyph. Re-emitting SGR 7 would double-apply it.
func TestBuildSGR_ReverseNotEmitted(t *testing.T) {
	// Simulate what vt10x stores for reverse video with default colors:
	// FG=DefaultBG (swapped), BG=DefaultFG (swapped), Mode has attrReverse.
	sgr := buildSGR(vt10x.DefaultBG, vt10x.DefaultFG, vtAttrReverse)
	// Should NOT contain ";7" (reverse SGR param).
	if strings.Contains(sgr, ";7") {
		t.Errorf("buildSGR should not emit reverse (;7), got %q", sgr)
	}
	// Both colors are default sentinels → should be a plain reset.
	if sgr != "\033[0m" {
		t.Errorf("reverse with default colors should reset, got %q", sgr)
	}
}

// TestBuildSGR_ReverseWithColorsNoDoubleApply verifies that reverse video
// with explicit colors doesn't re-emit SGR 7 and doesn't produce bogus
// color codes from default sentinels.
func TestBuildSGR_ReverseWithColorsNoDoubleApply(t *testing.T) {
	// Original: FG=Red(1), BG=DefaultBG, Mode=reverse
	// vt10x materializes: FG=DefaultBG, BG=Red(1)
	// The stored glyph: FG=DefaultBG (sentinel), BG=1 (red), Mode=reverse
	sgr := buildSGR(vt10x.DefaultBG, vt10x.Color(1), vtAttrReverse)
	if strings.Contains(sgr, ";7") {
		t.Errorf("should not emit reverse, got %q", sgr)
	}
	// FG is DefaultBG sentinel → no FG code, BG is red(1) → should have 41
	if !strings.Contains(sgr, "41") {
		t.Errorf("expected BG red (41), got %q", sgr)
	}
	// Should NOT contain 38;2 (default sentinel leaked as RGB)
	if strings.Contains(sgr, "38;2") {
		t.Errorf("default sentinel should not produce 38;2, got %q", sgr)
	}
}

// TestBuildSGR_DefaultSentinelsNotLeakedAsRGB verifies that default color
// sentinel values (1<<24, 1<<24+1) are never passed to fgSGR/bgSGR and
// thus never produce bogus 38;2 or 48;2 escape codes.
func TestBuildSGR_DefaultSentinelsNotLeakedAsRGB(t *testing.T) {
	// DefaultFG as foreground with non-default BG
	sgr := buildSGR(vt10x.DefaultFG, vt10x.Color(2), 0)
	if strings.Contains(sgr, "38;2") {
		t.Errorf("DefaultFG should not produce 38;2, got %q", sgr)
	}

	// DefaultBG as background with non-default FG
	sgr = buildSGR(vt10x.Color(1), vt10x.DefaultBG, 0)
	if strings.Contains(sgr, "48;2") {
		t.Errorf("DefaultBG should not produce 48;2, got %q", sgr)
	}

	// Swapped defaults (from reverse materialization)
	sgr = buildSGR(vt10x.DefaultBG, vt10x.DefaultFG, vtAttrReverse)
	if strings.Contains(sgr, "38;2") || strings.Contains(sgr, "48;2") {
		t.Errorf("swapped defaults should not produce 38;2/48;2, got %q", sgr)
	}
}

// TestFgSGR_Truecolor verifies that 24-bit RGB colors above 255 are emitted
// as 38;2;R;G;B truecolor sequences.
func TestFgSGR_Truecolor(t *testing.T) {
	// #0000FF = 255 collides with palette 255 — known limitation.
	// Use a value above 255 to verify truecolor path.
	c := vt10x.Color(0x010000) // R=1, G=0, B=0 (value 65536)
	params := fgSGR(c)
	got := strings.Join(params, ";")
	if got != "38;2;1;0;0" {
		t.Errorf("expected 38;2;1;0;0, got %q", got)
	}
}

// TestFgSGR_LowValuedTruecolor documents the known limitation where
// low-valued truecolors collide with palette indices.
func TestFgSGR_LowValuedTruecolor(t *testing.T) {
	// #0000FF = 255: vt10x stores Color(255), same as palette 255.
	// We emit 38;5;255 (palette) since we can't distinguish.
	c := vt10x.Color(255) // Could be palette 255 OR truecolor #0000FF
	params := fgSGR(c)
	got := strings.Join(params, ";")
	if got != "38;5;255" {
		t.Errorf("expected 38;5;255 (palette), got %q", got)
	}
}

// TestBgSGR_Truecolor verifies 24-bit background colors.
func TestBgSGR_Truecolor(t *testing.T) {
	c := vt10x.Color(0x000100) // R=0, G=1, B=0 (value 256)
	params := bgSGR(c)
	got := strings.Join(params, ";")
	if got != "48;2;0;1;0" {
		t.Errorf("expected 48;2;0;1;0, got %q", got)
	}
}

// TestBgSGR_LowValuedTruecolor documents the palette collision for BG too.
func TestBgSGR_LowValuedTruecolor(t *testing.T) {
	// #000001 = 1: indistinguishable from palette 1 (Red).
	c := vt10x.Color(1)
	params := bgSGR(c)
	got := strings.Join(params, ";")
	if got != "41" { // palette red
		t.Errorf("expected 41 (palette red), got %q", got)
	}
}

// TestIsDefaultColor verifies sentinel detection.
func TestIsDefaultColor(t *testing.T) {
	if !isDefaultColor(vt10x.DefaultFG) {
		t.Error("DefaultFG should be default")
	}
	if !isDefaultColor(vt10x.DefaultBG) {
		t.Error("DefaultBG should be default")
	}
	if isDefaultColor(vt10x.Color(0)) {
		t.Error("Color(0) should not be default")
	}
	if isDefaultColor(vt10x.Color(255)) {
		t.Error("Color(255) should not be default")
	}
}

// TestIsDefaultStyle verifies style detection.
func TestIsDefaultStyle(t *testing.T) {
	defGlyph := vt10x.Glyph{Char: ' ', FG: vt10x.DefaultFG, BG: vt10x.DefaultBG}
	if !isDefaultStyle(defGlyph) {
		t.Error("default glyph should have default style")
	}

	styledGlyph := vt10x.Glyph{Char: ' ', FG: vt10x.Color(1), BG: vt10x.DefaultBG}
	if isDefaultStyle(styledGlyph) {
		t.Error("glyph with FG color should not be default style")
	}

	bgGlyph := vt10x.Glyph{Char: ' ', FG: vt10x.DefaultFG, BG: vt10x.Color(2)}
	if isDefaultStyle(bgGlyph) {
		t.Error("glyph with BG color should not be default style")
	}

	boldGlyph := vt10x.Glyph{Char: ' ', FG: vt10x.DefaultFG, BG: vt10x.DefaultBG, Mode: vtAttrBold}
	if isDefaultStyle(boldGlyph) {
		t.Error("glyph with bold should not be default style")
	}
}

// TestScreenANSI_TrailingStyledSpacesPreserved verifies that trailing spaces
// with non-default styling are preserved in screen_ansi.
func TestScreenANSI_TrailingStyledSpacesPreserved(t *testing.T) {
	// Print colored spaces followed by visible text so waitForText can detect output.
	e, err := NewEmulator(EmulatorOpts{
		Command: []string{"bash", "-c", `printf '\033[41m  \033[0mX'; sleep 5`},
		Cols:    10,
		Rows:    2,
	})
	if err != nil {
		t.Fatalf("NewEmulator: %v", err)
	}
	defer func() { _ = e.Close() }()

	waitForText(t, e, 2)

	screen := e.Screen()
	firstLine := strings.Split(screen.ANSI, "\n")[0]

	// Must contain the background-colored spaces (not trimmed away).
	if !strings.Contains(firstLine, "  ") || !strings.Contains(firstLine, "\033[") {
		t.Errorf("styled trailing spaces should be preserved, got first line: %q", firstLine)
	}

	// screen_text may trim them (plain text), but screen_ansi must not.
	if !strings.Contains(firstLine, "41") {
		t.Errorf("expected red BG SGR code (41) in first line, got: %q", firstLine)
	}
}

// TestScreenANSI_RowOfOnlyStyledSpaces verifies a row made entirely of styled
// spaces doesn't collapse to empty or just escape codes.
func TestScreenANSI_RowOfOnlyStyledSpaces(t *testing.T) {
	// Fill row with green-bg spaces, then print visible text on second line.
	e, err := NewEmulator(EmulatorOpts{
		Command: []string{"bash", "-c", `printf '\033[42m     \033[0m\nX'; sleep 5`},
		Cols:    5,
		Rows:    3,
	})
	if err != nil {
		t.Fatalf("NewEmulator: %v", err)
	}
	defer func() { _ = e.Close() }()

	waitForText(t, e, 2)

	screen := e.Screen()
	firstLine := strings.Split(screen.ANSI, "\n")[0]

	// Count the spaces in the line (excluding escape sequences).
	plainContent := stripANSI(firstLine)
	spaceCount := strings.Count(plainContent, " ")
	if spaceCount < 5 {
		t.Errorf("row of 5 styled spaces should have at least 5 spaces, got %d in %q (plain: %q)",
			spaceCount, firstLine, plainContent)
	}
}

// waitForText polls the emulator screen until non-whitespace text appears or timeout.
func waitForText(t *testing.T, e *Emulator, maxSeconds int) {
	t.Helper()
	updates, cancel := e.Subscribe()
	defer cancel()

	for range maxSeconds * 20 {
		select {
		case <-updates:
		default:
		}
		screen := e.Screen()
		if strings.TrimSpace(screen.Text) != "" {
			return
		}
		<-updates
	}
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\033' {
			inEsc = true
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
