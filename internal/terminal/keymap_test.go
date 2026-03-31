package terminal

import "testing"

func TestResolveKey_Named(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Enter", "\r"},
		{"Tab", "\t"},
		{"Escape", "\x1b"},
		{"ArrowUp", "\x1b[A"},
		{"ArrowDown", "\x1b[B"},
		{"Ctrl+C", "\x03"},
		{"Ctrl+D", "\x04"},
		{"F1", "\x1bOP"},
		{"Delete", "\x1b[3~"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ResolveKey(tt.name)
			if !ok {
				t.Fatalf("ResolveKey(%q) returned not found", tt.name)
			}
			if got != tt.want {
				t.Errorf("ResolveKey(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestResolveKey_SingleChar(t *testing.T) {
	got, ok := ResolveKey("a")
	if !ok {
		t.Fatal("ResolveKey(a) returned not found")
	}
	if got != "a" {
		t.Errorf("got %q, want %q", got, "a")
	}
}

func TestResolveKey_Unknown(t *testing.T) {
	_, ok := ResolveKey("NoSuchKey")
	if ok {
		t.Error("ResolveKey(NoSuchKey) should return false")
	}
}
