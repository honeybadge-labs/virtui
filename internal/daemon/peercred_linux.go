//go:build linux

package daemon

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerUID returns the effective UID of the peer connected via a Unix socket.
func peerUID(conn net.Conn) (uint32, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("peercred: not a Unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("peercred: syscall conn: %w", err)
	}
	var (
		uid    uint32
		credErr error
	)
	err = raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil {
			credErr = fmt.Errorf("peercred: getsockopt: %w", e)
			return
		}
		uid = cred.Uid
	})
	if err != nil {
		return 0, fmt.Errorf("peercred: control: %w", err)
	}
	if credErr != nil {
		return 0, credErr
	}
	return uid, nil
}
