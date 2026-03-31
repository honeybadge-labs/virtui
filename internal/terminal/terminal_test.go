package terminal

import "testing"

func TestComputeHash(t *testing.T) {
	h1 := ComputeHash("hello")
	h2 := ComputeHash("hello")
	h3 := ComputeHash("world")

	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different input should produce different hash")
	}
	if len(h1) != 64 {
		t.Errorf("SHA-256 hex should be 64 chars, got %d", len(h1))
	}
}
