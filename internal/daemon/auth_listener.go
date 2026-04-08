package daemon

import (
	"fmt"
	"log"
	"net"
	"os"
)

// authListener wraps a net.Listener and rejects connections from processes
// whose effective UID does not match the daemon's EUID.
type authListener struct {
	net.Listener
	allowedUID uint32
}

// newAuthListener returns a listener that only accepts connections from the
// same effective user as the current process.
func newAuthListener(inner net.Listener) *authListener {
	return &authListener{
		Listener:   inner,
		allowedUID: uint32(os.Geteuid()),
	}
}

// Accept waits for and returns the next connection that passes the UID check.
// Connections from other UIDs are closed immediately.
func (l *authListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		uid, err := peerUID(conn)
		if err != nil {
			log.Printf("auth: failed to get peer credentials: %v", err)
			_ = conn.Close()
			continue
		}
		if uid != l.allowedUID {
			log.Printf("auth: rejected connection from UID %d (allowed: %d)", uid, l.allowedUID)
			_ = conn.Close()
			continue
		}
		return conn, nil
	}
}

// String implements fmt.Stringer for logging.
func (l *authListener) String() string {
	return fmt.Sprintf("authListener(uid=%d, %s)", l.allowedUID, l.Addr())
}
