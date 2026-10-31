package core

import (
	"net"
	"syscall"
)

// MarkedDialer dials with a firewall mark on the socket. With the core's
// own mark, the tunnel's rules leave the connection alone: it goes out
// directly, as a device on the "direct" policy would.
func MarkedDialer(mark int) *net.Dialer {
	return &net.Dialer{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark)
		}); err != nil {
			return err
		}
		return serr
	}}
}
