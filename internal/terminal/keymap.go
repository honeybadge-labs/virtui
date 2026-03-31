package terminal

// KeyMap maps human-readable key names to ANSI escape sequences.
var KeyMap = map[string]string{
	// Standard keys
	"Enter":     "\r",
	"Tab":       "\t",
	"Backspace": "\x7f",
	"Escape":    "\x1b",
	"Space":     " ",
	"Delete":    "\x1b[3~",

	// Arrow keys
	"ArrowUp":    "\x1b[A",
	"ArrowDown":  "\x1b[B",
	"ArrowRight": "\x1b[C",
	"ArrowLeft":  "\x1b[D",
	"Up":         "\x1b[A",
	"Down":       "\x1b[B",
	"Right":      "\x1b[C",
	"Left":       "\x1b[D",

	// Navigation
	"Home":     "\x1b[H",
	"End":      "\x1b[F",
	"PageUp":   "\x1b[5~",
	"PageDown": "\x1b[6~",
	"Insert":   "\x1b[2~",

	// Function keys
	"F1":  "\x1bOP",
	"F2":  "\x1bOQ",
	"F3":  "\x1bOR",
	"F4":  "\x1bOS",
	"F5":  "\x1b[15~",
	"F6":  "\x1b[17~",
	"F7":  "\x1b[18~",
	"F8":  "\x1b[19~",
	"F9":  "\x1b[20~",
	"F10": "\x1b[21~",
	"F11": "\x1b[23~",
	"F12": "\x1b[24~",

	// Ctrl combinations
	"Ctrl+A": "\x01",
	"Ctrl+B": "\x02",
	"Ctrl+C": "\x03",
	"Ctrl+D": "\x04",
	"Ctrl+E": "\x05",
	"Ctrl+F": "\x06",
	"Ctrl+G": "\x07",
	"Ctrl+H": "\x08",
	"Ctrl+I": "\x09",
	"Ctrl+J": "\x0a",
	"Ctrl+K": "\x0b",
	"Ctrl+L": "\x0c",
	"Ctrl+M": "\x0d",
	"Ctrl+N": "\x0e",
	"Ctrl+O": "\x0f",
	"Ctrl+P": "\x10",
	"Ctrl+Q": "\x11",
	"Ctrl+R": "\x12",
	"Ctrl+S": "\x13",
	"Ctrl+T": "\x14",
	"Ctrl+U": "\x15",
	"Ctrl+V": "\x16",
	"Ctrl+W": "\x17",
	"Ctrl+X": "\x18",
	"Ctrl+Y": "\x19",
	"Ctrl+Z": "\x1a",
	"Ctrl+[": "\x1b",
	"Ctrl+]": "\x1d",
	"Ctrl+\\": "\x1c",
}

// ResolveKey returns the ANSI escape sequence for a key name.
// If the key is a single character, it returns it as-is.
func ResolveKey(name string) (string, bool) {
	if seq, ok := KeyMap[name]; ok {
		return seq, true
	}
	// Single character keys
	if len(name) == 1 {
		return name, true
	}
	return "", false
}
