package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// shortTmpDir returns a temp directory under /tmp to keep Unix socket paths
// within the macOS 104-character limit.
func shortTmpDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "virtui-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestPeerUID_SameUser(t *testing.T) {
	dir := shortTmpDir(t)
	sock := filepath.Join(dir, "t.sock")

	lis, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	// Accept in background.
	type result struct {
		uid uint32
		err error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer func() { _ = conn.Close() }()
		uid, err := peerUID(conn)
		ch <- result{uid: uid, err: err}
	}()

	// Connect as the same user.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	r := <-ch
	if r.err != nil {
		t.Fatalf("peerUID: %v", r.err)
	}
	want := uint32(os.Geteuid())
	if r.uid != want {
		t.Errorf("peerUID = %d, want %d", r.uid, want)
	}
}

func TestAuthListener_AcceptsSameUser(t *testing.T) {
	dir := shortTmpDir(t)
	sock := filepath.Join(dir, "t.sock")

	inner, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = inner.Close() }()

	al := newAuthListener(inner)

	// Accept in background.
	errCh := make(chan error, 1)
	go func() {
		conn, err := al.Accept()
		if err != nil {
			errCh <- err
			return
		}
		_ = conn.Close()
		errCh <- nil
	}()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	if err := <-errCh; err != nil {
		t.Fatalf("authListener.Accept: %v", err)
	}
}
